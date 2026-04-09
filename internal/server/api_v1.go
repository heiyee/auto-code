package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type v1DataResponse[T any] struct {
	Success bool   `json:"success"`
	Data    T      `json:"data"`
	Message string `json:"message,omitempty"`
}

type v1ListResponse[T any] struct {
	Success  bool   `json:"success"`
	Data     []T    `json:"data"`
	Total    int    `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
	Message  string `json:"message,omitempty"`
}

type v1ErrorResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type v1Project struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Repository       string    `json:"repository"`
	Branch           string    `json:"branch"`
	WorkDir          string    `json:"workDir"`
	AutomationPaused bool      `json:"automationPaused"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type v1Requirement struct {
	ID                       string                      `json:"id"`
	ProjectID                string                      `json:"projectId"`
	SortOrder                int                         `json:"sortOrder"`
	Title                    string                      `json:"title"`
	Description              string                      `json:"description"`
	Status                   string                      `json:"status"`
	ExecutionMode            string                      `json:"executionMode"`
	CLIType                  string                      `json:"cliType"`
	AutoClearSession         bool                        `json:"autoClearSession"`
	NoResponseTimeoutMinutes int                         `json:"noResponseTimeoutMinutes"`
	NoResponseErrorAction    string                      `json:"noResponseErrorAction"`
	NoResponseIdleAction     string                      `json:"noResponseIdleAction"`
	RequiresDesignReview     bool                        `json:"requiresDesignReview"`
	RequiresCodeReview       bool                        `json:"requiresCodeReview"`
	RequiresAcceptanceReview bool                        `json:"requiresAcceptanceReview"`
	RequiresReleaseApproval  bool                        `json:"requiresReleaseApproval"`
	CreatedAt                time.Time                   `json:"createdAt"`
	StartedAt                *time.Time                  `json:"startedAt,omitempty"`
	EndedAt                  *time.Time                  `json:"endedAt,omitempty"`
	PromptSentAt             *time.Time                  `json:"promptSentAt,omitempty"`
	PromptReplayedAt         *time.Time                  `json:"promptReplayedAt,omitempty"`
	AutoRetryAttempts        int                         `json:"autoRetryAttempts"`
	RetryBudget              int                         `json:"retryBudget"`
	RetryBudgetExhausted     bool                        `json:"retryBudgetExhausted"`
	ProjectName              string                      `json:"projectName,omitempty"`
	ExecutionState           string                      `json:"executionState,omitempty"`
	ExecutionReason          string                      `json:"executionReason,omitempty"`
	LastOutputAt             *time.Time                  `json:"lastOutputAt,omitempty"`
	LastWatchdogEvent        *v1RequirementWatchdogEvent `json:"lastWatchdogEvent,omitempty"`
	UpdatedAt                time.Time                   `json:"updatedAt"`
}

type v1RequirementWatchdogEvent struct {
	TriggerKind   string     `json:"triggerKind"`
	TriggerReason string     `json:"triggerReason"`
	Action        string     `json:"action"`
	Status        string     `json:"status"`
	Detail        string     `json:"detail,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	FinishedAt    *time.Time `json:"finishedAt,omitempty"`
}

type v1Session struct {
	ID               string     `json:"id"`
	CLIType          string     `json:"cliType"`
	Profile          string     `json:"profile"`
	ProfileName      string     `json:"profileName,omitempty"`
	AgentID          string     `json:"agentId"`
	ProjectID        string     `json:"projectId"`
	RequirementID    string     `json:"requirementId"`
	WorkDir          string     `json:"workDir"`
	SessionState     string     `json:"sessionState"`
	ProcessPID       int        `json:"processPID"`
	CreatedAt        time.Time  `json:"createdAt"`
	LastActiveAt     time.Time  `json:"lastActiveAt"`
	ExitCode         *int       `json:"exitCode,omitempty"`
	LastError        string     `json:"lastError,omitempty"`
	ProjectName      string     `json:"projectName,omitempty"`
	RequirementTitle string     `json:"requirementTitle,omitempty"`
	ExecutionState   string     `json:"executionState,omitempty"`
	ExecutionReason  string     `json:"executionReason,omitempty"`
	LastOutputAt     *time.Time `json:"lastOutputAt,omitempty"`
}

type v1Workflow struct {
	ID               string     `json:"id"`
	ProjectID        string     `json:"projectId"`
	RequirementID    string     `json:"requirementId"`
	Status           string     `json:"status"`
	CurrentStage     string     `json:"currentStage"`
	TriggerMode      string     `json:"triggerMode"`
	RiskLevel        string     `json:"riskLevel"`
	StartedAt        time.Time  `json:"startedAt"`
	EndedAt          *time.Time `json:"endedAt,omitempty"`
	LastError        string     `json:"lastError,omitempty"`
	ResumeFromStage  string     `json:"resumeFromStage,omitempty"`
	ProjectName      string     `json:"projectName,omitempty"`
	RequirementTitle string     `json:"requirementTitle,omitempty"`
	Progress         int        `json:"progress"`
}

type v1StageRun struct {
	ID             string     `json:"id"`
	WorkflowRunID  string     `json:"workflowRunId"`
	StageName      string     `json:"stageName"`
	DisplayName    string     `json:"displayName"`
	Status         string     `json:"status"`
	Attempt        int        `json:"attempt"`
	OwnerType      string     `json:"ownerType"`
	AgentSessionID string     `json:"agentSessionId,omitempty"`
	StartedAt      *time.Time `json:"startedAt,omitempty"`
	EndedAt        *time.Time `json:"endedAt,omitempty"`
	ResultSummary  string     `json:"resultSummary,omitempty"`
	Artifacts      []string   `json:"artifacts,omitempty"`
	RuleReportID   string     `json:"ruleReportId,omitempty"`
	Order          int        `json:"order"`
}

type v1WorkflowDetail struct {
	v1Workflow
	Stages []v1StageRun `json:"stages"`
}

type v1Artifact struct {
	ID            string    `json:"id"`
	ProjectID     string    `json:"projectId"`
	RequirementID string    `json:"requirementId"`
	WorkflowRunID string    `json:"workflowRunId"`
	StageRunID    string    `json:"stageRunId,omitempty"`
	ArtifactType  string    `json:"artifactType"`
	Title         string    `json:"title"`
	Path          string    `json:"path"`
	Version       int       `json:"version"`
	Status        string    `json:"status"`
	Source        string    `json:"source"`
	ContentHash   string    `json:"contentHash,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type v1ReviewGate struct {
	ID            string     `json:"id"`
	WorkflowRunID string     `json:"workflowRunId"`
	StageName     string     `json:"stageName"`
	GateType      string     `json:"gateType"`
	Status        string     `json:"status"`
	Reviewer      string     `json:"reviewer,omitempty"`
	Decision      string     `json:"decision,omitempty"`
	Comment       string     `json:"comment,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	ResolvedAt    *time.Time `json:"resolvedAt,omitempty"`
	Title         string     `json:"title,omitempty"`
	Description   string     `json:"description,omitempty"`
	BlockingItems []string   `json:"blockingItems,omitempty"`
}

type v1TaskItem struct {
	ID                 string    `json:"id"`
	WorkflowRunID      string    `json:"workflowRunId"`
	StageRunID         string    `json:"stageRunId,omitempty"`
	ParentTaskID       string    `json:"parentTaskId,omitempty"`
	Title              string    `json:"title"`
	Description        string    `json:"description"`
	Scope              string    `json:"scope"`
	Required           bool      `json:"required"`
	Status             string    `json:"status"`
	OwnerSessionID     string    `json:"ownerSessionId,omitempty"`
	DependsOn          []string  `json:"dependsOn,omitempty"`
	EvidenceArtifactID string    `json:"evidenceArtifactId,omitempty"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

type v1DecisionOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type v1DecisionRequest struct {
	ID                string             `json:"id"`
	WorkflowRunID     string             `json:"workflowRunId"`
	StageRunID        string             `json:"stageRunId,omitempty"`
	RequestType       string             `json:"requestType"`
	Title             string             `json:"title"`
	Question          string             `json:"question"`
	Context           string             `json:"context,omitempty"`
	Options           []v1DecisionOption `json:"options,omitempty"`
	RecommendedOption string             `json:"recommendedOption,omitempty"`
	Blocking          bool               `json:"blocking"`
	Status            string             `json:"status"`
	Decision          string             `json:"decision,omitempty"`
	Decider           string             `json:"decider,omitempty"`
	CreatedAt         time.Time          `json:"createdAt"`
	ResolvedAt        *time.Time         `json:"resolvedAt,omitempty"`
}

type v1CodeSnapshot struct {
	ID                string    `json:"id"`
	ProjectID         string    `json:"projectId"`
	WorkflowRunID     string    `json:"workflowRunId"`
	StageRunID        string    `json:"stageRunId,omitempty"`
	SnapshotType      string    `json:"snapshotType"`
	GitCommit         string    `json:"gitCommit,omitempty"`
	GitBranch         string    `json:"gitBranch,omitempty"`
	WorkspaceRevision string    `json:"workspaceRevision,omitempty"`
	FileCount         int       `json:"fileCount"`
	CreatedAt         time.Time `json:"createdAt"`
}

type v1WorkflowFileChange struct {
	Path      string `json:"path"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	OldPath   string `json:"oldPath,omitempty"`
}

type v1ChangeSetStats struct {
	Added          int `json:"added"`
	Modified       int `json:"modified"`
	Deleted        int `json:"deleted"`
	Renamed        int `json:"renamed"`
	TotalAdditions int `json:"totalAdditions"`
	TotalDeletions int `json:"totalDeletions"`
}

type v1ChangeSet struct {
	ID               string                 `json:"id"`
	ProjectID        string                 `json:"projectId"`
	WorkflowRunID    string                 `json:"workflowRunId"`
	StageRunID       string                 `json:"stageRunId,omitempty"`
	BaseSnapshotID   string                 `json:"baseSnapshotId"`
	TargetSnapshotID string                 `json:"targetSnapshotId"`
	ChangeScope      string                 `json:"changeScope"`
	Summary          string                 `json:"summary"`
	FileStats        v1ChangeSetStats       `json:"fileStats"`
	Files            []v1WorkflowFileChange `json:"files"`
	PatchArtifactID  string                 `json:"patchArtifactId,omitempty"`
	CreatedAt        time.Time              `json:"createdAt"`
}

type v1DashboardStats struct {
	TotalProjects     int `json:"totalProjects"`
	TotalRequirements int `json:"totalRequirements"`
	RunningTasks      int `json:"runningTasks"`
	CompletedTasks    int `json:"completedTasks"`
	ActiveWorkflows   int `json:"activeWorkflows"`
	PendingReviews    int `json:"pendingReviews"`
	PendingDecisions  int `json:"pendingDecisions"`
}

type v1Activity struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Action      string    `json:"action"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Timestamp   time.Time `json:"timestamp"`
}

type v1ProjectMutationInput struct {
	Name             *string `json:"name"`
	Repository       *string `json:"repository"`
	Branch           *string `json:"branch"`
	WorkDir          *string `json:"workDir"`
	AutomationPaused *bool   `json:"automationPaused"`
}

type v1RequirementMutationInput struct {
	ProjectID                *string `json:"projectId"`
	SortOrder                *int    `json:"sortOrder"`
	Title                    *string `json:"title"`
	Description              *string `json:"description"`
	ExecutionMode            *string `json:"executionMode"`
	CLIType                  *string `json:"cliType"`
	AutoClearSession         *bool   `json:"autoClearSession"`
	NoResponseTimeoutMinutes *int    `json:"noResponseTimeoutMinutes"`
	NoResponseErrorAction    *string `json:"noResponseErrorAction"`
	NoResponseIdleAction     *string `json:"noResponseIdleAction"`
	RequiresDesignReview     *bool   `json:"requiresDesignReview"`
	RequiresCodeReview       *bool   `json:"requiresCodeReview"`
	RequiresAcceptanceReview *bool   `json:"requiresAcceptanceReview"`
	RequiresReleaseApproval  *bool   `json:"requiresReleaseApproval"`
	Status                   *string `json:"status"`
}

type v1ReviewUpdateInput struct {
	Status   string `json:"status"`
	Decision string `json:"decision"`
	Reviewer string `json:"reviewer"`
	Comment  string `json:"comment"`
}

type v1DecisionResolveInput struct {
	Decision string `json:"decision"`
	Decider  string `json:"decider"`
}

func (a *App) handleAPIV1DashboardStats(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	stats, err := a.workflowSvc.DashboardStats()
	if err != nil {
		writeV1Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeV1Data(w, http.StatusOK, toV1DashboardStats(stats))
}

func (a *App) handleAPIV1DashboardActivities(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	limit, _ := parseOptionalInt(r.URL.Query().Get("limit"))
	items, err := a.workflowSvc.DashboardActivities(limit)
	if err != nil {
		writeV1Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	result := make([]v1Activity, 0, len(items))
	for _, item := range items {
		result = append(result, toV1Activity(item))
	}
	writeV1Data(w, http.StatusOK, result)
}

func (a *App) handleAPIV1Projects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		page, pageSize := parseV1PageQuery(r)
		search := strings.TrimSpace(r.URL.Query().Get("search"))
		items, err := a.projectSvc.List()
		if err != nil {
			writeV1Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		filtered := filterProjectSummaries(items, search, "")
		pageItems, total := paginateSlice(filtered, page, pageSize)
		result := make([]v1Project, 0, len(pageItems))
		for _, item := range pageItems {
			result = append(result, toV1Project(item.Project))
		}
		writeV1List(w, http.StatusOK, result, total, page, pageSize)
	case http.MethodPost:
		var input v1ProjectMutationInput
		if err := decodeJSONBody(r, &input); err != nil {
			writeV1Error(w, http.StatusBadRequest, err.Error())
			return
		}
		project, err := a.projectSvc.Create(ProjectMutation{
			Name:             derefString(input.Name),
			Repository:       derefString(input.Repository),
			Branch:           derefString(input.Branch),
			WorkDir:          derefString(input.WorkDir),
			AutomationPaused: derefBool(input.AutomationPaused),
		})
		if err != nil {
			writeV1Error(w, http.StatusBadRequest, err.Error())
			return
		}
		writeV1Data(w, http.StatusCreated, toV1Project(*project))
	default:
		writeV1Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *App) handleAPIV1ProjectByID(w http.ResponseWriter, r *http.Request) {
	projectID, action, ok := parseSubRoute("/api/v1/projects/", r.URL.Path)
	if !ok || action != "" {
		writeV1Error(w, http.StatusNotFound, "resource not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		project, err := a.projectSvc.Get(projectID)
		if err != nil {
			writeV1ProjectError(w, err)
			return
		}
		writeV1Data(w, http.StatusOK, toV1Project(*project))
	case http.MethodPut:
		project, err := a.projectSvc.Get(projectID)
		if err != nil {
			writeV1ProjectError(w, err)
			return
		}
		var input v1ProjectMutationInput
		if err := decodeJSONBody(r, &input); err != nil {
			writeV1Error(w, http.StatusBadRequest, err.Error())
			return
		}
		updated, err := a.projectSvc.Update(projectID, ProjectMutation{
			Name:             firstNonEmpty(derefString(input.Name), project.Name),
			Repository:       firstNonEmpty(derefString(input.Repository), project.Repository),
			Branch:           firstNonEmpty(derefString(input.Branch), project.Branch),
			WorkDir:          firstNonEmptyAllowEmpty(input.WorkDir, project.WorkDir),
			AutomationPaused: pickBool(input.AutomationPaused, project.AutomationPaused),
		})
		if err != nil {
			writeV1Error(w, http.StatusBadRequest, err.Error())
			return
		}
		if a.requirementAuto != nil {
			a.requirementAuto.SyncProject(updated.ID, "")
		}
		writeV1Data(w, http.StatusOK, toV1Project(*updated))
	case http.MethodDelete:
		a.shutdownProjectCLIRuntimes(projectID)
		if _, err := a.projectSvc.Delete(projectID); err != nil {
			writeV1ProjectError(w, err)
			return
		}
		writeV1OK(w, http.StatusOK)
	default:
		writeV1Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *App) handleAPIV1Requirements(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		page, pageSize := parseV1PageQuery(r)
		projectID := strings.TrimSpace(r.URL.Query().Get("projectId"))
		status := normalizeRequirementStatusFilter(r.URL.Query().Get("status"))
		items, err := a.requirementSvc.List(projectID)
		if err != nil {
			writeV1Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		filtered := filterRequirements(items, status, "")
		pageItems, total := paginateSlice(filtered, page, pageSize)
		result := make([]v1Requirement, 0, len(pageItems))
		for _, item := range pageItems {
			result = append(result, a.toV1Requirement(item))
		}
		writeV1List(w, http.StatusOK, result, total, page, pageSize)
	case http.MethodPost:
		var input v1RequirementMutationInput
		if err := decodeJSONBody(r, &input); err != nil {
			writeV1Error(w, http.StatusBadRequest, err.Error())
			return
		}
		requirement, err := a.requirementSvc.Create(RequirementMutation{
			ProjectID:                derefString(input.ProjectID),
			Title:                    derefString(input.Title),
			Description:              derefString(input.Description),
			ExecutionMode:            derefString(input.ExecutionMode),
			CLIType:                  derefString(input.CLIType),
			AutoClearSession:         derefBool(input.AutoClearSession),
			NoResponseTimeoutMinutes: derefInt(input.NoResponseTimeoutMinutes),
			NoResponseErrorAction:    derefString(input.NoResponseErrorAction),
			NoResponseIdleAction:     derefString(input.NoResponseIdleAction),
			RequiresDesignReview:     derefBool(input.RequiresDesignReview),
			RequiresCodeReview:       derefBool(input.RequiresCodeReview),
			RequiresAcceptanceReview: derefBool(input.RequiresAcceptanceReview),
			RequiresReleaseApproval:  derefBool(input.RequiresReleaseApproval),
		})
		if err != nil {
			writeV1Error(w, http.StatusBadRequest, err.Error())
			return
		}
		if a.requirementAuto != nil {
			a.requirementAuto.SyncProject(requirement.ProjectID, "")
			if latest, latestErr := a.requirementSvc.Get(requirement.ID); latestErr == nil {
				requirement = latest
			}
		}
		writeV1Data(w, http.StatusCreated, a.toV1Requirement(*requirement))
	default:
		writeV1Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *App) handleAPIV1RequirementByID(w http.ResponseWriter, r *http.Request) {
	requirementID, action, ok := parseSubRoute("/api/v1/requirements/", r.URL.Path)
	if !ok || action != "" {
		writeV1Error(w, http.StatusNotFound, "resource not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		requirement, err := a.requirementSvc.Get(requirementID)
		if err != nil {
			writeV1RequirementError(w, err)
			return
		}
		writeV1Data(w, http.StatusOK, a.toV1Requirement(*requirement))
	case http.MethodPut:
		current, err := a.requirementSvc.Get(requirementID)
		if err != nil {
			writeV1RequirementError(w, err)
			return
		}
		var input v1RequirementMutationInput
		if err := decodeJSONBody(r, &input); err != nil {
			writeV1Error(w, http.StatusBadRequest, err.Error())
			return
		}
		updated, err := a.requirementSvc.Update(requirementID, RequirementMutation{
			ProjectID:                firstNonEmpty(derefString(input.ProjectID), current.ProjectID),
			Title:                    firstNonEmpty(derefString(input.Title), current.Title),
			Description:              firstNonEmpty(derefString(input.Description), current.Description),
			ExecutionMode:            firstNonEmpty(derefString(input.ExecutionMode), current.ExecutionMode),
			CLIType:                  firstNonEmpty(derefString(input.CLIType), current.CLIType),
			AutoClearSession:         pickBool(input.AutoClearSession, current.AutoClearSession),
			NoResponseTimeoutMinutes: pickInt(input.NoResponseTimeoutMinutes, current.NoResponseTimeoutMinutes),
			NoResponseErrorAction:    firstNonEmpty(derefString(input.NoResponseErrorAction), current.NoResponseErrorAction),
			NoResponseIdleAction:     firstNonEmpty(derefString(input.NoResponseIdleAction), current.NoResponseIdleAction),
			RequiresDesignReview:     pickBool(input.RequiresDesignReview, current.RequiresDesignReview),
			RequiresCodeReview:       pickBool(input.RequiresCodeReview, current.RequiresCodeReview),
			RequiresAcceptanceReview: pickBool(input.RequiresAcceptanceReview, current.RequiresAcceptanceReview),
			RequiresReleaseApproval:  pickBool(input.RequiresReleaseApproval, current.RequiresReleaseApproval),
		})
		if err != nil {
			writeV1Error(w, http.StatusBadRequest, err.Error())
			return
		}
		if input.Status != nil {
			action := requirementTransitionAction(updated.Status, strings.TrimSpace(strings.ToLower(*input.Status)))
			if action == "" && strings.TrimSpace(strings.ToLower(*input.Status)) != updated.Status {
				writeV1Error(w, http.StatusBadRequest, "unsupported requirement status transition")
				return
			}
			if action != "" {
				updated, err = a.requirementSvc.Transition(requirementID, action)
				if err != nil {
					writeV1Error(w, http.StatusBadRequest, err.Error())
					return
				}
			}
		}
		if input.SortOrder != nil {
			updated, err = a.requirementSvc.Move(requirementID, *input.SortOrder)
			if err != nil {
				writeV1Error(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		if a.requirementAuto != nil {
			if current.ProjectID != updated.ProjectID {
				a.requirementAuto.SyncProject(current.ProjectID, "")
			}
			a.requirementAuto.SyncProject(updated.ProjectID, "")
			if latest, latestErr := a.requirementSvc.Get(requirementID); latestErr == nil {
				updated = latest
			}
		}
		writeV1Data(w, http.StatusOK, a.toV1Requirement(*updated))
	case http.MethodDelete:
		current, _ := a.requirementSvc.Get(requirementID)
		if err := a.requirementSvc.Delete(requirementID); err != nil {
			writeV1RequirementError(w, err)
			return
		}
		if current != nil && a.requirementAuto != nil {
			a.requirementAuto.SyncProject(current.ProjectID, "")
		}
		writeV1OK(w, http.StatusOK)
	default:
		writeV1Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *App) handleAPIV1Sessions(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	page, pageSize := parseV1PageQuery(r)
	state := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("state")))
	items, err := a.cliSessionSvc.ListAllViews("")
	if err != nil {
		writeV1Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	filtered := make([]CLISessionView, 0, len(items))
	for _, item := range items {
		if state != "" && strings.ToLower(strings.TrimSpace(item.State)) != state {
			continue
		}
		filtered = append(filtered, item)
	}
	filtered = a.decorateSessionRequirementBindings(filtered)
	pageItems, total := paginateSlice(filtered, page, pageSize)
	result := make([]v1Session, 0, len(pageItems))
	for _, item := range pageItems {
		result = append(result, a.toV1Session(item))
	}
	writeV1List(w, http.StatusOK, result, total, page, pageSize)
}

func (a *App) handleAPIV1SessionByID(w http.ResponseWriter, r *http.Request) {
	sessionID, action, ok := parseSubRoute("/api/v1/sessions/", r.URL.Path)
	if !ok || action != "" {
		writeV1Error(w, http.StatusNotFound, "resource not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		view, err := a.cliSessionSvc.GetView(sessionID)
		if err != nil {
			writeV1SessionError(w, err)
			return
		}
		writeV1Data(w, http.StatusOK, a.toV1Session(a.decorateSessionRequirementBinding(*view)))
	case http.MethodDelete:
		if runtimeSession, ok := a.cliMgr.Get(sessionID); ok {
			_ = runtimeSession.Terminate()
			_ = a.cliMgr.Destroy(sessionID)
		}
		if events := a.cliMgr.Events(); events != nil {
			events.ClearSession(sessionID, "")
		}
		_ = a.cliSessionSvc.MarkTerminated(sessionID)
		if a.requirementAuto != nil {
			a.requirementAuto.clearSessionDetectionState(sessionID)
		}
		if err := a.cliSessionSvc.DeleteRecord(sessionID); err != nil {
			writeV1SessionError(w, err)
			return
		}
		if a.cliArchive != nil {
			_ = a.cliArchive.DeleteSession(sessionID)
		}
		writeV1OK(w, http.StatusOK)
	default:
		writeV1Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *App) handleAPIV1Workflows(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	page, pageSize := parseV1PageQuery(r)
	status := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("status")))
	projectID := strings.TrimSpace(r.URL.Query().Get("projectId"))
	requirementID := strings.TrimSpace(r.URL.Query().Get("requirementId"))
	items, err := a.workflowSvc.ListWorkflows(projectID, status, requirementID)
	if err != nil {
		writeV1Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	pageItems, total := paginateSlice(items, page, pageSize)
	result := make([]v1Workflow, 0, len(pageItems))
	for _, item := range pageItems {
		result = append(result, toV1Workflow(item))
	}
	writeV1List(w, http.StatusOK, result, total, page, pageSize)
}

func (a *App) handleAPIV1WorkflowByID(w http.ResponseWriter, r *http.Request) {
	workflowID, action, ok := parseSubRoute("/api/v1/workflows/", r.URL.Path)
	if !ok {
		writeV1Error(w, http.StatusNotFound, "resource not found")
		return
	}
	switch action {
	case "":
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		if err := a.workflowSvc.RefreshWorkflow(workflowID); err != nil && !errors.Is(err, ErrNotFound) {
			writeV1Error(w, http.StatusBadRequest, err.Error())
			return
		}
		workflow, err := a.workflowSvc.GetWorkflow(workflowID)
		if err != nil {
			writeV1WorkflowError(w, err)
			return
		}
		stages, err := a.workflowSvc.ListStageRuns(workflowID)
		if err != nil {
			writeV1Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		result := v1WorkflowDetail{v1Workflow: toV1Workflow(*workflow), Stages: make([]v1StageRun, 0, len(stages))}
		for _, stage := range stages {
			result.Stages = append(result.Stages, toV1StageRun(stage))
		}
		writeV1Data(w, http.StatusOK, result)
	case "stages":
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		stages, err := a.workflowSvc.ListStageRuns(workflowID)
		if err != nil {
			writeV1WorkflowError(w, err)
			return
		}
		result := make([]v1StageRun, 0, len(stages))
		for _, stage := range stages {
			result = append(result, toV1StageRun(stage))
		}
		writeV1Data(w, http.StatusOK, result)
	case "tasks":
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		items, err := a.workflowSvc.ListTasks(workflowID)
		if err != nil {
			writeV1WorkflowError(w, err)
			return
		}
		result := make([]v1TaskItem, 0, len(items))
		for _, item := range items {
			result = append(result, toV1TaskItem(item))
		}
		writeV1Data(w, http.StatusOK, result)
	default:
		writeV1Error(w, http.StatusNotFound, "resource not found")
	}
}

func (a *App) handleAPIV1Reviews(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	status := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("status")))
	workflowID := strings.TrimSpace(r.URL.Query().Get("workflowRunId"))
	items, err := a.workflowSvc.ListReviews(status, workflowID)
	if err != nil {
		writeV1Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	result := make([]v1ReviewGate, 0, len(items))
	for _, item := range items {
		result = append(result, toV1ReviewGate(item))
	}
	writeV1Data(w, http.StatusOK, result)
}

func (a *App) handleAPIV1ReviewByID(w http.ResponseWriter, r *http.Request) {
	reviewID, action, ok := parseSubRoute("/api/v1/reviews/", r.URL.Path)
	if !ok || action != "" {
		writeV1Error(w, http.StatusNotFound, "resource not found")
		return
	}
	if !requireMethod(w, r, http.MethodPut) {
		return
	}
	var input v1ReviewUpdateInput
	if err := decodeJSONBody(r, &input); err != nil {
		writeV1Error(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := a.workflowSvc.UpdateReview(reviewID, ReviewGateUpdateInput{
		Status:   input.Status,
		Decision: input.Decision,
		Reviewer: input.Reviewer,
		Comment:  input.Comment,
	})
	if err != nil {
		writeV1WorkflowError(w, err)
		return
	}
	a.tryResumeAutoRequirement(item.WorkflowRunID)
	writeV1Data(w, http.StatusOK, toV1ReviewGate(*item))
}

func (a *App) handleAPIV1ChangeSets(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	workflowID := strings.TrimSpace(r.URL.Query().Get("workflowRunId"))
	items, err := a.workflowSvc.ListChangeSets(workflowID)
	if err != nil {
		writeV1Error(w, http.StatusBadRequest, err.Error())
		return
	}
	result := make([]v1ChangeSet, 0, len(items))
	for _, item := range items {
		result = append(result, toV1ChangeSet(item))
	}
	writeV1Data(w, http.StatusOK, result)
}

func (a *App) handleAPIV1ChangeSetByID(w http.ResponseWriter, r *http.Request) {
	changeSetID, action, ok := parseSubRoute("/api/v1/changesets/", r.URL.Path)
	if !ok || action != "" {
		writeV1Error(w, http.StatusNotFound, "resource not found")
		return
	}
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	item, err := a.workflowSvc.GetChangeSet(changeSetID)
	if err != nil {
		writeV1WorkflowError(w, err)
		return
	}
	writeV1Data(w, http.StatusOK, toV1ChangeSet(*item))
}

func (a *App) handleAPIV1Artifacts(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	workflowID := strings.TrimSpace(r.URL.Query().Get("workflowRunId"))
	items, err := a.workflowSvc.ListArtifacts(workflowID)
	if err != nil {
		writeV1Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	result := make([]v1Artifact, 0, len(items))
	for _, item := range items {
		result = append(result, toV1Artifact(item))
	}
	writeV1Data(w, http.StatusOK, result)
}

type v1StageUpdateInput struct {
	Status        string `json:"status"`
	ResultSummary string `json:"resultSummary"`
}

func (a *App) handleAPIV1StageByID(w http.ResponseWriter, r *http.Request) {
	stageID, action, ok := parseSubRoute("/api/v1/stages/", r.URL.Path)
	if !ok || action != "" {
		writeV1Error(w, http.StatusNotFound, "resource not found")
		return
	}
	if !requireMethod(w, r, http.MethodPut) {
		return
	}
	var input v1StageUpdateInput
	if err := decodeJSONBody(r, &input); err != nil {
		writeV1Error(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := a.workflowSvc.UpdateStageManually(stageID, input.Status, input.ResultSummary)
	if err != nil {
		writeV1WorkflowError(w, err)
		return
	}
	writeV1Data(w, http.StatusOK, toV1StageRun(*updated))
}

func (a *App) handleAPIV1ArtifactByID(w http.ResponseWriter, r *http.Request) {
	artifactID, action, ok := parseSubRoute("/api/v1/artifacts/", r.URL.Path)
	if !ok {
		writeV1Error(w, http.StatusNotFound, "resource not found")
		return
	}
	switch action {
	case "content":
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		content, err := a.workflowSvc.GetArtifactContent(artifactID)
		if err != nil {
			writeV1WorkflowError(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(content))
	default:
		writeV1Error(w, http.StatusNotFound, "resource not found")
	}
}

func (a *App) handleAPIV1Decisions(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	workflowID := strings.TrimSpace(r.URL.Query().Get("workflowRunId"))
	items, err := a.workflowSvc.ListDecisions(workflowID)
	if err != nil {
		writeV1Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	result := make([]v1DecisionRequest, 0, len(items))
	for _, item := range items {
		result = append(result, toV1DecisionRequest(item))
	}
	writeV1Data(w, http.StatusOK, result)
}

func (a *App) handleAPIV1DecisionByID(w http.ResponseWriter, r *http.Request) {
	decisionID, action, ok := parseSubRoute("/api/v1/decisions/", r.URL.Path)
	if !ok || action != "" {
		writeV1Error(w, http.StatusNotFound, "resource not found")
		return
	}
	if !requireMethod(w, r, http.MethodPut) {
		return
	}
	var input v1DecisionResolveInput
	if err := decodeJSONBody(r, &input); err != nil {
		writeV1Error(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := a.workflowSvc.ResolveDecision(decisionID, DecisionResolutionInput{
		Decision: input.Decision,
		Decider:  input.Decider,
	})
	if err != nil {
		writeV1WorkflowError(w, err)
		return
	}
	a.tryResumeAutoRequirement(item.WorkflowRunID)
	writeV1Data(w, http.StatusOK, toV1DecisionRequest(*item))
}

func (a *App) handleAPIV1Snapshots(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	workflowID := strings.TrimSpace(r.URL.Query().Get("workflowRunId"))
	items, err := a.workflowSvc.ListSnapshots(workflowID)
	if err != nil {
		writeV1Error(w, http.StatusBadRequest, err.Error())
		return
	}
	result := make([]v1CodeSnapshot, 0, len(items))
	for _, item := range items {
		result = append(result, toV1CodeSnapshot(item))
	}
	writeV1Data(w, http.StatusOK, result)
}

func writeV1Data[T any](w http.ResponseWriter, status int, data T) {
	writeV1JSON(w, status, v1DataResponse[T]{Success: true, Data: data})
}

func writeV1List[T any](w http.ResponseWriter, status int, data []T, total, page, pageSize int) {
	writeV1JSON(w, status, v1ListResponse[T]{
		Success:  true,
		Data:     data,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

func writeV1OK(w http.ResponseWriter, status int) {
	writeV1JSON(w, status, map[string]bool{"success": true})
}

func writeV1Error(w http.ResponseWriter, status int, message string) {
	writeV1JSON(w, status, v1ErrorResponse{Success: false, Message: message})
}

func writeV1JSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func decodeJSONBody(r *http.Request, target any) error {
	if !strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		return errors.New("content type must be application/json")
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid json body")
	}
	return nil
}

func parseV1PageQuery(r *http.Request) (int, int) {
	return parsePageQuery(r.URL.Query().Get("page"), r.URL.Query().Get("pageSize"))
}

func parseOptionalInt(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	return strconv.Atoi(raw)
}

func paginateSlice[T any](items []T, page, pageSize int) ([]T, int) {
	total := len(items)
	offset := (page - 1) * pageSize
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	end := offset + pageSize
	if end > total {
		end = total
	}
	return items[offset:end], total
}

func requirementTransitionAction(currentStatus, desiredStatus string) string {
	currentStatus = strings.TrimSpace(strings.ToLower(currentStatus))
	desiredStatus = strings.TrimSpace(strings.ToLower(desiredStatus))
	if currentStatus == desiredStatus || desiredStatus == "" {
		return ""
	}
	switch {
	case (currentStatus == RequirementStatusPlanning || currentStatus == RequirementStatusPaused || currentStatus == RequirementStatusFailed) && desiredStatus == RequirementStatusRunning:
		return "start"
	case currentStatus == RequirementStatusRunning && desiredStatus == RequirementStatusPaused:
		return "pause"
	case (currentStatus == RequirementStatusRunning || currentStatus == RequirementStatusPaused) && desiredStatus == RequirementStatusFailed:
		return "fail"
	case currentStatus == RequirementStatusFailed && desiredStatus == RequirementStatusDone:
		return "skip"
	case (currentStatus == RequirementStatusRunning || currentStatus == RequirementStatusPaused) && desiredStatus == RequirementStatusDone:
		return "done"
	default:
		return ""
	}
}

func toV1Project(project Project) v1Project {
	return v1Project{
		ID:               project.ID,
		Name:             project.Name,
		Repository:       project.Repository,
		Branch:           project.Branch,
		WorkDir:          project.WorkDir,
		AutomationPaused: project.AutomationPaused,
		CreatedAt:        project.CreatedAt,
		UpdatedAt:        project.UpdatedAt,
	}
}

func (a *App) toV1Requirement(item Requirement) v1Requirement {
	health := a.resolveRequirementExecutionHealth(item)
	watchdogEvent := a.latestRequirementWatchdogEvent(item.ID)
	retryBudget := 0
	if a.requirementAuto != nil {
		retryBudget = a.requirementAuto.maxRequirementRetryAttempts()
	}
	var latestWatchdog *v1RequirementWatchdogEvent
	if watchdogEvent != nil {
		latestWatchdog = &v1RequirementWatchdogEvent{
			TriggerKind:   watchdogEvent.TriggerKind,
			TriggerReason: watchdogEvent.TriggerReason,
			Action:        watchdogEvent.Action,
			Status:        watchdogEvent.Status,
			Detail:        watchdogEvent.Detail,
			CreatedAt:     watchdogEvent.CreatedAt,
			FinishedAt:    watchdogEvent.FinishedAt,
		}
	}
	return v1Requirement{
		ID:                       item.ID,
		ProjectID:                item.ProjectID,
		SortOrder:                item.SortOrder,
		Title:                    item.Title,
		Description:              item.Description,
		Status:                   item.Status,
		ExecutionMode:            item.ExecutionMode,
		CLIType:                  item.CLIType,
		AutoClearSession:         item.AutoClearSession,
		NoResponseTimeoutMinutes: item.NoResponseTimeoutMinutes,
		NoResponseErrorAction:    item.NoResponseErrorAction,
		NoResponseIdleAction:     item.NoResponseIdleAction,
		RequiresDesignReview:     item.RequiresDesignReview,
		RequiresCodeReview:       item.RequiresCodeReview,
		RequiresAcceptanceReview: item.RequiresAcceptanceReview,
		RequiresReleaseApproval:  item.RequiresReleaseApproval,
		CreatedAt:                item.CreatedAt,
		StartedAt:                item.StartedAt,
		EndedAt:                  item.EndedAt,
		PromptSentAt:             item.PromptSentAt,
		PromptReplayedAt:         item.PromptReplayedAt,
		AutoRetryAttempts:        item.AutoRetryAttempts,
		RetryBudget:              retryBudget,
		RetryBudgetExhausted:     item.RetryBudgetExhaustedAt != nil && !item.RetryBudgetExhaustedAt.IsZero(),
		ProjectName:              item.ProjectName,
		ExecutionState:           health.State,
		ExecutionReason:          health.Reason,
		LastOutputAt:             health.LastOutputAt,
		LastWatchdogEvent:        latestWatchdog,
		UpdatedAt:                item.UpdatedAt,
	}
}

func (a *App) toV1Session(item CLISessionView) v1Session {
	health := a.resolveSessionExecutionHealth(item)
	return v1Session{
		ID:               item.ID,
		CLIType:          item.CLIType,
		Profile:          item.Profile,
		ProfileName:      item.ProfileName,
		AgentID:          item.AgentID,
		ProjectID:        item.ProjectID,
		RequirementID:    item.RequirementID,
		WorkDir:          item.WorkDir,
		SessionState:     item.State,
		ProcessPID:       item.ProcessPID,
		CreatedAt:        item.CreatedAt,
		LastActiveAt:     item.LastActiveAt,
		ExitCode:         item.ExitCode,
		LastError:        item.LastError,
		ProjectName:      item.ProjectName,
		RequirementTitle: item.RequirementTitle,
		ExecutionState:   health.State,
		ExecutionReason:  health.Reason,
		LastOutputAt:     health.LastOutputAt,
	}
}

func toV1Workflow(item WorkflowRun) v1Workflow {
	return v1Workflow{
		ID:               item.ID,
		ProjectID:        item.ProjectID,
		RequirementID:    item.RequirementID,
		Status:           item.Status,
		CurrentStage:     item.CurrentStage,
		TriggerMode:      item.TriggerMode,
		RiskLevel:        item.RiskLevel,
		StartedAt:        item.StartedAt,
		EndedAt:          item.EndedAt,
		LastError:        item.LastError,
		ResumeFromStage:  item.ResumeFromStage,
		ProjectName:      item.ProjectName,
		RequirementTitle: item.RequirementTitle,
		Progress:         item.Progress,
	}
}

func toV1StageRun(item StageRun) v1StageRun {
	return v1StageRun{
		ID:             item.ID,
		WorkflowRunID:  item.WorkflowRunID,
		StageName:      item.StageName,
		DisplayName:    item.DisplayName,
		Status:         item.Status,
		Attempt:        item.Attempt,
		OwnerType:      item.OwnerType,
		AgentSessionID: item.AgentSessionID,
		StartedAt:      item.StartedAt,
		EndedAt:        item.EndedAt,
		ResultSummary:  item.ResultSummary,
		Artifacts:      item.Artifacts,
		RuleReportID:   item.RuleReportID,
		Order:          item.Order,
	}
}

func toV1Artifact(item Artifact) v1Artifact {
	return v1Artifact{
		ID:            item.ID,
		ProjectID:     item.ProjectID,
		RequirementID: item.RequirementID,
		WorkflowRunID: item.WorkflowRunID,
		StageRunID:    item.StageRunID,
		ArtifactType:  item.ArtifactType,
		Title:         item.Title,
		Path:          item.Path,
		Version:       item.Version,
		Status:        item.Status,
		Source:        item.Source,
		ContentHash:   item.ContentHash,
		CreatedAt:     item.CreatedAt,
		UpdatedAt:     item.UpdatedAt,
	}
}

func toV1ReviewGate(item ReviewGate) v1ReviewGate {
	return v1ReviewGate{
		ID:            item.ID,
		WorkflowRunID: item.WorkflowRunID,
		StageName:     item.StageName,
		GateType:      item.GateType,
		Status:        item.Status,
		Reviewer:      item.Reviewer,
		Decision:      item.Decision,
		Comment:       item.Comment,
		CreatedAt:     item.CreatedAt,
		ResolvedAt:    item.ResolvedAt,
		Title:         item.Title,
		Description:   item.Description,
		BlockingItems: item.BlockingItems,
	}
}

func toV1TaskItem(item TaskItem) v1TaskItem {
	return v1TaskItem{
		ID:                 item.ID,
		WorkflowRunID:      item.WorkflowRunID,
		StageRunID:         item.StageRunID,
		ParentTaskID:       item.ParentTaskID,
		Title:              item.Title,
		Description:        item.Description,
		Scope:              item.Scope,
		Required:           item.Required,
		Status:             item.Status,
		OwnerSessionID:     item.OwnerSessionID,
		DependsOn:          item.DependsOn,
		EvidenceArtifactID: item.EvidenceArtifactID,
		CreatedAt:          item.CreatedAt,
		UpdatedAt:          item.UpdatedAt,
	}
}

func toV1DecisionRequest(item DecisionRequest) v1DecisionRequest {
	options := make([]v1DecisionOption, 0, len(item.Options))
	for _, option := range item.Options {
		options = append(options, v1DecisionOption{Value: option.Value, Label: option.Label})
	}
	return v1DecisionRequest{
		ID:                item.ID,
		WorkflowRunID:     item.WorkflowRunID,
		StageRunID:        item.StageRunID,
		RequestType:       item.RequestType,
		Title:             item.Title,
		Question:          item.Question,
		Context:           item.Context,
		Options:           options,
		RecommendedOption: item.RecommendedOption,
		Blocking:          item.Blocking,
		Status:            item.Status,
		Decision:          item.Decision,
		Decider:           item.Decider,
		CreatedAt:         item.CreatedAt,
		ResolvedAt:        item.ResolvedAt,
	}
}

func toV1CodeSnapshot(item CodeSnapshot) v1CodeSnapshot {
	return v1CodeSnapshot{
		ID:                item.ID,
		ProjectID:         item.ProjectID,
		WorkflowRunID:     item.WorkflowRunID,
		StageRunID:        item.StageRunID,
		SnapshotType:      item.SnapshotType,
		GitCommit:         item.GitCommit,
		GitBranch:         item.GitBranch,
		WorkspaceRevision: item.WorkspaceRevision,
		FileCount:         item.FileCount,
		CreatedAt:         item.CreatedAt,
	}
}

func toV1ChangeSet(item ChangeSet) v1ChangeSet {
	files := make([]v1WorkflowFileChange, 0, len(item.Files))
	for _, file := range item.Files {
		files = append(files, v1WorkflowFileChange{
			Path:      file.Path,
			Status:    file.Status,
			Additions: file.Additions,
			Deletions: file.Deletions,
			OldPath:   file.OldPath,
		})
	}
	return v1ChangeSet{
		ID:               item.ID,
		ProjectID:        item.ProjectID,
		WorkflowRunID:    item.WorkflowRunID,
		StageRunID:       item.StageRunID,
		BaseSnapshotID:   item.BaseSnapshotID,
		TargetSnapshotID: item.TargetSnapshotID,
		ChangeScope:      item.ChangeScope,
		Summary:          item.Summary,
		FileStats: v1ChangeSetStats{
			Added:          item.FileStats.Added,
			Modified:       item.FileStats.Modified,
			Deleted:        item.FileStats.Deleted,
			Renamed:        item.FileStats.Renamed,
			TotalAdditions: item.FileStats.TotalAdditions,
			TotalDeletions: item.FileStats.TotalDeletions,
		},
		Files:           files,
		PatchArtifactID: item.PatchArtifactID,
		CreatedAt:       item.CreatedAt,
	}
}

func toV1DashboardStats(item DashboardStats) v1DashboardStats {
	return v1DashboardStats{
		TotalProjects:     item.TotalProjects,
		TotalRequirements: item.TotalRequirements,
		RunningTasks:      item.RunningTasks,
		CompletedTasks:    item.CompletedTasks,
		ActiveWorkflows:   item.ActiveWorkflows,
		PendingReviews:    item.PendingReviews,
		PendingDecisions:  item.PendingDecisions,
	}
}

func toV1Activity(item Activity) v1Activity {
	return v1Activity{
		ID:          item.ID,
		Type:        item.Type,
		Action:      item.Action,
		Title:       item.Title,
		Description: item.Description,
		Timestamp:   item.Timestamp,
	}
}

// tryResumeAutoRequirement attempts to complete or re-activate the auto requirement
// associated with a workflow after a blocker (decision/review) has been resolved.
func (a *App) tryResumeAutoRequirement(workflowRunID string) {
	if a == nil || a.workflowSvc == nil || a.requirementSvc == nil {
		return
	}
	workflowRunID = strings.TrimSpace(workflowRunID)
	if workflowRunID == "" {
		return
	}
	workflow, err := a.workflowSvc.GetWorkflow(workflowRunID)
	if err != nil || workflow == nil {
		return
	}
	if a.requirementAuto != nil {
		a.requirementAuto.TryCompleteRunningRequirement(workflow.ProjectID)
	}
}

func writeV1ProjectError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		writeV1Error(w, http.StatusNotFound, "project not found")
		return
	}
	writeV1Error(w, http.StatusBadRequest, err.Error())
}

func writeV1RequirementError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		writeV1Error(w, http.StatusNotFound, "requirement not found")
		return
	}
	writeV1Error(w, http.StatusBadRequest, err.Error())
}

func writeV1SessionError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		writeV1Error(w, http.StatusNotFound, "session not found")
		return
	}
	writeV1Error(w, http.StatusBadRequest, err.Error())
}

func writeV1WorkflowError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		writeV1Error(w, http.StatusNotFound, "resource not found")
		return
	}
	writeV1Error(w, http.StatusBadRequest, err.Error())
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func derefBool(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

func pickBool(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func pickInt(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func firstNonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func firstNonEmptyAllowEmpty(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return strings.TrimSpace(*value)
}
