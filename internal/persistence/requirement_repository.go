package persistence

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"auto-code/internal/domain"
)

// CreateRequirement inserts a new requirement bound to a project.
func (s *SQLiteStore) CreateRequirement(input domain.RequirementMutation) (*domain.Requirement, error) {
	now := time.Now()
	id := newPrefixedID("req")
	executionMode := strings.TrimSpace(input.ExecutionMode)
	if executionMode == "" {
		executionMode = domain.RequirementExecutionModeManual
	}
	noResponseErrorAction := strings.TrimSpace(input.NoResponseErrorAction)
	if noResponseErrorAction == "" {
		noResponseErrorAction = domain.RequirementNoResponseActionNone
	}
	noResponseIdleAction := strings.TrimSpace(input.NoResponseIdleAction)
	if noResponseIdleAction == "" {
		noResponseIdleAction = domain.RequirementNoResponseActionNone
	}
	autoClearSession := 0
	if input.AutoClearSession {
		autoClearSession = 1
	}
	requiresDesignReview := 0
	if input.RequiresDesignReview {
		requiresDesignReview = 1
	}
	requiresCodeReview := 0
	if input.RequiresCodeReview {
		requiresCodeReview = 1
	}
	requiresAcceptanceReview := 0
	if input.RequiresAcceptanceReview {
		requiresAcceptanceReview = 1
	}
	requiresReleaseApproval := 0
	if input.RequiresReleaseApproval {
		requiresReleaseApproval = 1
	}
	projectID := strings.TrimSpace(input.ProjectID)
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	sortOrder, err := nextRequirementSortOrder(tx, projectID)
	if err != nil {
		return nil, err
	}

	if _, err := tx.Exec(`INSERT INTO requirements(
			id, project_id, sort_order, title, description, status, execution_mode, cli_type, auto_clear_session,
			no_response_timeout_minutes, no_response_error_action, no_response_idle_action,
			requires_design_review, requires_code_review, requires_acceptance_review, requires_release_approval,
			created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		projectID,
		sortOrder,
		strings.TrimSpace(input.Title),
		strings.TrimSpace(input.Description),
		domain.RequirementStatusPlanning,
		executionMode,
		strings.TrimSpace(strings.ToLower(input.CLIType)),
		autoClearSession,
		input.NoResponseTimeoutMinutes,
		noResponseErrorAction,
		noResponseIdleAction,
		requiresDesignReview,
		requiresCodeReview,
		requiresAcceptanceReview,
		requiresReleaseApproval,
		toUnixMilli(now),
		toUnixMilli(now),
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetRequirement(id)
}

// UpdateRequirement updates title/description/project binding fields of a requirement.
func (s *SQLiteStore) UpdateRequirement(requirementID string, input domain.RequirementMutation) (*domain.Requirement, error) {
	now := time.Now()
	executionMode := strings.TrimSpace(input.ExecutionMode)
	if executionMode == "" {
		executionMode = domain.RequirementExecutionModeManual
	}
	noResponseErrorAction := strings.TrimSpace(input.NoResponseErrorAction)
	if noResponseErrorAction == "" {
		noResponseErrorAction = domain.RequirementNoResponseActionNone
	}
	noResponseIdleAction := strings.TrimSpace(input.NoResponseIdleAction)
	if noResponseIdleAction == "" {
		noResponseIdleAction = domain.RequirementNoResponseActionNone
	}
	autoClearSession := 0
	if input.AutoClearSession {
		autoClearSession = 1
	}
	requiresDesignReview := 0
	if input.RequiresDesignReview {
		requiresDesignReview = 1
	}
	requiresCodeReview := 0
	if input.RequiresCodeReview {
		requiresCodeReview = 1
	}
	requiresAcceptanceReview := 0
	if input.RequiresAcceptanceReview {
		requiresAcceptanceReview = 1
	}
	requiresReleaseApproval := 0
	if input.RequiresReleaseApproval {
		requiresReleaseApproval = 1
	}
	requirementID = strings.TrimSpace(requirementID)
	targetProjectID := strings.TrimSpace(input.ProjectID)
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var currentProjectID string
	var currentSortOrder int
	if err := tx.QueryRow(`SELECT project_id, sort_order FROM requirements WHERE id = ?`, requirementID).Scan(&currentProjectID, &currentSortOrder); err != nil {
		return nil, translateNoRows(err)
	}

	nextSortOrder := currentSortOrder
	if targetProjectID != currentProjectID {
		nextSortOrder, err = nextRequirementSortOrder(tx, targetProjectID)
		if err != nil {
			return nil, err
		}
	}

	result, err := tx.Exec(`UPDATE requirements
		SET project_id = ?, sort_order = ?, title = ?, description = ?, execution_mode = ?, cli_type = ?, auto_clear_session = ?,
			no_response_timeout_minutes = ?, no_response_error_action = ?, no_response_idle_action = ?,
			requires_design_review = ?, requires_code_review = ?, requires_acceptance_review = ?, requires_release_approval = ?,
			updated_at = ?
		WHERE id = ?`,
		targetProjectID,
		nextSortOrder,
		strings.TrimSpace(input.Title),
		strings.TrimSpace(input.Description),
		executionMode,
		strings.TrimSpace(strings.ToLower(input.CLIType)),
		autoClearSession,
		input.NoResponseTimeoutMinutes,
		noResponseErrorAction,
		noResponseIdleAction,
		requiresDesignReview,
		requiresCodeReview,
		requiresAcceptanceReview,
		requiresReleaseApproval,
		toUnixMilli(now),
		requirementID,
	)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetRequirement(requirementID)
}

// DeleteRequirement deletes one requirement.
// Project-bound CLI sessions are kept; legacy requirement-bound sessions may still cascade via FK.
func (s *SQLiteStore) DeleteRequirement(requirementID string) error {
	result, err := s.db.Exec(`DELETE FROM requirements WHERE id = ?`, strings.TrimSpace(requirementID))
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// GetRequirement returns one requirement with project metadata.
func (s *SQLiteStore) GetRequirement(requirementID string) (*domain.Requirement, error) {
	row := s.db.QueryRow(`SELECT
		r.id, r.project_id, p.name, p.branch, p.work_dir, r.sort_order,
		r.title, r.description, r.status, r.execution_mode, r.cli_type, r.auto_clear_session,
		r.no_response_timeout_minutes, r.no_response_error_action, r.no_response_idle_action,
		r.requires_design_review, r.requires_code_review, r.requires_acceptance_review, r.requires_release_approval,
		r.created_at, r.started_at, r.ended_at, r.prompt_dispatched_at, r.prompt_replayed_at,
		r.auto_retry_attempts, r.last_auto_retry_reason, r.retry_budget_exhausted_at, r.updated_at
	FROM requirements r
	JOIN projects p ON p.id = r.project_id
	WHERE r.id = ?`, strings.TrimSpace(requirementID))
	return scanRequirement(row)
}

// ListRequirements returns requirements ordered by creation time with optional project filter.
func (s *SQLiteStore) ListRequirements(projectID string) ([]domain.Requirement, error) {
	projectID = strings.TrimSpace(projectID)
	baseSQL := `SELECT
		r.id, r.project_id, p.name, p.branch, p.work_dir, r.sort_order,
		r.title, r.description, r.status, r.execution_mode, r.cli_type, r.auto_clear_session,
		r.no_response_timeout_minutes, r.no_response_error_action, r.no_response_idle_action,
		r.requires_design_review, r.requires_code_review, r.requires_acceptance_review, r.requires_release_approval,
		r.created_at, r.started_at, r.ended_at, r.prompt_dispatched_at, r.prompt_replayed_at,
		r.auto_retry_attempts, r.last_auto_retry_reason, r.retry_budget_exhausted_at, r.updated_at
	FROM requirements r
	JOIN projects p ON p.id = r.project_id`
	args := make([]any, 0)
	if projectID != "" {
		baseSQL += ` WHERE r.project_id = ?`
		args = append(args, projectID)
	}
	baseSQL += ` ORDER BY r.sort_order ASC, r.created_at ASC, r.id ASC`

	rows, err := s.db.Query(baseSQL, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]domain.Requirement, 0)
	for rows.Next() {
		requirement, err := scanRequirement(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *requirement)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

// TransitionRequirement applies one valid status transition and persists all timestamps.
func (s *SQLiteStore) TransitionRequirement(requirementID, action string) (*domain.Requirement, error) {
	requirementID = strings.TrimSpace(requirementID)
	action = strings.TrimSpace(strings.ToLower(action))
	if action == "end" {
		action = "done"
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRow(`SELECT
			id, project_id, sort_order, title, description, status, execution_mode, cli_type, auto_clear_session,
			no_response_timeout_minutes, no_response_error_action, no_response_idle_action,
			requires_design_review, requires_code_review, requires_acceptance_review, requires_release_approval,
			created_at, started_at, ended_at, prompt_dispatched_at, prompt_replayed_at,
			auto_retry_attempts, last_auto_retry_reason, retry_budget_exhausted_at, updated_at
		FROM requirements WHERE id = ?`, requirementID)
	var requirement domain.Requirement
	var createdAt int64
	var startedAt sql.NullInt64
	var endedAt sql.NullInt64
	var promptDispatchedAt sql.NullInt64
	var promptReplayedAt sql.NullInt64
	var retryBudgetExhaustedAt sql.NullInt64
	var updatedAt int64
	var autoClearSession int64
	var requiresDesignReview int64
	var requiresCodeReview int64
	var requiresAcceptanceReview int64
	var requiresReleaseApproval int64
	if err := row.Scan(
		&requirement.ID,
		&requirement.ProjectID,
		&requirement.SortOrder,
		&requirement.Title,
		&requirement.Description,
		&requirement.Status,
		&requirement.ExecutionMode,
		&requirement.CLIType,
		&autoClearSession,
		&requirement.NoResponseTimeoutMinutes,
		&requirement.NoResponseErrorAction,
		&requirement.NoResponseIdleAction,
		&requiresDesignReview,
		&requiresCodeReview,
		&requiresAcceptanceReview,
		&requiresReleaseApproval,
		&createdAt,
		&startedAt,
		&endedAt,
		&promptDispatchedAt,
		&promptReplayedAt,
		&requirement.AutoRetryAttempts,
		&requirement.LastAutoRetryReason,
		&retryBudgetExhaustedAt,
		&updatedAt,
	); err != nil {
		return nil, translateNoRows(err)
	}
	requirement.AutoClearSession = autoClearSession != 0
	requirement.RequiresDesignReview = requiresDesignReview != 0
	requirement.RequiresCodeReview = requiresCodeReview != 0
	requirement.RequiresAcceptanceReview = requiresAcceptanceReview != 0
	requirement.RequiresReleaseApproval = requiresReleaseApproval != 0

	now := time.Now()
	nextStatus := requirement.Status
	nextStartedAt := startedAt
	nextEndedAt := endedAt
	nextPromptDispatchedAt := promptDispatchedAt
	nextPromptReplayedAt := promptReplayedAt
	nextAutoRetryAttempts := requirement.AutoRetryAttempts
	nextLastAutoRetryReason := strings.TrimSpace(requirement.LastAutoRetryReason)
	nextRetryBudgetExhaustedAt := retryBudgetExhaustedAt

	switch action {
	case "start":
		if requirement.Status != domain.RequirementStatusPlanning &&
			requirement.Status != domain.RequirementStatusPaused &&
			requirement.Status != domain.RequirementStatusFailed {
			return nil, fmt.Errorf("cannot start from status %s", requirement.Status)
		}
		nextStatus = domain.RequirementStatusRunning
		if requirement.Status == domain.RequirementStatusFailed {
			nextStartedAt = sql.NullInt64{Int64: toUnixMilli(now), Valid: true}
			nextEndedAt = sql.NullInt64{}
			nextPromptDispatchedAt = sql.NullInt64{}
			nextPromptReplayedAt = sql.NullInt64{}
			nextAutoRetryAttempts = 0
			nextLastAutoRetryReason = ""
			nextRetryBudgetExhaustedAt = sql.NullInt64{}
		} else if !nextStartedAt.Valid {
			nextStartedAt = sql.NullInt64{Int64: toUnixMilli(now), Valid: true}
		}
	case "pause":
		if requirement.Status != domain.RequirementStatusRunning {
			return nil, fmt.Errorf("cannot pause from status %s", requirement.Status)
		}
		nextStatus = domain.RequirementStatusPaused
	case "done":
		if requirement.Status != domain.RequirementStatusRunning && requirement.Status != domain.RequirementStatusPaused {
			return nil, fmt.Errorf("cannot complete from status %s", requirement.Status)
		}
		nextStatus = domain.RequirementStatusDone
		nextEndedAt = sql.NullInt64{Int64: toUnixMilli(now), Valid: true}
	case "fail":
		if requirement.Status != domain.RequirementStatusRunning && requirement.Status != domain.RequirementStatusPaused {
			return nil, fmt.Errorf("cannot fail from status %s", requirement.Status)
		}
		nextStatus = domain.RequirementStatusFailed
		nextEndedAt = sql.NullInt64{Int64: toUnixMilli(now), Valid: true}
	case "skip":
		if requirement.Status != domain.RequirementStatusFailed {
			return nil, fmt.Errorf("cannot skip from status %s", requirement.Status)
		}
		nextStatus = domain.RequirementStatusDone
		nextEndedAt = sql.NullInt64{Int64: toUnixMilli(now), Valid: true}
	default:
		return nil, errors.New("unsupported action")
	}

	if _, err := tx.Exec(`UPDATE requirements
		SET status = ?, started_at = ?, ended_at = ?, prompt_dispatched_at = ?, prompt_replayed_at = ?,
			auto_retry_attempts = ?, last_auto_retry_reason = ?, retry_budget_exhausted_at = ?, updated_at = ?
		WHERE id = ?`,
		nextStatus,
		nullInt64Arg(nextStartedAt),
		nullInt64Arg(nextEndedAt),
		nullInt64Arg(nextPromptDispatchedAt),
		nullInt64Arg(nextPromptReplayedAt),
		nextAutoRetryAttempts,
		nextLastAutoRetryReason,
		nullInt64Arg(nextRetryBudgetExhaustedAt),
		toUnixMilli(now),
		requirement.ID,
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetRequirement(requirement.ID)
}

// UpdateRequirementPromptDispatchedAt stores the latest prompt dispatch timestamp for one requirement.
func (s *SQLiteStore) UpdateRequirementPromptDispatchedAt(requirementID string, at time.Time) error {
	requirementID = strings.TrimSpace(requirementID)
	if requirementID == "" {
		return errors.New("requirement id is required")
	}
	if at.IsZero() {
		at = time.Now()
	}
	result, err := s.db.Exec(`UPDATE requirements
		SET prompt_dispatched_at = ?, updated_at = ?
		WHERE id = ?`,
		toUnixMilli(at),
		toUnixMilli(at),
		requirementID,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateRequirementPromptReplayedAt stores the latest replay prompt timestamp for one requirement.
func (s *SQLiteStore) UpdateRequirementPromptReplayedAt(requirementID string, at time.Time) error {
	requirementID = strings.TrimSpace(requirementID)
	if requirementID == "" {
		return errors.New("requirement id is required")
	}
	if at.IsZero() {
		at = time.Now()
	}
	result, err := s.db.Exec(`UPDATE requirements
		SET prompt_replayed_at = ?, updated_at = ?
		WHERE id = ?`,
		toUnixMilli(at),
		toUnixMilli(at),
		requirementID,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// ConsumeRequirementRetryBudget increments the requirement-level automation retry counter if budget remains.
func (s *SQLiteStore) ConsumeRequirementRetryBudget(requirementID string, maxAttempts int, reason string, at time.Time) (int, bool, error) {
	requirementID = strings.TrimSpace(requirementID)
	reason = strings.TrimSpace(reason)
	if requirementID == "" {
		return 0, false, errors.New("requirement id is required")
	}
	if maxAttempts <= 0 {
		return 0, true, nil
	}
	if at.IsZero() {
		at = time.Now()
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = tx.Rollback() }()

	var attempts int
	var exhaustedAt sql.NullInt64
	if err := tx.QueryRow(`SELECT auto_retry_attempts, retry_budget_exhausted_at
		FROM requirements
		WHERE id = ?`, requirementID).Scan(&attempts, &exhaustedAt); err != nil {
		return 0, false, translateNoRows(err)
	}
	if attempts >= maxAttempts || exhaustedAt.Valid {
		if !exhaustedAt.Valid {
			if _, err := tx.Exec(`UPDATE requirements
				SET last_auto_retry_reason = ?, retry_budget_exhausted_at = ?, updated_at = ?
				WHERE id = ?`,
				reason,
				toUnixMilli(at),
				toUnixMilli(at),
				requirementID,
			); err != nil {
				return attempts, true, err
			}
		}
		if err := tx.Commit(); err != nil {
			return attempts, true, err
		}
		return attempts, true, nil
	}

	attempts++
	if _, err := tx.Exec(`UPDATE requirements
		SET auto_retry_attempts = ?, last_auto_retry_reason = ?, updated_at = ?
		WHERE id = ?`,
		attempts,
		reason,
		toUnixMilli(at),
		requirementID,
	); err != nil {
		return attempts, false, err
	}
	if err := tx.Commit(); err != nil {
		return attempts, false, err
	}
	return attempts, false, nil
}

func nextRequirementSortOrder(queryer interface {
	QueryRow(query string, args ...any) *sql.Row
}, projectID string) (int, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return 0, errors.New("project id is required")
	}
	var currentMax sql.NullInt64
	if err := queryer.QueryRow(`SELECT MAX(sort_order) FROM requirements WHERE project_id = ?`, projectID).Scan(&currentMax); err != nil {
		return 0, err
	}
	if !currentMax.Valid || currentMax.Int64 < 0 {
		return 1, nil
	}
	return int(currentMax.Int64) + 1, nil
}

func (s *SQLiteStore) MoveRequirement(requirementID string, targetSortOrder int) (*domain.Requirement, error) {
	requirementID = strings.TrimSpace(requirementID)
	if requirementID == "" {
		return nil, errors.New("requirement id is required")
	}
	if targetSortOrder <= 0 {
		return nil, errors.New("target sort order must be positive")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var projectID string
	if err := tx.QueryRow(`SELECT project_id FROM requirements WHERE id = ?`, requirementID).Scan(&projectID); err != nil {
		return nil, translateNoRows(err)
	}

	rows, err := tx.Query(`SELECT id
		FROM requirements
		WHERE project_id = ?
		ORDER BY sort_order ASC, created_at ASC, id ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, ErrNotFound
	}

	currentIndex := -1
	for idx, id := range ids {
		if id == requirementID {
			currentIndex = idx
			break
		}
	}
	if currentIndex < 0 {
		return nil, ErrNotFound
	}

	if targetSortOrder > len(ids) {
		targetSortOrder = len(ids)
	}
	targetIndex := targetSortOrder - 1
	if currentIndex == targetIndex {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return s.GetRequirement(requirementID)
	}

	movedID := ids[currentIndex]
	reordered := make([]string, 0, len(ids))
	for idx, id := range ids {
		if idx == currentIndex {
			continue
		}
		reordered = append(reordered, id)
	}
	head := append([]string{}, reordered[:targetIndex]...)
	tail := append([]string{movedID}, reordered[targetIndex:]...)
	reordered = append(head, tail...)

	for idx, id := range reordered {
		if _, err := tx.Exec(`UPDATE requirements SET sort_order = ? WHERE id = ?`, -(idx + 1), id); err != nil {
			return nil, err
		}
	}
	for idx, id := range reordered {
		if _, err := tx.Exec(`UPDATE requirements SET sort_order = ? WHERE id = ?`, idx+1, id); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetRequirement(requirementID)
}

// MarkRequirementRetryBudgetExhausted persists the exhausted timestamp and reason.
func (s *SQLiteStore) MarkRequirementRetryBudgetExhausted(requirementID, reason string, at time.Time) error {
	requirementID = strings.TrimSpace(requirementID)
	reason = strings.TrimSpace(reason)
	if requirementID == "" {
		return errors.New("requirement id is required")
	}
	if at.IsZero() {
		at = time.Now()
	}
	result, err := s.db.Exec(`UPDATE requirements
		SET last_auto_retry_reason = ?, retry_budget_exhausted_at = ?, updated_at = ?
		WHERE id = ?`,
		reason,
		toUnixMilli(at),
		toUnixMilli(at),
		requirementID,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// scanRequirement converts sql row to domain.Requirement with project metadata.
func scanRequirement(row scanner) (*domain.Requirement, error) {
	var requirement domain.Requirement
	var createdAt int64
	var startedAt sql.NullInt64
	var endedAt sql.NullInt64
	var promptDispatchedAt sql.NullInt64
	var promptReplayedAt sql.NullInt64
	var retryBudgetExhaustedAt sql.NullInt64
	var updatedAt int64
	var autoClearSession int64
	var requiresDesignReview int64
	var requiresCodeReview int64
	var requiresAcceptanceReview int64
	var requiresReleaseApproval int64
	if err := row.Scan(
		&requirement.ID,
		&requirement.ProjectID,
		&requirement.ProjectName,
		&requirement.ProjectBranch,
		&requirement.ProjectWorkDir,
		&requirement.SortOrder,
		&requirement.Title,
		&requirement.Description,
		&requirement.Status,
		&requirement.ExecutionMode,
		&requirement.CLIType,
		&autoClearSession,
		&requirement.NoResponseTimeoutMinutes,
		&requirement.NoResponseErrorAction,
		&requirement.NoResponseIdleAction,
		&requiresDesignReview,
		&requiresCodeReview,
		&requiresAcceptanceReview,
		&requiresReleaseApproval,
		&createdAt,
		&startedAt,
		&endedAt,
		&promptDispatchedAt,
		&promptReplayedAt,
		&requirement.AutoRetryAttempts,
		&requirement.LastAutoRetryReason,
		&retryBudgetExhaustedAt,
		&updatedAt,
	); err != nil {
		return nil, translateNoRows(err)
	}
	requirement.AutoClearSession = autoClearSession != 0
	requirement.RequiresDesignReview = requiresDesignReview != 0
	requirement.RequiresCodeReview = requiresCodeReview != 0
	requirement.RequiresAcceptanceReview = requiresAcceptanceReview != 0
	requirement.RequiresReleaseApproval = requiresReleaseApproval != 0
	requirement.CreatedAt = fromUnixMilli(createdAt)
	requirement.UpdatedAt = fromUnixMilli(updatedAt)
	requirement.StartedAt = nullableUnixMilliToTime(startedAt)
	requirement.EndedAt = nullableUnixMilliToTime(endedAt)
	requirement.PromptSentAt = nullableUnixMilliToTime(promptDispatchedAt)
	requirement.PromptReplayedAt = nullableUnixMilliToTime(promptReplayedAt)
	requirement.RetryBudgetExhaustedAt = nullableUnixMilliToTime(retryBudgetExhaustedAt)
	return &requirement, nil
}
