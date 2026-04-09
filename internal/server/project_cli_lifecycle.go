package server

import (
	"strings"
)

func (a *App) retireProjectSessions(projectID, keepSessionID string) {
	if a == nil || a.cliSessionSvc == nil || a.cliMgr == nil {
		return
	}
	projectID = strings.TrimSpace(projectID)
	keepSessionID = strings.TrimSpace(keepSessionID)
	if projectID == "" {
		return
	}

	views, err := a.cliSessionSvc.ListProjectViews(projectID)
	if err != nil {
		return
	}
	for _, view := range views {
		sessionID := strings.TrimSpace(view.ID)
		if sessionID == "" || sessionID == keepSessionID {
			continue
		}
		if a.requirementAuto != nil {
			_ = a.requirementAuto.resetRequirementSession(sessionID)
			continue
		}

		agentID := ""
		if runtimeSession, ok := a.cliMgr.Get(sessionID); ok {
			agentID = strings.TrimSpace(runtimeSession.AgentID)
			_ = a.cliMgr.Destroy(sessionID)
		}
		if events := a.cliMgr.Events(); events != nil {
			events.ClearSession(sessionID, agentID)
		}
		_ = a.cliSessionSvc.MarkTerminated(sessionID)
		if a.cliArchive != nil {
			_ = a.cliArchive.DeleteSession(sessionID)
		}
	}
}
