package server

import (
	"auto-code/internal/httpapi"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type apiProjectCLIProviderItem struct {
	CLIType                  string                     `json:"cli_type"`
	DefaultProfile           string                     `json:"default_profile"`
	SupportsMultipleAccounts bool                       `json:"supports_multiple_accounts"`
	Profiles                 []apiProjectCLIProfileItem `json:"profiles"`
}

type apiProjectCLIProfileItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type apiProjectCLIProvidersData struct {
	Providers []apiProjectCLIProviderItem `json:"providers"`
}

type apiProjectCLISessionCreateData struct {
	SessionID string `json:"session_id"`
	AgentID   string `json:"agentid"`
	CLIType   string `json:"cli_type"`
	Profile   string `json:"profile"`
	Reused    bool   `json:"reused"`
	WorkDir   string `json:"work_dir"`
}

type apiProjectCLISessionItem struct {
	SessionID       string     `json:"session_id"`
	CLIType         string     `json:"cli_type"`
	Profile         string     `json:"profile"`
	ProfileName     string     `json:"profile_name"`
	State           string     `json:"state"`
	ProjectID       string     `json:"project_id"`
	ProjectName     string     `json:"project_name"`
	RequirementID   string     `json:"requirement_id"`
	LastActiveAt    int64      `json:"last_active_at"`
	ExecutionState  string     `json:"execution_state,omitempty"`
	ExecutionReason string     `json:"execution_reason,omitempty"`
	LastOutputAt    *time.Time `json:"last_output_at,omitempty"`
}

type apiProjectCLISessionsData struct {
	Sessions []apiProjectCLISessionItem `json:"sessions"`
}

type projectCLISessionCreateRequest struct {
	CLIType string `json:"cli_type"`
	Profile string `json:"profile"`
	Account string `json:"account"`
	Command string `json:"command"`
	Cols    int    `json:"cols"`
	Rows    int    `json:"rows"`
}

func (r *projectCLISessionCreateRequest) normalize() {
	if r == nil {
		return
	}
	r.CLIType = strings.TrimSpace(strings.ToLower(r.CLIType))
	r.Profile = strings.TrimSpace(r.Profile)
	r.Account = strings.TrimSpace(r.Account)
	if r.Profile == "" {
		r.Profile = r.Account
	}
	r.Command = strings.TrimSpace(r.Command)
	if cols, rows, ok := normalizeRequestedTerminalSize(r.Cols, r.Rows); ok {
		r.Cols = cols
		r.Rows = rows
	} else {
		r.Cols = 0
		r.Rows = 0
	}
}

func parseProjectCLISessionCreateRequest(r *http.Request) (projectCLISessionCreateRequest, error) {
	req := projectCLISessionCreateRequest{}
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return projectCLISessionCreateRequest{}, errors.New("invalid json body")
		}
		req.normalize()
		return req, nil
	}
	if err := r.ParseForm(); err != nil {
		return projectCLISessionCreateRequest{}, errors.New("invalid form body")
	}
	req.CLIType = r.FormValue("cli_type")
	req.Profile = r.FormValue("profile")
	req.Account = r.FormValue("account")
	req.Command = r.FormValue("command")
	req.Cols, _ = strconv.Atoi(strings.TrimSpace(r.FormValue("cols")))
	req.Rows, _ = strconv.Atoi(strings.TrimSpace(r.FormValue("rows")))
	req.normalize()
	return req, nil
}

func (a *App) handleAPIProjectCLI(w http.ResponseWriter, r *http.Request, projectID string, segments []string) {
	if len(segments) != 2 {
		httpapi.WriteError(w, http.StatusNotFound, http.StatusNotFound, "resource not found")
		return
	}
	switch segments[1] {
	case "providers":
		a.handleAPIProjectCLIProviders(w, r, projectID)
	case "sessions":
		a.handleAPIProjectCLISessions(w, r, projectID)
	default:
		httpapi.WriteError(w, http.StatusNotFound, http.StatusNotFound, "resource not found")
	}
}

func (a *App) handleAPIProjectCLIProviders(w http.ResponseWriter, r *http.Request, projectID string) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if _, err := a.projectSvc.Get(projectID); err != nil {
		writeProjectFileError(w, err)
		return
	}
	types := a.cliSessionSvc.Types()
	profilesByType := a.cliSessionSvc.Profiles()
	providers := make([]apiProjectCLIProviderItem, 0, len(types))
	for _, cliType := range types {
		normalized := strings.TrimSpace(strings.ToLower(cliType))
		if normalized == "" {
			continue
		}
		profiles := make([]apiProjectCLIProfileItem, 0)
		for _, item := range profilesByType[normalized] {
			id := strings.TrimSpace(item.ID)
			if id == "" {
				continue
			}
			name := strings.TrimSpace(item.Name)
			if name == "" {
				name = id
			}
			profiles = append(profiles, apiProjectCLIProfileItem{
				ID:          id,
				Name:        name,
				Description: strings.TrimSpace(item.Description),
			})
		}
		providers = append(providers, apiProjectCLIProviderItem{
			CLIType:                  normalized,
			DefaultProfile:           strings.TrimSpace(a.cliSessionSvc.DefaultProfileID(normalized)),
			SupportsMultipleAccounts: a.cliSessionSvc.SupportsMultipleAccounts(normalized),
			Profiles:                 profiles,
		})
	}
	httpapi.WriteSuccess(w, http.StatusOK, "ok", apiProjectCLIProvidersData{
		Providers: providers,
	})
}

func (a *App) handleAPIProjectCLISessions(w http.ResponseWriter, r *http.Request, projectID string) {
	switch r.Method {
	case http.MethodGet:
		a.handleAPIProjectCLISessionList(w, r, projectID)
	case http.MethodPost:
		a.handleAPIProjectCLISessionEnsure(w, r, projectID)
	default:
		httpapi.WriteError(w, http.StatusMethodNotAllowed, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *App) handleAPIProjectCLISessionList(w http.ResponseWriter, r *http.Request, projectID string) {
	if _, err := a.projectSvc.Get(projectID); err != nil {
		writeProjectFileError(w, err)
		return
	}
	filterCLIType := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("cli_type")))
	views, err := a.cliSessionSvc.ListProjectViews(projectID)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	items := make([]apiProjectCLISessionItem, 0, len(views))
	for _, item := range views {
		cliType := strings.TrimSpace(strings.ToLower(item.CLIType))
		if filterCLIType != "" && filterCLIType != cliType {
			continue
		}
		health := a.resolveSessionExecutionHealth(item)
		items = append(items, apiProjectCLISessionItem{
			SessionID:       item.ID,
			CLIType:         item.CLIType,
			Profile:         item.Profile,
			ProfileName:     item.ProfileName,
			State:           item.State,
			ProjectID:       item.ProjectID,
			ProjectName:     item.ProjectName,
			RequirementID:   item.RequirementID,
			LastActiveAt:    item.LastActiveAt.UnixMilli(),
			ExecutionState:  health.State,
			ExecutionReason: health.Reason,
			LastOutputAt:    health.LastOutputAt,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].LastActiveAt > items[j].LastActiveAt
	})
	httpapi.WriteSuccess(w, http.StatusOK, "ok", apiProjectCLISessionsData{Sessions: items})
}

func (a *App) handleAPIProjectCLISessionEnsure(w http.ResponseWriter, r *http.Request, projectID string) {
	req, err := parseProjectCLISessionCreateRequest(r)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	if req.CLIType == "" {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, "cli_type is required")
		return
	}
	profileID := strings.TrimSpace(req.Profile)
	if profileID == "" && !a.cliSessionSvc.SupportsMultipleAccounts(req.CLIType) {
		profileID = strings.TrimSpace(a.cliSessionSvc.DefaultProfileID(req.CLIType))
	}
	if profileID == "" {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, "profile is required")
		return
	}

	view, reused, err := a.cliSessionSvc.EnsureProjectSessionWithSize(projectID, req.CLIType, profileID, req.Command, req.Cols, req.Rows)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	a.retireProjectSessions(projectID, view.ID)
	a.applyRequestedTerminalSize(view.ID, req.Cols, req.Rows)
	statusCode := http.StatusCreated
	message := "会话已启动"
	if reused {
		statusCode = http.StatusOK
		message = "已复用项目关联会话"
	}
	httpapi.WriteSuccess(w, statusCode, message, apiProjectCLISessionCreateData{
		SessionID: view.ID,
		AgentID:   view.AgentID,
		CLIType:   view.CLIType,
		Profile:   view.Profile,
		Reused:    reused,
		WorkDir:   view.WorkDir,
	})
}
