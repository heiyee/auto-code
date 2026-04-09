package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"auto-code/internal/domain"
	"auto-code/internal/persistence"
)

const defaultWorkflowGitTimeout = 8 * time.Second

type workflowStageDefinition struct {
	Name        string
	DisplayName string
	Order       int
}

type workflowBootstrapPlan struct {
	CurrentStage string
	Stages       map[string]string
	Progress     int
	RiskLevel    string
}

type gitWorkspaceState struct {
	Available       bool
	Branch          string
	Commit          string
	FileCount       int
	Dirty           bool
	ChangedPaths    []string
	UntrackedPaths  []string
	BaseRef         string
	WorkspaceDigest string
}

type gitChangeSetResult struct {
	Files   []domain.WorkflowFileChange
	Stats   domain.ChangeSetFileStats
	Patch   string
	Summary string
}

var workflowStageDefinitions = []workflowStageDefinition{
	{Name: domain.WorkflowStageRequirementIntake, DisplayName: "需求接入", Order: 10},
	{Name: domain.WorkflowStageRequirementReview, DisplayName: "需求分析", Order: 20},
	{Name: domain.WorkflowStageSolutionDesign, DisplayName: "方案设计", Order: 30},
	{Name: domain.WorkflowStageDesignReview, DisplayName: "设计审核", Order: 40},
	{Name: domain.WorkflowStageTaskPlanning, DisplayName: "任务拆解", Order: 50},
	{Name: domain.WorkflowStageImplementation, DisplayName: "编码实施", Order: 60},
	{Name: domain.WorkflowStageCodeDiff, DisplayName: "变更归集", Order: 70},
	{Name: domain.WorkflowStageCodeStandards, DisplayName: "代码规范", Order: 80},
	{Name: domain.WorkflowStageSystemStandards, DisplayName: "系统规范", Order: 90},
	{Name: domain.WorkflowStageTesting, DisplayName: "测试验证", Order: 100},
	{Name: domain.WorkflowStageAcceptanceReview, DisplayName: "人工验收", Order: 110},
	{Name: domain.WorkflowStageGitDelivery, DisplayName: "Git 交付", Order: 120},
	{Name: domain.WorkflowStageReleaseGate, DisplayName: "发布门禁", Order: 130},
	{Name: domain.WorkflowStageRelease, DisplayName: "归档发布", Order: 140},
}

// WorkflowService manages workflow bootstrap, artifacts, reviews and change tracking.
type WorkflowService struct {
	store          *persistence.SQLiteStore
	projectService *ProjectService
	artifactRoot   string
	gitTimeout     time.Duration
}

// NewWorkflowService builds one workflow service instance.
func NewWorkflowService(store *persistence.SQLiteStore, projectService *ProjectService, artifactRoot string) *WorkflowService {
	return &WorkflowService{
		store:          store,
		projectService: projectService,
		artifactRoot:   strings.TrimSpace(artifactRoot),
		gitTimeout:     defaultWorkflowGitTimeout,
	}
}

// SyncRequirement keeps workflow state aligned with requirement transitions.
func (s *WorkflowService) SyncRequirement(requirement domain.Requirement, action string) error {
	if s == nil || s.store == nil || s.projectService == nil {
		return errors.New("workflow service is not initialized")
	}
	action = strings.TrimSpace(strings.ToLower(action))
	switch action {
	case "start":
		return s.handleRequirementStart(requirement)
	case "pause":
		return s.handleRequirementPause(requirement)
	case "fail":
		return s.handleRequirementFail(requirement)
	case "skip":
		return s.handleRequirementSkip(requirement)
	case "done":
		return s.handleRequirementDone(requirement)
	default:
		return nil
	}
}

// ListWorkflows returns workflows filtered by project, status and optionally requirementID.
func (s *WorkflowService) ListWorkflows(projectID, status, requirementID string) ([]domain.WorkflowRun, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("workflow service is not initialized")
	}
	items, err := s.store.ListWorkflowRuns(projectID, status)
	if err != nil {
		return nil, err
	}
	requirementID = strings.TrimSpace(requirementID)
	if requirementID == "" {
		return items, nil
	}
	filtered := items[:0]
	for _, item := range items {
		if item.RequirementID == requirementID {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

// GetArtifact returns one artifact by id.
func (s *WorkflowService) GetArtifact(artifactID string) (*domain.Artifact, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("workflow service is not initialized")
	}
	return s.store.GetArtifact(artifactID)
}

// GetArtifactContent reads the file content of an artifact from the project workDir.
func (s *WorkflowService) GetArtifactContent(artifactID string) (string, error) {
	if s == nil || s.store == nil {
		return "", errors.New("workflow service is not initialized")
	}
	artifact, err := s.store.GetArtifact(artifactID)
	if err != nil {
		return "", err
	}
	if artifact == nil {
		return "", errors.New("artifact not found")
	}
	// Resolve path: if artifact.Path is absolute use it directly, otherwise use artifactRoot
	artifactPath := artifact.Path
	if !filepath.IsAbs(artifactPath) {
		// Try project workDir first
		project, pErr := s.projectService.Get(artifact.ProjectID)
		if pErr == nil && project != nil {
			candidate := filepath.Join(project.WorkDir, artifactPath)
			if _, statErr := os.Stat(candidate); statErr == nil {
				artifactPath = candidate
			} else {
				artifactPath = filepath.Join(s.artifactRoot, artifactPath)
			}
		} else {
			artifactPath = filepath.Join(s.artifactRoot, artifactPath)
		}
	}
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		return "", fmt.Errorf("read artifact file: %w", err)
	}
	return string(data), nil
}

// GetWorkflow returns one workflow by id.
func (s *WorkflowService) GetWorkflow(workflowID string) (*domain.WorkflowRun, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("workflow service is not initialized")
	}
	return s.store.GetWorkflowRun(workflowID)
}

// ListStageRuns returns all stages for one workflow.
func (s *WorkflowService) ListStageRuns(workflowID string) ([]domain.StageRun, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("workflow service is not initialized")
	}
	return s.store.ListStageRuns(workflowID)
}

// ListTasks returns all tasks for one workflow.
func (s *WorkflowService) ListTasks(workflowID string) ([]domain.TaskItem, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("workflow service is not initialized")
	}
	return s.store.ListTaskItems(workflowID)
}

// UpdateStageManually lets external callers (UI, CI/CD) advance a specific stage and
// then re-reconciles the overall workflow state.
func (s *WorkflowService) UpdateStageManually(stageRunID, status, resultSummary string) (*domain.StageRun, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("workflow service is not initialized")
	}
	stage, err := s.store.GetStageRun(stageRunID)
	if err != nil {
		return nil, err
	}
	stage.Status = strings.TrimSpace(status)
	if strings.TrimSpace(resultSummary) != "" {
		stage.ResultSummary = strings.TrimSpace(resultSummary)
	}
	now := time.Now()
	if stage.Status == domain.StageStatusRunning && stage.StartedAt == nil {
		stage.StartedAt = &now
	}
	if (stage.Status == domain.StageStatusCompleted || stage.Status == domain.StageStatusFailed) && stage.EndedAt == nil {
		stage.EndedAt = &now
	}
	updated, err := s.store.UpdateStageRun(*stage)
	if err != nil {
		return nil, err
	}
	_ = s.reconcileWorkflowState(stage.WorkflowRunID)
	return updated, nil
}

// ListReviews returns review gates filtered by status and workflow.
func (s *WorkflowService) ListReviews(status, workflowID string) ([]domain.ReviewGate, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("workflow service is not initialized")
	}
	return s.store.ListReviewGates(status, workflowID)
}

// UpdateReview updates one review gate and reconciles the workflow state.
func (s *WorkflowService) UpdateReview(reviewID string, input domain.ReviewGateUpdateInput) (*domain.ReviewGate, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("workflow service is not initialized")
	}
	item, err := s.store.UpdateReviewGate(reviewID, input)
	if err != nil {
		return nil, err
	}
	if err := s.reconcileWorkflowState(item.WorkflowRunID); err != nil {
		return nil, err
	}
	return item, nil
}

// ListArtifacts returns artifacts for one workflow or all workflows.
func (s *WorkflowService) ListArtifacts(workflowID string) ([]domain.Artifact, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("workflow service is not initialized")
	}
	return s.store.ListArtifacts(workflowID)
}

// ListDecisions returns decision requests for one workflow or all workflows.
func (s *WorkflowService) ListDecisions(workflowID string) ([]domain.DecisionRequest, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("workflow service is not initialized")
	}
	return s.store.ListDecisionRequests(workflowID)
}

// ResolveDecision resolves one pending decision and reconciles workflow state.
func (s *WorkflowService) ResolveDecision(decisionID string, input domain.DecisionResolutionInput) (*domain.DecisionRequest, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("workflow service is not initialized")
	}
	item, err := s.store.ResolveDecisionRequest(decisionID, input)
	if err != nil {
		return nil, err
	}
	if err := s.reconcileWorkflowState(item.WorkflowRunID); err != nil {
		return nil, err
	}
	return item, nil
}

// ListChangeSets returns workflow change sets. When scoped to one workflow it refreshes the live diff first.
func (s *WorkflowService) ListChangeSets(workflowID string) ([]domain.ChangeSet, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("workflow service is not initialized")
	}
	if strings.TrimSpace(workflowID) != "" {
		if err := s.RefreshWorkflow(workflowID); err != nil {
			return nil, err
		}
	} else {
		workflows, err := s.store.ListWorkflowRuns("", "")
		if err != nil {
			return nil, err
		}
		for _, workflow := range workflows {
			if refreshErr := s.refreshWorkflowDiff(workflow); refreshErr != nil {
				if errors.Is(refreshErr, persistence.ErrNotFound) {
					continue
				}
				return nil, refreshErr
			}
		}
	}
	return s.store.ListChangeSets(workflowID)
}

// GetChangeSet returns one stored change set by id.
func (s *WorkflowService) GetChangeSet(changeSetID string) (*domain.ChangeSet, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("workflow service is not initialized")
	}
	return s.store.GetChangeSet(changeSetID)
}

// ListSnapshots returns stored snapshots for one workflow or all workflows.
func (s *WorkflowService) ListSnapshots(workflowID string) ([]domain.CodeSnapshot, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("workflow service is not initialized")
	}
	if strings.TrimSpace(workflowID) != "" {
		if err := s.RefreshWorkflow(workflowID); err != nil {
			return nil, err
		}
	}
	return s.store.ListCodeSnapshots(workflowID)
}

// RefreshWorkflow updates live change tracking for one workflow.
func (s *WorkflowService) RefreshWorkflow(workflowID string) error {
	if s == nil || s.store == nil || s.projectService == nil {
		return errors.New("workflow service is not initialized")
	}
	workflow, err := s.store.GetWorkflowRun(workflowID)
	if err != nil {
		return err
	}
	if err := s.refreshWorkflowDiff(*workflow); err != nil {
		return err
	}
	return s.reconcileWorkflowState(workflow.ID)
}

// CanAutoComplete reports whether an auto-running requirement may be safely marked done.
func (s *WorkflowService) CanAutoComplete(requirementID string) (bool, string, error) {
	if s == nil || s.store == nil {
		return false, "", errors.New("workflow service is not initialized")
	}
	workflow, err := s.store.GetWorkflowRunByRequirement(requirementID)
	if err != nil {
		if errors.Is(err, persistence.ErrNotFound) {
			return true, "", nil
		}
		return false, "", err
	}
	decisions, err := s.store.ListDecisionRequests(workflow.ID)
	if err != nil {
		return false, "", err
	}
	for _, item := range decisions {
		if item.Blocking && item.Status == domain.DecisionStatusPending {
			return false, "存在待确认决策项", nil
		}
	}
	reviews, err := s.store.ListReviewGates("", workflow.ID)
	if err != nil {
		return false, "", err
	}
	for _, item := range reviews {
		switch item.Status {
		case domain.ReviewGateStatusPending:
			return false, "存在待审核节点", nil
		case domain.ReviewGateStatusRejected:
			return false, "存在被拒绝的审核节点", nil
		}
	}
	return true, "", nil
}

// DashboardStats aggregates top-level system metrics.
func (s *WorkflowService) DashboardStats() (domain.DashboardStats, error) {
	if s == nil || s.store == nil || s.projectService == nil {
		return domain.DashboardStats{}, errors.New("workflow service is not initialized")
	}
	projects, err := s.projectService.List()
	if err != nil {
		return domain.DashboardStats{}, err
	}
	requirements, err := s.store.ListRequirements("")
	if err != nil {
		return domain.DashboardStats{}, err
	}
	workflows, err := s.store.ListWorkflowRuns("", "")
	if err != nil {
		return domain.DashboardStats{}, err
	}
	tasksRunning := 0
	tasksDone := 0
	for _, workflow := range workflows {
		items, listErr := s.store.ListTaskItems(workflow.ID)
		if listErr != nil {
			return domain.DashboardStats{}, listErr
		}
		for _, item := range items {
			switch item.Status {
			case domain.TaskStatusRunning:
				tasksRunning++
			case domain.TaskStatusDone:
				tasksDone++
			}
		}
	}
	reviews, err := s.store.ListReviewGates(domain.ReviewGateStatusPending, "")
	if err != nil {
		return domain.DashboardStats{}, err
	}
	decisions, err := s.store.ListDecisionRequests("")
	if err != nil {
		return domain.DashboardStats{}, err
	}
	pendingDecisions := 0
	for _, d := range decisions {
		if d.Status == domain.DecisionStatusPending {
			pendingDecisions++
		}
	}
	activeWorkflows := 0
	for _, workflow := range workflows {
		if workflow.Status == domain.WorkflowStatusRunning || workflow.Status == domain.WorkflowStatusPaused {
			activeWorkflows++
		}
	}
	return domain.DashboardStats{
		TotalProjects:     len(projects),
		TotalRequirements: len(requirements),
		RunningTasks:      tasksRunning,
		CompletedTasks:    tasksDone,
		ActiveWorkflows:   activeWorkflows,
		PendingReviews:    len(reviews),
		PendingDecisions:  pendingDecisions,
	}, nil
}

// DashboardActivities returns recent activities synthesized from workflow objects.
func (s *WorkflowService) DashboardActivities(limit int) ([]domain.Activity, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("workflow service is not initialized")
	}
	if limit <= 0 {
		limit = 20
	}

	activities := make([]domain.Activity, 0, limit*3)

	workflows, err := s.store.ListWorkflowRuns("", "")
	if err != nil {
		return nil, err
	}
	for _, workflow := range workflows {
		activities = append(activities, domain.Activity{
			ID:          workflow.ID,
			Type:        "workflow",
			Action:      workflowAction(workflow.Status),
			Title:       workflow.RequirementTitle,
			Description: fmt.Sprintf("工作流阶段：%s", workflow.CurrentStage),
			Timestamp:   workflow.UpdatedAt,
		})
	}

	reviews, err := s.store.ListReviewGates("", "")
	if err != nil {
		return nil, err
	}
	for _, review := range reviews {
		timestamp := review.CreatedAt
		if review.ResolvedAt != nil {
			timestamp = *review.ResolvedAt
		}
		activities = append(activities, domain.Activity{
			ID:          review.ID,
			Type:        "review",
			Action:      reviewAction(review.Status),
			Title:       defaultActivityTitle(review.Title, review.GateType),
			Description: review.Comment,
			Timestamp:   timestamp,
		})
	}

	artifacts, err := s.store.ListArtifacts("")
	if err != nil {
		return nil, err
	}
	for _, artifact := range artifacts {
		activities = append(activities, domain.Activity{
			ID:          artifact.ID,
			Type:        "artifact",
			Action:      "updated",
			Title:       artifact.Title,
			Description: fmt.Sprintf("产物类型：%s", artifact.ArtifactType),
			Timestamp:   artifact.UpdatedAt,
		})
	}

	sort.SliceStable(activities, func(i, j int) bool {
		if activities[i].Timestamp.Equal(activities[j].Timestamp) {
			return activities[i].ID > activities[j].ID
		}
		return activities[i].Timestamp.After(activities[j].Timestamp)
	})
	if len(activities) > limit {
		activities = activities[:limit]
	}
	return activities, nil
}

func (s *WorkflowService) handleRequirementStart(requirement domain.Requirement) error {
	project, err := s.projectService.Get(requirement.ProjectID)
	if err != nil {
		return err
	}
	existing, err := s.store.GetWorkflowRunByRequirement(requirement.ID)
	if err != nil && !errors.Is(err, persistence.ErrNotFound) {
		return err
	}
	if existing != nil {
		if strings.TrimSpace(existing.Status) == domain.WorkflowStatusPaused || strings.TrimSpace(existing.Status) == domain.WorkflowStatusFailed {
			if err := s.reopenExistingWorkflow(*existing); err != nil {
				return err
			}
		}
		if err := s.reconcileWorkflowState(existing.ID); err != nil {
			return err
		}
		return s.refreshWorkflowDiff(*existing)
	}

	gitState := s.inspectGitWorkspace(project.WorkDir)
	decisions := s.buildDecisionRequests(requirement, gitState)
	reviews := s.buildReviewGates(requirement)
	plan := buildWorkflowBootstrapPlan(decisions, reviews, gitState)
	now := time.Now()
	workflow, err := s.store.CreateWorkflowRun(domain.WorkflowRun{
		ProjectID:        requirement.ProjectID,
		RequirementID:    requirement.ID,
		Status:           domain.WorkflowStatusRunning,
		CurrentStage:     plan.CurrentStage,
		TriggerMode:      requirement.ExecutionMode,
		RiskLevel:        plan.RiskLevel,
		StartedAt:        coalesceTime(requirement.StartedAt, now),
		Progress:         plan.Progress,
		CreatedAt:        now,
		UpdatedAt:        now,
		ResumeFromStage:  "",
		ProjectName:      requirement.ProjectName,
		RequirementTitle: requirement.Title,
	})
	if err != nil {
		return err
	}

	stageRuns := make([]domain.StageRun, 0, len(workflowStageDefinitions))
	for _, def := range workflowStageDefinitions {
		stageRuns = append(stageRuns, domain.StageRun{
			WorkflowRunID: workflow.ID,
			StageName:     def.Name,
			DisplayName:   def.DisplayName,
			Status:        plan.Stages[def.Name],
			Attempt:       0,
			OwnerType:     domain.StageOwnerTypeSystem,
			Order:         def.Order,
		})
	}
	if err := s.store.CreateStageRuns(stageRuns); err != nil {
		return err
	}

	stageMap, err := s.stageMap(workflow.ID)
	if err != nil {
		return err
	}

	if _, err := s.writeWorkflowArtifact(*workflow, requirement, stageMap[domain.WorkflowStageRequirementReview], domain.ArtifactTypeRequirementBrief, "需求摘要", buildRequirementBriefContent(requirement, gitState, decisions, reviews)); err != nil {
		return err
	}
	if _, err := s.writeWorkflowArtifact(*workflow, requirement, stageMap[domain.WorkflowStageSolutionDesign], domain.ArtifactTypeSolutionDesign, "方案设计", buildSolutionDesignContent(requirement, gitState, decisions, reviews)); err != nil {
		return err
	}
	taskPlanningStage := stageMap[domain.WorkflowStageTaskPlanning]
	taskArtifact, err := s.writeWorkflowArtifact(*workflow, requirement, taskPlanningStage, domain.ArtifactTypeTaskBreakdown, "任务拆解", buildTaskBreakdownContent(requirement))
	if err != nil {
		return err
	}
	if _, err := s.writeWorkflowArtifact(*workflow, requirement, stageMap[domain.WorkflowStageTesting], domain.ArtifactTypeTestPlan, "测试计划", buildTestPlanContent(requirement)); err != nil {
		return err
	}

	taskItems := s.buildTaskItems(*workflow, taskPlanningStage, taskArtifact, requirement)
	if err := s.store.ReplaceTaskItems(workflow.ID, taskPlanningStage.ID, taskItems); err != nil {
		return err
	}

	for _, decision := range decisions {
		decision.WorkflowRunID = workflow.ID
		decision.StageRunID = stageMap[domain.WorkflowStageRequirementReview].ID
		if _, err := s.store.UpsertDecisionRequest(decision); err != nil {
			return err
		}
	}
	for _, review := range reviews {
		review.WorkflowRunID = workflow.ID
		if _, err := s.store.UpsertReviewGate(review); err != nil {
			return err
		}
	}

	if gitState.Available {
		if _, err := s.store.UpsertCodeSnapshot(domain.CodeSnapshot{
			ProjectID:         workflow.ProjectID,
			WorkflowRunID:     workflow.ID,
			StageRunID:        "",
			SnapshotType:      domain.CodeSnapshotTypeWorkflowStart,
			GitCommit:         gitState.Commit,
			GitBranch:         gitState.Branch,
			WorkspaceRevision: gitState.WorkspaceDigest,
			FileCount:         gitState.FileCount,
			CreatedAt:         now,
		}); err != nil {
			return err
		}
		if implementationStage := stageMap[domain.WorkflowStageImplementation]; implementationStage.ID != "" {
			if _, err := s.store.UpsertCodeSnapshot(domain.CodeSnapshot{
				ProjectID:         workflow.ProjectID,
				WorkflowRunID:     workflow.ID,
				StageRunID:        implementationStage.ID,
				SnapshotType:      domain.CodeSnapshotTypeStageStart,
				GitCommit:         gitState.Commit,
				GitBranch:         gitState.Branch,
				WorkspaceRevision: gitState.WorkspaceDigest,
				FileCount:         gitState.FileCount,
				CreatedAt:         now,
			}); err != nil {
				return err
			}
		}
	}

	return s.reconcileWorkflowState(workflow.ID)
}

func (s *WorkflowService) handleRequirementPause(requirement domain.Requirement) error {
	workflow, err := s.store.GetWorkflowRunByRequirement(requirement.ID)
	if err != nil {
		if errors.Is(err, persistence.ErrNotFound) {
			return nil
		}
		return err
	}
	workflow.Status = domain.WorkflowStatusPaused
	workflow.UpdatedAt = time.Now()
	if _, err := s.store.UpdateWorkflowRun(*workflow); err != nil {
		return err
	}
	stages, err := s.store.ListStageRuns(workflow.ID)
	if err != nil {
		return err
	}
	for _, stage := range stages {
		if stage.Status != domain.StageStatusRunning {
			continue
		}
		stage.Status = domain.StageStatusInterrupted
		stage.ResultSummary = defaultResultSummary(stage.ResultSummary, "需求已暂停，等待后续恢复。")
		if _, err := s.store.UpdateStageRun(stage); err != nil {
			return err
		}
	}
	// Mark running tasks as blocked on pause.
	_ = s.store.BulkUpdateTaskStatus(workflow.ID, domain.TaskStatusRunning, domain.TaskStatusBlocked)
	return nil
}

func (s *WorkflowService) handleRequirementDone(requirement domain.Requirement) error {
	workflow, err := s.store.GetWorkflowRunByRequirement(requirement.ID)
	if err != nil {
		if errors.Is(err, persistence.ErrNotFound) {
			return nil
		}
		return err
	}
	if err := s.refreshWorkflowDiff(*workflow); err != nil {
		return err
	}
	changeSets, err := s.store.ListChangeSets(workflow.ID)
	if err != nil {
		return err
	}
	if len(changeSets) > 0 {
		stages, listErr := s.stageMap(workflow.ID)
		if listErr != nil {
			return listErr
		}
		if _, err := s.writeWorkflowArtifact(*workflow, requirement, stages[domain.WorkflowStageCodeDiff], domain.ArtifactTypeChangeSummary, "变更摘要", buildChangeSummaryContent(changeSets[0])); err != nil {
			return err
		}
	}
	// Mark all still-planned tasks as done — the agent completed its work.
	_ = s.store.BulkUpdateTaskStatus(workflow.ID, domain.TaskStatusPlanned, domain.TaskStatusDone)
	_ = s.store.BulkUpdateTaskStatus(workflow.ID, domain.TaskStatusRunning, domain.TaskStatusDone)
	return s.reconcileWorkflowState(workflow.ID)
}

func (s *WorkflowService) handleRequirementSkip(requirement domain.Requirement) error {
	workflow, err := s.store.GetWorkflowRunByRequirement(requirement.ID)
	if err != nil {
		if errors.Is(err, persistence.ErrNotFound) {
			return nil
		}
		return err
	}
	if err := s.prepareWorkflowForCompletion(*workflow, true); err != nil {
		return err
	}
	_ = s.store.BulkUpdateTaskStatus(workflow.ID, domain.TaskStatusFailed, domain.TaskStatusDone)
	_ = s.store.BulkUpdateTaskStatus(workflow.ID, domain.TaskStatusBlocked, domain.TaskStatusDone)
	return s.handleRequirementDone(requirement)
}

func (s *WorkflowService) handleRequirementFail(requirement domain.Requirement) error {
	workflow, err := s.store.GetWorkflowRunByRequirement(requirement.ID)
	if err != nil {
		if errors.Is(err, persistence.ErrNotFound) {
			return nil
		}
		return err
	}
	_ = s.store.BulkUpdateTaskStatus(workflow.ID, domain.TaskStatusPlanned, domain.TaskStatusFailed)
	_ = s.store.BulkUpdateTaskStatus(workflow.ID, domain.TaskStatusRunning, domain.TaskStatusFailed)
	_ = s.store.BulkUpdateTaskStatus(workflow.ID, domain.TaskStatusBlocked, domain.TaskStatusFailed)
	return s.reconcileWorkflowState(workflow.ID)
}

func (s *WorkflowService) reopenExistingWorkflow(workflow domain.WorkflowRun) error {
	if s == nil || s.store == nil {
		return errors.New("workflow service is not initialized")
	}
	if err := s.prepareWorkflowForCompletion(workflow, false); err != nil {
		return err
	}
	_ = s.store.BulkUpdateTaskStatus(workflow.ID, domain.TaskStatusBlocked, domain.TaskStatusPlanned)
	_ = s.store.BulkUpdateTaskStatus(workflow.ID, domain.TaskStatusRunning, domain.TaskStatusPlanned)
	_ = s.store.BulkUpdateTaskStatus(workflow.ID, domain.TaskStatusFailed, domain.TaskStatusPlanned)

	workflow.Status = domain.WorkflowStatusRunning
	workflow.CurrentStage = domain.WorkflowStageImplementation
	workflow.EndedAt = nil
	workflow.LastError = ""
	workflow.ResumeFromStage = domain.WorkflowStageImplementation
	workflow.UpdatedAt = time.Now()
	_, err := s.store.UpdateWorkflowRun(workflow)
	return err
}

func (s *WorkflowService) prepareWorkflowForCompletion(workflow domain.WorkflowRun, skipped bool) error {
	stages, err := s.store.ListStageRuns(workflow.ID)
	if err != nil {
		return err
	}
	for _, stage := range stages {
		if workflowStageOrder(stage.StageName) < workflowStageOrder(domain.WorkflowStageImplementation) {
			continue
		}
		stage.EndedAt = nil
		stage.AgentSessionID = ""
		stage.ResultSummary = ""
		if skipped {
			stage.Status = domain.StageStatusPending
		}
		if !skipped && strings.TrimSpace(workflow.Status) == domain.WorkflowStatusFailed && stage.StageName == domain.WorkflowStageImplementation {
			stage.Attempt += 1
		}
		if _, err := s.store.UpdateStageRun(stage); err != nil {
			return err
		}
	}
	return nil
}

func (s *WorkflowService) reconcileWorkflowState(workflowID string) error {
	workflow, err := s.store.GetWorkflowRun(workflowID)
	if err != nil {
		return err
	}
	requirement, err := s.store.GetRequirement(workflow.RequirementID)
	if err != nil {
		return err
	}
	stages, err := s.store.ListStageRuns(workflowID)
	if err != nil {
		return err
	}
	stageMap := make(map[string]domain.StageRun, len(stages))
	for _, stage := range stages {
		stageMap[stage.StageName] = stage
	}
	decisions, err := s.store.ListDecisionRequests(workflowID)
	if err != nil {
		return err
	}
	reviews, err := s.store.ListReviewGates("", workflowID)
	if err != nil {
		return err
	}

	blockerStage := ""
	workflowStatus := domain.WorkflowStatusRunning
	lastError := ""
	progress := workflow.Progress
	if requirement.Status == domain.RequirementStatusPaused {
		workflowStatus = domain.WorkflowStatusPaused
	} else if requirement.Status == domain.RequirementStatusFailed {
		workflowStatus = domain.WorkflowStatusFailed
		blockerStage = domain.WorkflowStageImplementation
		lastError = "需求执行失败。"
	}
	if blockerStage == "" {
		if pendingDecision := firstBlockingPendingDecision(decisions); pendingDecision != nil {
			blockerStage = domain.WorkflowStageRequirementReview
			workflowStatus = domain.WorkflowStatusPaused
			lastError = "存在待确认决策，工作流暂停。"
		}
	}
	if blockerStage == "" {
		if rejectedReview := firstRejectedReview(reviews); rejectedReview != nil {
			blockerStage = rejectedReview.StageName
			workflowStatus = domain.WorkflowStatusFailed
			lastError = defaultIfBlank(rejectedReview.Comment, "存在被拒绝的人工审核。")
		} else if pendingReview := firstPendingReview(reviews); pendingReview != nil {
			blockerStage = pendingReview.StageName
			workflowStatus = domain.WorkflowStatusPaused
			lastError = "存在待审核节点，工作流暂停。"
		}
	}
	if blockerStage == "" && requirement.Status == domain.RequirementStatusDone {
		blockerStage = domain.WorkflowStageRelease
		workflowStatus = domain.WorkflowStatusCompleted
		progress = 100
	} else if blockerStage == "" && requirement.Status == domain.RequirementStatusRunning {
		blockerStage = domain.WorkflowStageImplementation
		progress = max(progress, 45)
	}
	if blockerStage == "" {
		blockerStage = workflow.CurrentStage
	}

	for _, def := range workflowStageDefinitions {
		stage := stageMap[def.Name]
		switch def.Name {
		case domain.WorkflowStageRequirementIntake:
			stage.Status = domain.StageStatusCompleted
			stage.ResultSummary = defaultResultSummary(stage.ResultSummary, "需求已接入系统。")
		case domain.WorkflowStageRequirementReview:
			if firstBlockingPendingDecision(decisions) != nil {
				stage.Status = domain.StageStatusAwaitingInput
				stage.ResultSummary = "存在待确认事项，需人工决策后继续。"
			} else {
				stage.Status = domain.StageStatusCompleted
				stage.ResultSummary = defaultResultSummary(stage.ResultSummary, "需求分析已形成结构化产物。")
			}
		case domain.WorkflowStageSolutionDesign:
			stage.Status = domain.StageStatusCompleted
			stage.ResultSummary = defaultResultSummary(stage.ResultSummary, "方案设计文档已生成。")
		case domain.WorkflowStageDesignReview:
			if hasPendingReviewForStage(reviews, def.Name) {
				stage.Status = domain.StageStatusAwaitingReview
				stage.ResultSummary = "设计审核待人工处理。"
			} else if hasRejectedReviewForStage(reviews, def.Name) {
				stage.Status = domain.StageStatusBlocked
				stage.ResultSummary = "设计审核未通过。"
			} else {
				stage.Status = domain.StageStatusCompleted
				stage.ResultSummary = defaultResultSummary(stage.ResultSummary, "设计审核状态已收敛。")
			}
		case domain.WorkflowStageTaskPlanning:
			stage.Status = domain.StageStatusCompleted
			stage.ResultSummary = defaultResultSummary(stage.ResultSummary, "任务拆解已生成。")
		case domain.WorkflowStageImplementation:
			switch requirement.Status {
			case domain.RequirementStatusFailed:
				stage.Status = domain.StageStatusFailed
				stage.ResultSummary = "需求执行失败，编码流程已终止。"
			case domain.RequirementStatusDone:
				stage.Status = domain.StageStatusCompleted
				stage.ResultSummary = "编码任务已完成。"
			case domain.RequirementStatusPaused:
				stage.Status = domain.StageStatusInterrupted
				stage.ResultSummary = "编码已暂停。"
			default:
				if blockerStage != "" && blockerStage != def.Name {
					stage.Status = domain.StageStatusBlocked
					stage.ResultSummary = "存在前置阻塞项，编码暂缓。"
				} else {
					stage.Status = domain.StageStatusRunning
					stage.ResultSummary = "编码进行中。"
				}
			}
		case domain.WorkflowStageCodeDiff, domain.WorkflowStageCodeStandards, domain.WorkflowStageSystemStandards, domain.WorkflowStageTesting:
			if requirement.Status == domain.RequirementStatusDone {
				stage.Status = domain.StageStatusCompleted
				stage.ResultSummary = defaultResultSummary(stage.ResultSummary, "已完成自动归集。")
			} else if requirement.Status == domain.RequirementStatusFailed {
				stage.Status = domain.StageStatusBlocked
				stage.ResultSummary = defaultResultSummary(stage.ResultSummary, "前置编码阶段失败，后续自动阶段已停止。")
			} else {
				stage.Status = domain.StageStatusPending
			}
		case domain.WorkflowStageAcceptanceReview, domain.WorkflowStageReleaseGate:
			if hasPendingReviewForStage(reviews, def.Name) {
				stage.Status = domain.StageStatusAwaitingReview
				stage.ResultSummary = "等待人工审核。"
			} else if hasRejectedReviewForStage(reviews, def.Name) {
				stage.Status = domain.StageStatusBlocked
				stage.ResultSummary = "人工审核未通过。"
			} else if requirement.Status == domain.RequirementStatusFailed {
				stage.Status = domain.StageStatusBlocked
				stage.ResultSummary = defaultResultSummary(stage.ResultSummary, "需求已失败，人工审核不再继续。")
			} else if requirement.Status == domain.RequirementStatusDone {
				stage.Status = domain.StageStatusCompleted
				stage.ResultSummary = defaultResultSummary(stage.ResultSummary, "审核状态已收敛。")
			} else {
				stage.Status = domain.StageStatusPending
			}
		case domain.WorkflowStageGitDelivery, domain.WorkflowStageRelease:
			if workflowStatus == domain.WorkflowStatusCompleted {
				stage.Status = domain.StageStatusCompleted
				stage.ResultSummary = defaultResultSummary(stage.ResultSummary, "交付已归档。")
			} else if workflowStatus == domain.WorkflowStatusFailed {
				stage.Status = domain.StageStatusBlocked
				stage.ResultSummary = defaultResultSummary(stage.ResultSummary, "需求失败，交付流程已终止。")
			} else {
				stage.Status = domain.StageStatusPending
			}
		}
		if _, err := s.store.UpdateStageRun(stage); err != nil {
			return err
		}
		stageMap[def.Name] = stage
	}

	workflow.Status = workflowStatus
	workflow.CurrentStage = blockerStage
	workflow.LastError = lastError
	updatedStages := make([]domain.StageRun, 0, len(stageMap))
	for _, stage := range stageMap {
		updatedStages = append(updatedStages, stage)
	}
	workflow.Progress = computeWorkflowProgress(updatedStages, requirement.Status, blockerStage, progress)
	if workflowStatus == domain.WorkflowStatusCompleted {
		workflow.EndedAt = coalesceTimePtr(requirement.EndedAt, time.Now())
	} else {
		workflow.EndedAt = nil
	}
	_, err = s.store.UpdateWorkflowRun(*workflow)
	return err
}

func (s *WorkflowService) refreshWorkflowDiff(workflow domain.WorkflowRun) error {
	project, err := s.projectService.Get(workflow.ProjectID)
	if err != nil {
		return err
	}
	stageMap, err := s.stageMap(workflow.ID)
	if err != nil {
		return err
	}
	implementationStage := stageMap[domain.WorkflowStageImplementation]
	baseSnapshot, err := s.store.GetCodeSnapshotByScope(workflow.ID, implementationStage.ID, domain.CodeSnapshotTypeStageStart)
	if err != nil {
		if errors.Is(err, persistence.ErrNotFound) {
			return nil
		}
		return err
	}
	gitState := s.inspectGitWorkspace(project.WorkDir)
	if !gitState.Available {
		return nil
	}
	now := time.Now()
	targetSnapshot, err := s.store.UpsertCodeSnapshot(domain.CodeSnapshot{
		ProjectID:         workflow.ProjectID,
		WorkflowRunID:     workflow.ID,
		StageRunID:        implementationStage.ID,
		SnapshotType:      domain.CodeSnapshotTypePreReview,
		GitCommit:         gitState.Commit,
		GitBranch:         gitState.Branch,
		WorkspaceRevision: gitState.WorkspaceDigest,
		FileCount:         gitState.FileCount,
		CreatedAt:         now,
	})
	if err != nil {
		return err
	}

	changes, err := s.collectChangeSet(project.WorkDir, baseSnapshot)
	if err != nil {
		return err
	}
	requirement, err := s.store.GetRequirement(workflow.RequirementID)
	if err != nil {
		return err
	}
	patchArtifact, err := s.writeWorkflowArtifact(workflow, *requirement, stageMap[domain.WorkflowStageCodeDiff], domain.ArtifactTypeDiffPatch, "代码差异", changes.Patch)
	if err != nil {
		return err
	}
	_, err = s.store.UpsertChangeSet(domain.ChangeSet{
		ProjectID:        workflow.ProjectID,
		WorkflowRunID:    workflow.ID,
		StageRunID:       implementationStage.ID,
		BaseSnapshotID:   baseSnapshot.ID,
		TargetSnapshotID: targetSnapshot.ID,
		ChangeScope:      domain.ChangeScopeWorkflow,
		Summary:          changes.Summary,
		FileStats:        changes.Stats,
		Files:            changes.Files,
		PatchArtifactID:  patchArtifact.ID,
		CreatedAt:        now,
	})
	return err
}

func (s *WorkflowService) stageMap(workflowID string) (map[string]domain.StageRun, error) {
	stages, err := s.store.ListStageRuns(workflowID)
	if err != nil {
		return nil, err
	}
	result := make(map[string]domain.StageRun, len(stages))
	for _, stage := range stages {
		result[stage.StageName] = stage
	}
	return result, nil
}

func (s *WorkflowService) buildDecisionRequests(requirement domain.Requirement, gitState gitWorkspaceState) []domain.DecisionRequest {
	lines := splitMeaningfulLines(requirement.Description)
	items := make([]domain.DecisionRequest, 0, 4)
	for _, line := range lines {
		normalized := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(normalized, "待确认:"),
			strings.HasPrefix(normalized, "待确认："),
			strings.HasPrefix(normalized, "需确认:"),
			strings.HasPrefix(normalized, "需确认："),
			strings.HasPrefix(normalized, "待定:"),
			strings.HasPrefix(normalized, "待定："):
			question := strings.TrimSpace(trimDecisionPrefix(normalized))
			if question == "" {
				continue
			}
			items = append(items, domain.DecisionRequest{
				RequestType:       domain.DecisionRequestTypeClarification,
				Title:             "需求待确认",
				Question:          question,
				Context:           requirement.Title,
				RecommendedOption: "confirm_and_continue",
				Blocking:          true,
				Status:            domain.DecisionStatusPending,
				Options: []domain.DecisionOption{
					{Value: "confirm_and_continue", Label: "确认继续"},
					{Value: "revise_plan", Label: "调整方案"},
				},
			})
		}
	}
	if gitState.Available && gitState.Dirty {
		item := domain.DecisionRequest{
			RequestType:       domain.DecisionRequestTypeRiskConfirmation,
			Title:             "确认脏工作区基线",
			Question:          "工作流开始时项目已存在未提交改动，是否接受以当前工作区作为本次需求的对比基线？",
			Context:           buildDirtyWorkspaceContext(gitState),
			Blocking:          true,
			Status:            domain.DecisionStatusPending,
			RecommendedOption: "clean_then_restart",
			Options: []domain.DecisionOption{
				{Value: "clean_then_restart", Label: "清理后重启"},
				{Value: "accept_current_workspace", Label: "接受当前基线"},
			},
		}
		if requirement.ExecutionMode == domain.RequirementExecutionModeAuto {
			resolvedAt := time.Now()
			item.Status = domain.DecisionStatusResolved
			item.Decision = "accept_current_workspace"
			item.Decider = "system:auto_requirement"
			item.ResolvedAt = &resolvedAt
		}
		items = append(items, item)
	}
	if !gitState.Available && gitState.FileCount > 0 {
		items = append(items, domain.DecisionRequest{
			RequestType:       domain.DecisionRequestTypeRiskConfirmation,
			Title:             "确认无 Git 基线模式",
			Question:          "当前项目目录不是 Git 工作区，系统无法提供精确 diff 与提交对账，是否继续？",
			Context:           "建议将项目初始化为 Git 仓库后再启动无人值守工作流。",
			Blocking:          true,
			Status:            domain.DecisionStatusPending,
			RecommendedOption: "init_git_then_continue",
			Options: []domain.DecisionOption{
				{Value: "init_git_then_continue", Label: "先初始化 Git"},
				{Value: "continue_without_git", Label: "继续但降级"},
			},
		})
	}
	return items
}

func (s *WorkflowService) buildReviewGates(requirement domain.Requirement) []domain.ReviewGate {
	items := make([]domain.ReviewGate, 0, 4)
	appendReview := func(stageName, gateType, title, desc string) {
		items = append(items, domain.ReviewGate{
			StageName:     stageName,
			GateType:      gateType,
			Status:        domain.ReviewGateStatusPending,
			Decision:      "",
			Title:         title,
			Description:   desc,
			BlockingItems: []string{"需要人工确认后才能继续推进"},
		})
	}
	if requirement.RequiresDesignReview {
		appendReview(domain.WorkflowStageDesignReview, domain.ReviewGateTypeDesignReview, "设计审核", "需人工确认设计方案与约束是否一致。")
	}
	if requirement.RequiresCodeReview {
		appendReview(domain.WorkflowStageCodeStandards, domain.ReviewGateTypeCodeReview, "代码评审", "需人工确认代码改动范围、可读性与风险。")
	}
	if requirement.RequiresAcceptanceReview {
		appendReview(domain.WorkflowStageAcceptanceReview, domain.ReviewGateTypeAcceptanceReview, "人工验收", "需人工确认结果满足需求目标。")
	}
	if requirement.RequiresReleaseApproval {
		appendReview(domain.WorkflowStageReleaseGate, domain.ReviewGateTypeReleaseApproval, "发布审批", "需人工确认发布风险与窗口。")
	}
	return items
}

func (s *WorkflowService) buildTaskItems(workflow domain.WorkflowRun, stage domain.StageRun, taskArtifact *domain.Artifact, requirement domain.Requirement) []domain.TaskItem {
	lines := splitMeaningfulLines(requirement.Description)
	taskLines := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.Contains(line, "待确认") || strings.Contains(line, "需确认") || strings.Contains(line, "待定") {
			continue
		}
		taskLines = append(taskLines, strings.TrimSpace(line))
	}
	if len(taskLines) == 0 {
		taskLines = []string{
			"完成方案设计与关键约束确认",
			"实现核心功能代码改动",
			"补充或更新测试用例",
			"归集代码差异并准备人工审核材料",
		}
	}
	items := make([]domain.TaskItem, 0, len(taskLines))
	for idx, line := range taskLines {
		scope := domain.TaskScopeModule
		if idx == len(taskLines)-1 {
			scope = domain.TaskScopeVerification
		} else if strings.Contains(strings.ToLower(line), "测试") {
			scope = domain.TaskScopeTest
		}
		item := domain.TaskItem{
			WorkflowRunID:      workflow.ID,
			StageRunID:         stage.ID,
			ID:                 fmt.Sprintf("%s-task-%02d", workflow.ID, idx+1),
			Title:              line,
			Description:        fmt.Sprintf("任务 %d：%s", idx+1, line),
			Scope:              scope,
			Required:           true,
			Status:             domain.TaskStatusPlanned,
			EvidenceArtifactID: "",
		}
		if taskArtifact != nil {
			item.EvidenceArtifactID = taskArtifact.ID
		}
		if idx > 0 && len(items) > 0 {
			item.DependsOn = []string{items[idx-1].ID}
		}
		items = append(items, item)
	}
	return items
}

func buildWorkflowBootstrapPlan(decisions []domain.DecisionRequest, reviews []domain.ReviewGate, gitState gitWorkspaceState) workflowBootstrapPlan {
	plan := workflowBootstrapPlan{
		CurrentStage: domain.WorkflowStageImplementation,
		Stages:       make(map[string]string, len(workflowStageDefinitions)),
		Progress:     45,
		RiskLevel:    "medium",
	}
	for _, def := range workflowStageDefinitions {
		plan.Stages[def.Name] = domain.StageStatusPending
	}
	plan.Stages[domain.WorkflowStageRequirementIntake] = domain.StageStatusCompleted
	plan.Stages[domain.WorkflowStageSolutionDesign] = domain.StageStatusCompleted
	plan.Stages[domain.WorkflowStageTaskPlanning] = domain.StageStatusCompleted
	if firstBlockingPendingDecision(decisions) != nil {
		plan.CurrentStage = domain.WorkflowStageRequirementReview
		plan.Stages[domain.WorkflowStageRequirementReview] = domain.StageStatusAwaitingInput
		plan.Stages[domain.WorkflowStageImplementation] = domain.StageStatusBlocked
		plan.Progress = 20
		plan.RiskLevel = "high"
	} else {
		plan.Stages[domain.WorkflowStageRequirementReview] = domain.StageStatusCompleted
	}
	if len(reviews) > 0 {
		for _, review := range reviews {
			plan.Stages[review.StageName] = domain.StageStatusAwaitingReview
		}
		if plan.CurrentStage == domain.WorkflowStageImplementation {
			plan.CurrentStage = reviews[0].StageName
			plan.Stages[domain.WorkflowStageImplementation] = domain.StageStatusBlocked
			plan.Progress = 35
		}
	}
	if plan.CurrentStage == domain.WorkflowStageImplementation {
		plan.Stages[domain.WorkflowStageImplementation] = domain.StageStatusRunning
	}
	if gitState.Dirty {
		plan.RiskLevel = "high"
	}
	return plan
}

func (s *WorkflowService) writeWorkflowArtifact(workflow domain.WorkflowRun, requirement domain.Requirement, stage domain.StageRun, artifactType, title, content string) (*domain.Artifact, error) {
	if strings.TrimSpace(content) == "" {
		content = "# Empty Artifact\n"
	}
	root := s.artifactRoot
	if root == "" {
		root = filepath.Join(".", "data", "workflow-artifacts")
	}
	dir := filepath.Join(root, workflow.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	fileName := fmt.Sprintf("%02d-%s.md", stage.Order, strings.ReplaceAll(strings.TrimSpace(artifactType), "_", "-"))
	if stage.ID == "" {
		fileName = fmt.Sprintf("%s.md", strings.ReplaceAll(strings.TrimSpace(artifactType), "_", "-"))
	}
	target := filepath.Join(dir, fileName)
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return nil, err
	}
	artifacts, err := s.store.ListArtifacts(workflow.ID)
	if err != nil {
		return nil, err
	}
	version := 1
	artifactID := ""
	for _, item := range artifacts {
		if item.ArtifactType == artifactType && item.StageRunID == stage.ID {
			version = item.Version + 1
			artifactID = item.ID
			break
		}
	}
	hash := sha256.Sum256([]byte(content))
	return s.store.UpsertArtifact(domain.Artifact{
		ID:            artifactID,
		ProjectID:     workflow.ProjectID,
		RequirementID: requirement.ID,
		WorkflowRunID: workflow.ID,
		StageRunID:    stage.ID,
		ArtifactType:  artifactType,
		Title:         title,
		Path:          filepath.Clean(target),
		Version:       version,
		Status:        domain.ArtifactStatusGenerated,
		Source:        domain.ArtifactSourceSystem,
		ContentHash:   hex.EncodeToString(hash[:]),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	})
}

func (s *WorkflowService) inspectGitWorkspace(workDir string) gitWorkspaceState {
	state := gitWorkspaceState{WorkspaceDigest: hashString(filepath.Clean(workDir))}
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		return state
	}
	if text, err := s.runGit(workDir, "rev-parse", "--is-inside-work-tree"); err == nil && strings.TrimSpace(text) == "true" {
		state.Available = true
	}
	state.FileCount = countWorkspaceFiles(workDir)
	if !state.Available {
		return state
	}
	state.Branch, _ = s.runGit(workDir, "rev-parse", "--abbrev-ref", "HEAD")
	state.Commit, _ = s.runGit(workDir, "rev-parse", "HEAD")
	statusText, _ := s.runGit(workDir, "status", "--porcelain=1", "--branch")
	lines := splitMeaningfulLines(statusText)
	for _, line := range lines {
		if strings.HasPrefix(line, "##") {
			continue
		}
		if len(line) < 3 {
			continue
		}
		code := line[:2]
		pathText := strings.TrimSpace(line[3:])
		pathText = strings.Trim(strings.ReplaceAll(pathText, "\"", ""), " ")
		if strings.Contains(pathText, " -> ") {
			parts := strings.Split(pathText, " -> ")
			pathText = strings.TrimSpace(parts[len(parts)-1])
		}
		if pathText == "" {
			continue
		}
		if code == "??" {
			state.Dirty = true
			state.UntrackedPaths = append(state.UntrackedPaths, pathText)
			continue
		}
		if strings.TrimSpace(code) != "" {
			state.Dirty = true
			state.ChangedPaths = append(state.ChangedPaths, pathText)
		}
	}
	state.BaseRef = strings.TrimSpace(state.Commit)
	state.WorkspaceDigest = hashString(strings.Join(append(append([]string{state.Commit, state.Branch}, state.ChangedPaths...), state.UntrackedPaths...), "|"))
	return state
}

func (s *WorkflowService) collectChangeSet(workDir string, baseSnapshot *domain.CodeSnapshot) (gitChangeSetResult, error) {
	if baseSnapshot == nil {
		return gitChangeSetResult{}, nil
	}
	baseRef := strings.TrimSpace(baseSnapshot.GitCommit)
	if baseRef == "" {
		baseRef = "HEAD"
	}
	nameStatus, err := s.runGit(workDir, "diff", "--name-status", "--find-renames", "--no-color", baseRef)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "exit status 1") {
		return gitChangeSetResult{}, err
	}
	numstat, err := s.runGit(workDir, "diff", "--numstat", "--find-renames", "--no-color", baseRef)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "exit status 1") {
		return gitChangeSetResult{}, err
	}
	patch, err := s.runGit(workDir, "diff", "--find-renames", "--no-color", baseRef)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "exit status 1") {
		return gitChangeSetResult{}, err
	}
	statusText, err := s.runGit(workDir, "status", "--porcelain=1")
	if err != nil {
		return gitChangeSetResult{}, err
	}

	files := parseGitNameStatus(nameStatus)
	applyNumstat(files, parseGitNumstat(numstat))
	appendUntrackedChanges(workDir, files, statusText, &patch)
	fileList := make([]domain.WorkflowFileChange, 0, len(files))
	for _, item := range files {
		fileList = append(fileList, item)
	}
	sort.SliceStable(fileList, func(i, j int) bool {
		return fileList[i].Path < fileList[j].Path
	})
	stats := summarizeWorkflowFileChanges(fileList)
	return gitChangeSetResult{
		Files:   fileList,
		Stats:   stats,
		Patch:   defaultIfBlank(strings.TrimSpace(patch), "# No diff\n"),
		Summary: buildChangeSetSummary(stats),
	}, nil
}

func (s *WorkflowService) runGit(workDir string, args ...string) (string, error) {
	ctx := context.Background()
	if s.gitTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.gitTimeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", workDir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		if text == "" {
			return "", err
		}
		return text, fmt.Errorf("%w: %s", err, text)
	}
	return text, nil
}

func workflowAction(status string) string {
	switch status {
	case domain.WorkflowStatusCompleted:
		return "completed"
	case domain.WorkflowStatusFailed:
		return "rejected"
	case domain.WorkflowStatusPaused:
		return "updated"
	default:
		return "started"
	}
}

func reviewAction(status string) string {
	switch status {
	case domain.ReviewGateStatusApproved, domain.ReviewGateStatusWaived:
		return "approved"
	case domain.ReviewGateStatusRejected:
		return "rejected"
	default:
		return "updated"
	}
}

func buildRequirementBriefContent(requirement domain.Requirement, gitState gitWorkspaceState, decisions []domain.DecisionRequest, reviews []domain.ReviewGate) string {
	pendingDecisions := countDecisionsByStatus(decisions, domain.DecisionStatusPending)
	resolvedDecisions := countDecisionsByStatus(decisions, domain.DecisionStatusResolved)
	var builder strings.Builder
	builder.WriteString("# 需求摘要\n\n")
	builder.WriteString("## 基本信息\n\n")
	builder.WriteString("- 标题：" + requirement.Title + "\n")
	builder.WriteString("- 执行模式：" + defaultIfBlank(requirement.ExecutionMode, "manual") + "\n")
	builder.WriteString("- 当前项目：" + defaultIfBlank(requirement.ProjectName, requirement.ProjectID) + "\n\n")
	builder.WriteString("## 原始描述\n\n")
	builder.WriteString(requirement.Description + "\n\n")
	builder.WriteString("## 基线判断\n\n")
	if gitState.Available {
		builder.WriteString("- Git 分支：" + defaultIfBlank(gitState.Branch, "unknown") + "\n")
		builder.WriteString("- Git Commit：" + defaultIfBlank(gitState.Commit, "unknown") + "\n")
		builder.WriteString("- 工作区状态：" + ternaryString(gitState.Dirty, "脏工作区", "干净工作区") + "\n\n")
	} else {
		builder.WriteString("- 当前目录不是 Git 工作区，后续 diff 与交付能力将降级。\n\n")
	}
	builder.WriteString("## 待确认与审核\n\n")
	builder.WriteString(fmt.Sprintf("- 待确认项：%d\n", pendingDecisions))
	if resolvedDecisions > 0 {
		builder.WriteString(fmt.Sprintf("- 已自动处理风险项：%d\n", resolvedDecisions))
	}
	builder.WriteString(fmt.Sprintf("- 人工审核门：%d\n", len(reviews)))
	return builder.String()
}

func buildSolutionDesignContent(requirement domain.Requirement, gitState gitWorkspaceState, decisions []domain.DecisionRequest, reviews []domain.ReviewGate) string {
	pendingDecisions := countDecisionsByStatus(decisions, domain.DecisionStatusPending)
	resolvedDecisions := countDecisionsByStatus(decisions, domain.DecisionStatusResolved)
	var builder strings.Builder
	builder.WriteString("# 方案设计\n\n")
	builder.WriteString("## 目标\n\n")
	builder.WriteString("- 将需求拆解为可自动执行、可人工接管、可追踪 diff 的工作流。\n")
	builder.WriteString("- 在设计、审核、测试、交付节点保留人工控制权。\n\n")
	builder.WriteString("## 判定策略\n\n")
	builder.WriteString("1. CLI 进程存活不等于阶段完成，必须结合结构化产物、任务项、审核门、决策点综合判定。\n")
	builder.WriteString("2. 若存在待确认问题，则工作流进入 `awaiting_input`，禁止继续自动推进。\n")
	builder.WriteString("3. 若存在待审核或被拒绝审核，则工作流进入 `awaiting_review` 或 `blocked`。\n")
	builder.WriteString("4. 代码差异统一从 Git 基线归集；若启动时工作区不干净，默认需要确认基线，自动需求可接受当前工作区并记录风险决策。\n\n")
	builder.WriteString("## 规范接入点\n\n")
	builder.WriteString("- 设计规范：设计文档、关键约束、接口变更说明。\n")
	builder.WriteString("- 代码规范：改动 diff、文件范围、测试覆盖、人工 code review。\n")
	builder.WriteString("- 系统规范：目录结构、交付方式、发布门禁与审核结论。\n\n")
	builder.WriteString("## 当前输入信号\n\n")
	builder.WriteString(fmt.Sprintf("- 待确认项：%d\n", pendingDecisions))
	if resolvedDecisions > 0 {
		builder.WriteString(fmt.Sprintf("- 已自动处理风险项：%d\n", resolvedDecisions))
	}
	builder.WriteString(fmt.Sprintf("- 审核门：%d\n", len(reviews)))
	if gitState.Available {
		builder.WriteString("- Git 基线：" + defaultIfBlank(gitState.Commit, "unknown") + "\n")
	}
	return builder.String()
}

func buildTaskBreakdownContent(requirement domain.Requirement) string {
	lines := splitMeaningfulLines(requirement.Description)
	items := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.Contains(line, "待确认") || strings.Contains(line, "需确认") || strings.Contains(line, "待定") {
			continue
		}
		items = append(items, line)
	}
	if len(items) == 0 {
		items = []string{
			"整理需求与约束",
			"输出实现方案",
			"完成代码改动",
			"补充测试与差异审阅材料",
		}
	}
	var builder strings.Builder
	builder.WriteString("# 任务拆解\n\n")
	for idx, item := range items {
		builder.WriteString(fmt.Sprintf("%d. %s\n", idx+1, item))
	}
	return builder.String()
}

func buildTestPlanContent(requirement domain.Requirement) string {
	var builder strings.Builder
	builder.WriteString("# 测试计划\n\n")
	builder.WriteString("- 验证需求主路径是否达成。\n")
	builder.WriteString("- 验证受影响模块的回归风险。\n")
	builder.WriteString("- 验证 diff 中新增和修改文件是否都有对应说明。\n")
	if strings.Contains(requirement.Description, "测试") {
		builder.WriteString("- 补充需求中显式要求的测试项。\n")
	}
	return builder.String()
}

func buildChangeSummaryContent(changeSet domain.ChangeSet) string {
	var builder strings.Builder
	builder.WriteString("# 变更摘要\n\n")
	builder.WriteString(changeSet.Summary + "\n\n")
	builder.WriteString("## 文件列表\n\n")
	for _, file := range changeSet.Files {
		builder.WriteString(fmt.Sprintf("- [%s] %s (+%d/-%d)\n", file.Status, file.Path, file.Additions, file.Deletions))
	}
	return builder.String()
}

func defaultActivityTitle(title, fallback string) string {
	if strings.TrimSpace(title) != "" {
		return title
	}
	return fallback
}

func firstBlockingPendingDecision(items []domain.DecisionRequest) *domain.DecisionRequest {
	for i := range items {
		if items[i].Blocking && items[i].Status == domain.DecisionStatusPending {
			return &items[i]
		}
	}
	return nil
}

func countDecisionsByStatus(items []domain.DecisionRequest, status string) int {
	total := 0
	for i := range items {
		if items[i].Status == status {
			total++
		}
	}
	return total
}

func firstPendingReview(items []domain.ReviewGate) *domain.ReviewGate {
	for i := range items {
		if items[i].Status == domain.ReviewGateStatusPending {
			return &items[i]
		}
	}
	return nil
}

func firstRejectedReview(items []domain.ReviewGate) *domain.ReviewGate {
	for i := range items {
		if items[i].Status == domain.ReviewGateStatusRejected {
			return &items[i]
		}
	}
	return nil
}

func hasPendingReviewForStage(items []domain.ReviewGate, stageName string) bool {
	for _, item := range items {
		if item.StageName == stageName && item.Status == domain.ReviewGateStatusPending {
			return true
		}
	}
	return false
}

func hasRejectedReviewForStage(items []domain.ReviewGate, stageName string) bool {
	for _, item := range items {
		if item.StageName == stageName && item.Status == domain.ReviewGateStatusRejected {
			return true
		}
	}
	return false
}

func computeWorkflowProgress(stages []domain.StageRun, requirementStatus, currentStage string, fallback int) int {
	if requirementStatus == domain.RequirementStatusDone && currentStage == domain.WorkflowStageRelease {
		return 100
	}
	completedWeight := 0
	for _, stage := range stages {
		switch stage.Status {
		case domain.StageStatusCompleted:
			completedWeight += 100
		case domain.StageStatusRunning:
			completedWeight += 60
		case domain.StageStatusAwaitingInput, domain.StageStatusAwaitingReview:
			completedWeight += 40
		case domain.StageStatusBlocked, domain.StageStatusInterrupted:
			completedWeight += 20
		}
	}
	if len(stages) == 0 {
		return fallback
	}
	value := completedWeight / len(stages)
	if value < fallback && fallback > 0 {
		return fallback
	}
	if value > 100 {
		return 100
	}
	return value
}

func summarizeWorkflowFileChanges(files []domain.WorkflowFileChange) domain.ChangeSetFileStats {
	stats := domain.ChangeSetFileStats{}
	for _, file := range files {
		switch file.Status {
		case "A":
			stats.Added++
		case "D":
			stats.Deleted++
		case "R":
			stats.Renamed++
		default:
			stats.Modified++
		}
		stats.TotalAdditions += file.Additions
		stats.TotalDeletions += file.Deletions
	}
	return stats
}

func buildChangeSetSummary(stats domain.ChangeSetFileStats) string {
	totalFiles := stats.Added + stats.Modified + stats.Deleted + stats.Renamed
	if totalFiles == 0 {
		return "未检测到相对基线的代码改动。"
	}
	return fmt.Sprintf(
		"%d 个文件发生变化：新增 %d，修改 %d，删除 %d，重命名 %d，累计 +%d/-%d。",
		totalFiles,
		stats.Added,
		stats.Modified,
		stats.Deleted,
		stats.Renamed,
		stats.TotalAdditions,
		stats.TotalDeletions,
	)
}

func parseGitNameStatus(raw string) map[string]domain.WorkflowFileChange {
	result := make(map[string]domain.WorkflowFileChange)
	for _, line := range splitMeaningfulLines(raw) {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		status := strings.TrimSpace(fields[0])
		switch {
		case strings.HasPrefix(status, "R") && len(fields) >= 3:
			oldPath := strings.TrimSpace(fields[1])
			newPath := strings.TrimSpace(fields[2])
			result[newPath] = domain.WorkflowFileChange{Path: newPath, Status: "R", OldPath: oldPath}
		default:
			pathText := strings.TrimSpace(fields[1])
			result[pathText] = domain.WorkflowFileChange{Path: pathText, Status: string(status[0])}
		}
	}
	return result
}

func parseGitNumstat(raw string) map[string][2]int {
	result := make(map[string][2]int)
	for _, line := range splitMeaningfulLines(raw) {
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			continue
		}
		additions, _ := strconv.Atoi(strings.TrimSpace(fields[0]))
		deletions, _ := strconv.Atoi(strings.TrimSpace(fields[1]))
		pathText := strings.TrimSpace(fields[2])
		if strings.Contains(pathText, " => ") {
			parts := strings.Split(pathText, " => ")
			pathText = strings.TrimSpace(parts[len(parts)-1])
		}
		result[pathText] = [2]int{additions, deletions}
	}
	return result
}

func applyNumstat(files map[string]domain.WorkflowFileChange, numstat map[string][2]int) {
	for pathText, stats := range numstat {
		item := files[pathText]
		item.Path = pathText
		if item.Status == "" {
			item.Status = "M"
		}
		item.Additions = stats[0]
		item.Deletions = stats[1]
		files[pathText] = item
	}
}

func appendUntrackedChanges(workDir string, files map[string]domain.WorkflowFileChange, statusText string, patch *string) {
	for _, line := range splitMeaningfulLines(statusText) {
		if !strings.HasPrefix(line, "?? ") {
			continue
		}
		pathText := strings.TrimSpace(strings.TrimPrefix(line, "?? "))
		if pathText == "" {
			continue
		}
		if _, ok := files[pathText]; ok {
			continue
		}
		content, err := os.ReadFile(filepath.Join(workDir, filepath.Clean(pathText)))
		if err != nil {
			continue
		}
		additions := countLines(content)
		files[pathText] = domain.WorkflowFileChange{
			Path:      pathText,
			Status:    "A",
			Additions: additions,
			Deletions: 0,
		}
		if patch == nil {
			continue
		}
		*patch = strings.TrimSpace(*patch + "\n\n" + buildUntrackedPatch(pathText, content))
	}
}

func buildUntrackedPatch(pathText string, content []byte) string {
	var builder strings.Builder
	builder.WriteString("diff --git a/" + pathText + " b/" + pathText + "\n")
	builder.WriteString("new file mode 100644\n")
	builder.WriteString("--- /dev/null\n")
	builder.WriteString("+++ b/" + pathText + "\n")
	builder.WriteString("@@\n")
	for _, line := range strings.Split(string(content), "\n") {
		if line == "" {
			builder.WriteString("+\n")
			continue
		}
		builder.WriteString("+" + line + "\n")
	}
	return strings.TrimRight(builder.String(), "\n")
}

func splitMeaningfulLines(raw string) []string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		result = append(result, line)
	}
	return result
}

func trimDecisionPrefix(line string) string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "待确认:")
	line = strings.TrimPrefix(line, "待确认：")
	line = strings.TrimPrefix(line, "需确认:")
	line = strings.TrimPrefix(line, "需确认：")
	line = strings.TrimPrefix(line, "待定:")
	line = strings.TrimPrefix(line, "待定：")
	return strings.TrimSpace(line)
}

func buildDirtyWorkspaceContext(state gitWorkspaceState) string {
	parts := make([]string, 0, len(state.ChangedPaths)+len(state.UntrackedPaths))
	for _, pathText := range state.ChangedPaths {
		parts = append(parts, "changed:"+pathText)
	}
	for _, pathText := range state.UntrackedPaths {
		parts = append(parts, "untracked:"+pathText)
	}
	if len(parts) == 0 {
		return "检测到工作区不干净，但未成功解析具体路径。"
	}
	if len(parts) > 8 {
		parts = append(parts[:8], "...")
	}
	return strings.Join(parts, "\n")
}

func countWorkspaceFiles(root string) int {
	count := 0
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.Type().IsRegular() {
			count++
		}
		return nil
	})
	return count
}

func countLines(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	return bytes.Count(content, []byte{'\n'}) + 1
}

func workflowStageOrder(name string) int {
	for _, def := range workflowStageDefinitions {
		if def.Name == strings.TrimSpace(name) {
			return def.Order
		}
	}
	return 0
}

func defaultResultSummary(current, fallback string) string {
	if strings.TrimSpace(current) != "" {
		return current
	}
	return fallback
}

func defaultIfBlank(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func coalesceTime(value *time.Time, fallback time.Time) time.Time {
	if value != nil && !value.IsZero() {
		return *value
	}
	return fallback
}

func coalesceTimePtr(value *time.Time, fallback time.Time) *time.Time {
	if value != nil && !value.IsZero() {
		return value
	}
	ts := fallback
	return &ts
}

func ternaryString(condition bool, truthy, falsy string) string {
	if condition {
		return truthy
	}
	return falsy
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
