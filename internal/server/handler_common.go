package server

import (
	"auto-code/internal/httpapi"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	// defaultListPage defines default page index for list APIs.
	defaultListPage = 1
	// defaultListPageSize defines default page size for list APIs.
	defaultListPageSize = 20
	// maxListPageSize defines max allowed page size for list APIs.
	maxListPageSize = 200
)

// apiPagination describes paginated list metadata in response payload.
type apiPagination struct {
	Page      int `json:"page"`
	PageSize  int `json:"page_size"`
	Total     int `json:"total"`
	TotalPage int `json:"total_page"`
}

// apiDeleteMessageData is an empty payload for successful delete responses.
type apiDeleteMessageData struct{}

// apiCLIProfileItem is one profile item in CLI profile API response.
type apiCLIProfileItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// apiCLIProfileGroup groups profiles by CLI type.
type apiCLIProfileGroup struct {
	CLIType        string              `json:"cli_type"`
	DefaultProfile string              `json:"default_profile,omitempty"`
	Profiles       []apiCLIProfileItem `json:"profiles"`
}

// apiSnapshotData is the snapshot response payload for CLI session output archive.
type apiSnapshotData struct {
	SessionID        string                   `json:"session_id"`
	AgentID          string                   `json:"agentid"`
	Entries          []CLIOutputSnapshotEntry `json:"entries"`
	LastSeq          int64                    `json:"last_seq"`
	CurrentOutputB64 string                   `json:"current_output_b64,omitempty"`
	PollResumeOffset int64                    `json:"poll_resume_offset,omitempty"`
	SideErrors       []RuntimeSideError       `json:"side_errors,omitempty"`
	SessionState     string                   `json:"session_state,omitempty"`
	ExitCode         *int                     `json:"exit_code,omitempty"`
	LastError        string                   `json:"last_error,omitempty"`
	Connected        bool                     `json:"connected"`
	Reconnectable    bool                     `json:"reconnectable"`
	DisconnectReason string                   `json:"disconnect_reason,omitempty"`
}

// apiReconnectData is the reconnect response payload after session recovery attempt.
type apiReconnectData struct {
	SessionID string `json:"session_id"`
	AgentID   string `json:"agentid"`
	Reused    bool   `json:"reused"`
}

// apiOKData is a generic boolean acknowledgement payload.
type apiOKData struct {
	OK bool `json:"ok"`
}

// apiBytesData is a byte-count acknowledgement payload.
type apiBytesData struct {
	OK    bool `json:"ok"`
	Bytes int  `json:"bytes"`
}

// apiResizeData is a resize acknowledgement payload.
type apiResizeData struct {
	OK   bool `json:"ok"`
	Cols int  `json:"cols"`
	Rows int  `json:"rows"`
}

// apiCLISessionCreateData is the create-session response payload for /cli/sessions.
type apiCLISessionCreateData struct {
	SessionID string `json:"session_id"`
	AgentID   string `json:"agentid"`
	CLIType   string `json:"cli_type"`
	Profile   string `json:"profile"`
}

// parsePageQuery parses page/page_size and applies sane default bounds.
func parsePageQuery(pageRaw, pageSizeRaw string) (int, int) {
	page := defaultListPage
	pageSize := defaultListPageSize

	if value, err := strconv.Atoi(strings.TrimSpace(pageRaw)); err == nil && value > 0 {
		page = value
	}
	if value, err := strconv.Atoi(strings.TrimSpace(pageSizeRaw)); err == nil && value > 0 {
		pageSize = value
	}
	if pageSize > maxListPageSize {
		pageSize = maxListPageSize
	}
	return page, pageSize
}

// buildPagination computes pagination metadata for list responses.
func buildPagination(page, pageSize, total int) apiPagination {
	totalPage := 0
	if pageSize > 0 {
		totalPage = (total + pageSize - 1) / pageSize
	}
	return apiPagination{
		Page:      page,
		PageSize:  pageSize,
		Total:     total,
		TotalPage: totalPage,
	}
}

// requireMethod returns false and writes 405 when request method is unexpected.
func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	httpapi.WriteError(w, http.StatusMethodNotAllowed, http.StatusMethodNotAllowed, "method not allowed")
	return false
}

// parseSubRoute parses `/prefix/{id}/{action?}` style paths.
func parseSubRoute(prefix, rawPath string) (id string, action string, ok bool) {
	subPath := strings.Trim(strings.TrimPrefix(rawPath, prefix), "/")
	if subPath == "" {
		return "", "", false
	}
	parts := strings.Split(subPath, "/")
	id, err := url.PathUnescape(parts[0])
	if err != nil || strings.TrimSpace(id) == "" {
		return "", "", false
	}
	if len(parts) > 2 {
		return "", "", false
	}
	if len(parts) == 2 {
		action = strings.TrimSpace(parts[1])
	}
	return id, action, true
}

// pickActiveSession picks requested session or first item as default.
func pickActiveSession(sessions []CLISessionView, preferredID string) *CLISessionView {
	preferredID = strings.TrimSpace(preferredID)
	if preferredID != "" {
		for i := range sessions {
			if sessions[i].ID == preferredID {
				picked := sessions[i]
				return &picked
			}
		}
	}
	if len(sessions) == 0 {
		return nil
	}
	picked := sessions[0]
	return &picked
}
