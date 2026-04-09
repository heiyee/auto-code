package server

import (
	"auto-code/internal/httpapi"
	"net/http"
	"strings"
	"time"
)

type automationStatusItem struct {
	RequirementID         string     `json:"requirement_id"`
	RequirementTitle      string     `json:"requirement_title"`
	ProjectID             string     `json:"project_id"`
	ProjectName           string     `json:"project_name"`
	RequirementStatus     string     `json:"requirement_status"`
	SessionID             string     `json:"session_id,omitempty"`
	DispatchBlockedReason string     `json:"dispatch_blocked_reason,omitempty"`
	RetryAttempts         int        `json:"retry_attempts"`
	RetryBudget           int        `json:"retry_budget"`
	RetryBudgetExhausted  bool       `json:"retry_budget_exhausted"`
	ExecutionState        string     `json:"execution_state,omitempty"`
	ExecutionReason       string     `json:"execution_reason,omitempty"`
	LastOutputAt          *time.Time `json:"last_output_at,omitempty"`
	LastScanAt            *time.Time `json:"last_scan_at,omitempty"`
	NextScanAt            *time.Time `json:"next_scan_at,omitempty"`
	ScanCount             int64      `json:"scan_count"`
}

type automationStatusData struct {
	Items      []automationStatusItem `json:"items"`
	LastScanAt *time.Time             `json:"last_scan_at,omitempty"`
	NextScanAt *time.Time             `json:"next_scan_at,omitempty"`
	ScanCount  int64                  `json:"scan_count"`
}

func (a *App) handleAPIAutomationStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if a == nil || a.requirementAuto == nil {
		httpapi.WriteSuccess(w, http.StatusOK, "ok", automationStatusData{})
		return
	}

	projectID := strings.TrimSpace(firstNonEmpty(r.URL.Query().Get("project_id"), r.URL.Query().Get("projectId")))
	requirementID := strings.TrimSpace(firstNonEmpty(r.URL.Query().Get("requirement_id"), r.URL.Query().Get("requirementId")))
	items, lastScanAt, nextScanAt, scanCount, err := a.requirementAuto.ListAutomationStatus(projectID, requirementID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, http.StatusInternalServerError, err.Error())
		return
	}

	data := automationStatusData{
		Items:     items,
		ScanCount: scanCount,
	}
	if !lastScanAt.IsZero() {
		data.LastScanAt = &lastScanAt
	}
	if !nextScanAt.IsZero() {
		data.NextScanAt = &nextScanAt
	}
	httpapi.WriteSuccess(w, http.StatusOK, "ok", data)
}

func (c *RequirementAutomationCoordinator) ListAutomationStatus(projectID, requirementID string) ([]automationStatusItem, time.Time, time.Time, int64, error) {
	if c == nil || c.app == nil || c.app.requirementSvc == nil {
		return nil, time.Time{}, time.Time{}, 0, nil
	}

	projectID = strings.TrimSpace(projectID)
	requirementID = strings.TrimSpace(requirementID)

	var requirements []Requirement
	if requirementID != "" {
		requirement, err := c.app.requirementSvc.Get(requirementID)
		if err != nil {
			return nil, time.Time{}, time.Time{}, 0, err
		}
		requirements = []Requirement{*requirement}
	} else {
		list, err := c.app.requirementSvc.List(projectID)
		if err != nil {
			return nil, time.Time{}, time.Time{}, 0, err
		}
		requirements = list
	}

	lastScanAt, nextScanAt, scanCount := c.scanStats()
	items := make([]automationStatusItem, 0, len(requirements))
	for _, requirement := range requirements {
		if requirement.ExecutionMode != RequirementExecutionModeAuto {
			continue
		}
		health := c.app.resolveRequirementExecutionHealth(requirement)
		blockedReason := strings.TrimSpace(c.requirementDispatchBlockedReason(requirement.ID))
		sessionID := c.resolveObservedSessionIDForRequirement(requirement)
		if requirement.RetryBudgetExhaustedAt != nil && !requirement.RetryBudgetExhaustedAt.IsZero() {
			blockedReason = automationDispatchBlockedRetryBudgetExhausted
		}
		if requirement.Status == RequirementStatusRunning {
			if sessionID == "" {
				if blockedReason == "" || (requirement.PromptSentAt != nil && !requirement.PromptSentAt.IsZero()) {
					blockedReason = automationDispatchBlockedNoSessionBound
				}
			} else if session, ok := c.app.cliMgr.Get(sessionID); !ok || !isRuntimeSessionRunning(session) {
				snapshot := c.getSessionSnapshot(sessionID)
				if blockedReason == "" ||
					blockedReason == automationDispatchBlockedNoSessionBound ||
					blockedReason == automationDispatchBlockedSessionNotRunning ||
					blockedReason == automationDispatchBlockedWaitingReconnect ||
					(requirement.PromptSentAt != nil && !requirement.PromptSentAt.IsZero()) {
					if !snapshot.NextReconnectAfter.IsZero() && time.Now().Before(snapshot.NextReconnectAfter) {
						blockedReason = automationDispatchBlockedWaitingReconnect
					} else {
						blockedReason = automationDispatchBlockedSessionNotRunning
					}
				}
			} else if requirement.PromptSentAt != nil && !requirement.PromptSentAt.IsZero() {
				blockedReason = automationDispatchBlockedNone
			} else if blockedReason == automationDispatchBlockedNoSessionBound ||
				blockedReason == automationDispatchBlockedSessionNotRunning ||
				blockedReason == automationDispatchBlockedWaitingReconnect {
				blockedReason = automationDispatchBlockedNone
			}
		}

		item := automationStatusItem{
			RequirementID:         requirement.ID,
			RequirementTitle:      requirement.Title,
			ProjectID:             requirement.ProjectID,
			ProjectName:           requirement.ProjectName,
			RequirementStatus:     requirement.Status,
			SessionID:             sessionID,
			DispatchBlockedReason: blockedReason,
			RetryAttempts:         requirement.AutoRetryAttempts,
			RetryBudget:           c.maxRequirementRetryAttempts(),
			RetryBudgetExhausted:  requirement.RetryBudgetExhaustedAt != nil && !requirement.RetryBudgetExhaustedAt.IsZero(),
			ExecutionState:        health.State,
			ExecutionReason:       health.Reason,
			ScanCount:             scanCount,
		}
		if health.LastOutputAt != nil {
			at := *health.LastOutputAt
			item.LastOutputAt = &at
		}
		if !lastScanAt.IsZero() {
			at := lastScanAt
			item.LastScanAt = &at
		}
		if !nextScanAt.IsZero() {
			at := nextScanAt
			item.NextScanAt = &at
		}
		items = append(items, item)
	}
	return items, lastScanAt, nextScanAt, scanCount, nil
}

func (c *RequirementAutomationCoordinator) scanStats() (time.Time, time.Time, int64) {
	if c == nil {
		return time.Time{}, time.Time{}, 0
	}
	c.syncMu.Lock()
	defer c.syncMu.Unlock()
	return c.lastScanAt, c.nextScanAt, c.scanCount
}
