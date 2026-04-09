package server

import (
	"fmt"
	"strings"
	"time"
)

type executionHealthView struct {
	State        string
	Reason       string
	LastOutputAt *time.Time
}

func sessionExecutionReason(view CLISessionView) string {
	state := strings.TrimSpace(view.State)
	lastError := strings.TrimSpace(view.LastError)
	switch state {
	case CLISessionStateTerminated, "exited":
		if lastError != "" {
			if view.ExitCode != nil {
				return fmt.Sprintf("session_exited (exit_code=%d): %s", *view.ExitCode, lastError)
			}
			return "session_exited: " + lastError
		}
		if view.ExitCode != nil {
			return fmt.Sprintf("session_exited (exit_code=%d)", *view.ExitCode)
		}
		return "session_exited"
	default:
		if lastError != "" {
			return lastError
		}
		if state != "" && state != CLISessionStateRunning {
			return "session_" + state
		}
		return "session_not_running"
	}
}

func (a *App) failedRequirementReason(requirementID string) string {
	if a != nil && a.requirementSvc != nil {
		if requirement, err := a.requirementSvc.Get(requirementID); err == nil && requirement != nil {
			if reason := strings.TrimSpace(requirement.LastAutoRetryReason); reason != "" {
				return reason
			}
		}
	}
	if watchdog := a.latestRequirementWatchdogEventByStatus(requirementID, RequirementWatchdogEventStatusFailed); watchdog != nil && watchdog.Detail != "" {
		return watchdog.Detail
	}
	return "requirement_failed"
}

func (a *App) resolveRequirementExecutionHealth(requirement Requirement) executionHealthView {
	if a != nil && a.requirementAuto != nil {
		if health, ok := a.requirementAuto.GetRequirementExecutionHealth(requirement.ID); ok {
			view := executionHealthView{
				State:  health.State,
				Reason: health.Reason,
			}
			if !health.LastOutputAt.IsZero() {
				at := health.LastOutputAt
				view.LastOutputAt = &at
			}
			if requirement.Status == RequirementStatusRunning && strings.TrimSpace(health.SessionID) != "" && a != nil && a.cliSessionSvc != nil {
				if sessionView, err := a.cliSessionSvc.GetView(health.SessionID); err == nil && sessionView != nil && sessionView.State != CLISessionStateRunning {
					view.State = requirementAutomationHealthStalledInterrupted
					view.Reason = sessionExecutionReason(*sessionView)
				}
			}
			if requirement.Status == RequirementStatusDone {
				view.State = requirementAutomationHealthCompleted
				if view.Reason == "" {
					view.Reason = requirementAutomationCompletionMarker
				}
			} else if requirement.Status == RequirementStatusFailed {
				view.State = requirementAutomationHealthFailed
				view.Reason = a.failedRequirementReason(requirement.ID)
			}
			return view
		}
	}

	switch requirement.Status {
	case RequirementStatusFailed:
		return executionHealthView{
			State:  requirementAutomationHealthFailed,
			Reason: a.failedRequirementReason(requirement.ID),
		}
	case RequirementStatusDone:
		return executionHealthView{
			State:  requirementAutomationHealthCompleted,
			Reason: requirementAutomationCompletionMarker,
		}
	case RequirementStatusRunning:
		return executionHealthView{
			State: requirementAutomationHealthRunning,
		}
	default:
		return executionHealthView{}
	}
}

func (a *App) resolveSessionExecutionHealth(view CLISessionView) executionHealthView {
	if a != nil && a.requirementAuto != nil {
		if health, ok := a.requirementAuto.GetSessionExecutionHealth(view.ID); ok {
			result := executionHealthView{
				State:  health.State,
				Reason: health.Reason,
			}
			if !health.LastOutputAt.IsZero() {
				at := health.LastOutputAt
				result.LastOutputAt = &at
			}
			return result
		}
	}

	if view.RequirementID == "" {
		return executionHealthView{}
	}
	if view.State != CLISessionStateRunning {
		return executionHealthView{
			State:  requirementAutomationHealthStalledInterrupted,
			Reason: sessionExecutionReason(view),
		}
	}
	return executionHealthView{
		State: requirementAutomationHealthRunning,
	}
}
