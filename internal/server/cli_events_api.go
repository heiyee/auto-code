package server

import (
	"auto-code/internal/httpapi"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"
)

// handleCLIEvents streams CLI output events via Server-Sent Events.
func (a *App) handleCLIEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpapi.WriteError(w, http.StatusMethodNotAllowed, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	events := a.cliMgr.Events()
	if events == nil {
		httpapi.WriteError(w, http.StatusServiceUnavailable, http.StatusServiceUnavailable, "events are unavailable")
		return
	}

	filter := CLIEventFilter{
		SessionID: r.URL.Query().Get("session_id"),
		AgentID:   r.URL.Query().Get("agentid"),
	}
	lastSeq, _ := strconv.ParseInt(r.URL.Query().Get("last_seq"), 10, 64)
	subscription := events.Subscribe(filter, lastSeq)
	defer subscription.Close()

	flusher, ok := w.(http.Flusher)
	if !ok {
		httpapi.WriteError(w, http.StatusInternalServerError, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-subscription.Events:
			if !ok {
				return
			}
			if err := writeSSEEvent(w, event); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// writeSSEEvent writes one SSE event payload for browser EventSource.
func writeSSEEvent(w io.Writer, event CLIEvent) error {
	if _, err := io.WriteString(w, "id: "+event.EventID+"\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "event: "+event.Type+"\n"); err != nil {
		return err
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, "data: "); err != nil {
		return err
	}
	if _, err := w.Write(payload); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "\n\n"); err != nil {
		return err
	}
	return nil
}
