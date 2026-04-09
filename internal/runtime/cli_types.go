package runtime

import "time"

const (
	// maxSessionOutputBytes defines per-session in-memory output ring buffer size.
	maxSessionOutputBytes = 2 * 1024 * 1024
	// pollChunkBytes limits one poll response chunk size to bound payloads.
	pollChunkBytes = 64 * 1024
)

// PollResult is the incremental output payload returned by /cli/sessions/{id}/poll.
type PollResult struct {
	SessionID  string             `json:"session_id"`
	AgentID    string             `json:"agentid"`
	State      string             `json:"state"`
	Output     string             `json:"output"`
	RawB64     string             `json:"raw_b64,omitempty"`
	SideErrors []RuntimeSideError `json:"side_errors,omitempty"`
	NextOffset int64              `json:"next_offset"`
	Rewind     bool               `json:"rewind"`
	Done       bool               `json:"done"`
	More       bool               `json:"more"`
	ExitCode   *int               `json:"exit_code,omitempty"`
	LastError  string             `json:"last_error,omitempty"`
}

// RuntimeSideError is an internal runtime warning surfaced outside the terminal buffer.
type RuntimeSideError struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// SessionSummary describes runtime state and metadata of one CLI session.
type SessionSummary struct {
	ID          string
	AgentID     string
	Command     string
	State       string
	LaunchMode  string
	WorkDir     string
	ProcessPID  int
	ProcessPGID int
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ExitCode    *int
	LastError   string
}
