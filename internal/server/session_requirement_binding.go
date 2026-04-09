package server

import "strings"

type sessionRequirementBindingResolver struct {
	app                    *App
	projectActiveByID      map[string]*Requirement
	requirementSessionByID map[string]string
}

func newSessionRequirementBindingResolver(app *App) *sessionRequirementBindingResolver {
	return &sessionRequirementBindingResolver{
		app:                    app,
		projectActiveByID:      make(map[string]*Requirement),
		requirementSessionByID: make(map[string]string),
	}
}

func (a *App) decorateSessionRequirementBinding(view CLISessionView) CLISessionView {
	return newSessionRequirementBindingResolver(a).decorate(view)
}

func (a *App) decorateSessionRequirementBindings(views []CLISessionView) []CLISessionView {
	if len(views) == 0 {
		return views
	}
	resolver := newSessionRequirementBindingResolver(a)
	items := make([]CLISessionView, 0, len(views))
	for _, view := range views {
		items = append(items, resolver.decorate(view))
	}
	return items
}

func (r *sessionRequirementBindingResolver) decorate(view CLISessionView) CLISessionView {
	if r == nil || r.app == nil || r.app.requirementSvc == nil {
		return view
	}
	requirementID := strings.TrimSpace(view.RequirementID)
	if requirementID != "" {
		if strings.TrimSpace(view.RequirementTitle) == "" || strings.TrimSpace(view.ProjectID) == "" || strings.TrimSpace(view.ProjectName) == "" {
			if requirement, err := r.app.requirementSvc.Get(requirementID); err == nil && requirement != nil {
				if strings.TrimSpace(view.RequirementTitle) == "" {
					view.RequirementTitle = requirement.Title
				}
				if strings.TrimSpace(view.ProjectID) == "" {
					view.ProjectID = requirement.ProjectID
				}
				if strings.TrimSpace(view.ProjectName) == "" {
					view.ProjectName = requirement.ProjectName
				}
			}
		}
		return view
	}

	requirement, ok := r.resolve(view)
	if !ok {
		return view
	}
	view.RequirementID = requirement.ID
	view.RequirementTitle = requirement.Title
	if strings.TrimSpace(view.ProjectID) == "" {
		view.ProjectID = requirement.ProjectID
	}
	if strings.TrimSpace(view.ProjectName) == "" {
		view.ProjectName = requirement.ProjectName
	}
	return view
}

func (r *sessionRequirementBindingResolver) resolve(view CLISessionView) (Requirement, bool) {
	if r == nil || r.app == nil || r.app.requirementSvc == nil {
		return Requirement{}, false
	}
	if r.app.requirementAuto != nil {
		if health, ok := r.app.requirementAuto.GetSessionExecutionHealth(view.ID); ok {
			if requirement, err := r.app.requirementSvc.Get(health.RequirementID); err == nil && requirement != nil {
				return *requirement, true
			}
		}
	}

	projectID := strings.TrimSpace(view.ProjectID)
	if projectID == "" {
		return Requirement{}, false
	}
	active := r.activeRequirement(projectID)
	if active == nil {
		return Requirement{}, false
	}
	if cliType := strings.TrimSpace(active.CLIType); cliType != "" && !strings.EqualFold(cliType, strings.TrimSpace(view.CLIType)) {
		return Requirement{}, false
	}
	if r.app.requirementAuto == nil {
		return Requirement{}, false
	}
	sessionID := r.requirementSessionID(*active)
	if sessionID == "" || sessionID != strings.TrimSpace(view.ID) {
		return Requirement{}, false
	}
	return *active, true
}

func (r *sessionRequirementBindingResolver) activeRequirement(projectID string) *Requirement {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil
	}
	if requirement, ok := r.projectActiveByID[projectID]; ok {
		return requirement
	}
	requirements, err := r.app.requirementSvc.ListByProject(projectID)
	if err != nil {
		r.projectActiveByID[projectID] = nil
		return nil
	}
	active := pickActiveRequirement(sortRequirementsForAutomation(requirements))
	if active == nil || (active.Status != RequirementStatusRunning && active.Status != RequirementStatusPaused) {
		r.projectActiveByID[projectID] = nil
		return nil
	}
	r.projectActiveByID[projectID] = active
	return active
}

func (r *sessionRequirementBindingResolver) requirementSessionID(requirement Requirement) string {
	requirementID := strings.TrimSpace(requirement.ID)
	if requirementID == "" {
		return ""
	}
	if sessionID, ok := r.requirementSessionByID[requirementID]; ok {
		return sessionID
	}
	sessionID := strings.TrimSpace(r.app.requirementAuto.resolveTrackedRequirementSessionID(requirement, ""))
	r.requirementSessionByID[requirementID] = sessionID
	return sessionID
}
