package server

import (
	"regexp"
	"strings"
	"time"
)

var (
	cliConfirmationCSIRegexp = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	cliConfirmationOSCRegexp = regexp.MustCompile(`\x1b\][^\x07]*(?:\x07|\x1b\\)`)
	cliEnterContinueRegex    = regexp.MustCompile(`(?i)(?:press|hit)\s+enter\s+to\s+(?:continue|confirm|proceed|resume)`)
	cliYesNoPromptRegex      = regexp.MustCompile(`(?i)(?:continue|proceed|allow|approve|confirm|resume|retry|trust).*(?:\[(?:y|yes)\/(?:n|no)\]|\[(?:y|Y)\/(?:n|N)\]|\((?:y|Y)\/(?:n|N)\)|\[(?:y|Y)\/(?:n|N)\])`)
	cliGenericYesNoRegex     = regexp.MustCompile(`(?i)(?:\[y/n\]|\[y/N\]|\[Y/n\]|\(y/n\)|\(y/N\)|\(Y/n\))`)
	cliChinesePromptRegex    = regexp.MustCompile(`(?:是否继续|是否确认|确认继续|允许继续|是否允许)`)
	cliInterruptLineRegex    = regexp.MustCompile(`(?im)^\s*(?:\[[^\]]+\]\s*)?(?:operation\s+)?(?:interrupted|cancelled|canceled|aborted|stopped)\b.*$`)
)

const (
	cliConfirmationSettleDelay = 900 * time.Millisecond
	cliConfirmationRetryDelay  = 180 * time.Millisecond
	cliConfirmationDefaultWait = 10 * time.Second
)

type cliConfirmationMatch struct {
	Key      string
	Line     string
	Response []byte
}

func stripCLIConfirmationANSI(text string) string {
	if text == "" {
		return ""
	}
	text = cliConfirmationOSCRegexp.ReplaceAllString(text, "")
	text = cliConfirmationCSIRegexp.ReplaceAllString(text, "")
	return strings.ReplaceAll(text, "\x1b", "")
}

func normalizeCLIConfirmationWindow(text string) string {
	text = stripCLIConfirmationANSI(text)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text
}

func detectCLIConfirmation(window string) *cliConfirmationMatch {
	normalized := normalizeCLIConfirmationWindow(window)
	if strings.TrimSpace(normalized) == "" {
		return nil
	}
	lower := strings.ToLower(normalized)

	if strings.Contains(lower, "do you trust the contents of this directory?") {
		return &cliConfirmationMatch{
			Key:      "codex_trust_directory",
			Line:     "do you trust the contents of this directory?",
			Response: []byte("\r"),
		}
	}

	lines := strings.Split(normalized, "\n")
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		lowerLine := strings.ToLower(line)
		switch {
		case cliEnterContinueRegex.MatchString(line),
			strings.Contains(lowerLine, "press enter to continue"),
			strings.Contains(lowerLine, "hit enter to continue"),
			strings.Contains(line, "按回车继续"):
			return &cliConfirmationMatch{
				Key:      "enter_to_continue",
				Line:     lowerLine,
				Response: []byte("\r"),
			}
		case cliYesNoPromptRegex.MatchString(line),
			cliGenericYesNoRegex.MatchString(line),
			cliChinesePromptRegex.MatchString(line):
			return &cliConfirmationMatch{
				Key:      "yes_no_confirmation",
				Line:     lowerLine,
				Response: []byte("y\r"),
			}
		}
	}
	return nil
}

func cliConfirmationFingerprint(match *cliConfirmationMatch) string {
	if match == nil {
		return ""
	}
	return match.Key + "|" + strings.TrimSpace(strings.ToLower(match.Line))
}

func cliWindowSuggestsIdlePrompt(window string) bool {
	normalized := normalizeCLIConfirmationWindow(window)
	if strings.TrimSpace(normalized) == "" {
		return false
	}
	lower := strings.ToLower(normalized)
	if detectCLIConfirmation(normalized) != nil {
		return false
	}
	if strings.Contains(lower, "esc to interrupt") {
		return false
	}
	trimmed := strings.TrimSpace(normalized)
	const codexPrompt = "\u203a"
	return strings.Contains(normalized, "\n❯") ||
		strings.Contains(normalized, "\n> ") ||
		strings.Contains(normalized, codexPrompt) ||
		strings.Contains(normalized, "\n"+codexPrompt) ||
		strings.HasSuffix(trimmed, "❯") ||
		strings.HasSuffix(trimmed, ">") ||
		strings.HasPrefix(trimmed, codexPrompt) ||
		strings.HasSuffix(trimmed, codexPrompt)
}

func cliWindowShowsInterruption(window string) bool {
	normalized := normalizeCLIConfirmationWindow(window)
	if strings.TrimSpace(normalized) == "" {
		return false
	}
	return cliInterruptLineRegex.MatchString(normalized)
}

func prepareSessionForInput(session *CLISession, timeout time.Duration, waitForPrompt bool) error {
	if session == nil {
		return nil
	}
	if timeout <= 0 {
		timeout = cliConfirmationDefaultWait
	}
	startedAt := time.Now()
	deadline := time.Now().Add(timeout)
	lastFingerprint := ""
	lastHandledAt := time.Time{}
	sawConfirmation := false
	clearedAt := time.Time{}

	for {
		window, _ := session.Window(64 * 1024)
		match := detectCLIConfirmation(window)
		now := time.Now()
		if match != nil {
			sawConfirmation = true
			clearedAt = time.Time{}
			fingerprint := cliConfirmationFingerprint(match)
			if fingerprint != "" && fingerprint == lastFingerprint && now.Sub(lastHandledAt) >= cliConfirmationSettleDelay {
				return nil
			}
			if fingerprint != "" && (fingerprint != lastFingerprint || now.Sub(lastHandledAt) >= 700*time.Millisecond) {
				if err := session.WriteRawBytes(match.Response); err != nil {
					return err
				}
				lastFingerprint = fingerprint
				lastHandledAt = now
			}
		} else if sawConfirmation {
			if clearedAt.IsZero() {
				clearedAt = now
			}
			if now.Sub(clearedAt) >= cliConfirmationSettleDelay {
				return nil
			}
		} else if waitForPrompt && (cliWindowSuggestsIdlePrompt(window) || now.Sub(startedAt) >= 2*time.Second) {
			return nil
		} else if !waitForPrompt {
			return nil
		}

		if now.After(deadline) {
			return nil
		}
		time.Sleep(cliConfirmationRetryDelay)
	}
}
