package server

import "strings"

const (
	requestedTerminalMinCols = 24
	requestedTerminalMinRows = 6
	requestedTerminalMaxCols = 400
	requestedTerminalMaxRows = 200
)

func normalizeRequestedTerminalSize(cols, rows int) (int, int, bool) {
	if cols < requestedTerminalMinCols || rows < requestedTerminalMinRows {
		return 0, 0, false
	}
	if cols > requestedTerminalMaxCols {
		cols = requestedTerminalMaxCols
	}
	if rows > requestedTerminalMaxRows {
		rows = requestedTerminalMaxRows
	}
	return cols, rows, true
}

func (a *App) applyRequestedTerminalSize(sessionID string, cols, rows int) {
	cols, rows, ok := normalizeRequestedTerminalSize(cols, rows)
	if !ok || a == nil || a.cliMgr == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	session, exists := a.cliMgr.Get(sessionID)
	if !exists || session == nil {
		return
	}
	_ = session.Resize(cols, rows)
}
