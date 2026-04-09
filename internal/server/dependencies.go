package server

import (
	appconfig "auto-code/internal/config"
	"auto-code/internal/domain"
	"auto-code/internal/persistence"
	"auto-code/internal/runtime"
	"auto-code/internal/service"
)

// Domain and configuration type aliases keep app layer focused on orchestration.
type (
	AppConfig                = appconfig.AppConfig
	AutomationConfig         = appconfig.AutomationConfig
	CLIProfile               = appconfig.CLIProfile
	CLIProfileRegistry       = appconfig.CLIProfileRegistry
	SQLiteStore              = persistence.SQLiteStore
	Project                  = domain.Project
	ProjectSummary           = domain.ProjectSummary
	ProjectMutation          = domain.ProjectMutation
	DeleteProjectStats       = domain.DeleteProjectStats
	Requirement              = domain.Requirement
	RequirementMutation      = domain.RequirementMutation
	RequirementWatchdogEvent = domain.RequirementWatchdogEvent
	CurrentRequirementInfo   = domain.CurrentRequirementInfo
	FileNode                 = domain.FileNode
	FileContent              = domain.FileContent
	FileSaveInput            = domain.FileSaveInput
	FileCreateInput          = domain.FileCreateInput
	DirectoryCreateInput     = domain.DirectoryCreateInput
	FileRenameInput          = domain.FileRenameInput
	GitStatus                = domain.GitStatus
	GitFileDiff              = domain.GitFileDiff
	GitBranchList            = domain.GitBranchList
	GitActionResult          = domain.GitActionResult
	FileRevisionConflict     = domain.FileRevisionConflictError
	CLISessionRecord         = domain.CLISessionRecord
	CLISessionSelection      = domain.CLISessionSelection
	CLISessionView           = domain.CLISessionView
	WorkflowRun              = domain.WorkflowRun
	StageRun                 = domain.StageRun
	Artifact                 = domain.Artifact
	ReviewGate               = domain.ReviewGate
	TaskItem                 = domain.TaskItem
	DecisionRequest          = domain.DecisionRequest
	CodeSnapshot             = domain.CodeSnapshot
	ChangeSet                = domain.ChangeSet
	DashboardStats           = domain.DashboardStats
	Activity                 = domain.Activity
	ReviewGateUpdateInput    = domain.ReviewGateUpdateInput
	DecisionResolutionInput  = domain.DecisionResolutionInput
	ProjectService           = service.ProjectService
	RequirementService       = service.RequirementService
	WorkflowService          = service.WorkflowService
	ProjectFileService       = service.ProjectFileService
	CLISessionService        = service.CLISessionService
	CLISessionManager        = runtime.CLISessionManager
	CLISession               = runtime.CLISession
	CLIEvent                 = runtime.CLIEvent
	CLIEventFilter           = runtime.CLIEventFilter
	CLIOutputArchive         = runtime.CLIOutputArchive
	CLIOutputProcessor       = runtime.CLIOutputProcessor
	CLIOutputSnapshotEntry   = runtime.CLIOutputSnapshotEntry
	PollResult               = runtime.PollResult
	RuntimeSideError         = runtime.RuntimeSideError
	SessionSummary           = runtime.SessionSummary
)

const (
	// Requirement status constants re-exported for app layer.
	RequirementStatusPlanning                            = domain.RequirementStatusPlanning
	RequirementStatusRunning                             = domain.RequirementStatusRunning
	RequirementStatusPaused                              = domain.RequirementStatusPaused
	RequirementStatusFailed                              = domain.RequirementStatusFailed
	RequirementStatusDone                                = domain.RequirementStatusDone
	RequirementExecutionModeManual                       = domain.RequirementExecutionModeManual
	RequirementExecutionModeAuto                         = domain.RequirementExecutionModeAuto
	RequirementNoResponseActionNone                      = domain.RequirementNoResponseActionNone
	RequirementNoResponseActionResendRequirement         = domain.RequirementNoResponseActionResendRequirement
	RequirementNoResponseActionCloseAndResendRequirement = domain.RequirementNoResponseActionCloseAndResendRequirement
	RequirementWatchdogTriggerCLIError                   = domain.RequirementWatchdogTriggerCLIError
	RequirementWatchdogTriggerCLIIdle                    = domain.RequirementWatchdogTriggerCLIIdle
	RequirementWatchdogEventStatusPending                = domain.RequirementWatchdogEventStatusPending
	RequirementWatchdogEventStatusSucceeded              = domain.RequirementWatchdogEventStatusSucceeded
	RequirementWatchdogEventStatusFailed                 = domain.RequirementWatchdogEventStatusFailed
	RequirementWatchdogEventStatusSkipped                = domain.RequirementWatchdogEventStatusSkipped
	// CLI session state constants re-exported for app layer.
	CLISessionStateRunning    = domain.CLISessionStateRunning
	CLISessionStateTerminated = domain.CLISessionStateTerminated
)

var (
	// ErrNotFound is re-exported for server-layer HTTP handlers.
	ErrNotFound = persistence.ErrNotFound
	// errSessionNotRunning keeps legacy name while delegating to runtime package error.
	errSessionNotRunning = runtime.ErrSessionNotRunning
)

// LoadAppConfig loads runtime config from environment and app.yaml.
func LoadAppConfig() (AppConfig, error) {
	return appconfig.LoadAppConfig()
}

// NewSQLiteStore initializes sqlite persistence and schema.
func NewSQLiteStore(dbPath, appRoot string) (*SQLiteStore, error) {
	_ = appRoot
	return persistence.NewSQLiteStore(dbPath)
}

// NewProjectService builds project domain service.
func NewProjectService(store *SQLiteStore, appRoot string) *ProjectService {
	return service.NewProjectService(store, appRoot)
}

// NewRequirementService builds requirement domain service.
func NewRequirementService(store *SQLiteStore, projectService *ProjectService) *RequirementService {
	return service.NewRequirementService(store, projectService)
}

// NewWorkflowService builds workflow orchestration service.
func NewWorkflowService(store *SQLiteStore, projectService *ProjectService, artifactRoot string) *WorkflowService {
	return service.NewWorkflowService(store, projectService, artifactRoot)
}

// NewProjectFileService builds project file manager service.
func NewProjectFileService(projectService *ProjectService, requirementService *RequirementService) *ProjectFileService {
	return service.NewProjectFileService(projectService, requirementService)
}

// NewCLISessionService builds CLI session orchestration service with runtime adapter.
func NewCLISessionService(
	store *SQLiteStore,
	projectService *ProjectService,
	requirementService *RequirementService,
	profileRegistry *CLIProfileRegistry,
	cliMgr *CLISessionManager,
	appRoot string,
) *CLISessionService {
	return service.NewCLISessionService(
		store,
		projectService,
		requirementService,
		profileRegistry,
		newCLIRuntimeManagerAdapter(cliMgr),
		appRoot,
	)
}

// NewCLISessionManager builds CLI runtime session manager.
func NewCLISessionManager(defaultCommand string) *CLISessionManager {
	return runtime.NewCLISessionManager(defaultCommand)
}

// NewCLIOutputArchive builds disk archive for CLI output snapshots.
func NewCLIOutputArchive(rootDir string, maxEntries int) (*CLIOutputArchive, error) {
	return runtime.NewCLIOutputArchive(rootDir, maxEntries)
}

// DefaultCLIOutputProcessors returns the standard runtime output processor chain.
func DefaultCLIOutputProcessors() []CLIOutputProcessor {
	return runtime.DefaultCLIOutputProcessors()
}
