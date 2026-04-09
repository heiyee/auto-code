package domain

import (
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	// RequirementStatusPlanning represents a requirement that has not started.
	RequirementStatusPlanning = "planning"
	// RequirementStatusRunning represents a requirement that is currently executing.
	RequirementStatusRunning = "running"
	// RequirementStatusPaused represents a requirement temporarily paused by user.
	RequirementStatusPaused = "paused"
	// RequirementStatusFailed represents a requirement that stopped after unrecoverable automation failure.
	RequirementStatusFailed = "failed"
	// RequirementStatusDone represents a requirement that has finished execution.
	RequirementStatusDone = "done"
)

const (
	// RequirementExecutionModeManual means requirement execution is manually driven.
	RequirementExecutionModeManual = "manual"
	// RequirementExecutionModeAuto means requirement execution is chained automatically.
	RequirementExecutionModeAuto = "auto"
)

const (
	// RequirementNoResponseActionNone keeps watchdog detection passive.
	RequirementNoResponseActionNone = "none"
	// RequirementNoResponseActionResendRequirement resends requirement prompt into current session.
	RequirementNoResponseActionResendRequirement = "resend_requirement"
	// RequirementNoResponseActionCloseAndResendRequirement rebuilds session context before resending prompt.
	RequirementNoResponseActionCloseAndResendRequirement = "close_and_resend_requirement"
)

const (
	// RequirementWatchdogTriggerCLIError represents CLI/session failure conditions.
	RequirementWatchdogTriggerCLIError = "cli_error"
	// RequirementWatchdogTriggerCLIIdle represents healthy session but no-output timeout.
	RequirementWatchdogTriggerCLIIdle = "cli_idle"
)

const (
	// RequirementWatchdogEventStatusPending marks one action waiting to finish.
	RequirementWatchdogEventStatusPending = "pending"
	// RequirementWatchdogEventStatusSucceeded marks one action finished successfully.
	RequirementWatchdogEventStatusSucceeded = "succeeded"
	// RequirementWatchdogEventStatusFailed marks one action finished with failure.
	RequirementWatchdogEventStatusFailed = "failed"
	// RequirementWatchdogEventStatusSkipped marks one action intentionally skipped.
	RequirementWatchdogEventStatusSkipped = "skipped"
)

const (
	// CLISessionStateRunning means the CLI runtime process is alive.
	CLISessionStateRunning = "running"
	// CLISessionStateTerminated means the CLI runtime has terminated.
	CLISessionStateTerminated = "terminated"
)

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9-]+`)

// Project is the domain model for a source-code project.
type Project struct {
	ID               string
	Name             string
	Repository       string
	Branch           string
	WorkDir          string
	AutomationPaused bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// EffectiveWorkDir returns the runtime working directory for a project.
func (p Project) EffectiveWorkDir(appRoot string) string {
	if strings.TrimSpace(p.WorkDir) != "" {
		return p.WorkDir
	}
	return filepath.Join(appRoot, sanitizeProjectDirName(p.Name))
}

// ProjectSummary extends Project with requirement counters.
type ProjectSummary struct {
	Project
	RequirementCount        int
	RunningRequirementCount int
	DoneRequirementCount    int
}

// ProjectMutation contains mutable fields for project create/update operations.
type ProjectMutation struct {
	Name             string
	Repository       string
	Branch           string
	WorkDir          string
	AutomationPaused bool
}

// DeleteProjectStats returns the cascade impact summary for a deleted project.
type DeleteProjectStats struct {
	RequirementCount int
	CLISessionCount  int
}

// Requirement is the domain model for a project-scoped requirement task.
type Requirement struct {
	ID                       string
	ProjectID                string
	ProjectName              string
	ProjectBranch            string
	ProjectWorkDir           string
	SortOrder                int
	Title                    string
	Description              string
	Status                   string
	ExecutionMode            string
	CLIType                  string
	AutoClearSession         bool
	NoResponseTimeoutMinutes int
	NoResponseErrorAction    string
	NoResponseIdleAction     string
	RequiresDesignReview     bool
	RequiresCodeReview       bool
	RequiresAcceptanceReview bool
	RequiresReleaseApproval  bool
	CreatedAt                time.Time
	StartedAt                *time.Time
	EndedAt                  *time.Time
	PromptSentAt             *time.Time
	PromptReplayedAt         *time.Time
	AutoRetryAttempts        int
	LastAutoRetryReason      string
	RetryBudgetExhaustedAt   *time.Time
	UpdatedAt                time.Time
}

// RequirementMutation contains mutable fields for requirement create/update operations.
type RequirementMutation struct {
	ProjectID                string
	Title                    string
	Description              string
	ExecutionMode            string
	CLIType                  string
	AutoClearSession         bool
	NoResponseTimeoutMinutes int
	NoResponseErrorAction    string
	NoResponseIdleAction     string
	RequiresDesignReview     bool
	RequiresCodeReview       bool
	RequiresAcceptanceReview bool
	RequiresReleaseApproval  bool
}

// RequirementWatchdogEvent stores one requirement-level watchdog action record.
type RequirementWatchdogEvent struct {
	ID            string
	RequirementID string
	SessionID     string
	TriggerKind   string
	TriggerReason string
	Action        string
	Status        string
	Detail        string
	CreatedAt     time.Time
	FinishedAt    *time.Time
}

// CLISessionRecord stores persisted metadata for a CLI session.
type CLISessionRecord struct {
	ID               string
	CLIType          string
	Profile          string
	AgentID          string
	ProjectID        string
	ProjectName      string
	RequirementID    string
	RequirementTitle string
	WorkDir          string
	SessionState     string
	LaunchMode       string
	ProcessPID       int
	ProcessPGID      int
	CreatedAt        time.Time
	LastActiveAt     time.Time
}

// CLISessionSelection describes one selected profile to launch for a requirement.
type CLISessionSelection struct {
	CLIType string
	Profile string
}

// CLIProfile defines one account profile loaded from app.yaml.
type CLIProfile struct {
	ID            string            `yaml:"id" json:"id"`
	Name          string            `yaml:"name" json:"name"`
	PreScript     string            `yaml:"pre_script" json:"pre_script,omitempty"`
	ScriptCommand string            `yaml:"script_command" json:"script_command"`
	Env           map[string]string `yaml:"env" json:"env,omitempty"`
	Description   string            `yaml:"description" json:"description,omitempty"`
}

// CLISessionView is the UI-friendly model merged from persistence and runtime state.
type CLISessionView struct {
	ID               string
	CLIType          string
	Profile          string
	ProfileName      string
	AgentID          string
	ProjectID        string
	ProjectName      string
	RequirementID    string
	RequirementTitle string
	WorkDir          string
	State            string
	LaunchMode       string
	ProcessPID       int
	ProcessPGID      int
	CreatedAt        time.Time
	LastActiveAt     time.Time
	ExitCode         *int
	LastError        string
}

// sanitizeProjectDirName converts project names into safe directory names.
func sanitizeProjectDirName(name string) string {
	cleaned := strings.ToLower(strings.TrimSpace(name))
	cleaned = strings.ReplaceAll(cleaned, "_", "-")
	cleaned = strings.ReplaceAll(cleaned, " ", "-")
	cleaned = nonAlphaNum.ReplaceAllString(cleaned, "-")
	cleaned = strings.Trim(cleaned, "-")
	if cleaned == "" {
		return "project"
	}
	return cleaned
}

// IsRequirementStatus reports whether a value is one of supported requirement statuses.
func IsRequirementStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case RequirementStatusPlanning, RequirementStatusRunning, RequirementStatusPaused, RequirementStatusFailed, RequirementStatusDone:
		return true
	default:
		return false
	}
}

// IsRequirementExecutionMode reports whether a value is one of supported requirement execution modes.
func IsRequirementExecutionMode(mode string) bool {
	switch strings.TrimSpace(mode) {
	case RequirementExecutionModeManual, RequirementExecutionModeAuto:
		return true
	default:
		return false
	}
}

// IsRequirementNoResponseAction reports whether a watchdog action enum is supported.
func IsRequirementNoResponseAction(action string) bool {
	switch strings.TrimSpace(action) {
	case RequirementNoResponseActionNone,
		RequirementNoResponseActionResendRequirement,
		RequirementNoResponseActionCloseAndResendRequirement:
		return true
	default:
		return false
	}
}

// IsRequirementNoResponseErrorAction reports whether an error-path action is supported.
func IsRequirementNoResponseErrorAction(action string) bool {
	switch strings.TrimSpace(action) {
	case RequirementNoResponseActionNone, RequirementNoResponseActionCloseAndResendRequirement:
		return true
	default:
		return false
	}
}

// IsRequirementNoResponseIdleAction reports whether an idle-path action is supported.
func IsRequirementNoResponseIdleAction(action string) bool {
	switch strings.TrimSpace(action) {
	case RequirementNoResponseActionNone, RequirementNoResponseActionResendRequirement:
		return true
	default:
		return false
	}
}
