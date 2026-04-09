package server

import (
	"fmt"
	"strings"
	"time"
)

const (
	cliRebuildRequirementPrefix = "之前的 CLI 会话已经断开。请基于当前仓库真实状态继续以下需求，不要重复已完成步骤。"
)

func (a *App) rehydrateRebuiltSession(sessionID, budgetReason string) (bool, error) {
	if a == nil || a.cliMgr == nil {
		return false, nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false, nil
	}
	session, ok := a.cliMgr.Get(sessionID)
	if !ok || !isRuntimeSessionRunning(session) {
		return false, nil
	}

	view, err := a.cliSessionSvc.GetView(sessionID)
	if err != nil || view == nil {
		return false, nil
	}

	recoveryPrompt, requirement := a.buildRebuildRecoveryPrompt(*view)
	if strings.TrimSpace(recoveryPrompt) == "" {
		return false, nil
	}
	dispatchedAt := time.Now()
	if requirement != nil && a.requirementSvc != nil && strings.TrimSpace(budgetReason) != "" {
		attempts, exhausted, err := a.requirementSvc.ConsumeRetryBudget(requirement.ID, a.requirementAuto.maxRequirementRetryAttempts(), budgetReason, dispatchedAt)
		if err != nil {
			return false, err
		}
		if exhausted {
			if a.requirementAuto != nil {
				if failErr := a.requirementAuto.failRequirementForRetryBudget(*requirement, sessionID, automationRetryReasonBudgetExhausted); failErr != nil {
					return false, failErr
				}
			}
			return false, fmt.Errorf("requirement retry budget exhausted after %d attempts", attempts)
		}
	}
	if err := prepareSessionForInput(session, 12*time.Second, true); err != nil {
		return false, err
	}
	payload := recoveryPrompt
	if !strings.HasSuffix(payload, "\n") {
		payload += "\n"
	}
	if a.requirementAuto != nil {
		a.requirementAuto.TrackOutboundInput(sessionID, payload)
		if requirement != nil {
			trackingPrompt := buildRequirementDispatchPrompt(*requirement)
			if a.requirementSvc != nil {
				_ = a.requirementSvc.MarkPromptReplayed(requirement.ID, dispatchedAt)
			}
			a.requirementAuto.setRequirementOwner(*requirement, sessionID, dispatchedAt)
			a.requirementAuto.bindRequirementSession(
				sessionID,
				requirement.ID,
				trackingPrompt,
				dispatchedAt,
				true,
			)
			a.requirementAuto.setRequirementDispatchBlockedReason(requirement.ID, automationDispatchBlockedNone)
		}
	}
	if err := session.WriteInput(payload); err != nil {
		return false, err
	}
	a.cliSessionSvc.Touch(sessionID)
	return true, nil
}

func (a *App) buildRebuildRecoveryPrompt(view CLISessionView) (string, *Requirement) {
	if a == nil {
		return "", nil
	}

	requirement := a.resolveSessionRecoveryRequirement(view)
	if requirement == nil {
		return "", nil
	}
	prompt := buildRequirementDispatchPrompt(*requirement)
	if strings.TrimSpace(prompt) == "" {
		return "", requirement
	}

	lines := make([]string, 0, 6)
	lines = append(lines, cliRebuildRequirementPrefix)

	if strings.TrimSpace(view.WorkDir) != "" {
		lines = append(lines, "工作目录："+strings.TrimSpace(view.WorkDir))
	}
	lines = append(lines, "")
	lines = append(lines, prompt)

	return strings.Join(lines, "\n"), requirement
}

func (a *App) resolveSessionRecoveryRequirement(view CLISessionView) *Requirement {
	if a == nil || a.requirementSvc == nil {
		return nil
	}
	if strings.TrimSpace(view.RequirementID) != "" {
		requirement, err := a.requirementSvc.Get(view.RequirementID)
		if err == nil && requirement != nil && requirement.Status == RequirementStatusRunning && requirement.PromptSentAt != nil && !requirement.PromptSentAt.IsZero() {
			return requirement
		}
	}
	projectID := strings.TrimSpace(view.ProjectID)
	if projectID == "" {
		return nil
	}
	requirements, err := a.requirementSvc.ListByProject(projectID)
	if err != nil {
		return nil
	}
	for _, item := range sortRequirementsForAutomation(requirements) {
		if item.Status == RequirementStatusRunning && item.PromptSentAt != nil && !item.PromptSentAt.IsZero() {
			candidate := item
			return &candidate
		}
	}
	return nil
}
