package server

import "strings"

type sessionFailureDiagnosis struct {
	Kind      string
	Summary   string
	Retryable bool
	Matched   bool
}

type sessionFailurePattern struct {
	kind      string
	retryable bool
	needles   []string
}

var quotaFailureNeedles = []string{
	"quota_or_rate_limit",
	"rate limit",
	"quota exceeded",
	"insufficient credits",
	"billing hard limit",
	"额度不足",
	"余额不足",
}

var networkFailureNeedles = []string{
	"network_timeout",
	"network error",
	"timed out",
	"context deadline exceeded",
	"connection reset",
	"econnreset",
	"enotfound",
	"socket hang up",
	"tls handshake timeout",
	"i/o timeout",
}

var sessionFailurePatterns = []sessionFailurePattern{
	{
		kind:      "quota_or_rate_limit",
		retryable: false,
		needles:   quotaFailureNeedles,
	},
	{
		kind:      "network_timeout",
		retryable: true,
		needles:   networkFailureNeedles,
	},
	{
		kind:      "terminal_setup_failed",
		retryable: false,
		needles: []string{
			"stdin is not a terminal",
			"not a terminal",
			"inappropriate ioctl for device",
		},
	},
	{
		kind:      "launch_command_missing",
		retryable: false,
		needles: []string{
			"command not found",
			"not found in path",
			"executable file not found",
		},
	},
	{
		kind:      "authentication_failed",
		retryable: false,
		needles: []string{
			"authentication failed",
			"unauthorized",
			"forbidden",
			"invalid api key",
			"invalid token",
			"token expired",
		},
	},
	{
		kind:      "permission_denied",
		retryable: false,
		needles: []string{
			"permission denied",
			"operation not permitted",
		},
	},
}

func classifySessionFailureSources(sources ...string) sessionFailureDiagnosis {
	trimmedSources := make([]string, 0, len(sources))
	for _, source := range sources {
		source = strings.TrimSpace(source)
		if source == "" {
			continue
		}
		trimmedSources = append(trimmedSources, source)
	}

	for _, pattern := range sessionFailurePatterns {
		for _, source := range trimmedSources {
			lower := strings.ToLower(source)
			for _, needle := range pattern.needles {
				if strings.Contains(lower, needle) {
					return sessionFailureDiagnosis{
						Kind:      pattern.kind,
						Summary:   formatSessionFailureSummary(pattern.kind, source),
						Retryable: pattern.retryable,
						Matched:   true,
					}
				}
			}
		}
	}

	for _, source := range trimmedSources {
		if source == "" ||
			source == "session_disconnected" ||
			source == "session_not_running" ||
			source == "watchdog_cli_error" {
			continue
		}
		return sessionFailureDiagnosis{
			Kind:      "unknown",
			Summary:   source,
			Retryable: true,
			Matched:   false,
		}
	}

	return sessionFailureDiagnosis{
		Kind:      "unknown",
		Summary:   "",
		Retryable: true,
		Matched:   false,
	}
}

func formatSessionFailureSummary(kind, detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" || detail == kind {
		return kind
	}
	lowerDetail := strings.ToLower(detail)
	if strings.Contains(lowerDetail, kind) {
		return detail
	}
	return kind + ": " + detail
}

func tailSessionDiagnosticText(text string, maxChars int) string {
	text = strings.TrimSpace(text)
	if text == "" || maxChars <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}
	return string(runes[len(runes)-maxChars:])
}

func (c *RequirementAutomationCoordinator) sessionDiagnosticText(sessionID string) string {
	if c == nil {
		return ""
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	state, ok := c.sessionState[sessionID]
	if !ok {
		return ""
	}
	return tailSessionDiagnosticText(state.cleanOutput, 4000)
}

func (a *App) diagnoseSessionFailure(sessionID, fallbackReason string) sessionFailureDiagnosis {
	sources := make([]string, 0, 8)
	fallbackReason = strings.TrimSpace(fallbackReason)
	if fallbackReason != "" {
		sources = append(sources, fallbackReason)
	}
	if a == nil {
		return classifySessionFailureSources(sources...)
	}
	if a.requirementAuto != nil {
		snapshot := a.requirementAuto.getSessionSnapshot(sessionID)
		if strings.TrimSpace(snapshot.HealthReason) != "" {
			sources = append(sources, snapshot.HealthReason)
		}
		if diagnosticText := a.requirementAuto.sessionDiagnosticText(sessionID); diagnosticText != "" {
			sources = append(sources, diagnosticText)
		}
	}
	if a.cliMgr != nil {
		if session, ok := a.cliMgr.Get(strings.TrimSpace(sessionID)); ok && session != nil {
			summary := session.Summary()
			if strings.TrimSpace(summary.LastError) != "" {
				sources = append(sources, summary.LastError)
			}
			if window, _ := session.Window(64 * 1024); strings.TrimSpace(window) != "" {
				sources = append(sources, tailSessionDiagnosticText(normalizeRequirementAutomationText(window), 4000))
			}
			for _, sideErr := range session.SideErrors() {
				if strings.TrimSpace(sideErr.Code) != "" {
					sources = append(sources, sideErr.Code)
				}
				if strings.TrimSpace(sideErr.Message) != "" {
					sources = append(sources, sideErr.Message)
				}
			}
		}
	}
	if a.cliSessionSvc != nil {
		if view, err := a.cliSessionSvc.GetView(sessionID); err == nil && view != nil {
			if strings.TrimSpace(view.LastError) != "" {
				sources = append(sources, view.LastError)
			}
			if formatted := sessionExecutionReason(*view); strings.TrimSpace(formatted) != "" {
				sources = append(sources, formatted)
			}
		}
	}
	return classifySessionFailureSources(sources...)
}
