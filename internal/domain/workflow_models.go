package domain

import "time"

const (
	WorkflowStatusPending   = "pending"
	WorkflowStatusRunning   = "running"
	WorkflowStatusPaused    = "paused"
	WorkflowStatusCompleted = "completed"
	WorkflowStatusFailed    = "failed"
	WorkflowStatusCanceled  = "canceled"
)

const (
	WorkflowTriggerModeManual    = "manual"
	WorkflowTriggerModeAuto      = "auto"
	WorkflowTriggerModeScheduled = "scheduled"
	WorkflowTriggerModeWebhook   = "webhook"
)

const (
	StageStatusPending        = "pending"
	StageStatusRunning        = "running"
	StageStatusStalled        = "stalled"
	StageStatusVerifying      = "verifying"
	StageStatusAwaitingInput  = "awaiting_input"
	StageStatusAwaitingReview = "awaiting_review"
	StageStatusBlocked        = "blocked"
	StageStatusPartial        = "partial"
	StageStatusCompleted      = "completed"
	StageStatusFailed         = "failed"
	StageStatusInterrupted    = "interrupted"
	StageStatusCanceled       = "canceled"
)

const (
	StageOwnerTypeSystem = "system"
	StageOwnerTypeHuman  = "human"
)

const (
	ArtifactSourceSystem = "system"
	ArtifactSourceAgent  = "agent"
	ArtifactSourceHuman  = "human"
)

const (
	ArtifactTypeRequirementBrief = "requirement_brief"
	ArtifactTypeSolutionDesign   = "solution_design"
	ArtifactTypeTaskBreakdown    = "task_breakdown"
	ArtifactTypeImplementation   = "implementation_plan"
	ArtifactTypeTestPlan         = "test_plan"
	ArtifactTypeReviewRecord     = "review_record"
	ArtifactTypeRuleReport       = "rule_report"
	ArtifactTypeReleaseNote      = "release_note"
	ArtifactTypeChangeSummary    = "change_summary"
	ArtifactTypeReviewPackage    = "review_package"
	ArtifactTypeDiffPatch        = "diff_patch"
)

const (
	ArtifactStatusDraft       = "draft"
	ArtifactStatusGenerated   = "generated"
	ArtifactStatusUnderReview = "under_review"
	ArtifactStatusApproved    = "approved"
	ArtifactStatusRejected    = "rejected"
	ArtifactStatusArchived    = "archived"
)

const (
	ReviewGateTypeDesignReview     = "design_review"
	ReviewGateTypeCodeReview       = "code_review"
	ReviewGateTypeAcceptanceReview = "acceptance_review"
	ReviewGateTypeReleaseApproval  = "release_approval"
)

const (
	ReviewGateStatusPending  = "pending"
	ReviewGateStatusApproved = "approved"
	ReviewGateStatusRejected = "rejected"
	ReviewGateStatusWaived   = "waived"
)

const (
	ReviewGateDecisionPass              = "pass"
	ReviewGateDecisionReject            = "reject"
	ReviewGateDecisionReturnForRevision = "return_for_revision"
)

const (
	TaskStatusPlanned = "planned"
	TaskStatusRunning = "running"
	TaskStatusDone    = "done"
	TaskStatusWaived  = "waived"
	TaskStatusBlocked = "blocked"
	TaskStatusFailed  = "failed"
)

const (
	TaskScopeFile         = "file"
	TaskScopeModule       = "module"
	TaskScopeTest         = "test"
	TaskScopeVerification = "verification"
)

const (
	DecisionRequestTypeClarification    = "clarification"
	DecisionRequestTypeOptionSelection  = "option_selection"
	DecisionRequestTypeRiskConfirmation = "risk_confirmation"
	DecisionRequestTypeScopeConfirm     = "scope_confirmation"
)

const (
	DecisionStatusPending  = "pending"
	DecisionStatusResolved = "resolved"
	DecisionStatusExpired  = "expired"
)

const (
	CodeSnapshotTypeWorkflowStart = "workflow_start"
	CodeSnapshotTypeStageStart    = "stage_start"
	CodeSnapshotTypeStageEnd      = "stage_end"
	CodeSnapshotTypePreReview     = "pre_review"
	CodeSnapshotTypePreCommit     = "pre_commit"
	CodeSnapshotTypePostCommit    = "post_commit"
)

const (
	ChangeScopeStage    = "stage"
	ChangeScopeWorkflow = "workflow"
	ChangeScopeReview   = "review"
	ChangeScopeDelivery = "delivery"
)

const (
	RulePackScopeDesign   = "design"
	RulePackScopeCode     = "code"
	RulePackScopeSystem   = "system"
	RulePackScopeTest     = "test"
	RulePackScopeDelivery = "delivery"
)

const (
	RuleReportStatusPass    = "pass"
	RuleReportStatusWarning = "warning"
	RuleReportStatusFail    = "fail"
	RuleReportStatusWaived  = "waived"
)

const (
	WorkflowStageRequirementIntake = "requirement_intake"
	WorkflowStageRequirementReview = "requirement_analysis"
	WorkflowStageSolutionDesign    = "solution_design"
	WorkflowStageDesignReview      = "design_review"
	WorkflowStageTaskPlanning      = "task_planning"
	WorkflowStageImplementation    = "implementation"
	WorkflowStageCodeDiff          = "code_diff"
	WorkflowStageCodeStandards     = "code_standards"
	WorkflowStageSystemStandards   = "system_standards"
	WorkflowStageTesting           = "testing"
	WorkflowStageAcceptanceReview  = "acceptance_review"
	WorkflowStageGitDelivery       = "git_delivery"
	WorkflowStageReleaseGate       = "release_gate"
	WorkflowStageRelease           = "release"
)

type WorkflowRun struct {
	ID               string
	ProjectID        string
	ProjectName      string
	RequirementID    string
	RequirementTitle string
	Status           string
	CurrentStage     string
	TriggerMode      string
	RiskLevel        string
	StartedAt        time.Time
	EndedAt          *time.Time
	LastError        string
	ResumeFromStage  string
	Progress         int
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type StageRun struct {
	ID             string
	WorkflowRunID  string
	StageName      string
	DisplayName    string
	Status         string
	Attempt        int
	OwnerType      string
	AgentSessionID string
	StartedAt      *time.Time
	EndedAt        *time.Time
	ResultSummary  string
	Artifacts      []string
	RuleReportID   string
	Order          int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Artifact struct {
	ID            string
	ProjectID     string
	RequirementID string
	WorkflowRunID string
	StageRunID    string
	ArtifactType  string
	Title         string
	Path          string
	Version       int
	Status        string
	Source        string
	ContentHash   string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type ReviewGate struct {
	ID            string
	WorkflowRunID string
	StageName     string
	GateType      string
	Status        string
	Reviewer      string
	Decision      string
	Comment       string
	Title         string
	Description   string
	BlockingItems []string
	CreatedAt     time.Time
	ResolvedAt    *time.Time
}

type TaskItem struct {
	ID                 string
	WorkflowRunID      string
	StageRunID         string
	ParentTaskID       string
	Title              string
	Description        string
	Scope              string
	Required           bool
	Status             string
	OwnerSessionID     string
	DependsOn          []string
	EvidenceArtifactID string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type DecisionOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type DecisionRequest struct {
	ID                string
	WorkflowRunID     string
	StageRunID        string
	RequestType       string
	Title             string
	Question          string
	Context           string
	Options           []DecisionOption
	RecommendedOption string
	Blocking          bool
	Status            string
	Decision          string
	Decider           string
	CreatedAt         time.Time
	ResolvedAt        *time.Time
}

type CodeSnapshot struct {
	ID                string
	ProjectID         string
	WorkflowRunID     string
	StageRunID        string
	SnapshotType      string
	GitCommit         string
	GitBranch         string
	WorkspaceRevision string
	FileCount         int
	CreatedAt         time.Time
}

type WorkflowFileChange struct {
	Path      string
	Status    string
	Additions int
	Deletions int
	OldPath   string
}

type ChangeSetFileStats struct {
	Added          int `json:"added"`
	Modified       int `json:"modified"`
	Deleted        int `json:"deleted"`
	Renamed        int `json:"renamed"`
	TotalAdditions int `json:"totalAdditions"`
	TotalDeletions int `json:"totalDeletions"`
}

type ChangeSet struct {
	ID               string
	ProjectID        string
	WorkflowRunID    string
	StageRunID       string
	BaseSnapshotID   string
	TargetSnapshotID string
	ChangeScope      string
	Summary          string
	FileStats        ChangeSetFileStats
	Files            []WorkflowFileChange
	PatchArtifactID  string
	CreatedAt        time.Time
}

type RulePack struct {
	ID          string
	Name        string
	Scope       string
	Version     string
	Enabled     bool
	Blocking    bool
	SourceType  string
	SourceRef   string
	Description string
}

type RuleExecutionReport struct {
	ID                    string
	WorkflowRunID         string
	StageRunID            string
	RulePackID            string
	RulePackName          string
	Status                string
	Score                 *int
	BlockingViolations    int
	NonBlockingViolations int
	OutputPath            string
	CreatedAt             time.Time
}

type DashboardStats struct {
	TotalProjects     int
	TotalRequirements int
	RunningTasks      int
	CompletedTasks    int
	ActiveWorkflows   int
	PendingReviews    int
	PendingDecisions  int
}

type Activity struct {
	ID          string
	Type        string
	Action      string
	Title       string
	Description string
	Timestamp   time.Time
}

type ReviewGateUpdateInput struct {
	Status   string
	Decision string
	Reviewer string
	Comment  string
}

type DecisionResolutionInput struct {
	Decision string
	Decider  string
}
