package server

import (
	"auto-code/internal/domain"
	"auto-code/internal/httpapi"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type apiProjectFileListData struct {
	Path  string     `json:"path"`
	Nodes []FileNode `json:"nodes"`
}

type apiProjectFileMutationData struct {
	Path     string `json:"path,omitempty"`
	NewPath  string `json:"newPath,omitempty"`
	Revision string `json:"revision,omitempty"`
}

type apiProjectFileConflictData struct {
	Path            string `json:"path"`
	CurrentRevision string `json:"currentRevision"`
}

type projectFilePathRequest struct {
	Path string `json:"path"`
}

type projectFileSaveRequest struct {
	Path         string `json:"path"`
	Content      string `json:"content"`
	BaseRevision string `json:"baseRevision"`
}

type projectFileCreateRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type projectFileRenameRequest struct {
	OldPath string `json:"oldPath"`
	NewPath string `json:"newPath"`
}

type projectGitCheckoutRequest struct {
	Branch string `json:"branch"`
}

type projectGitCreateBranchRequest struct {
	Name     string `json:"name"`
	Checkout bool   `json:"checkout"`
}

type projectGitCreateTagRequest struct {
	Name string `json:"name"`
}

type projectGitCommitRequest struct {
	Message string `json:"message"`
}

type projectGitPullRequest struct {
	Remote string `json:"remote"`
	Branch string `json:"branch"`
}

type projectGitPushRequest struct {
	Remote string `json:"remote"`
	Branch string `json:"branch"`
}

// handleAPIProjectScopedRoutes dispatches project-scoped file-manager and git APIs.
func (a *App) handleAPIProjectScopedRoutes(w http.ResponseWriter, r *http.Request) {
	projectID, segments, ok := parseProjectScopedRoute("/api/projects/", r.URL.Path)
	if !ok || len(segments) == 0 {
		httpapi.WriteError(w, http.StatusNotFound, http.StatusNotFound, "resource not found")
		return
	}

	switch segments[0] {
	case "current-requirement":
		a.handleAPIProjectCurrentRequirement(w, r, projectID, segments)
	case "files":
		a.handleAPIProjectFiles(w, r, projectID, segments)
	case "git":
		a.handleAPIProjectGit(w, r, projectID, segments)
	case "cli":
		a.handleAPIProjectCLI(w, r, projectID, segments)
	default:
		httpapi.WriteError(w, http.StatusNotFound, http.StatusNotFound, "resource not found")
	}
}

func (a *App) handleAPIProjectCurrentRequirement(w http.ResponseWriter, r *http.Request, projectID string, segments []string) {
	if len(segments) != 1 {
		httpapi.WriteError(w, http.StatusNotFound, http.StatusNotFound, "resource not found")
		return
	}
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	item, err := a.projectFileSvc.CurrentRequirement(projectID)
	if err != nil {
		writeProjectFileError(w, err)
		return
	}
	if item == nil {
		httpapi.WriteSuccess(w, http.StatusOK, "ok", CurrentRequirementInfo{})
		return
	}
	httpapi.WriteSuccess(w, http.StatusOK, "ok", item)
}

func (a *App) handleAPIProjectFiles(w http.ResponseWriter, r *http.Request, projectID string, segments []string) {
	switch {
	case len(segments) == 1 && r.Method == http.MethodGet:
		path := strings.TrimSpace(r.URL.Query().Get("path"))
		nodes, err := a.projectFileSvc.ListFiles(r.Context(), projectID, path)
		if err != nil {
			writeProjectFileError(w, err)
			return
		}
		httpapi.WriteSuccess(w, http.StatusOK, "ok", apiProjectFileListData{
			Path:  path,
			Nodes: nodes,
		})
	case len(segments) == 1 && r.Method == http.MethodDelete:
		path := strings.TrimSpace(r.URL.Query().Get("path"))
		if path == "" {
			httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, "path is required")
			return
		}
		if err := a.projectFileSvc.Delete(projectID, path); err != nil {
			writeProjectFileError(w, err)
			return
		}
		httpapi.WriteSuccess(w, http.StatusOK, "删除成功", apiDeleteMessageData{})
	case len(segments) == 2 && segments[1] == "content" && r.Method == http.MethodGet:
		path := strings.TrimSpace(r.URL.Query().Get("path"))
		if path == "" {
			httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, "path is required")
			return
		}
		content, err := a.projectFileSvc.ReadFile(projectID, path)
		if err != nil {
			writeProjectFileError(w, err)
			return
		}
		httpapi.WriteSuccess(w, http.StatusOK, "ok", content)
	case len(segments) == 2 && segments[1] == "save" && r.Method == http.MethodPost:
		a.handleAPIProjectFileSave(w, r, projectID)
	case len(segments) == 2 && segments[1] == "create" && r.Method == http.MethodPost:
		a.handleAPIProjectFileCreate(w, r, projectID)
	case len(segments) == 2 && segments[1] == "mkdir" && r.Method == http.MethodPost:
		a.handleAPIProjectFileMkdir(w, r, projectID)
	case len(segments) == 2 && segments[1] == "rename" && r.Method == http.MethodPost:
		a.handleAPIProjectFileRename(w, r, projectID)
	default:
		httpapi.WriteError(w, http.StatusNotFound, http.StatusNotFound, "resource not found")
	}
}

func (a *App) handleAPIProjectGit(w http.ResponseWriter, r *http.Request, projectID string, segments []string) {
	if len(segments) == 2 && segments[1] == "status" {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		status, err := a.projectFileSvc.GitStatus(r.Context(), projectID)
		if err != nil {
			writeProjectFileError(w, err)
			return
		}
		httpapi.WriteSuccess(w, http.StatusOK, "ok", status)
		return
	}
	if len(segments) == 2 && segments[1] == "diff" {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		path := strings.TrimSpace(r.URL.Query().Get("path"))
		if path == "" {
			httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, "path is required")
			return
		}
		diff, err := a.projectFileSvc.GitDiff(r.Context(), projectID, path)
		if err != nil {
			writeProjectFileError(w, err)
			return
		}
		httpapi.WriteSuccess(w, http.StatusOK, "ok", diff)
		return
	}
	if len(segments) == 2 && segments[1] == "stage" {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		a.handleAPIProjectGitStage(w, r, projectID)
		return
	}
	if len(segments) == 2 && segments[1] == "unstage" {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		a.handleAPIProjectGitUnstage(w, r, projectID)
		return
	}
	if len(segments) == 2 && segments[1] == "branches" {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		branches, err := a.projectFileSvc.GitBranches(r.Context(), projectID)
		if err != nil {
			writeProjectFileError(w, err)
			return
		}
		httpapi.WriteSuccess(w, http.StatusOK, "ok", branches)
		return
	}
	if len(segments) == 2 && segments[1] == "checkout" {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		a.handleAPIProjectGitCheckout(w, r, projectID)
		return
	}
	if len(segments) == 3 && segments[1] == "branch" && segments[2] == "create" {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		a.handleAPIProjectGitBranchCreate(w, r, projectID)
		return
	}
	if len(segments) == 3 && segments[1] == "tag" && segments[2] == "create" {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		a.handleAPIProjectGitTagCreate(w, r, projectID)
		return
	}
	if len(segments) == 2 && segments[1] == "commit" {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		a.handleAPIProjectGitCommit(w, r, projectID)
		return
	}
	if len(segments) == 2 && segments[1] == "pull" {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		a.handleAPIProjectGitPull(w, r, projectID)
		return
	}
	if len(segments) == 2 && segments[1] == "push" {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		a.handleAPIProjectGitPush(w, r, projectID)
		return
	}
	httpapi.WriteError(w, http.StatusNotFound, http.StatusNotFound, "resource not found")
}

func (a *App) handleAPIProjectFileSave(w http.ResponseWriter, r *http.Request, projectID string) {
	request, err := parseProjectFileSaveRequest(r)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.projectFileSvc.SaveFile(projectID, FileSaveInput{
		Path:         request.Path,
		Content:      request.Content,
		BaseRevision: request.BaseRevision,
	})
	if err != nil {
		var conflict *FileRevisionConflict
		if errors.As(err, &conflict) {
			httpapi.Write(w, http.StatusConflict, httpapi.Response[apiProjectFileConflictData]{
				Code:    http.StatusConflict,
				Message: "file revision conflict",
				Data: apiProjectFileConflictData{
					Path:            conflict.Path,
					CurrentRevision: conflict.CurrentRevision,
				},
			})
			return
		}
		writeProjectFileError(w, err)
		return
	}
	httpapi.WriteSuccess(w, http.StatusOK, "保存成功", apiProjectFileMutationData{
		Path:     result.Path,
		Revision: result.Revision,
	})
}

func (a *App) handleAPIProjectFileCreate(w http.ResponseWriter, r *http.Request, projectID string) {
	request, err := parseProjectFileCreateRequest(r)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.projectFileSvc.CreateFile(projectID, FileCreateInput{
		Path:    request.Path,
		Content: request.Content,
	})
	if err != nil {
		writeProjectFileError(w, err)
		return
	}
	httpapi.WriteSuccess(w, http.StatusCreated, "创建成功", apiProjectFileMutationData{
		Path:     result.Path,
		Revision: result.Revision,
	})
}

func (a *App) handleAPIProjectFileMkdir(w http.ResponseWriter, r *http.Request, projectID string) {
	request, err := parseProjectFilePathRequest(r)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.projectFileSvc.CreateDirectory(projectID, DirectoryCreateInput{
		Path: request.Path,
	}); err != nil {
		writeProjectFileError(w, err)
		return
	}
	httpapi.WriteSuccess(w, http.StatusCreated, "目录已创建", apiProjectFileMutationData{
		Path: request.Path,
	})
}

func (a *App) handleAPIProjectFileRename(w http.ResponseWriter, r *http.Request, projectID string) {
	request, err := parseProjectFileRenameRequest(r)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	newPath, err := a.projectFileSvc.Rename(projectID, FileRenameInput{
		OldPath: request.OldPath,
		NewPath: request.NewPath,
	})
	if err != nil {
		writeProjectFileError(w, err)
		return
	}
	httpapi.WriteSuccess(w, http.StatusOK, "重命名成功", apiProjectFileMutationData{
		Path:    request.OldPath,
		NewPath: newPath,
	})
}

func (a *App) handleAPIProjectGitCheckout(w http.ResponseWriter, r *http.Request, projectID string) {
	request, err := parseProjectGitCheckoutRequest(r)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.projectFileSvc.GitCheckoutBranch(r.Context(), projectID, request.Branch)
	if err != nil {
		writeProjectFileError(w, err)
		return
	}
	httpapi.WriteSuccess(w, http.StatusOK, "切换分支成功", result)
}

func (a *App) handleAPIProjectGitStage(w http.ResponseWriter, r *http.Request, projectID string) {
	request, err := parseProjectFilePathRequest(r)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(request.Path) == "" {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, "path is required")
		return
	}
	result, err := a.projectFileSvc.GitStagePath(r.Context(), projectID, request.Path)
	if err != nil {
		writeProjectFileError(w, err)
		return
	}
	httpapi.WriteSuccess(w, http.StatusOK, "暂存成功", result)
}

func (a *App) handleAPIProjectGitUnstage(w http.ResponseWriter, r *http.Request, projectID string) {
	request, err := parseProjectFilePathRequest(r)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(request.Path) == "" {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, "path is required")
		return
	}
	result, err := a.projectFileSvc.GitUnstagePath(r.Context(), projectID, request.Path)
	if err != nil {
		writeProjectFileError(w, err)
		return
	}
	httpapi.WriteSuccess(w, http.StatusOK, "取消暂存成功", result)
}

func (a *App) handleAPIProjectGitBranchCreate(w http.ResponseWriter, r *http.Request, projectID string) {
	request, err := parseProjectGitCreateBranchRequest(r)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.projectFileSvc.GitCreateBranch(r.Context(), projectID, request.Name, request.Checkout)
	if err != nil {
		writeProjectFileError(w, err)
		return
	}
	httpapi.WriteSuccess(w, http.StatusOK, "创建分支成功", result)
}

func (a *App) handleAPIProjectGitTagCreate(w http.ResponseWriter, r *http.Request, projectID string) {
	request, err := parseProjectGitCreateTagRequest(r)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.projectFileSvc.GitCreateTag(r.Context(), projectID, request.Name)
	if err != nil {
		writeProjectFileError(w, err)
		return
	}
	httpapi.WriteSuccess(w, http.StatusOK, "创建标签成功", result)
}

func (a *App) handleAPIProjectGitCommit(w http.ResponseWriter, r *http.Request, projectID string) {
	request, err := parseProjectGitCommitRequest(r)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.projectFileSvc.GitCommit(r.Context(), projectID, request.Message)
	if err != nil {
		writeProjectFileError(w, err)
		return
	}
	httpapi.WriteSuccess(w, http.StatusOK, "提交成功", result)
}

func (a *App) handleAPIProjectGitPull(w http.ResponseWriter, r *http.Request, projectID string) {
	request, err := parseProjectGitPullRequest(r)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.projectFileSvc.GitPull(r.Context(), projectID, request.Remote, request.Branch)
	if err != nil {
		writeProjectFileError(w, err)
		return
	}
	httpapi.WriteSuccess(w, http.StatusOK, "拉取成功", result)
}

func (a *App) handleAPIProjectGitPush(w http.ResponseWriter, r *http.Request, projectID string) {
	request, err := parseProjectGitPushRequest(r)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.projectFileSvc.GitPush(r.Context(), projectID, request.Remote, request.Branch)
	if err != nil {
		writeProjectFileError(w, err)
		return
	}
	httpapi.WriteSuccess(w, http.StatusOK, "推送成功", result)
}

func parseProjectFilePathRequest(r *http.Request) (projectFilePathRequest, error) {
	request := projectFilePathRequest{}
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			return projectFilePathRequest{}, errors.New("invalid json body")
		}
		return request, nil
	}
	if err := r.ParseForm(); err != nil {
		return projectFilePathRequest{}, errors.New("invalid form body")
	}
	request.Path = r.FormValue("path")
	return request, nil
}

func parseProjectFileSaveRequest(r *http.Request) (projectFileSaveRequest, error) {
	request := projectFileSaveRequest{}
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			return projectFileSaveRequest{}, errors.New("invalid json body")
		}
		return request, nil
	}
	if err := r.ParseForm(); err != nil {
		return projectFileSaveRequest{}, errors.New("invalid form body")
	}
	request.Path = r.FormValue("path")
	request.Content = r.FormValue("content")
	request.BaseRevision = r.FormValue("baseRevision")
	return request, nil
}

func parseProjectFileCreateRequest(r *http.Request) (projectFileCreateRequest, error) {
	request := projectFileCreateRequest{}
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			return projectFileCreateRequest{}, errors.New("invalid json body")
		}
		return request, nil
	}
	if err := r.ParseForm(); err != nil {
		return projectFileCreateRequest{}, errors.New("invalid form body")
	}
	request.Path = r.FormValue("path")
	request.Content = r.FormValue("content")
	return request, nil
}

func parseProjectFileRenameRequest(r *http.Request) (projectFileRenameRequest, error) {
	request := projectFileRenameRequest{}
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			return projectFileRenameRequest{}, errors.New("invalid json body")
		}
		return request, nil
	}
	if err := r.ParseForm(); err != nil {
		return projectFileRenameRequest{}, errors.New("invalid form body")
	}
	request.OldPath = r.FormValue("oldPath")
	request.NewPath = r.FormValue("newPath")
	return request, nil
}

func parseProjectGitCheckoutRequest(r *http.Request) (projectGitCheckoutRequest, error) {
	request := projectGitCheckoutRequest{}
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			return projectGitCheckoutRequest{}, errors.New("invalid json body")
		}
		return request, nil
	}
	if err := r.ParseForm(); err != nil {
		return projectGitCheckoutRequest{}, errors.New("invalid form body")
	}
	request.Branch = r.FormValue("branch")
	return request, nil
}

func parseProjectGitCreateBranchRequest(r *http.Request) (projectGitCreateBranchRequest, error) {
	request := projectGitCreateBranchRequest{Checkout: true}
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			return projectGitCreateBranchRequest{}, errors.New("invalid json body")
		}
		return request, nil
	}
	if err := r.ParseForm(); err != nil {
		return projectGitCreateBranchRequest{}, errors.New("invalid form body")
	}
	request.Name = r.FormValue("name")
	request.Checkout = parseBoolString(r.FormValue("checkout"), true)
	return request, nil
}

func parseProjectGitCreateTagRequest(r *http.Request) (projectGitCreateTagRequest, error) {
	request := projectGitCreateTagRequest{}
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			return projectGitCreateTagRequest{}, errors.New("invalid json body")
		}
		return request, nil
	}
	if err := r.ParseForm(); err != nil {
		return projectGitCreateTagRequest{}, errors.New("invalid form body")
	}
	request.Name = r.FormValue("name")
	return request, nil
}

func parseProjectGitCommitRequest(r *http.Request) (projectGitCommitRequest, error) {
	request := projectGitCommitRequest{}
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			return projectGitCommitRequest{}, errors.New("invalid json body")
		}
		return request, nil
	}
	if err := r.ParseForm(); err != nil {
		return projectGitCommitRequest{}, errors.New("invalid form body")
	}
	request.Message = r.FormValue("message")
	return request, nil
}

func parseProjectGitPullRequest(r *http.Request) (projectGitPullRequest, error) {
	request := projectGitPullRequest{}
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			return projectGitPullRequest{}, errors.New("invalid json body")
		}
		return request, nil
	}
	if err := r.ParseForm(); err != nil {
		return projectGitPullRequest{}, errors.New("invalid form body")
	}
	request.Remote = r.FormValue("remote")
	request.Branch = r.FormValue("branch")
	return request, nil
}

func parseProjectGitPushRequest(r *http.Request) (projectGitPushRequest, error) {
	request := projectGitPushRequest{}
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			return projectGitPushRequest{}, errors.New("invalid json body")
		}
		return request, nil
	}
	if err := r.ParseForm(); err != nil {
		return projectGitPushRequest{}, errors.New("invalid form body")
	}
	request.Remote = r.FormValue("remote")
	request.Branch = r.FormValue("branch")
	return request, nil
}

func parseProjectScopedRoute(prefix, rawPath string) (string, []string, bool) {
	subPath := strings.Trim(strings.TrimPrefix(rawPath, prefix), "/")
	if subPath == "" {
		return "", nil, false
	}
	parts := strings.Split(subPath, "/")
	if len(parts) < 2 {
		return "", nil, false
	}
	projectID, err := url.PathUnescape(strings.TrimSpace(parts[0]))
	if err != nil || strings.TrimSpace(projectID) == "" {
		return "", nil, false
	}
	return projectID, parts[1:], true
}

func parseBoolString(raw string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func writeProjectFileError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		httpapi.WriteError(w, http.StatusNotFound, http.StatusNotFound, "resource not found")
		return
	}
	switch {
	case errors.Is(err, domain.ErrForbiddenPath), errors.Is(err, domain.ErrPathOutsideProject):
		httpapi.WriteError(w, http.StatusForbidden, http.StatusForbidden, err.Error())
	case errors.Is(err, domain.ErrInvalidFilePath),
		errors.Is(err, domain.ErrDirectoryExpected),
		errors.Is(err, domain.ErrFileRevisionRequired),
		errors.Is(err, domain.ErrTextFileOnly),
		errors.Is(err, domain.ErrFileTooLarge),
		errors.Is(err, domain.ErrGitRefNameRequired),
		errors.Is(err, domain.ErrGitRefNameInvalid),
		errors.Is(err, domain.ErrGitCommitMessageRequired),
		errors.Is(err, domain.ErrGitNoStagedChanges):
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
	default:
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
	}
}
