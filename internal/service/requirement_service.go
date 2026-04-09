package service

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"auto-code/internal/domain"
	"auto-code/internal/persistence"
)

// RequirementService encapsulates requirement domain rules and status transitions.
type RequirementService struct {
	store           *persistence.SQLiteStore
	projectService  *ProjectService
	workflowService *WorkflowService
}

// NewRequirementService constructs a requirement service.
func NewRequirementService(store *persistence.SQLiteStore, projectService *ProjectService) *RequirementService {
	return &RequirementService{store: store, projectService: projectService}
}

// SetWorkflowService wires the derived workflow service used for lifecycle synchronization.
func (s *RequirementService) SetWorkflowService(workflowService *WorkflowService) {
	if s == nil {
		return
	}
	s.workflowService = workflowService
}

// List returns requirements with optional project filter.
func (s *RequirementService) List(projectID string) ([]domain.Requirement, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("requirement service is not initialized")
	}
	list, err := s.store.ListRequirements(projectID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		list[i].ProjectWorkDir = s.projectService.EffectiveWorkDir(domain.Project{
			Name:    list[i].ProjectName,
			WorkDir: list[i].ProjectWorkDir,
		})
	}
	return list, nil
}

// ListByProject returns requirements scoped to one project.
func (s *RequirementService) ListByProject(projectID string) ([]domain.Requirement, error) {
	return s.List(projectID)
}

// Get loads one requirement by id.
func (s *RequirementService) Get(requirementID string) (*domain.Requirement, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("requirement service is not initialized")
	}
	requirement, err := s.store.GetRequirement(requirementID)
	if err != nil {
		return nil, err
	}
	requirement.ProjectWorkDir = s.projectService.EffectiveWorkDir(domain.Project{
		Name:    requirement.ProjectName,
		WorkDir: requirement.ProjectWorkDir,
	})
	return requirement, nil
}

// Create validates payload and creates one requirement.
func (s *RequirementService) Create(input domain.RequirementMutation) (*domain.Requirement, error) {
	normalized, err := s.normalizeMutation(input)
	if err != nil {
		return nil, err
	}
	return s.store.CreateRequirement(normalized)
}

// Update validates payload and updates one requirement.
func (s *RequirementService) Update(requirementID string, input domain.RequirementMutation) (*domain.Requirement, error) {
	normalized, err := s.normalizeMutation(input)
	if err != nil {
		return nil, err
	}
	return s.store.UpdateRequirement(requirementID, normalized)
}

// Delete removes one requirement.
func (s *RequirementService) Delete(requirementID string) error {
	if s == nil || s.store == nil {
		return errors.New("requirement service is not initialized")
	}
	requirementID = strings.TrimSpace(requirementID)
	if requirementID == "" {
		return errors.New("requirement id is required")
	}
	return s.store.DeleteRequirement(requirementID)
}

// Move adjusts one requirement's explicit order within its project.
func (s *RequirementService) Move(requirementID string, targetSortOrder int) (*domain.Requirement, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("requirement service is not initialized")
	}
	requirementID = strings.TrimSpace(requirementID)
	if requirementID == "" {
		return nil, errors.New("requirement id is required")
	}
	if targetSortOrder <= 0 {
		return nil, errors.New("target sort order must be positive")
	}
	return s.store.MoveRequirement(requirementID, targetSortOrder)
}

// Transition applies one status action following defined state machine constraints.
func (s *RequirementService) Transition(requirementID, action string) (*domain.Requirement, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("requirement service is not initialized")
	}
	requirementID = strings.TrimSpace(requirementID)
	if requirementID == "" {
		return nil, errors.New("requirement id is required")
	}
	action = strings.TrimSpace(strings.ToLower(action))
	if action == "end" {
		action = "done"
	}
	if action == "" {
		return nil, errors.New("action is required")
	}
	if action == "start" {
		requirement, err := s.Get(requirementID)
		if err != nil {
			return nil, err
		}
		requirements, err := s.ListByProject(requirement.ProjectID)
		if err != nil {
			return nil, err
		}
		for _, item := range requirements {
			if item.ID == requirementID {
				continue
			}
			if item.Status == domain.RequirementStatusRunning {
				return nil, fmt.Errorf("project already has running requirement: %s", item.Title)
			}
		}
	}
	requirement, err := s.store.TransitionRequirement(requirementID, action)
	if err != nil {
		return nil, err
	}
	if s.workflowService != nil {
		if syncErr := s.workflowService.SyncRequirement(*requirement, action); syncErr != nil {
			return nil, syncErr
		}
	}
	return requirement, nil
}

// MarkPromptDispatched records the latest automatic prompt dispatch time.
func (s *RequirementService) MarkPromptDispatched(requirementID string, at time.Time) error {
	if s == nil || s.store == nil {
		return errors.New("requirement service is not initialized")
	}
	requirementID = strings.TrimSpace(requirementID)
	if requirementID == "" {
		return errors.New("requirement id is required")
	}
	requirement, err := s.store.GetRequirement(requirementID)
	if err != nil {
		return err
	}
	if requirement.Status != domain.RequirementStatusRunning {
		return nil
	}
	return s.store.UpdateRequirementPromptDispatchedAt(requirementID, at)
}

// MarkPromptReplayed records the latest automatic prompt replay time.
func (s *RequirementService) MarkPromptReplayed(requirementID string, at time.Time) error {
	if s == nil || s.store == nil {
		return errors.New("requirement service is not initialized")
	}
	requirementID = strings.TrimSpace(requirementID)
	if requirementID == "" {
		return errors.New("requirement id is required")
	}
	requirement, err := s.store.GetRequirement(requirementID)
	if err != nil {
		return err
	}
	if requirement.Status != domain.RequirementStatusRunning {
		return nil
	}
	return s.store.UpdateRequirementPromptReplayedAt(requirementID, at)
}

// ConsumeRetryBudget increments one requirement-level automation retry attempt if budget remains.
func (s *RequirementService) ConsumeRetryBudget(requirementID string, maxAttempts int, reason string, at time.Time) (int, bool, error) {
	if s == nil || s.store == nil {
		return 0, false, errors.New("requirement service is not initialized")
	}
	requirementID = strings.TrimSpace(requirementID)
	if requirementID == "" {
		return 0, false, errors.New("requirement id is required")
	}
	return s.store.ConsumeRequirementRetryBudget(requirementID, maxAttempts, reason, at)
}

// MarkRetryBudgetExhausted persists that the requirement exhausted its automation retry budget.
func (s *RequirementService) MarkRetryBudgetExhausted(requirementID, reason string, at time.Time) error {
	if s == nil || s.store == nil {
		return errors.New("requirement service is not initialized")
	}
	requirementID = strings.TrimSpace(requirementID)
	if requirementID == "" {
		return errors.New("requirement id is required")
	}
	return s.store.MarkRequirementRetryBudgetExhausted(requirementID, reason, at)
}

// normalizeMutation validates requirement create/update payload.
func (s *RequirementService) normalizeMutation(input domain.RequirementMutation) (domain.RequirementMutation, error) {
	if s == nil || s.store == nil {
		return domain.RequirementMutation{}, errors.New("requirement service is not initialized")
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	input.ExecutionMode = strings.TrimSpace(strings.ToLower(input.ExecutionMode))
	input.CLIType = strings.TrimSpace(strings.ToLower(input.CLIType))
	input.NoResponseErrorAction = strings.TrimSpace(strings.ToLower(input.NoResponseErrorAction))
	input.NoResponseIdleAction = strings.TrimSpace(strings.ToLower(input.NoResponseIdleAction))

	if input.ProjectID == "" {
		return domain.RequirementMutation{}, errors.New("project_id is required")
	}
	if input.Title == "" {
		return domain.RequirementMutation{}, errors.New("title is required")
	}
	if input.ExecutionMode == "" {
		input.ExecutionMode = domain.RequirementExecutionModeManual
	}
	if !domain.IsRequirementExecutionMode(input.ExecutionMode) {
		return domain.RequirementMutation{}, errors.New("execution_mode must be manual or auto")
	}
	if input.NoResponseTimeoutMinutes < 0 {
		return domain.RequirementMutation{}, errors.New("no_response_timeout_minutes must be greater than or equal to 0")
	}
	if input.NoResponseErrorAction == "" {
		input.NoResponseErrorAction = domain.RequirementNoResponseActionNone
	}
	if !domain.IsRequirementNoResponseErrorAction(input.NoResponseErrorAction) {
		return domain.RequirementMutation{}, errors.New("no_response_error_action must be none or close_and_resend_requirement")
	}
	if input.NoResponseIdleAction == "" {
		input.NoResponseIdleAction = domain.RequirementNoResponseActionNone
	}
	if !domain.IsRequirementNoResponseIdleAction(input.NoResponseIdleAction) {
		return domain.RequirementMutation{}, errors.New("no_response_idle_action must be none or resend_requirement")
	}
	if _, err := s.projectService.Get(input.ProjectID); err != nil {
		if errors.Is(err, persistence.ErrNotFound) {
			return domain.RequirementMutation{}, errors.New("project not found")
		}
		return domain.RequirementMutation{}, err
	}
	return input, nil
}

// SortRequirementsByQueue returns a created-at ascending ordering suitable for queue processing.
func SortRequirementsByQueue(items []domain.Requirement) []domain.Requirement {
	list := append([]domain.Requirement(nil), items...)
	sort.SliceStable(list, func(i, j int) bool {
		if !list[i].CreatedAt.Equal(list[j].CreatedAt) {
			return list[i].CreatedAt.Before(list[j].CreatedAt)
		}
		return list[i].ID < list[j].ID
	})
	return list
}
