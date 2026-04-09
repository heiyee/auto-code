package server

import (
	"auto-code/internal/httpapi"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// handleAPIProjectList returns filtered project list with pagination metadata.
func (a *App) handleAPIProjectList(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))
	branch := strings.TrimSpace(r.URL.Query().Get("branch"))
	page, pageSize := parsePageQuery(r.URL.Query().Get("page"), r.URL.Query().Get("page_size"))

	projects, err := a.projectSvc.List()
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, http.StatusInternalServerError, err.Error())
		return
	}
	filteredProjects := filterProjectSummaries(projects, keyword, branch)
	pageItems, paging := paginateProjectSummaries(filteredProjects, page, pageSize)

	data := apiProjectListData{
		Items:             pageItems,
		AvailableBranches: collectProjectBranches(projects),
		Filters: apiProjectListFilters{
			Keyword: keyword,
			Branch:  branch,
		},
		Pagination: paging,
	}
	httpapi.WriteSuccess(w, http.StatusOK, "ok", data)
}

// handleAPIProjectCreate creates one project from async request payload.
func (a *App) handleAPIProjectCreate(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	input, err := parseProjectMutationRequest(r)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateProjectCreateInput(input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	project, err := a.projectSvc.Create(input)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	httpapi.WriteSuccess(w, http.StatusCreated, "项目已创建", apiProjectCreateData{
		Item: toAPIProject(*project),
	})
}

// handleAPIProjectDelete deletes one project by id.
func (a *App) handleAPIProjectDelete(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodDelete) {
		return
	}
	projectID, action, ok := parseSubRoute("/api/projects/delete/", r.URL.Path)
	if !ok || action != "" {
		httpapi.WriteError(w, http.StatusNotFound, http.StatusNotFound, "resource not found")
		return
	}
	a.shutdownProjectCLIRuntimes(projectID)
	stats, err := a.projectSvc.Delete(projectID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpapi.WriteError(w, http.StatusNotFound, http.StatusNotFound, "project not found")
			return
		}
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	httpapi.WriteSuccess(w, http.StatusOK, fmt.Sprintf("项目已删除（需求 %d 条，CLI 会话 %d 条）", stats.RequirementCount, stats.CLISessionCount), apiProjectDeleteData{
		Stats: stats,
	})
}

type projectMutationRequest struct {
	Name       string `json:"name"`
	Repository string `json:"repository"`
	Branch     string `json:"branch"`
	WorkDir    string `json:"work_dir"`
}

// parseProjectMutationRequest decodes project mutation payload from JSON or form body.
func parseProjectMutationRequest(r *http.Request) (ProjectMutation, error) {
	req := projectMutationRequest{}
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return ProjectMutation{}, errors.New("invalid json body")
		}
	} else {
		if err := r.ParseForm(); err != nil {
			return ProjectMutation{}, errors.New("invalid form body")
		}
		req.Name = r.FormValue("name")
		req.Repository = r.FormValue("repository")
		req.Branch = r.FormValue("branch")
		req.WorkDir = r.FormValue("work_dir")
	}
	return ProjectMutation{
		Name:       req.Name,
		Repository: req.Repository,
		Branch:     req.Branch,
		WorkDir:    req.WorkDir,
	}, nil
}

// validateProjectCreateInput enforces create-only constraints for project creation API.
func validateProjectCreateInput(input ProjectMutation) error {
	if strings.TrimSpace(input.WorkDir) == "" {
		return errors.New("work_dir is required")
	}
	return nil
}

// apiProject is the JSON model returned by project create API.
type apiProject struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Repository string    `json:"repository"`
	Branch     string    `json:"branch"`
	WorkDir    string    `json:"work_dir"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// apiProjectSummary is the JSON model returned by project list API.
type apiProjectSummary struct {
	apiProject
	RequirementCount        int `json:"requirement_count"`
	RunningRequirementCount int `json:"running_requirement_count"`
	DoneRequirementCount    int `json:"done_requirement_count"`
}

// apiProjectListFilters describes query filters in project list responses.
type apiProjectListFilters struct {
	Keyword string `json:"keyword"`
	Branch  string `json:"branch"`
}

// apiProjectListData is the project list response data payload.
type apiProjectListData struct {
	Items             []apiProjectSummary   `json:"items"`
	AvailableBranches []string              `json:"available_branches"`
	Filters           apiProjectListFilters `json:"filters"`
	Pagination        apiPagination         `json:"pagination"`
}

// apiProjectCreateData is the project create response data payload.
type apiProjectCreateData struct {
	Item apiProject `json:"item"`
}

// apiProjectDeleteData is the project delete response data payload.
type apiProjectDeleteData struct {
	Stats DeleteProjectStats `json:"stats"`
}

// toAPIProject converts one domain project into API JSON model.
func toAPIProject(project Project) apiProject {
	return apiProject{
		ID:         project.ID,
		Name:       project.Name,
		Repository: project.Repository,
		Branch:     project.Branch,
		WorkDir:    project.WorkDir,
		CreatedAt:  project.CreatedAt,
		UpdatedAt:  project.UpdatedAt,
	}
}

// toAPIProjectSummary converts one domain project summary into API JSON model.
func toAPIProjectSummary(project ProjectSummary) apiProjectSummary {
	return apiProjectSummary{
		apiProject: apiProject{
			ID:         project.ID,
			Name:       project.Name,
			Repository: project.Repository,
			Branch:     project.Branch,
			WorkDir:    project.WorkDir,
			CreatedAt:  project.CreatedAt,
			UpdatedAt:  project.UpdatedAt,
		},
		RequirementCount:        project.RequirementCount,
		RunningRequirementCount: project.RunningRequirementCount,
		DoneRequirementCount:    project.DoneRequirementCount,
	}
}

// paginateProjectSummaries slices project list and returns API models with pagination metadata.
func paginateProjectSummaries(items []ProjectSummary, page, pageSize int) ([]apiProjectSummary, apiPagination) {
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

	result := make([]apiProjectSummary, 0, end-offset)
	for _, item := range items[offset:end] {
		result = append(result, toAPIProjectSummary(item))
	}
	return result, buildPagination(page, pageSize, total)
}

// filterProjectSummaries applies keyword/branch filter to project list.
func filterProjectSummaries(items []ProjectSummary, keyword, branch string) []ProjectSummary {
	keyword = strings.TrimSpace(strings.ToLower(keyword))
	branch = strings.TrimSpace(branch)
	filtered := make([]ProjectSummary, 0, len(items))
	for _, item := range items {
		if branch != "" && item.Branch != branch {
			continue
		}
		if keyword != "" {
			if !strings.Contains(strings.ToLower(item.Name), keyword) &&
				!strings.Contains(strings.ToLower(item.Repository), keyword) &&
				!strings.Contains(strings.ToLower(item.WorkDir), keyword) {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	return filtered
}

// collectProjectBranches returns unique branch values from project list.
func collectProjectBranches(items []ProjectSummary) []string {
	seen := make(map[string]struct{}, len(items))
	branches := make([]string, 0, len(items))
	for _, item := range items {
		branch := strings.TrimSpace(item.Branch)
		if branch == "" {
			continue
		}
		if _, ok := seen[branch]; ok {
			continue
		}
		seen[branch] = struct{}{}
		branches = append(branches, branch)
	}
	sort.Strings(branches)
	return branches
}
