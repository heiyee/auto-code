package server

import "strings"

func (a *App) latestRequirementWatchdogEvent(requirementID string) *RequirementWatchdogEvent {
	if a == nil || a.store == nil {
		return nil
	}
	requirementID = strings.TrimSpace(requirementID)
	if requirementID == "" {
		return nil
	}
	event, err := a.store.GetLatestRequirementWatchdogEvent(requirementID)
	if err != nil {
		return nil
	}
	return event
}

func (a *App) latestRequirementWatchdogEventByStatus(requirementID, status string) *RequirementWatchdogEvent {
	if a == nil || a.store == nil {
		return nil
	}
	requirementID = strings.TrimSpace(requirementID)
	status = strings.TrimSpace(status)
	if requirementID == "" || status == "" {
		return nil
	}
	event, err := a.store.GetLatestRequirementWatchdogEventByStatus(requirementID, status)
	if err != nil {
		return nil
	}
	return event
}
