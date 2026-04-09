package server

import (
	"auto-code/internal/httpapi"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// cliSessionCreateRequest is the create payload for /cli/sessions endpoint.
type cliSessionCreateRequest struct {
	CLIType string `json:"cli_type"`
	Profile string `json:"profile"`
	Account string `json:"account"`
	Command string `json:"command"`
	Cols    int    `json:"cols"`
	Rows    int    `json:"rows"`
}

// normalize trims request fields and applies compatible aliases.
func (r *cliSessionCreateRequest) normalize() {
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

// wantsProfileLaunch returns true when request explicitly asks profile/account based launch.
func (r cliSessionCreateRequest) wantsProfileLaunch() bool {
	return r.CLIType != "" || r.Profile != ""
}

// parseCLISessionCreateRequest decodes create request from JSON or form payload.
func parseCLISessionCreateRequest(r *http.Request) (cliSessionCreateRequest, error) {
	req := cliSessionCreateRequest{}
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return req, errors.New("invalid json body")
		}
		req.normalize()
		return req, nil
	}

	if err := r.ParseForm(); err != nil {
		return req, errors.New("invalid form body")
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

// handleCLISessions creates one standalone session for global CLI console.
func (a *App) handleCLISessions(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	req, err := parseCLISessionCreateRequest(r)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}

	view, err := a.createStandaloneSession(req)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	a.applyRequestedTerminalSize(view.ID, req.Cols, req.Rows)

	httpapi.WriteSuccess(w, http.StatusCreated, "会话已启动", apiCLISessionCreateData{
		SessionID: view.ID,
		AgentID:   view.AgentID,
		CLIType:   view.CLIType,
		Profile:   view.Profile,
	})
}

// createStandaloneSession dispatches standalone creation by manual command or selected profile.
func (a *App) createStandaloneSession(req cliSessionCreateRequest) (*CLISessionView, error) {
	if req.wantsProfileLaunch() {
		if req.CLIType == "" {
			return nil, errors.New("cli_type is required")
		}
		if a.cliSessionSvc.SupportsMultipleAccounts(req.CLIType) && strings.TrimSpace(req.Profile) == "" {
			return nil, errors.New("profile is required")
		}
		return a.cliSessionSvc.CreateStandaloneWithProfileAndSize(req.CLIType, req.Profile, req.Command, req.Cols, req.Rows)
	}
	return a.cliSessionSvc.CreateStandaloneWithSize(req.Command, req.Cols, req.Rows)
}
