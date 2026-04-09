package server

import (
	"auto-code/internal/httpapi"
	"auto-code/internal/logging"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// handleCLISessionSubRoutes dispatches CLI session action endpoints.
func (a *App) handleCLISessionSubRoutes(w http.ResponseWriter, r *http.Request) {
	route, ok := a.resolveCLISessionRoute(w, r)
	if !ok {
		return
	}

	switch route.action {
	case "snapshot":
		a.handleCLISessionSnapshot(w, r, route)
		return
	case "reconnect":
		a.handleCLISessionReconnect(w, r, route)
		return
	case "destroy":
		a.handleCLISessionDestroy(w, r, route)
		return
	}

	handler, ok := a.cliSessionActionHandlers()[route.action]
	if !ok {
		httpapi.WriteError(w, http.StatusNotFound, http.StatusNotFound, "resource not found")
		return
	}

	runtimeRoute, ok := a.resolveRuntimeCLISessionRoute(w, route, requiresRunningCLISession(route.action))
	if !ok {
		return
	}
	handler(w, r, runtimeRoute)
}

type cliSessionRoute struct {
	id      string
	action  string
	session *CLISession
}

type cliSessionActionHandler func(http.ResponseWriter, *http.Request, cliSessionRoute)

// cliSessionActionHandlers maps runtime-required actions to handlers.
func (a *App) cliSessionActionHandlers() map[string]cliSessionActionHandler {
	return map[string]cliSessionActionHandler{
		"poll":      a.handleCLISessionPoll,
		"input":     a.handleCLISessionInput,
		"keys":      a.handleCLISessionKeys,
		"resize":    a.handleCLISessionResize,
		"interrupt": a.handleCLISessionInterrupt,
		"terminate": a.handleCLISessionTerminate,
	}
}

// resolveCLISessionRoute parses session route path and returns route metadata.
func (a *App) resolveCLISessionRoute(w http.ResponseWriter, r *http.Request) (cliSessionRoute, bool) {
	subPath := strings.Trim(strings.TrimPrefix(r.URL.Path, "/cli/sessions/"), "/")
	if subPath == "" {
		httpapi.WriteError(w, http.StatusNotFound, http.StatusNotFound, "resource not found")
		return cliSessionRoute{}, false
	}
	parts := strings.Split(subPath, "/")
	if len(parts) != 2 {
		httpapi.WriteError(w, http.StatusNotFound, http.StatusNotFound, "resource not found")
		return cliSessionRoute{}, false
	}
	id, err := url.PathUnescape(parts[0])
	if err != nil || strings.TrimSpace(id) == "" {
		httpapi.WriteError(w, http.StatusNotFound, http.StatusNotFound, "resource not found")
		return cliSessionRoute{}, false
	}
	action := strings.TrimSpace(parts[1])
	if action == "" {
		httpapi.WriteError(w, http.StatusNotFound, http.StatusNotFound, "resource not found")
		return cliSessionRoute{}, false
	}
	return cliSessionRoute{id: id, action: action}, true
}

// resolveRuntimeCLISessionRoute resolves the in-memory runtime session for runtime actions.
func (a *App) resolveRuntimeCLISessionRoute(w http.ResponseWriter, route cliSessionRoute, requireRunning bool) (cliSessionRoute, bool) {
	session, ok := a.cliMgr.Get(route.id)
	if !ok {
		httpapi.WriteError(w, http.StatusConflict, http.StatusConflict, "session disconnected")
		return cliSessionRoute{}, false
	}
	if requireRunning && !isRuntimeSessionRunning(session) {
		httpapi.WriteError(w, http.StatusConflict, http.StatusConflict, "session disconnected")
		return cliSessionRoute{}, false
	}
	route.session = session
	return route, true
}

// requiresRunningCLISession reports whether one runtime action requires an active running process.
func requiresRunningCLISession(action string) bool {
	switch action {
	case "input", "keys", "resize", "interrupt":
		return true
	default:
		return false
	}
}

// handleCLISessionSnapshot serves output archive snapshots for one session.
func (a *App) handleCLISessionSnapshot(w http.ResponseWriter, r *http.Request, route cliSessionRoute) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	entries := []CLIOutputSnapshotEntry(nil)
	if a.cliArchive != nil {
		var err error
		entries, err = a.cliArchive.Snapshot(route.id, limit)
		if err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, http.StatusInternalServerError, err.Error())
			return
		}
	}
	lastSeq := int64(0)
	if n := len(entries); n > 0 {
		lastSeq = entries[n-1].Seq
	}

	session, ok := a.cliMgr.Get(route.id)
	if ok && isRuntimeSessionRunning(session) {
		summary := session.Summary()
		windowOutput, resumeOffset := session.WindowBytes(128 * 1024)
		if events := a.cliMgr.Events(); events != nil {
			lastSeq = max(lastSeq, events.CurrentSequence(session.ID))
		}
		httpapi.WriteSuccess(w, http.StatusOK, "ok", apiSnapshotData{
			SessionID:        session.ID,
			AgentID:          session.AgentID,
			Entries:          entries,
			LastSeq:          lastSeq,
			CurrentOutputB64: base64.StdEncoding.EncodeToString(windowOutput),
			PollResumeOffset: resumeOffset,
			SideErrors:       session.SideErrors(),
			SessionState:     summary.State,
			ExitCode:         summary.ExitCode,
			LastError:        summary.LastError,
			Connected:        true,
			Reconnectable:    false,
		})
		return
	}

	view, err := a.cliSessionSvc.GetView(route.id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpapi.WriteError(w, http.StatusNotFound, http.StatusNotFound, "session not found")
			return
		}
		httpapi.WriteError(w, http.StatusInternalServerError, http.StatusInternalServerError, err.Error())
		return
	}

	httpapi.WriteSuccess(w, http.StatusOK, "session disconnected", apiSnapshotData{
		SessionID:        route.id,
		AgentID:          view.AgentID,
		Entries:          entries,
		LastSeq:          lastSeq,
		SessionState:     view.State,
		ExitCode:         view.ExitCode,
		LastError:        view.LastError,
		Connected:        false,
		Reconnectable:    true,
		DisconnectReason: buildCLIDisconnectReason(ok),
	})
}

// handleCLISessionReconnect recreates one disconnected session from persisted metadata.
func (a *App) handleCLISessionReconnect(w http.ResponseWriter, r *http.Request, route cliSessionRoute) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	view, reused, err := a.cliSessionSvc.Reconnect(route.id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpapi.WriteError(w, http.StatusNotFound, http.StatusNotFound, "session not found")
			return
		}
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	message := "session reconnected"
	if reused {
		message = "session already connected"
	} else {
		if _, rehydrateErr := a.rehydrateRebuiltSession(view.ID, automationRetryReasonSessionRecovery); rehydrateErr != nil {
			httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, rehydrateErr.Error())
			return
		}
		message = "session rebuilt from archived context"
	}
	httpapi.WriteSuccess(w, http.StatusOK, message, apiReconnectData{
		SessionID: view.ID,
		AgentID:   view.AgentID,
		Reused:    reused,
	})
}

// isRuntimeSessionRunning reports whether one runtime session process is currently active.
func isRuntimeSessionRunning(session *CLISession) bool {
	if session == nil {
		return false
	}
	state := strings.TrimSpace(strings.ToLower(session.Summary().State))
	return state == "running"
}

// buildCLIDisconnectReason builds user-facing disconnect reason by runtime presence.
func buildCLIDisconnectReason(hasRuntimeSession bool) string {
	if hasRuntimeSession {
		return "会话进程已退出，请点击重连恢复实例"
	}
	return "会话实例已断开，服务器内存中不存在该实例"
}

// writeCLIRuntimeActionError maps runtime write/resize errors into stable HTTP API errors.
func writeCLIRuntimeActionError(w http.ResponseWriter, err error) {
	if errors.Is(err, errSessionNotRunning) {
		httpapi.WriteError(w, http.StatusConflict, http.StatusConflict, "session disconnected")
		return
	}
	httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
}

// handleCLISessionPoll serves incremental runtime output chunks.
func (a *App) handleCLISessionPoll(w http.ResponseWriter, r *http.Request, route cliSessionRoute) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	offset, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	result := route.session.Poll(offset)
	a.cliSessionSvc.Touch(route.id)
	httpapi.WriteSuccess(w, http.StatusOK, "ok", result)
}

// handleCLISessionInput writes plain-text input into one runtime session.
func (a *App) handleCLISessionInput(w http.ResponseWriter, r *http.Request, route cliSessionRoute) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	req, err := parseCLISessionInputRequest(r)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	if req.AppendNewline && !strings.HasSuffix(req.Text, "\n") {
		req.Text += "\n"
	}
	if strings.TrimSpace(req.Text) == "" {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, "text is required")
		return
	}
	if err := prepareSessionForInput(route.session, 0, false); err != nil {
		writeCLIRuntimeActionError(w, err)
		return
	}
	if a.requirementAuto != nil {
		a.requirementAuto.TrackOutboundInput(route.id, req.Text)
	}
	if err := route.session.WriteInput(req.Text); err != nil {
		writeCLIRuntimeActionError(w, err)
		return
	}
	a.cliSessionSvc.Touch(route.id)
	httpapi.WriteSuccess(w, http.StatusOK, "ok", apiOKData{OK: true})
}

// handleCLISessionKeys writes raw key payload into one runtime session.
func (a *App) handleCLISessionKeys(w http.ResponseWriter, r *http.Request, route cliSessionRoute) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	req, err := parseCLISessionKeysRequest(r)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	req.B64 = strings.TrimSpace(req.B64)
	if req.B64 == "" {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, "b64 is required")
		return
	}
	raw, err := base64.StdEncoding.DecodeString(req.B64)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, "invalid b64 payload")
		return
	}
	if len(raw) == 0 {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, "decoded payload is empty")
		return
	}
	if err := route.session.WriteRawBytes(raw); err != nil {
		writeCLIRuntimeActionError(w, err)
		return
	}
	a.cliSessionSvc.Touch(route.id)
	httpapi.WriteSuccess(w, http.StatusOK, "ok", apiBytesData{OK: true, Bytes: len(raw)})
}

// handleCLISessionResize resizes pty dimensions for one runtime session.
func (a *App) handleCLISessionResize(w http.ResponseWriter, r *http.Request, route cliSessionRoute) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	req, err := parseCLISessionResizeRequest(r)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
		return
	}
	if req.Cols <= 0 || req.Rows <= 0 {
		httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, "cols and rows must be positive")
		return
	}
	if err := route.session.Resize(req.Cols, req.Rows); err != nil {
		writeCLIRuntimeActionError(w, err)
		return
	}
	a.cliSessionSvc.Touch(route.id)
	httpapi.WriteSuccess(w, http.StatusOK, "ok", apiResizeData{OK: true, Cols: req.Cols, Rows: req.Rows})
}

// handleCLISessionInterrupt sends Ctrl+C into one runtime session.
func (a *App) handleCLISessionInterrupt(w http.ResponseWriter, r *http.Request, route cliSessionRoute) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if err := route.session.WriteInput("\x03"); err != nil {
		writeCLIRuntimeActionError(w, err)
		return
	}
	a.cliSessionSvc.Touch(route.id)
	httpapi.WriteSuccess(w, http.StatusOK, "ok", apiOKData{OK: true})
}

// handleCLISessionTerminate stops one runtime session and updates persisted state.
func (a *App) handleCLISessionTerminate(w http.ResponseWriter, r *http.Request, route cliSessionRoute) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if err := route.session.Terminate(); err != nil && !errors.Is(err, errSessionNotRunning) {
		writeCLIRuntimeActionError(w, err)
		return
	}
	_ = a.cliSessionSvc.MarkTerminated(route.id)
	if a.requirementAuto != nil {
		a.requirementAuto.clearSessionDetectionState(route.id)
	}
	httpapi.WriteSuccess(w, http.StatusOK, "ok", apiOKData{OK: true})
}

// handleCLISessionDestroy removes one session from runtime manager and persistence.
func (a *App) handleCLISessionDestroy(w http.ResponseWriter, r *http.Request, route cliSessionRoute) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	logger := logging.Named("server.cli-session")

	agentID := ""
	if view, err := a.cliSessionSvc.GetView(route.id); err == nil {
		agentID = strings.TrimSpace(view.AgentID)
	} else if !errors.Is(err, ErrNotFound) {
		logger.Warn(
			"lookup cli session before destroy failed",
			zap.String("session_id", route.id),
			zap.Error(err),
		)
	}
	logger.Info(
		"destroy cli session requested",
		zap.String("session_id", route.id),
		zap.String("agent_id", agentID),
	)

	runtimePresent := false
	if runtimeSession, ok := a.cliMgr.Get(route.id); ok {
		runtimePresent = true
		if agentID == "" {
			agentID = runtimeSession.AgentID
		}
		if err := a.cliMgr.Destroy(route.id); err != nil {
			logger.Warn(
				"destroy cli runtime failed",
				zap.String("session_id", route.id),
				zap.String("agent_id", agentID),
				zap.Error(err),
			)
			httpapi.WriteError(w, http.StatusBadRequest, http.StatusBadRequest, err.Error())
			return
		}
	}
	if events := a.cliMgr.Events(); events != nil {
		events.ClearSession(route.id, agentID)
	}
	_ = a.cliSessionSvc.MarkTerminated(route.id)
	_ = a.cliSessionSvc.DeleteRecord(route.id)
	if a.requirementAuto != nil {
		a.requirementAuto.clearSessionDetectionState(route.id)
	}
	if a.cliArchive != nil {
		if err := a.cliArchive.DeleteSession(route.id); err != nil && !errors.Is(err, os.ErrNotExist) {
			logger.Warn(
				"cleanup cli archive failed",
				zap.String("session_id", route.id),
				zap.Error(err),
			)
		}
	}
	logger.Info(
		"destroy cli session completed",
		zap.String("session_id", route.id),
		zap.String("agent_id", agentID),
		zap.Bool("runtime_present", runtimePresent),
	)
	httpapi.WriteSuccess(w, http.StatusOK, "ok", apiOKData{OK: true})
}

type cliInputRequest struct {
	Text          string `json:"text"`
	AppendNewline bool   `json:"append_newline"`
}

// parseCLISessionInputRequest decodes session input request from JSON or form body.
func parseCLISessionInputRequest(r *http.Request) (cliInputRequest, error) {
	var req cliInputRequest
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return req, errors.New("invalid json body")
		}
		return req, nil
	}
	if err := r.ParseForm(); err != nil {
		return req, errors.New("invalid form body")
	}
	req.Text = r.FormValue("text")
	req.AppendNewline = r.FormValue("append_newline") == "true"
	return req, nil
}

type cliKeysRequest struct {
	B64 string `json:"b64"`
}

// parseCLISessionKeysRequest decodes base64 key payload from JSON or form body.
func parseCLISessionKeysRequest(r *http.Request) (cliKeysRequest, error) {
	var req cliKeysRequest
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return req, errors.New("invalid json body")
		}
		return req, nil
	}
	if err := r.ParseForm(); err != nil {
		return req, errors.New("invalid form body")
	}
	req.B64 = r.FormValue("b64")
	return req, nil
}

func prepareCodexSessionForInput(session *CLISession, timeout time.Duration, waitForPrompt bool) error {
	return prepareSessionForInput(session, timeout, waitForPrompt)
}

type cliResizeRequest struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// parseCLISessionResizeRequest decodes terminal size change request.
func parseCLISessionResizeRequest(r *http.Request) (cliResizeRequest, error) {
	var req cliResizeRequest
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return req, errors.New("invalid json body")
		}
		return req, nil
	}
	if err := r.ParseForm(); err != nil {
		return req, errors.New("invalid form body")
	}
	req.Cols, _ = strconv.Atoi(r.FormValue("cols"))
	req.Rows, _ = strconv.Atoi(r.FormValue("rows"))
	return req, nil
}
