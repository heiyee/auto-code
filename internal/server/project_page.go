package server

import "net/http"

// handleIndex redirects root path to the React workbench.
func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/app/", http.StatusFound)
}

// shutdownProjectCLIRuntimes terminates in-memory CLI runtime sessions bound to one project.
// Persistence records are removed by project deletion.
func (a *App) shutdownProjectCLIRuntimes(projectID string) {
	if a == nil || a.cliSessionSvc == nil || a.cliMgr == nil {
		return
	}
	views, err := a.cliSessionSvc.ListProjectViews(projectID)
	if err != nil {
		return
	}
	events := a.cliMgr.Events()
	for _, view := range views {
		runtimeSession, ok := a.cliMgr.Get(view.ID)
		if !ok {
			continue
		}
		agentID := runtimeSession.AgentID
		_ = a.cliMgr.Destroy(view.ID)
		if events != nil {
			events.ClearSession(view.ID, agentID)
		}
	}
}
