package server

import (
	"auto-code/internal/httpapi"
	"net/http"
)

// handleAPICLIProfiles returns profile definitions for frontend dynamic profile selector.
func (a *App) handleAPICLIProfiles(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	profiles := a.cliSessionSvc.Profiles()
	types := a.cliSessionSvc.Types()

	result := make([]apiCLIProfileGroup, 0, len(types))
	for _, cliType := range types {
		list := profiles[cliType]
		items := make([]apiCLIProfileItem, 0, len(list))
		for _, profile := range list {
			items = append(items, apiCLIProfileItem{
				ID:          profile.ID,
				Name:        profile.Name,
				Description: profile.Description,
			})
		}
		result = append(result, apiCLIProfileGroup{
			CLIType:        cliType,
			DefaultProfile: a.cliSessionSvc.DefaultProfileID(cliType),
			Profiles:       items,
		})
	}
	httpapi.WriteSuccess(w, http.StatusOK, "ok", result)
}
