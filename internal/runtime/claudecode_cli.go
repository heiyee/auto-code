package runtime

import (
	"auto-code/internal/logging"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/creack/pty"
	"go.uber.org/zap"
)

var errSessionNotRunning = errors.New("session is not running")

var (
	ansiCSIRegexp = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	ansiOSCRegexp = regexp.MustCompile(`\x1b\][^\x07]*(?:\x07|\x1b\\)`)
)

// claudeCodeRuntime is the low-level wrapper for a Claude Code CLI process.
// It owns process lifecycle, stdin/stdout, output buffering and poll offsets.
type claudeCodeRuntime struct {
	command     string
	workDir     string
	env         map[string]string
	initialCols int
	initialRows int

	mu                      sync.Mutex
	inputMu                 sync.Mutex
	cmd                     *exec.Cmd
	stdin                   io.WriteCloser
	ptyMaster               *os.File
	ready                   bool
	launchMode              string
	processPID              int
	processPGID             int
	createdAt               time.Time
	updatedAt               time.Time
	running                 bool
	exited                  bool
	exitCode                *int
	lastError               string
	output                  []byte
	baseOffset              int64
	sideErrors              []RuntimeSideError
	nextSideErrorID         uint64
	outputHook              func(chunk []byte, at time.Time)
	stateHook               func(summary SessionSummary)
	sideErrorHook           func(sideErr RuntimeSideError)
	codexTrustChecked       bool
	codexTrustAutoAccepting bool
}

const (
	claudeStartupMinSettle = 4 * time.Second
	codexStartupMinSettle  = 700 * time.Millisecond
	claudeStartupQuiesce   = 700 * time.Millisecond
	defaultPTYCols         = 120
	defaultPTYRows         = 40
	maxRuntimeSideErrors   = 32
)

func normalizePTYSize(cols, rows int) (int, int) {
	if cols <= 0 {
		cols = defaultPTYCols
	}
	if rows <= 0 {
		rows = defaultPTYRows
	}
	return cols, rows
}

func newClaudeCodeRuntime(command, workDir string, env map[string]string, cols, rows int) *claudeCodeRuntime {
	now := time.Now()
	cols, rows = normalizePTYSize(cols, rows)

	// 确保 workDir 是绝对路径
	workDir = strings.TrimSpace(workDir)
	if workDir != "" {
		if absPath, err := filepath.Abs(workDir); err == nil {
			workDir = absPath
		}
	}

	return &claudeCodeRuntime{
		command:     command,
		workDir:     workDir,
		env:         cloneStringMap(env),
		initialCols: cols,
		initialRows: rows,
		createdAt:   now,
		updatedAt:   now,
	}
}

func (r *claudeCodeRuntime) start() error {
	command := strings.TrimSpace(r.command)
	if command == "" {
		return errors.New("command is required")
	}
	commandEnv := r.commandEnv()
	if err := validateCommandAvailable(command, commandEnv); err != nil {
		return err
	}
	commandToRun := command
	cols, rows := normalizePTYSize(r.initialCols, r.initialRows)
	ptyCommandToRun := wrapPTYBootstrapCommand(commandToRun, cols, rows)

	type candidate struct {
		mode string
		run  func() error
	}
	candidates := make([]candidate, 0, 4)
	candidates = append(candidates, candidate{
		mode: "pty-shell",
		run: func() error {
			return r.attachAndStartPTY(exec.Command("/bin/sh", "-lc", ptyCommandToRun), "pty-shell")
		},
	})
	switch runtime.GOOS {
	case "darwin", "freebsd", "openbsd", "netbsd", "dragonfly":
		candidates = append(candidates,
			candidate{mode: "script-bsd", run: func() error {
				return r.attachAndStartPipes(exec.Command("script", "-q", "/dev/null", "/bin/sh", "-lc", ptyCommandToRun), "script-bsd")
			}},
			candidate{mode: "script-linux", run: func() error {
				return r.attachAndStartPipes(exec.Command("script", "-q", "-c", ptyCommandToRun, "/dev/null"), "script-linux")
			}},
		)
	default:
		candidates = append(candidates,
			candidate{mode: "script-linux", run: func() error {
				return r.attachAndStartPipes(exec.Command("script", "-q", "-c", ptyCommandToRun, "/dev/null"), "script-linux")
			}},
			candidate{mode: "script-bsd", run: func() error {
				return r.attachAndStartPipes(exec.Command("script", "-q", "/dev/null", "/bin/sh", "-lc", ptyCommandToRun), "script-bsd")
			}},
		)
	}
	candidates = append(candidates, candidate{
		mode: "direct-shell",
		run: func() error {
			return r.attachAndStartPipes(exec.Command("/bin/sh", "-lc", commandToRun), "direct-shell")
		},
	})

	failures := make([]string, 0, len(candidates))
	for _, item := range candidates {
		if err := item.run(); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", item.mode, err))
			continue
		}
		return nil
	}
	return fmt.Errorf("cannot start command: %s", strings.Join(failures, " | "))
}

func (r *claudeCodeRuntime) attachAndStartPTY(cmd *exec.Cmd, mode string) error {
	cmd.Env = r.commandEnv()
	if strings.TrimSpace(r.workDir) != "" {
		cmd.Dir = r.workDir
	}
	// pty.StartWithSize configures its own controlling terminal/session wiring.
	// Forcing Setpgid here can make the PTY launch fail on macOS and push the
	// runtime into script-based fallback, which does not support terminal resize.
	cols, rows := normalizePTYSize(r.initialCols, r.initialRows)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
	if err != nil {
		return err
	}
	if err := pty.Setsize(ptmx, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	}); err != nil {
		_ = ptmx.Close()
		return err
	}
	pid := 0
	pgid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
		pgid = resolveProcessGroupID(pid)
		if pgid <= 0 {
			pgid = pid
		}
	}

	r.mu.Lock()
	r.cmd = cmd
	r.stdin = ptmx
	r.ptyMaster = ptmx
	r.launchMode = mode
	r.processPID = pid
	r.processPGID = pgid
	r.running = true
	r.exited = false
	r.ready = false
	r.exitCode = nil
	r.lastError = ""
	r.updatedAt = time.Now()
	r.mu.Unlock()

	go r.captureOutput(ptmx)
	go r.wait()
	return nil
}

func (r *claudeCodeRuntime) attachAndStartPipes(cmd *exec.Cmd, mode string) error {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	cmd.Env = r.commandEnv()
	if strings.TrimSpace(r.workDir) != "" {
		cmd.Dir = r.workDir
	}
	applyCommandProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		return err
	}
	pid := 0
	pgid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
		pgid = resolveProcessGroupID(pid)
		if pgid <= 0 {
			pgid = pid
		}
	}

	r.mu.Lock()
	r.cmd = cmd
	r.stdin = stdin
	r.ptyMaster = nil
	r.launchMode = mode
	r.processPID = pid
	r.processPGID = pgid
	r.running = true
	r.exited = false
	r.ready = false
	r.exitCode = nil
	r.lastError = ""
	r.updatedAt = time.Now()
	r.mu.Unlock()

	go r.captureOutput(stdout)
	go r.captureOutput(stderr)
	go r.wait()
	return nil
}

func (r *claudeCodeRuntime) captureOutput(reader io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			r.appendOutput(buf[:n])
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !isBenignTerminalReadError(err) {
				r.appendSideError("stream_error", err.Error())
			}
			return
		}
	}
}

func (r *claudeCodeRuntime) appendOutput(chunk []byte) {
	if len(chunk) == 0 {
		return
	}
	eventChunk := append([]byte(nil), chunk...)
	now := time.Now()
	var hook func(chunk []byte, at time.Time)

	r.mu.Lock()
	overflow := len(r.output) + len(chunk) - maxSessionOutputBytes
	if overflow > 0 {
		if overflow >= len(r.output) {
			r.baseOffset += int64(len(r.output))
			r.output = r.output[:0]
		} else {
			r.baseOffset += int64(overflow)
			r.output = append([]byte(nil), r.output[overflow:]...)
		}
	}

	r.output = append(r.output, chunk...)
	r.updatedAt = now
	hook = r.outputHook
	r.mu.Unlock()

	if hook != nil {
		hook(eventChunk, now)
	}
}

func (r *claudeCodeRuntime) appendSideError(code, message string) {
	code = strings.TrimSpace(code)
	message = strings.TrimSpace(message)
	if code == "" || message == "" {
		return
	}
	now := time.Now()
	sideErr := RuntimeSideError{
		ID:        fmt.Sprintf("side-%012d", atomic.AddUint64(&r.nextSideErrorID, 1)),
		Code:      code,
		Message:   message,
		Timestamp: now,
	}
	var hook func(sideErr RuntimeSideError)

	r.mu.Lock()
	r.sideErrors = append(r.sideErrors, sideErr)
	if overflow := len(r.sideErrors) - maxRuntimeSideErrors; overflow > 0 {
		r.sideErrors = append([]RuntimeSideError(nil), r.sideErrors[overflow:]...)
	}
	r.updatedAt = now
	hook = r.sideErrorHook
	r.mu.Unlock()

	if hook != nil {
		hook(sideErr)
	}
}

func (r *claudeCodeRuntime) wait() {
	r.mu.Lock()
	cmd := r.cmd
	stateHook := r.stateHook
	r.mu.Unlock()
	if cmd == nil {
		return
	}

	err := cmd.Wait()
	exitCode := 0
	lastErr := ""
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
		lastErr = err.Error()
	}

	r.inputMu.Lock()
	r.mu.Lock()
	stdin := r.stdin
	ptyMaster := r.ptyMaster
	r.stdin = nil
	r.ptyMaster = nil
	r.mu.Unlock()
	if ptyMaster != nil {
		_ = ptyMaster.Close()
	}
	if stdin != nil && stdin != ptyMaster {
		_ = stdin.Close()
	}
	r.inputMu.Unlock()

	r.mu.Lock()
	code := exitCode
	r.exitCode = &code
	r.running = false
	r.exited = true
	r.processPID = 0
	r.processPGID = 0
	r.ready = true
	r.updatedAt = time.Now()
	if lastErr != "" {
		r.lastError = lastErr
	}
	summary := SessionSummary{
		Command:     r.command,
		State:       "exited",
		LaunchMode:  r.launchMode,
		WorkDir:     r.workDir,
		ProcessPID:  r.processPID,
		ProcessPGID: r.processPGID,
		CreatedAt:   r.createdAt,
		UpdatedAt:   r.updatedAt,
		ExitCode:    r.exitCode,
		LastError:   r.lastError,
	}
	r.mu.Unlock()
	if stateHook != nil {
		stateHook(summary)
	}
}

func (r *claudeCodeRuntime) writeInput(text string) error {
	r.inputMu.Lock()
	defer r.inputMu.Unlock()

	r.mu.Lock()
	stdin := r.stdin
	running := r.running
	launchMode := r.launchMode
	r.mu.Unlock()

	if !running || stdin == nil {
		return errSessionNotRunning
	}
	if launchMode == "pty-shell" {
		if err := r.writePTYInput(stdin, text); err != nil {
			return err
		}
		r.mu.Lock()
		r.updatedAt = time.Now()
		r.mu.Unlock()
		return nil
	}

	_, err := io.WriteString(stdin, text)
	if err != nil {
		return err
	}

	r.mu.Lock()
	r.updatedAt = time.Now()
	r.mu.Unlock()
	return nil
}

func (r *claudeCodeRuntime) writeRawInput(raw []byte) error {
	if len(raw) == 0 {
		return nil
	}

	r.inputMu.Lock()
	defer r.inputMu.Unlock()

	r.mu.Lock()
	stdin := r.stdin
	running := r.running
	r.mu.Unlock()
	if !running || stdin == nil {
		return errSessionNotRunning
	}

	if _, err := stdin.Write(raw); err != nil {
		return err
	}
	r.mu.Lock()
	r.updatedAt = time.Now()
	r.mu.Unlock()
	return nil
}

func (r *claudeCodeRuntime) writeSerializedString(text string) error {
	if text == "" {
		return nil
	}
	r.inputMu.Lock()
	defer r.inputMu.Unlock()

	r.mu.Lock()
	stdin := r.stdin
	running := r.running
	r.mu.Unlock()
	if !running || stdin == nil {
		return errSessionNotRunning
	}
	if _, err := io.WriteString(stdin, text); err != nil {
		return err
	}
	r.mu.Lock()
	r.updatedAt = time.Now()
	r.mu.Unlock()
	return nil
}

func (r *claudeCodeRuntime) resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return errors.New("invalid terminal size")
	}

	r.mu.Lock()
	running := r.running
	ptmx := r.ptyMaster
	r.mu.Unlock()
	if !running {
		return errSessionNotRunning
	}
	if ptmx == nil {
		return nil
	}
	if err := pty.Setsize(ptmx, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	}); err != nil {
		return err
	}
	r.mu.Lock()
	r.updatedAt = time.Now()
	r.mu.Unlock()
	return nil
}

func (r *claudeCodeRuntime) writePTYInput(stdin io.WriteCloser, text string) error {
	if text == "" {
		return nil
	}
	if text == "\x03" {
		_, err := io.WriteString(stdin, text)
		return err
	}
	if r.shouldWaitForStartup() {
		r.waitForStartupReady(12 * time.Second)
	}
	if r.isLikelyCodexCommand() {
		return r.writeCodexInput(stdin, text)
	}
	// Claude expects Enter as a distinct key event. Sending prompt+newline in a
	// single write can be treated as pasted text and miss submit.
	if r.shouldWaitForStartup() && hasTrailingLineBreak(text) && !r.isLikelyCodexCommand() {
		payload := trimOneTrailingLineBreak(text)
		payload = normalizePTYInput(payload)
		if payload != "" {
			if _, err := io.WriteString(stdin, payload); err != nil {
				return err
			}
			time.Sleep(120 * time.Millisecond)
		}
		_, err := io.WriteString(stdin, "\r")
		return err
	}

	_, err := io.WriteString(stdin, normalizePTYInput(text))
	return err
}

func (r *claudeCodeRuntime) isLikelyCodexCommand() bool {
	r.mu.Lock()
	command := r.command
	r.mu.Unlock()
	return isLikelyCodexCommandString(command)
}

func isLikelyCodexCommandString(command string) bool {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return false
	}
	binary := strings.Trim(fields[0], `"'`)
	return filepath.Base(binary) == "codex"
}

func (r *claudeCodeRuntime) maybeAcceptCodexTrustPrompt() {
	r.mu.Lock()
	if r.codexTrustAutoAccepting || !isLikelyCodexCommandString(r.command) || !r.running || r.stdin == nil {
		r.mu.Unlock()
		return
	}
	window := r.output
	if len(window) > 64*1024 {
		window = window[len(window)-64*1024:]
	}
	if !isCodexTrustPromptWindow(window) {
		r.mu.Unlock()
		return
	}
	r.codexTrustAutoAccepting = true
	r.mu.Unlock()

	go r.acceptCodexTrustPrompt()
}

func (r *claudeCodeRuntime) acceptCodexTrustPrompt() {
	for attempt := 0; attempt < 12; attempt++ {
		if err := r.writeSerializedString("\r"); err != nil {
			break
		}
		time.Sleep(350 * time.Millisecond)

		r.mu.Lock()
		window := r.output
		if len(window) > 64*1024 {
			window = window[len(window)-64*1024:]
		}
		if !isCodexTrustPromptWindow(window) {
			r.codexTrustChecked = true
			r.codexTrustAutoAccepting = false
			r.updatedAt = time.Now()
			r.mu.Unlock()
			return
		}
		r.mu.Unlock()
	}

	r.mu.Lock()
	r.codexTrustAutoAccepting = false
	r.mu.Unlock()
}

func (r *claudeCodeRuntime) ensureCodexDirectoryTrusted(timeout time.Duration) error {
	r.mu.Lock()
	if r.codexTrustChecked {
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()

	deadline := time.Now().Add(timeout)
	sawPrompt := false
	for time.Now().Before(deadline) {
		r.mu.Lock()
		window := r.output
		r.mu.Unlock()
		if len(window) > 64*1024 {
			window = window[len(window)-64*1024:]
		}
		if isCodexTrustPromptWindow(window) {
			sawPrompt = true
			if err := r.writeSerializedString("\r"); err != nil {
				return err
			}
			time.Sleep(800 * time.Millisecond)
			continue
		}
		if sawPrompt {
			r.mu.Lock()
			r.codexTrustChecked = true
			r.mu.Unlock()
			return nil
		}
		time.Sleep(120 * time.Millisecond)
	}
	r.mu.Lock()
	r.codexTrustChecked = true
	r.mu.Unlock()
	return nil
}

func (r *claudeCodeRuntime) writeCodexInput(stdin io.WriteCloser, text string) error {
	if text == "" {
		return nil
	}
	if text == "\x03" {
		_, err := io.WriteString(stdin, text)
		return err
	}

	normalized := normalizeCodexTypedInput(text)
	for _, ch := range normalized {
		if _, err := io.WriteString(stdin, string(ch)); err != nil {
			return err
		}
		time.Sleep(18 * time.Millisecond)
	}
	if hasTrailingLineBreak(text) {
		for attempt := 0; attempt < 2; attempt++ {
			if attempt == 0 {
				time.Sleep(1800 * time.Millisecond)
			} else {
				time.Sleep(1200 * time.Millisecond)
			}
			if _, err := io.WriteString(stdin, "\r"); err != nil {
				return err
			}
			time.Sleep(900 * time.Millisecond)
			if r.isCodexBusy() {
				return nil
			}
		}
		return nil
	}
	return nil
}

func normalizeCodexTypedInput(text string) string {
	if text == "" || text == "\x03" {
		return text
	}
	if hasTrailingLineBreak(text) {
		text = trimOneTrailingLineBreak(text)
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if !strings.Contains(text, "\n") {
		return text
	}

	parts := strings.Split(text, "\n")
	collapsed := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		collapsed = append(collapsed, part)
	}
	return strings.Join(collapsed, " ")
}

func (r *claudeCodeRuntime) shouldWaitForStartup() bool {
	// All interactive CLIs should wait for initial startup settle before first input.
	return true
}

func (r *claudeCodeRuntime) waitForStartupReady(timeout time.Duration) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	startedAt := time.Now()
	deadline := time.Now().Add(timeout)
	lastLen := -1
	unchangedSince := time.Now()
	promptSeenAt := time.Time{}
	isCodex := r.isLikelyCodexCommand()
	for {
		r.mu.Lock()
		if r.ready || !r.running || r.exited {
			r.mu.Unlock()
			return
		}
		window := r.output
		outputLen := len(r.output)
		if len(window) > 64*1024 {
			window = window[len(window)-64*1024:]
		}
		r.mu.Unlock()

		now := time.Now()
		if outputLen != lastLen {
			lastLen = outputLen
			unchangedSince = now
		}
		switch {
		case isCodex && (isCodexPromptWindow(window) || isCodexTrustPromptWindow(window)):
			if promptSeenAt.IsZero() {
				promptSeenAt = now
			}
		case isClaudeStartupReadyWindow(window):
			if promptSeenAt.IsZero() {
				promptSeenAt = now
			}
		}
		minSettle := claudeStartupMinSettle
		if isCodex {
			minSettle = codexStartupMinSettle
		}
		if !promptSeenAt.IsZero() &&
			now.Sub(promptSeenAt) >= minSettle &&
			now.Sub(unchangedSince) >= claudeStartupQuiesce {
			r.mu.Lock()
			if !r.ready {
				r.ready = true
				r.updatedAt = now
			}
			r.mu.Unlock()
			return
		}
		// Generic fallback for non-Codex CLIs: mark ready after startup settle and output quiesce.
		if !isCodex &&
			promptSeenAt.IsZero() &&
			now.Sub(startedAt) >= 1200*time.Millisecond &&
			now.Sub(unchangedSince) >= claudeStartupQuiesce {
			r.mu.Lock()
			if !r.ready {
				r.ready = true
				r.updatedAt = now
			}
			r.mu.Unlock()
			return
		}
		if now.After(deadline) {
			return
		}
		time.Sleep(80 * time.Millisecond)
	}
}

func isClaudeStartupReadyWindow(window []byte) bool {
	if len(window) == 0 {
		return false
	}
	raw := string(window)
	if strings.Contains(raw, "-- INSERT --") {
		return true
	}
	clean := stripANSI(raw)
	clean = strings.ReplaceAll(clean, "\r", "\n")
	if strings.Contains(clean, "-- INSERT --") {
		return true
	}
	return strings.Contains(clean, "\n\u276f") || strings.Contains(clean, "\u276f ")
}

func isCodexTrustPromptWindow(window []byte) bool {
	if len(window) == 0 {
		return false
	}
	raw := string(window)
	if strings.Contains(raw, "Do you trust the contents of this directory?") {
		return true
	}
	clean := stripANSI(raw)
	clean = strings.ReplaceAll(clean, "\r", "\n")
	return strings.Contains(clean, "Do you trust the contents of this directory?")
}

func isCodexPromptWindow(window []byte) bool {
	if len(window) == 0 {
		return false
	}
	raw := stripANSI(string(window))
	raw = strings.ReplaceAll(raw, "\r", "\n")
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	const prompt = "\u203a"
	return strings.Contains(raw, "\n"+prompt) ||
		strings.HasPrefix(trimmed, prompt) ||
		strings.HasSuffix(trimmed, prompt)
}

func isCodexBusyWindow(window []byte) bool {
	if len(window) == 0 {
		return false
	}
	if isCodexTrustPromptWindow(window) {
		return false
	}
	raw := string(window)
	if strings.Contains(raw, "esc to interrupt") {
		return true
	}
	clean := stripANSI(raw)
	clean = strings.ReplaceAll(clean, "\r", "\n")
	return strings.Contains(clean, "esc to interrupt")
}

func (r *claudeCodeRuntime) isCodexBusy() bool {
	r.mu.Lock()
	window := r.output
	r.mu.Unlock()
	if len(window) > 64*1024 {
		window = window[len(window)-64*1024:]
	}
	return isCodexBusyWindow(window)
}

func stripANSI(text string) string {
	if text == "" {
		return ""
	}
	text = ansiOSCRegexp.ReplaceAllString(text, "")
	text = ansiCSIRegexp.ReplaceAllString(text, "")
	return strings.ReplaceAll(text, "\x1b", "")
}

func (r *claudeCodeRuntime) commandEnv() []string {
	env := withDefaultTerminalEnv(os.Environ())
	for key, value := range r.env {
		name := strings.TrimSpace(key)
		if name == "" {
			continue
		}
		env = withEnvValue(env, name, value)
	}
	env = withConfiguredCLIExtraPath(env)
	env = withResolvedCommandBinaryPath(env, r.command)
	return env
}

func withDefaultTerminalEnv(env []string) []string {
	if hasEnvKey(env, "TERM") {
		return env
	}
	return append(append([]string(nil), env...), "TERM=xterm-256color")
}

func hasEnvKey(env []string, key string) bool {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return true
		}
	}
	return false
}

func withEnvValue(env []string, key, value string) []string {
	updated := append([]string(nil), env...)
	prefix := key + "="
	for i, item := range updated {
		if strings.HasPrefix(item, prefix) {
			updated[i] = prefix + value
			return updated
		}
	}
	return append(updated, prefix+value)
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return item[len(prefix):]
		}
	}
	return ""
}

func withConfiguredCLIExtraPath(env []string) []string {
	extraPath := strings.TrimSpace(envValue(env, "AUTO_CODE_CLI_EXTRA_PATH"))
	if extraPath == "" {
		return env
	}
	return withPathEntries(env, filepath.SplitList(extraPath), true, false)
}

func withResolvedCommandBinaryPath(env []string, command string) []string {
	binary := directCommandBinary(command)
	if binary == "" {
		return env
	}
	pathValue := envValue(env, "PATH")
	if _, err := lookPathInEnv(binary, pathValue); err == nil {
		return env
	}

	foundDirs := make([]string, 0, 4)
	for _, dir := range discoverCLIPathDirs(env) {
		if isExecutableFile(filepath.Join(dir, binary)) {
			foundDirs = append(foundDirs, dir)
		}
	}
	if len(foundDirs) == 0 {
		return env
	}
	return withPathEntries(env, foundDirs, true, true)
}

func withPathEntries(env []string, entries []string, prepend bool, requireExisting bool) []string {
	currentEntries := splitPathEntries(envValue(env, "PATH"))
	extraEntries := normalizePathEntries(entries, requireExisting)
	if len(extraEntries) == 0 {
		return env
	}

	merged := make([]string, 0, len(currentEntries)+len(extraEntries))
	seen := make(map[string]struct{}, len(currentEntries)+len(extraEntries))
	appendUnique := func(items []string) {
		for _, item := range items {
			if item == "" {
				continue
			}
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			merged = append(merged, item)
		}
	}

	if prepend {
		appendUnique(extraEntries)
		appendUnique(currentEntries)
	} else {
		appendUnique(currentEntries)
		appendUnique(extraEntries)
	}

	return withEnvValue(env, "PATH", strings.Join(merged, string(os.PathListSeparator)))
}

func splitPathEntries(pathValue string) []string {
	if strings.TrimSpace(pathValue) == "" {
		return nil
	}
	rawItems := filepath.SplitList(pathValue)
	items := make([]string, 0, len(rawItems))
	for _, item := range rawItems {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		items = append(items, item)
	}
	return items
}

func normalizePathEntries(entries []string, requireExisting bool) []string {
	normalized := make([]string, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, item := range entries {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		item = filepath.Clean(item)
		if requireExisting {
			info, err := os.Stat(item)
			if err != nil || !info.IsDir() {
				continue
			}
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		normalized = append(normalized, item)
	}
	return normalized
}

func discoverCLIPathDirs(env []string) []string {
	dirs := []string{
		"/usr/local/bin",
		"/usr/local/sbin",
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/snap/bin",
	}

	home := strings.TrimSpace(envValue(env, "HOME"))
	if home == "" {
		if resolvedHome, err := os.UserHomeDir(); err == nil {
			home = strings.TrimSpace(resolvedHome)
		}
	}
	if home == "" {
		return normalizePathEntries(dirs, true)
	}

	homeDirs := []string{
		filepath.Join(home, "bin"),
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".npm-global", "bin"),
		filepath.Join(home, ".yarn", "bin"),
		filepath.Join(home, ".config", "yarn", "global", "node_modules", ".bin"),
		filepath.Join(home, ".volta", "bin"),
		filepath.Join(home, ".local", "share", "pnpm"),
		filepath.Join(home, "node_modules", ".bin"),
	}
	dirs = append(homeDirs, dirs...)

	nvmDirs, _ := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin"))
	sort.Strings(nvmDirs)
	for i := len(nvmDirs) - 1; i >= 0; i-- {
		dirs = append(dirs, nvmDirs[i])
	}

	return normalizePathEntries(dirs, true)
}

func directCommandBinary(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	command = trailingCommandSegment(command)
	if shellHasTopLevelOperator(command) {
		return ""
	}

	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	first := unquoteShellToken(fields[0])
	if strings.EqualFold(first, "exec") && len(fields) > 1 {
		first = unquoteShellToken(fields[1])
	}
	if first == "" {
		return ""
	}

	lower := strings.ToLower(first)
	if strings.Contains(first, "/") {
		return ""
	}
	if strings.HasSuffix(lower, ".sh") || strings.HasSuffix(lower, ".bash") || strings.HasSuffix(lower, ".zsh") {
		return ""
	}
	return first
}

func shellHasTopLevelOperator(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	inSingleQuote := false
	inDoubleQuote := false
	escaped := false
	for index := 0; index < len(command); index++ {
		ch := command[index]
		if escaped {
			escaped = false
			continue
		}
		switch ch {
		case '\\':
			if !inSingleQuote {
				escaped = true
			}
		case '\'':
			if !inDoubleQuote {
				inSingleQuote = !inSingleQuote
			}
		case '"':
			if !inSingleQuote {
				inDoubleQuote = !inDoubleQuote
			}
		}
		if inSingleQuote || inDoubleQuote {
			continue
		}
		if index+1 < len(command) {
			token := command[index : index+2]
			if token == "&&" || token == "||" {
				return true
			}
		}
		if strings.ContainsRune(";|<>", rune(ch)) {
			return true
		}
	}
	return false
}

func trailingCommandSegment(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	lastIndex := -1
	lastSize := 0
	inSingleQuote := false
	inDoubleQuote := false
	escaped := false
	for index := 0; index < len(command); index++ {
		ch := command[index]
		if escaped {
			escaped = false
			continue
		}
		switch ch {
		case '\\':
			if !inSingleQuote {
				escaped = true
			}
		case '\'':
			if !inDoubleQuote {
				inSingleQuote = !inSingleQuote
			}
		case '"':
			if !inSingleQuote {
				inDoubleQuote = !inDoubleQuote
			}
		}
		if inSingleQuote || inDoubleQuote {
			continue
		}
		if index+1 < len(command) {
			token := command[index : index+2]
			if token == "&&" || token == "||" {
				lastIndex = index
				lastSize = 2
				index++
				continue
			}
		}
		if ch == ';' {
			lastIndex = index
			lastSize = 1
		}
	}
	if lastIndex < 0 {
		return command
	}
	return strings.TrimSpace(command[lastIndex+lastSize:])
}

func unquoteShellToken(token string) string {
	token = strings.TrimSpace(token)
	if len(token) < 2 {
		return token
	}
	if (strings.HasPrefix(token, "'") && strings.HasSuffix(token, "'")) ||
		(strings.HasPrefix(token, `"`) && strings.HasSuffix(token, `"`)) {
		return token[1 : len(token)-1]
	}
	return token
}

func lookPathInEnv(binary, pathValue string) (string, error) {
	binary = strings.TrimSpace(binary)
	if binary == "" {
		return "", exec.ErrNotFound
	}
	if strings.Contains(binary, string(os.PathSeparator)) {
		if isExecutableFile(binary) {
			return binary, nil
		}
		return "", exec.ErrNotFound
	}

	for _, dir := range splitPathEntries(pathValue) {
		candidate := filepath.Join(dir, binary)
		if isExecutableFile(candidate) {
			return candidate, nil
		}
	}
	return "", exec.ErrNotFound
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode().IsRegular() && info.Mode()&0o111 != 0
}

func validateCommandAvailable(command string, env []string) error {
	binary := directCommandBinary(command)
	if binary == "" {
		return nil
	}
	pathValue := envValue(env, "PATH")
	if _, err := lookPathInEnv(binary, pathValue); err == nil {
		return nil
	}
	return fmt.Errorf(
		"launch command %q not found in PATH=%q; the backend starts CLI sessions via /bin/sh in a non-interactive environment, so TeamCity/deploy processes do not load your login-shell PATH. Set AUTO_CODE_CLI_EXTRA_PATH to the directory containing %s, export PATH before starting the service, or change script_command to an absolute path",
		binary,
		pathValue,
		binary,
	)
}

func normalizePTYInput(text string) string {
	if text == "" || text == "\x03" {
		return text
	}
	text = strings.ReplaceAll(text, "\r\n", "\r")
	text = strings.ReplaceAll(text, "\n", "\r")
	return text
}

func hasTrailingLineBreak(text string) bool {
	return strings.HasSuffix(text, "\r\n") || strings.HasSuffix(text, "\n") || strings.HasSuffix(text, "\r")
}

func trimOneTrailingLineBreak(text string) string {
	switch {
	case strings.HasSuffix(text, "\r\n"):
		return strings.TrimSuffix(text, "\r\n")
	case strings.HasSuffix(text, "\n"):
		return strings.TrimSuffix(text, "\n")
	case strings.HasSuffix(text, "\r"):
		return strings.TrimSuffix(text, "\r")
	default:
		return text
	}
}

func wrapPTYBootstrapCommand(command string, cols, rows int) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return command
	}
	cols, rows = normalizePTYSize(cols, rows)
	return fmt.Sprintf(
		"stty cols %d rows %d 2>/dev/null || true; %s",
		cols,
		rows,
		command,
	)
}

func (r *claudeCodeRuntime) isLikelyVimInsertMode() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.output) == 0 {
		return false
	}
	window := r.output
	if len(window) > 8192 {
		window = window[len(window)-8192:]
	}
	return strings.Contains(string(window), "-- INSERT --")
}

func (r *claudeCodeRuntime) setOutputHook(hook func(chunk []byte, at time.Time)) {
	r.mu.Lock()
	r.outputHook = hook
	r.mu.Unlock()
}

func (r *claudeCodeRuntime) setStateHook(hook func(summary SessionSummary)) {
	r.mu.Lock()
	r.stateHook = hook
	r.mu.Unlock()
}

func (r *claudeCodeRuntime) setSideErrorHook(hook func(sideErr RuntimeSideError)) {
	r.mu.Lock()
	r.sideErrorHook = hook
	r.mu.Unlock()
}

func isBenignTerminalReadError(err error) bool {
	// PTY streams frequently surface EIO / closed-file errors during normal teardown.
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "input/output error") || strings.Contains(msg, "file already closed")
}

func (r *claudeCodeRuntime) terminate() error {
	r.mu.Lock()
	running := r.running
	pid := r.processPID
	pgid := r.processPGID
	r.mu.Unlock()

	if !running || pid <= 0 {
		return errSessionNotRunning
	}
	return terminateProcessTree(pid, pgid)
}

func (r *claudeCodeRuntime) poll(offset int64) PollResult {
	r.mu.Lock()
	defer r.mu.Unlock()

	base := r.baseOffset
	end := r.baseOffset + int64(len(r.output))
	if offset > end {
		offset = end
	}

	rewind := false
	if offset < base {
		offset = base
		rewind = true
	}

	start := int(offset - base)
	remaining := len(r.output) - start
	chunkLen := remaining
	more := false
	if chunkLen > pollChunkBytes {
		chunkLen = pollChunkBytes
		more = true
	}
	chunkBytes := r.output[start : start+chunkLen]
	chunk := string(chunkBytes)
	chunkB64 := ""
	if len(chunkBytes) > 0 {
		chunkB64 = base64.StdEncoding.EncodeToString(chunkBytes)
	}

	state := "stopped"
	if r.running {
		state = "running"
	}
	if r.exited {
		state = "exited"
	}

	return PollResult{
		State:      state,
		Output:     chunk,
		RawB64:     chunkB64,
		SideErrors: append([]RuntimeSideError(nil), r.sideErrors...),
		NextOffset: offset + int64(chunkLen),
		Rewind:     rewind,
		Done:       r.exited,
		More:       more,
		ExitCode:   r.exitCode,
		LastError:  r.lastError,
	}
}

func (r *claudeCodeRuntime) window(maxBytes int) ([]byte, int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if maxBytes <= 0 {
		maxBytes = 128 * 1024
	}
	base := r.baseOffset
	end := r.baseOffset + int64(len(r.output))
	start := end - int64(maxBytes)
	if start < base {
		start = base
	}
	idx := int(start - base)
	return append([]byte(nil), r.output[idx:]...), end
}

func (r *claudeCodeRuntime) sideErrorsSnapshot() []RuntimeSideError {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]RuntimeSideError(nil), r.sideErrors...)
}

func (r *claudeCodeRuntime) summary() SessionSummary {
	r.mu.Lock()
	defer r.mu.Unlock()

	state := "stopped"
	if r.running {
		state = "running"
	}
	if r.exited {
		state = "exited"
	}
	return SessionSummary{
		Command:     r.command,
		State:       state,
		LaunchMode:  r.launchMode,
		WorkDir:     r.workDir,
		ProcessPID:  r.processPID,
		ProcessPGID: r.processPGID,
		CreatedAt:   r.createdAt,
		UpdatedAt:   r.updatedAt,
		ExitCode:    r.exitCode,
		LastError:   r.lastError,
	}
}

// CLISession is the application layer object with stable external session ID.
type CLISession struct {
	ID      string
	AgentID string
	runtime *claudeCodeRuntime
}

// NewCLISession creates a runtime session with command and optional working directory.
func NewCLISession(id, agentID, command, workDir string, env map[string]string) *CLISession {
	return NewCLISessionWithSize(id, agentID, command, workDir, env, 0, 0)
}

// NewCLISessionWithSize creates a runtime session with explicit initial PTY size.
func NewCLISessionWithSize(id, agentID, command, workDir string, env map[string]string, cols, rows int) *CLISession {
	return &CLISession{
		ID:      id,
		AgentID: agentID,
		runtime: newClaudeCodeRuntime(command, workDir, env, cols, rows),
	}
}

func (s *CLISession) start() error {
	return s.runtime.start()
}

func (s *CLISession) WriteInput(text string) error {
	return s.runtime.writeInput(text)
}

func (s *CLISession) WriteRawBytes(raw []byte) error {
	return s.runtime.writeRawInput(raw)
}

func (s *CLISession) Resize(cols, rows int) error {
	return s.runtime.resize(cols, rows)
}

func (s *CLISession) Terminate() error {
	return s.runtime.terminate()
}

func (s *CLISession) Poll(offset int64) PollResult {
	result := s.runtime.poll(offset)
	result.SessionID = s.ID
	result.AgentID = s.AgentID
	return result
}

func (s *CLISession) Window(maxBytes int) (string, int64) {
	window, resumeOffset := s.runtime.window(maxBytes)
	return string(window), resumeOffset
}

func (s *CLISession) WindowBytes(maxBytes int) ([]byte, int64) {
	return s.runtime.window(maxBytes)
}

func (s *CLISession) SideErrors() []RuntimeSideError {
	return s.runtime.sideErrorsSnapshot()
}

func (s *CLISession) Summary() SessionSummary {
	summary := s.runtime.summary()
	summary.ID = s.ID
	summary.AgentID = s.AgentID
	return summary
}

func (s *CLISession) setOutputHook(hook func(chunk []byte, at time.Time)) {
	s.runtime.setOutputHook(hook)
}

func (s *CLISession) setStateHook(hook func(summary SessionSummary)) {
	s.runtime.setStateHook(func(summary SessionSummary) {
		summary.ID = s.ID
		summary.AgentID = s.AgentID
		hook(summary)
	})
}

func (s *CLISession) setSideErrorHook(hook func(sideErr RuntimeSideError)) {
	s.runtime.setSideErrorHook(hook)
}

type CLISessionManager struct {
	mu             sync.RWMutex
	sessions       map[string]*CLISession
	defaultCommand string
	nextID         uint64
	events         *CLIEventHub
}

func NewCLISessionManager(defaultCommand string) *CLISessionManager {
	defaultCommand = strings.TrimSpace(defaultCommand)
	if defaultCommand == "" {
		defaultCommand = "claude"
	}
	defaultCommand = sanitizeDefaultCommand(defaultCommand)
	return &CLISessionManager{
		sessions:       make(map[string]*CLISession),
		defaultCommand: defaultCommand,
		events:         NewCLIEventHub(nil),
	}
}

func sanitizeDefaultCommand(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return command
	}
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return command
	}
	binary := strings.Trim(fields[0], `"'`)
	if filepath.Base(binary) != "codex" {
		return command
	}

	mode := ""
	extras := make([]string, 0, len(fields)-1)
	for i := 1; i < len(fields); i++ {
		token := fields[i]
		normalized := strings.Trim(token, `"'`)
		switch normalized {
		case "--dangerously-bypass-approvals-and-sandbox":
			mode = "--dangerously-bypass-approvals-and-sandbox"
			continue
		case "--yolo":
			if mode == "" {
				mode = "--yolo"
			}
			continue
		case "-c", "--config":
			if i+1 < len(fields) && isLegacyCodexDefaultConfig(fields[i+1]) {
				i++
				continue
			}
		}
		extras = append(extras, token)
	}
	if mode == "" {
		mode = "--yolo"
	}

	result := []string{fields[0], mode}
	result = append(result, extras...)
	return strings.Join(result, " ")
}

func isLegacyCodexDefaultConfig(raw string) bool {
	value := strings.Trim(raw, `"'`)
	return value == "check_for_update_on_startup=false" ||
		value == "trust_level=trusted" ||
		strings.Contains(value, `.trust_level="trusted"`)
}

// Create starts a standalone CLI session using command override or default command.
func (m *CLISessionManager) Create(command string) (*CLISession, error) {
	return m.CreateWithSize(command, 0, 0)
}

// CreateWithSize starts a standalone CLI session with explicit initial PTY size.
func (m *CLISessionManager) CreateWithSize(command string, cols, rows int) (*CLISession, error) {
	return m.CreateWithWorkDirAndEnvAndSize(command, "", nil, cols, rows)
}

// CreateWithWorkDir starts a runtime session with command override and optional work directory.
func (m *CLISessionManager) CreateWithWorkDir(command, workDir string) (*CLISession, error) {
	return m.CreateWithWorkDirAndEnvAndSize(command, workDir, nil, 0, 0)
}

// CreateWithWorkDirAndEnv starts one runtime session with command/workdir and env overrides.
func (m *CLISessionManager) CreateWithWorkDirAndEnv(command, workDir string, env map[string]string) (*CLISession, error) {
	return m.CreateWithWorkDirAndEnvAndSize(command, workDir, env, 0, 0)
}

// CreateWithWorkDirAndEnvAndSize starts one runtime session with explicit initial PTY size.
func (m *CLISessionManager) CreateWithWorkDirAndEnvAndSize(command, workDir string, env map[string]string, cols, rows int) (*CLISession, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		command = m.defaultCommand
	}
	workDir = strings.TrimSpace(workDir)

	id := fmt.Sprintf("cli-%06d", atomic.AddUint64(&m.nextID, 1))
	agentID := newAgentID()
	session := NewCLISessionWithSize(id, agentID, command, workDir, env, cols, rows)
	session.setOutputHook(func(chunk []byte, at time.Time) {
		m.emitOutputChunk(id, agentID, chunk, at)
	})
	session.setStateHook(func(summary SessionSummary) {
		m.emitStateChange(summary)
	})
	session.setSideErrorHook(func(sideErr RuntimeSideError) {
		m.emitSideError(id, agentID, sideErr)
	})
	if err := session.start(); err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.sessions[id] = session
	m.mu.Unlock()
	return session, nil
}

func (m *CLISessionManager) emitOutputChunk(sessionID, agentID string, chunk []byte, at time.Time) {
	if len(chunk) == 0 {
		return
	}
	m.mu.RLock()
	events := m.events
	m.mu.RUnlock()
	if events == nil {
		return
	}
	events.EmitOutputChunk(CLIOutputChunk{
		SessionID: sessionID,
		AgentID:   agentID,
		Timestamp: at,
		Payload:   chunk,
	})
}

func (m *CLISessionManager) emitStateChange(summary SessionSummary) {
	m.mu.RLock()
	events := m.events
	m.mu.RUnlock()
	if events == nil {
		return
	}
	events.EmitStateChange(summary)
}

func (m *CLISessionManager) emitSideError(sessionID, agentID string, sideErr RuntimeSideError) {
	m.mu.RLock()
	events := m.events
	m.mu.RUnlock()
	if events == nil {
		return
	}
	events.EmitSideError(sessionID, agentID, sideErr)
}

func (m *CLISessionManager) Destroy(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("session id is required")
	}
	logger := logging.Named("runtime.cli-session")

	m.mu.Lock()
	session, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if !ok {
		logger.Warn("destroy cli session skipped: session not found", zap.String("session_id", id))
		return errors.New("session not found")
	}

	summary := session.Summary()
	logger.Info(
		"destroying cli session",
		zap.String("session_id", session.ID),
		zap.String("agent_id", session.AgentID),
		zap.String("state", summary.State),
		zap.String("launch_mode", summary.LaunchMode),
		zap.Int("process_pid", summary.ProcessPID),
		zap.Int("process_pgid", summary.ProcessPGID),
	)

	if err := session.Terminate(); err != nil && !errors.Is(err, errSessionNotRunning) {
		logger.Warn(
			"destroy cli session terminate failed",
			zap.String("session_id", session.ID),
			zap.String("agent_id", session.AgentID),
			zap.Error(err),
		)
		return err
	}
	m.mu.RLock()
	events := m.events
	m.mu.RUnlock()
	if events != nil {
		events.ClearSession(session.ID, session.AgentID)
	}
	logger.Info(
		"destroy cli session completed",
		zap.String("session_id", session.ID),
		zap.String("agent_id", session.AgentID),
	)
	return nil
}

func (m *CLISessionManager) Get(id string) (*CLISession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

func (m *CLISessionManager) List() []SessionSummary {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := make([]SessionSummary, 0, len(m.sessions))
	for _, session := range m.sessions {
		list = append(list, session.Summary())
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.After(list[j].CreatedAt)
	})
	return list
}

func (m *CLISessionManager) Events() *CLIEventHub {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.events
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		name := strings.TrimSpace(key)
		if name == "" {
			continue
		}
		out[name] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
