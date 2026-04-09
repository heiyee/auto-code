package server

import (
	"auto-code/internal/gitops"
	"auto-code/internal/httpapi"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

type gitQueryRequest struct {
	Operation  string `json:"operation"`
	Repository string `json:"repository"`
	Limit      int    `json:"limit"`
}

// handleAPIGitQuery serves generic git operation queries used by async forms.
func (a *App) handleAPIGitQuery(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if a == nil || a.gitOpsClient == nil {
		httpapi.WriteError(w, http.StatusServiceUnavailable, http.StatusServiceUnavailable, "git query service unavailable")
		return
	}
	req := gitQueryRequest{}
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, "invalid json body")
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, "invalid form body")
			return
		}
		req.Operation = r.FormValue("operation")
		req.Repository = r.FormValue("repository")
		req.Limit, _ = strconv.Atoi(r.FormValue("limit"))
	}

	result, err := a.gitOpsClient.Query(r.Context(), gitops.QueryRequest{
		Operation:  req.Operation,
		Repository: req.Repository,
		Limit:      req.Limit,
	})
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	httpapi.WriteSuccess(w, http.StatusOK, "ok", result)
}
