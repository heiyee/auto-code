package runtime

import (
	"bufio"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultCLIOutputArchiveLimit = 500
)

type CLIOutputSnapshotEntry struct {
	Seq       int64     `json:"seq"`
	Timestamp time.Time `json:"timestamp"`
	RawB64    string    `json:"raw_b64"`
}

type CLIOutputArchive struct {
	mu         sync.Mutex
	rootDir    string
	maxEntries int
	compactAt  int
	counts     map[string]int
}

// NewCLIOutputArchive initializes JSONL archive storage for CLI output snapshots.
func NewCLIOutputArchive(rootDir string, maxEntries int) (*CLIOutputArchive, error) {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		rootDir = filepath.Join(os.TempDir(), "auto-code-cli-output")
	}
	if maxEntries <= 0 {
		maxEntries = defaultCLIOutputArchiveLimit
	}
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return nil, err
	}

	compactGap := maxEntries / 5
	if compactGap < 50 {
		compactGap = 50
	}
	return &CLIOutputArchive{
		rootDir:    rootDir,
		maxEntries: maxEntries,
		compactAt:  maxEntries + compactGap,
		counts:     make(map[string]int),
	}, nil
}

// Root returns the archive root directory.
func (a *CLIOutputArchive) Root() string {
	if a == nil {
		return ""
	}
	return a.rootDir
}

// MaxEntries returns the max retained snapshot entries per session.
func (a *CLIOutputArchive) MaxEntries() int {
	if a == nil {
		return 0
	}
	return a.maxEntries
}

// Append persists one output event into the session archive file.
func (a *CLIOutputArchive) Append(event CLIEvent) error {
	if a == nil {
		return nil
	}
	if event.Type != cliEventTypeOutput {
		return nil
	}
	sessionID := strings.TrimSpace(event.SessionID)
	if sessionID == "" {
		return nil
	}
	if strings.TrimSpace(event.RawB64) == "" {
		return nil
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	entry := CLIOutputSnapshotEntry{
		Seq:       event.Seq,
		Timestamp: event.Timestamp,
		RawB64:    event.RawB64,
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	filePath := a.pathForSession(sessionID)
	count, ok := a.counts[sessionID]
	if !ok {
		lines, err := countLines(filePath)
		if err != nil {
			return err
		}
		count = lines
	}

	if err := appendArchiveEntry(filePath, entry); err != nil {
		return err
	}
	count++

	if count >= a.compactAt {
		trimmed, err := compactArchiveFile(filePath, a.maxEntries)
		if err != nil {
			return err
		}
		count = trimmed
	}
	a.counts[sessionID] = count
	return nil
}

// Snapshot reads recent archived output entries for one session.
func (a *CLIOutputArchive) Snapshot(sessionID string, limit int) ([]CLIOutputSnapshotEntry, error) {
	if a == nil {
		return nil, nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("session id is required")
	}
	if limit <= 0 {
		limit = a.maxEntries
	}
	if limit > a.maxEntries {
		limit = a.maxEntries
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	filePath := a.pathForSession(sessionID)
	all, err := readArchiveEntries(filePath)
	if err != nil {
		return nil, err
	}
	if len(all) <= limit {
		return all, nil
	}
	return append([]CLIOutputSnapshotEntry(nil), all[len(all)-limit:]...), nil
}

// DeleteSession removes one session archive file and in-memory counters.
func (a *CLIOutputArchive) DeleteSession(sessionID string) error {
	if a == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session id is required")
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	delete(a.counts, sessionID)
	filePath := a.pathForSession(sessionID)
	if err := os.Remove(filePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// pathForSession builds a deterministic archive path for one session id.
func (a *CLIOutputArchive) pathForSession(sessionID string) string {
	sum := sha1.Sum([]byte(sessionID))
	safe := sanitizeArchiveToken(sessionID)
	if len(safe) > 32 {
		safe = safe[:32]
	}
	if safe == "" {
		safe = "session"
	}
	filename := fmt.Sprintf("%s-%x.jsonl", safe, sum[:6])
	return filepath.Join(a.rootDir, filename)
}

// sanitizeArchiveToken removes non file-safe characters from session identifiers.
func sanitizeArchiveToken(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		}
	}
	return b.String()
}

// appendArchiveEntry appends one JSON line entry to the archive file.
func appendArchiveEntry(path string, entry CLIOutputSnapshotEntry) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	bytes, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if _, err := file.Write(bytes); err != nil {
		return err
	}
	if _, err := file.Write([]byte("\n")); err != nil {
		return err
	}
	return nil
}

// compactArchiveFile trims archive entries to the configured retention limit.
func compactArchiveFile(path string, maxEntries int) (int, error) {
	entries, err := readArchiveEntries(path)
	if err != nil {
		return 0, err
	}
	if len(entries) <= maxEntries {
		return len(entries), nil
	}
	trimmed := entries[len(entries)-maxEntries:]
	if err := writeArchiveEntries(path, trimmed); err != nil {
		return 0, err
	}
	return len(trimmed), nil
}

// writeArchiveEntries rewrites archive entries to file via temp-file rename.
func writeArchiveEntries(path string, entries []CLIOutputSnapshotEntry) error {
	tmpPath := path + ".tmp"
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}

	writer := bufio.NewWriter(file)
	for _, entry := range entries {
		line, err := json.Marshal(entry)
		if err != nil {
			_ = file.Close()
			return err
		}
		if _, err := writer.Write(line); err != nil {
			_ = file.Close()
			return err
		}
		if err := writer.WriteByte('\n'); err != nil {
			_ = file.Close()
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// readArchiveEntries loads all valid JSONL entries from one archive file.
func readArchiveEntries(path string) ([]CLIOutputSnapshotEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 256*1024)
	scanner.Buffer(buf, 8*1024*1024)

	out := make([]CLIOutputSnapshotEntry, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry CLIOutputSnapshotEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if strings.TrimSpace(entry.RawB64) == "" {
			continue
		}
		out = append(out, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// countLines returns line count for one archive file path.
func countLines(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	count := 0
	for scanner.Scan() {
		count++
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return count, nil
}
