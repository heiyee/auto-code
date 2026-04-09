package server

import (
	"auto-code/internal/httpapi"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// handleAPIRequirementList returns filtered requirement list with pagination metadata.
func (a *App) handleAPIRequirementList(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectID := strings.TrimSpace(r.URL.Query().Get("project"))
	status := normalizeRequirementStatusFilter(r.URL.Query().Get("status"))
	keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))
	page, pageSize := parsePageQuery(r.URL.Query().Get("page"), r.URL.Query().Get("page_size"))

	requirements, err := a.requirementSvc.List(projectID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, http.StatusInternalServerError, err.Error())
		return
	}
	projects, err := a.projectSvc.List()
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, http.StatusInternalServerError, err.Error())
		return
	}

	filteredRequirements := filterRequirements(requirements, status, keyword)
	pageItems, paging := a.paginateRequirements(filteredRequirements, page, pageSize)
	projectOptions := make([]apiProjectSummary, 0, len(projects))
	for _, item := range projects {
		projectOptions = append(projectOptions, toAPIProjectSummary(item))
	}

	data := apiRequirementListData{
		Items:    pageItems,
		Projects: projectOptions,
		Filters: apiRequirementListFilters{
			Project: projectID,
			Status:  status,
			Keyword: keyword,
		},
		Pagination: paging,
	}
	httpapi.WriteSuccess(w, http.StatusOK, "ok", data)
}

// handleAPIRequirementCreate creates one requirement from async request payload.
func (a *App) handleAPIRequirementCreate(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	input, err := parseRequirementMutationRequest(r)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	requirement, err := a.requirementSvc.Create(input)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	if a.requirementAuto != nil {
		a.requirementAuto.SyncProject(requirement.ProjectID, "")
		if latest, latestErr := a.requirementSvc.Get(requirement.ID); latestErr == nil {
			requirement = latest
		}
	}
	httpapi.WriteSuccess(w, http.StatusCreated, "需求已创建", apiRequirementCreateData{
		Item: a.toAPIRequirement(*requirement),
	})
}

// handleAPIRequirementDelete deletes one requirement by id.
func (a *App) handleAPIRequirementDelete(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodDelete) {
		return
	}
	requirementID, action, ok := parseSubRoute("/api/requirements/delete/", r.URL.Path)
	if !ok || action != "" {
		httpapi.WriteError(w, http.StatusNotFound, http.StatusNotFound, "resource not found")
		return
	}
	requirement, _ := a.requirementSvc.Get(requirementID)
	if err := a.requirementSvc.Delete(requirementID); err != nil {
		if errors.Is(err, ErrNotFound) {
			httpapi.WriteError(w, http.StatusNotFound, http.StatusNotFound, "requirement not found")
			return
		}
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	if requirement != nil && a.requirementAuto != nil {
		a.requirementAuto.SyncProject(requirement.ProjectID, "")
	}
	httpapi.WriteSuccess(w, http.StatusOK, "需求已删除", apiDeleteMessageData{})
}

type requirementMutationRequest struct {
	ProjectID                string `json:"project_id"`
	Title                    string `json:"title"`
	Description              string `json:"description"`
	ExecutionMode            string `json:"execution_mode"`
	CLIType                  string `json:"cli_type"`
	AutoClearSession         bool   `json:"auto_clear_session"`
	NoResponseTimeoutMinutes int    `json:"no_response_timeout_minutes"`
	NoResponseErrorAction    string `json:"no_response_error_action"`
	NoResponseIdleAction     string `json:"no_response_idle_action"`
	RequiresDesignReview     bool   `json:"requires_design_review"`
	RequiresCodeReview       bool   `json:"requires_code_review"`
	RequiresAcceptanceReview bool   `json:"requires_acceptance_review"`
	RequiresReleaseApproval  bool   `json:"requires_release_approval"`
}

// parseRequirementMutationRequest decodes requirement mutation payload from JSON or form body.
func parseRequirementMutationRequest(r *http.Request) (RequirementMutation, error) {
	req := requirementMutationRequest{}
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return RequirementMutation{}, errors.New("invalid json body")
		}
	} else {
		if err := r.ParseForm(); err != nil {
			return RequirementMutation{}, errors.New("invalid form body")
		}
		req.ProjectID = r.FormValue("project_id")
		req.Title = r.FormValue("title")
		req.Description = r.FormValue("description")
		req.ExecutionMode = r.FormValue("execution_mode")
		req.CLIType = r.FormValue("cli_type")
		req.AutoClearSession = r.FormValue("auto_clear_session") == "1" || r.FormValue("auto_clear_session") == "true" || r.FormValue("auto_clear_session") == "on"
		req.NoResponseTimeoutMinutes = parseIntWithDefault(r.FormValue("no_response_timeout_minutes"), 0)
		req.NoResponseErrorAction = r.FormValue("no_response_error_action")
		req.NoResponseIdleAction = r.FormValue("no_response_idle_action")
		req.RequiresDesignReview = r.FormValue("requires_design_review") == "1" || r.FormValue("requires_design_review") == "true" || r.FormValue("requires_design_review") == "on"
		req.RequiresCodeReview = r.FormValue("requires_code_review") == "1" || r.FormValue("requires_code_review") == "true" || r.FormValue("requires_code_review") == "on"
		req.RequiresAcceptanceReview = r.FormValue("requires_acceptance_review") == "1" || r.FormValue("requires_acceptance_review") == "true" || r.FormValue("requires_acceptance_review") == "on"
		req.RequiresReleaseApproval = r.FormValue("requires_release_approval") == "1" || r.FormValue("requires_release_approval") == "true" || r.FormValue("requires_release_approval") == "on"
	}
	return RequirementMutation{
		ProjectID:                req.ProjectID,
		Title:                    req.Title,
		Description:              req.Description,
		ExecutionMode:            req.ExecutionMode,
		CLIType:                  req.CLIType,
		AutoClearSession:         req.AutoClearSession,
		NoResponseTimeoutMinutes: req.NoResponseTimeoutMinutes,
		NoResponseErrorAction:    req.NoResponseErrorAction,
		NoResponseIdleAction:     req.NoResponseIdleAction,
		RequiresDesignReview:     req.RequiresDesignReview,
		RequiresCodeReview:       req.RequiresCodeReview,
		RequiresAcceptanceReview: req.RequiresAcceptanceReview,
		RequiresReleaseApproval:  req.RequiresReleaseApproval,
	}, nil
}

type apiRequirementWatchdogEvent struct {
	TriggerKind   string     `json:"trigger_kind"`
	TriggerReason string     `json:"trigger_reason"`
	Action        string     `json:"action"`
	Status        string     `json:"status"`
	Detail        string     `json:"detail,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
}

// apiRequirement is the JSON model returned by requirement APIs.
type apiRequirement struct {
	ID                       string                       `json:"id"`
	ProjectID                string                       `json:"project_id"`
	ProjectName              string                       `json:"project_name"`
	ProjectBranch            string                       `json:"project_branch"`
	ProjectWorkDir           string                       `json:"project_work_dir"`
	SortOrder                int                          `json:"sort_order"`
	Title                    string                       `json:"title"`
	Description              string                       `json:"description"`
	Status                   string                       `json:"status"`
	ExecutionMode            string                       `json:"execution_mode"`
	CLIType                  string                       `json:"cli_type"`
	AutoClearSession         bool                         `json:"auto_clear_session"`
	NoResponseTimeoutMinutes int                          `json:"no_response_timeout_minutes"`
	NoResponseErrorAction    string                       `json:"no_response_error_action"`
	NoResponseIdleAction     string                       `json:"no_response_idle_action"`
	RequiresDesignReview     bool                         `json:"requires_design_review"`
	RequiresCodeReview       bool                         `json:"requires_code_review"`
	RequiresAcceptanceReview bool                         `json:"requires_acceptance_review"`
	RequiresReleaseApproval  bool                         `json:"requires_release_approval"`
	CreatedAt                time.Time                    `json:"created_at"`
	StartedAt                *time.Time                   `json:"started_at"`
	EndedAt                  *time.Time                   `json:"ended_at"`
	PromptSentAt             *time.Time                   `json:"prompt_sent_at"`
	PromptReplayedAt         *time.Time                   `json:"prompt_replayed_at,omitempty"`
	ExecutionState           string                       `json:"execution_state,omitempty"`
	ExecutionReason          string                       `json:"execution_reason,omitempty"`
	LastOutputAt             *time.Time                   `json:"last_output_at,omitempty"`
	LastWatchdogEvent        *apiRequirementWatchdogEvent `json:"last_watchdog_event,omitempty"`
	UpdatedAt                time.Time                    `json:"updated_at"`
}

// apiRequirementListFilters describes query filters in requirement list responses.
type apiRequirementListFilters struct {
	Project string `json:"project"`
	Status  string `json:"status"`
	Keyword string `json:"keyword"`
}

// apiRequirementListData is the requirement list response data payload.
type apiRequirementListData struct {
	Items      []apiRequirement          `json:"items"`
	Projects   []apiProjectSummary       `json:"projects"`
	Filters    apiRequirementListFilters `json:"filters"`
	Pagination apiPagination             `json:"pagination"`
}

// apiRequirementCreateData is the requirement create response data payload.
type apiRequirementCreateData struct {
	Item apiRequirement `json:"item"`
}

// toAPIRequirement converts one domain requirement into API JSON model.
func (a *App) toAPIRequirement(requirement Requirement) apiRequirement {
	health := a.resolveRequirementExecutionHealth(requirement)
	watchdogEvent := a.latestRequirementWatchdogEvent(requirement.ID)
	var latestWatchdog *apiRequirementWatchdogEvent
	if watchdogEvent != nil {
		latestWatchdog = &apiRequirementWatchdogEvent{
			TriggerKind:   watchdogEvent.TriggerKind,
			TriggerReason: watchdogEvent.TriggerReason,
			Action:        watchdogEvent.Action,
			Status:        watchdogEvent.Status,
			Detail:        watchdogEvent.Detail,
			CreatedAt:     watchdogEvent.CreatedAt,
			FinishedAt:    watchdogEvent.FinishedAt,
		}
	}
	return apiRequirement{
		ID:                       requirement.ID,
		ProjectID:                requirement.ProjectID,
		ProjectName:              requirement.ProjectName,
		ProjectBranch:            requirement.ProjectBranch,
		ProjectWorkDir:           requirement.ProjectWorkDir,
		SortOrder:                requirement.SortOrder,
		Title:                    requirement.Title,
		Description:              requirement.Description,
		Status:                   requirement.Status,
		ExecutionMode:            requirement.ExecutionMode,
		CLIType:                  requirement.CLIType,
		AutoClearSession:         requirement.AutoClearSession,
		NoResponseTimeoutMinutes: requirement.NoResponseTimeoutMinutes,
		NoResponseErrorAction:    requirement.NoResponseErrorAction,
		NoResponseIdleAction:     requirement.NoResponseIdleAction,
		RequiresDesignReview:     requirement.RequiresDesignReview,
		RequiresCodeReview:       requirement.RequiresCodeReview,
		RequiresAcceptanceReview: requirement.RequiresAcceptanceReview,
		RequiresReleaseApproval:  requirement.RequiresReleaseApproval,
		CreatedAt:                requirement.CreatedAt,
		StartedAt:                requirement.StartedAt,
		EndedAt:                  requirement.EndedAt,
		PromptSentAt:             requirement.PromptSentAt,
		PromptReplayedAt:         requirement.PromptReplayedAt,
		ExecutionState:           health.State,
		ExecutionReason:          health.Reason,
		LastOutputAt:             health.LastOutputAt,
		LastWatchdogEvent:        latestWatchdog,
		UpdatedAt:                requirement.UpdatedAt,
	}
}

// paginateRequirements slices requirement list and returns API models with pagination metadata.
func (a *App) paginateRequirements(items []Requirement, page, pageSize int) ([]apiRequirement, apiPagination) {
	total := len(items)
	offset := (page - 1) * pageSize
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	end := offset + pageSize
	if end > total {
		end = total
	}

	result := make([]apiRequirement, 0, end-offset)
	for _, item := range items[offset:end] {
		result = append(result, a.toAPIRequirement(item))
	}
	return result, buildPagination(page, pageSize, total)
}

// filterRequirements applies status/keyword filters to requirement list.
func filterRequirements(items []Requirement, status, keyword string) []Requirement {
	status = normalizeRequirementStatusFilter(status)
	keyword = strings.TrimSpace(strings.ToLower(keyword))
	filtered := make([]Requirement, 0, len(items))
	for _, item := range items {
		if status != "" && item.Status != status {
			continue
		}
		if keyword != "" {
			if !strings.Contains(strings.ToLower(item.Title), keyword) &&
				!strings.Contains(strings.ToLower(item.Description), keyword) &&
				!strings.Contains(strings.ToLower(item.ProjectName), keyword) {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	return filtered
}

// normalizeRequirementStatusFilter validates and normalizes requirement status query input.
func normalizeRequirementStatusFilter(status string) string {
	status = strings.TrimSpace(strings.ToLower(status))
	switch status {
	case RequirementStatusPlanning, RequirementStatusRunning, RequirementStatusPaused, RequirementStatusFailed, RequirementStatusDone:
		return status
	default:
		return ""
	}
}

func parseIntWithDefault(raw string, defaultValue int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return defaultValue
	}
	return value
}
