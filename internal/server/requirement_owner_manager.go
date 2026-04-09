package server

import (
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"
)

type requirementSessionOwner struct {
	ProjectID     string
	RequirementID string
	SessionID     string
	AcquiredAt    time.Time
}

func (c *RequirementAutomationCoordinator) ownerSessionForRequirement(requirementID string) string {
	if c == nil {
		return ""
	}
	requirementID = strings.TrimSpace(requirementID)
	if requirementID == "" {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.TrimSpace(c.requirementOwners[requirementID])
}

func (c *RequirementAutomationCoordinator) ownerRequirementForSession(sessionID string) string {
	if c == nil {
		return ""
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.TrimSpace(c.sessionOwners[sessionID])
}

func (c *RequirementAutomationCoordinator) setRequirementOwner(requirement Requirement, sessionID string, at time.Time) {
	if c == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	requirementID := strings.TrimSpace(requirement.ID)
	projectID := strings.TrimSpace(requirement.ProjectID)
	if sessionID == "" || requirementID == "" || projectID == "" {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if previousSessionID := strings.TrimSpace(c.requirementOwners[requirementID]); previousSessionID != "" && previousSessionID != sessionID {
		delete(c.sessionOwners, previousSessionID)
	}
	if previousRequirementID := strings.TrimSpace(c.sessionOwners[sessionID]); previousRequirementID != "" && previousRequirementID != requirementID {
		delete(c.requirementOwners, previousRequirementID)
	}
	if previousOwner, ok := c.projectOwners[projectID]; ok && previousOwner != nil {
		if strings.TrimSpace(previousOwner.RequirementID) != requirementID {
			delete(c.requirementOwners, strings.TrimSpace(previousOwner.RequirementID))
			delete(c.sessionOwners, strings.TrimSpace(previousOwner.SessionID))
		}
	}

	c.requirementOwners[requirementID] = sessionID
	c.sessionOwners[sessionID] = requirementID
	c.projectOwners[projectID] = &requirementSessionOwner{
		ProjectID:     projectID,
		RequirementID: requirementID,
		SessionID:     sessionID,
		AcquiredAt:    at,
	}
}

func (c *RequirementAutomationCoordinator) clearRequirementOwnerByRequirement(requirementID string) {
	if c == nil {
		return
	}
	requirementID = strings.TrimSpace(requirementID)
	if requirementID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clearRequirementOwnerByRequirementLocked(requirementID)
}

func (c *RequirementAutomationCoordinator) clearRequirementOwnerByRequirementLocked(requirementID string) {
	requirementID = strings.TrimSpace(requirementID)
	if requirementID == "" {
		return
	}
	sessionID := strings.TrimSpace(c.requirementOwners[requirementID])
	delete(c.requirementOwners, requirementID)
	if sessionID != "" {
		delete(c.sessionOwners, sessionID)
	}
	for projectID, owner := range c.projectOwners {
		if owner == nil {
			continue
		}
		if strings.TrimSpace(owner.RequirementID) == requirementID {
			delete(c.projectOwners, projectID)
		}
	}
}

func (c *RequirementAutomationCoordinator) clearRequirementOwnerBySession(sessionID string) {
	if c == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clearRequirementOwnerBySessionLocked(sessionID)
}

func (c *RequirementAutomationCoordinator) clearRequirementOwnerBySessionLocked(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	requirementID := strings.TrimSpace(c.sessionOwners[sessionID])
	delete(c.sessionOwners, sessionID)
	if requirementID != "" {
		delete(c.requirementOwners, requirementID)
	}
	for projectID, owner := range c.projectOwners {
		if owner == nil {
			continue
		}
		if strings.TrimSpace(owner.SessionID) == sessionID {
			delete(c.projectOwners, projectID)
		}
	}
}

func (c *RequirementAutomationCoordinator) findRunningRequirementOwnedSession(requirement Requirement, preferredSessionID string) string {
	if c == nil || c.app == nil || c.app.cliSessionSvc == nil || c.app.cliMgr == nil {
		return ""
	}

	preferredSessionID = strings.TrimSpace(preferredSessionID)
	if preferredSessionID != "" {
		if ownerRequirementID := c.ownerRequirementForSession(preferredSessionID); ownerRequirementID == requirement.ID {
			if session, ok := c.app.cliMgr.Get(preferredSessionID); ok && isRuntimeSessionRunning(session) {
				c.setRequirementOwner(requirement, preferredSessionID, time.Now())
				return preferredSessionID
			}
		}
	}

	if ownerSessionID := c.ownerSessionForRequirement(requirement.ID); ownerSessionID != "" {
		if session, ok := c.app.cliMgr.Get(ownerSessionID); ok && isRuntimeSessionRunning(session) {
			c.setRequirementOwner(requirement, ownerSessionID, time.Now())
			return ownerSessionID
		}
		c.clearRequirementOwnerBySession(ownerSessionID)
	}

	if trackedSessionID := c.findTrackedSessionIDForRequirement(requirement.ID); trackedSessionID != "" {
		if session, ok := c.app.cliMgr.Get(trackedSessionID); ok && isRuntimeSessionRunning(session) {
			c.setRequirementOwner(requirement, trackedSessionID, time.Now())
			return trackedSessionID
		}
		c.clearSessionDetectionState(trackedSessionID)
	}

	views, err := c.app.cliSessionSvc.ListRequirementViews(requirement.ID)
	if err != nil {
		return ""
	}
	for _, view := range views {
		if view.State != CLISessionStateRunning {
			continue
		}
		session, ok := c.app.cliMgr.Get(view.ID)
		if !ok || !isRuntimeSessionRunning(session) {
			continue
		}
		c.setRequirementOwner(requirement, view.ID, time.Now())
		return view.ID
	}
	return ""
}

func (c *RequirementAutomationCoordinator) requirementSessionIsActive(requirementID string) bool {
	if c == nil || c.app == nil || c.app.requirementSvc == nil {
		return false
	}
	requirementID = strings.TrimSpace(requirementID)
	if requirementID == "" {
		return false
	}
	requirement, err := c.app.requirementSvc.Get(requirementID)
	if err != nil || requirement == nil {
		return false
	}
	return requirement.Status == RequirementStatusRunning || requirement.Status == RequirementStatusPaused
}

func (c *RequirementAutomationCoordinator) reuseProjectSessionForRequirement(requirement Requirement, preferredSessionID string) string {
	if c == nil || c.app == nil || c.app.cliSessionSvc == nil || c.app.cliMgr == nil {
		return ""
	}

	projectID := strings.TrimSpace(requirement.ProjectID)
	requiredCLIType := strings.TrimSpace(strings.ToLower(requirement.CLIType))
	if projectID == "" || requiredCLIType == "" {
		return ""
	}

	views, err := c.app.cliSessionSvc.ListProjectViews(projectID)
	if err != nil {
		return ""
	}
	preferredSessionID = strings.TrimSpace(preferredSessionID)
	sort.SliceStable(views, func(i, j int) bool {
		leftID := strings.TrimSpace(views[i].ID)
		rightID := strings.TrimSpace(views[j].ID)
		leftPreferred := preferredSessionID != "" && leftID == preferredSessionID
		rightPreferred := preferredSessionID != "" && rightID == preferredSessionID
		if leftPreferred != rightPreferred {
			return leftPreferred
		}
		leftUnbound := strings.TrimSpace(views[i].RequirementID) == ""
		rightUnbound := strings.TrimSpace(views[j].RequirementID) == ""
		if leftUnbound != rightUnbound {
			return leftUnbound
		}
		if !views[i].LastActiveAt.Equal(views[j].LastActiveAt) {
			return views[i].LastActiveAt.After(views[j].LastActiveAt)
		}
		return views[i].CreatedAt.After(views[j].CreatedAt)
	})

	for _, view := range views {
		sessionID := strings.TrimSpace(view.ID)
		if sessionID == "" {
			continue
		}
		if strings.TrimSpace(view.ProjectID) != "" && strings.TrimSpace(view.ProjectID) != projectID {
			continue
		}
		if strings.TrimSpace(strings.ToLower(view.CLIType)) != requiredCLIType {
			continue
		}
		session, ok := c.app.cliMgr.Get(sessionID)
		if !ok || !isRuntimeSessionRunning(session) {
			continue
		}
		if ownerRequirementID := c.ownerRequirementForSession(sessionID); ownerRequirementID != "" && ownerRequirementID != requirement.ID {
			if c.requirementSessionIsActive(ownerRequirementID) {
				continue
			}
		}
		if boundRequirementID := strings.TrimSpace(view.RequirementID); boundRequirementID != "" && boundRequirementID != requirement.ID {
			if c.requirementSessionIsActive(boundRequirementID) {
				continue
			}
		}
		if _, err := c.app.cliSessionSvc.RebindSessionToRequirement(sessionID, requirement.ID); err != nil {
			requirementAutomationLogger().Warn(
				"reuse project session for requirement failed",
				zap.String("project_id", requirement.ProjectID),
				zap.String("requirement_id", requirement.ID),
				zap.String("session_id", sessionID),
				zap.Error(err),
			)
			continue
		}
		c.setRequirementOwner(requirement, sessionID, time.Now())
		return sessionID
	}
	return ""
}

func (c *RequirementAutomationCoordinator) cleanupObsoleteRequirementSessions(requirement Requirement, keepSessionID string) {
	if c == nil || c.app == nil {
		return
	}
	c.app.retireProjectSessions(requirement.ProjectID, keepSessionID)
}

func (c *RequirementAutomationCoordinator) preemptProjectOwner(requirement Requirement) {
	if c == nil || c.app == nil {
		return
	}
	projectID := strings.TrimSpace(requirement.ProjectID)
	if projectID == "" {
		return
	}

	var sessionID string
	var requirementID string
	c.mu.Lock()
	if owner, ok := c.projectOwners[projectID]; ok && owner != nil {
		if strings.TrimSpace(owner.RequirementID) != "" && strings.TrimSpace(owner.RequirementID) != requirement.ID {
			sessionID = strings.TrimSpace(owner.SessionID)
			requirementID = strings.TrimSpace(owner.RequirementID)
			delete(c.projectOwners, projectID)
			if requirementID != "" {
				delete(c.requirementOwners, requirementID)
				state := c.ensureRequirementStateLocked(requirementID)
				state.dispatchBlockedReason = automationDispatchBlockedNoSessionBound
			}
			if sessionID != "" {
				delete(c.sessionOwners, sessionID)
			}
		}
	}
	c.mu.Unlock()

	if sessionID == "" {
		return
	}
	c.clearSessionDetectionState(sessionID)
	_ = c.resetRequirementSession(sessionID)
}

func (c *RequirementAutomationCoordinator) ensureOwnedRequirementSession(requirement Requirement, preferredSessionID string) (string, bool, string) {
	if c == nil || c.app == nil || c.app.cliSessionSvc == nil {
		return "", false, automationDispatchBlockedNoSessionBound
	}

	if sessionID := c.findRunningRequirementOwnedSession(requirement, preferredSessionID); sessionID != "" {
		return sessionID, false, automationDispatchBlockedNone
	}

	cliType := strings.TrimSpace(requirement.CLIType)
	if cliType == "" {
		return "", false, automationDispatchBlockedNoSessionBound
	}
	if sessionID := c.reuseProjectSessionForRequirement(requirement, preferredSessionID); sessionID != "" {
		c.cleanupObsoleteRequirementSessions(requirement, sessionID)
		return sessionID, false, automationDispatchBlockedNone
	}

	c.preemptProjectOwner(requirement)
	view, err := c.app.cliSessionSvc.CreateRequirementBoundSession(requirement.ID, cliType, "", "")
	if err != nil {
		return "", false, automationDispatchBlockedNoSessionBound
	}
	c.setRequirementOwner(requirement, view.ID, time.Now())
	c.cleanupObsoleteRequirementSessions(requirement, view.ID)
	return view.ID, false, automationDispatchBlockedNone
}
