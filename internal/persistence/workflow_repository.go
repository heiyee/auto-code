package persistence

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"auto-code/internal/domain"
)

// CreateWorkflowRun inserts one workflow run and returns stored row.
func (s *SQLiteStore) CreateWorkflowRun(input domain.WorkflowRun) (*domain.WorkflowRun, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	now := time.Now()
	if input.ID == "" {
		input.ID = newPrefixedID("wf")
	}
	if input.StartedAt.IsZero() {
		input.StartedAt = now
	}
	if input.CreatedAt.IsZero() {
		input.CreatedAt = now
	}
	if input.UpdatedAt.IsZero() {
		input.UpdatedAt = now
	}
	if _, err := s.db.Exec(`INSERT INTO workflow_runs(
		id, project_id, requirement_id, status, current_stage, trigger_mode, risk_level,
		started_at, ended_at, last_error, resume_from_stage, progress, created_at, updated_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		input.ID,
		strings.TrimSpace(input.ProjectID),
		strings.TrimSpace(input.RequirementID),
		strings.TrimSpace(input.Status),
		strings.TrimSpace(input.CurrentStage),
		strings.TrimSpace(input.TriggerMode),
		defaultIfEmpty(input.RiskLevel, "medium"),
		toUnixMilli(input.StartedAt),
		toNullableUnixMilli(input.EndedAt),
		strings.TrimSpace(input.LastError),
		strings.TrimSpace(input.ResumeFromStage),
		input.Progress,
		toUnixMilli(input.CreatedAt),
		toUnixMilli(input.UpdatedAt),
	); err != nil {
		return nil, err
	}
	return s.GetWorkflowRun(input.ID)
}

// GetWorkflowRun loads one workflow run with project/requirement metadata.
func (s *SQLiteStore) GetWorkflowRun(workflowID string) (*domain.WorkflowRun, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	row := s.db.QueryRow(`SELECT
		w.id, w.project_id, COALESCE(p.name, ''), w.requirement_id, COALESCE(r.title, ''),
		w.status, w.current_stage, w.trigger_mode, w.risk_level,
		w.started_at, w.ended_at, w.last_error, w.resume_from_stage,
		w.progress, w.created_at, w.updated_at
	FROM workflow_runs w
	LEFT JOIN projects p ON p.id = w.project_id
	LEFT JOIN requirements r ON r.id = w.requirement_id
	WHERE w.id = ?`, strings.TrimSpace(workflowID))
	return scanWorkflowRun(row)
}

// GetWorkflowRunByRequirement loads workflow by requirement id.
func (s *SQLiteStore) GetWorkflowRunByRequirement(requirementID string) (*domain.WorkflowRun, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	row := s.db.QueryRow(`SELECT
		w.id, w.project_id, COALESCE(p.name, ''), w.requirement_id, COALESCE(r.title, ''),
		w.status, w.current_stage, w.trigger_mode, w.risk_level,
		w.started_at, w.ended_at, w.last_error, w.resume_from_stage,
		w.progress, w.created_at, w.updated_at
	FROM workflow_runs w
	LEFT JOIN projects p ON p.id = w.project_id
	LEFT JOIN requirements r ON r.id = w.requirement_id
	WHERE w.requirement_id = ?`, strings.TrimSpace(requirementID))
	return scanWorkflowRun(row)
}

// ListWorkflowRuns lists workflow runs filtered by project/status.
func (s *SQLiteStore) ListWorkflowRuns(projectID, status string) ([]domain.WorkflowRun, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	query := `SELECT
		w.id, w.project_id, COALESCE(p.name, ''), w.requirement_id, COALESCE(r.title, ''),
		w.status, w.current_stage, w.trigger_mode, w.risk_level,
		w.started_at, w.ended_at, w.last_error, w.resume_from_stage,
		w.progress, w.created_at, w.updated_at
	FROM workflow_runs w
	LEFT JOIN projects p ON p.id = w.project_id
	LEFT JOIN requirements r ON r.id = w.requirement_id`
	args := make([]any, 0, 2)
	where := make([]string, 0, 2)
	if strings.TrimSpace(projectID) != "" {
		where = append(where, "w.project_id = ?")
		args = append(args, strings.TrimSpace(projectID))
	}
	if strings.TrimSpace(status) != "" {
		where = append(where, "w.status = ?")
		args = append(args, strings.TrimSpace(status))
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY w.updated_at DESC, w.created_at DESC"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]domain.WorkflowRun, 0)
	for rows.Next() {
		item, err := scanWorkflowRun(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *item)
	}
	return list, rows.Err()
}

// UpdateWorkflowRun persists workflow fields.
func (s *SQLiteStore) UpdateWorkflowRun(input domain.WorkflowRun) (*domain.WorkflowRun, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	input.UpdatedAt = time.Now()
	result, err := s.db.Exec(`UPDATE workflow_runs
		SET status = ?, current_stage = ?, trigger_mode = ?, risk_level = ?, started_at = ?,
		    ended_at = ?, last_error = ?, resume_from_stage = ?, progress = ?, updated_at = ?
		WHERE id = ?`,
		strings.TrimSpace(input.Status),
		strings.TrimSpace(input.CurrentStage),
		strings.TrimSpace(input.TriggerMode),
		defaultIfEmpty(input.RiskLevel, "medium"),
		toUnixMilli(input.StartedAt),
		toNullableUnixMilli(input.EndedAt),
		strings.TrimSpace(input.LastError),
		strings.TrimSpace(input.ResumeFromStage),
		input.Progress,
		toUnixMilli(input.UpdatedAt),
		strings.TrimSpace(input.ID),
	)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrNotFound
	}
	return s.GetWorkflowRun(input.ID)
}

// CreateStageRuns inserts all provided stage runs.
func (s *SQLiteStore) CreateStageRuns(items []domain.StageRun) error {
	if s == nil || s.db == nil {
		return errors.New("sqlite store is not initialized")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, item := range items {
		now := time.Now()
		if item.ID == "" {
			item.ID = newPrefixedID("stg")
		}
		if item.CreatedAt.IsZero() {
			item.CreatedAt = now
		}
		if item.UpdatedAt.IsZero() {
			item.UpdatedAt = now
		}
		if _, err := tx.Exec(`INSERT INTO stage_runs(
			id, workflow_run_id, stage_name, display_name, status, attempt, owner_type,
			agent_session_id, started_at, ended_at, result_summary, artifacts_json, rule_report_id,
			sort_order, created_at, updated_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			item.ID,
			strings.TrimSpace(item.WorkflowRunID),
			strings.TrimSpace(item.StageName),
			strings.TrimSpace(item.DisplayName),
			strings.TrimSpace(item.Status),
			item.Attempt,
			strings.TrimSpace(item.OwnerType),
			strings.TrimSpace(item.AgentSessionID),
			toNullableUnixMilli(item.StartedAt),
			toNullableUnixMilli(item.EndedAt),
			strings.TrimSpace(item.ResultSummary),
			jsonString(item.Artifacts),
			strings.TrimSpace(item.RuleReportID),
			item.Order,
			toUnixMilli(item.CreatedAt),
			toUnixMilli(item.UpdatedAt),
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListStageRuns lists workflow stage runs ordered by sort order.
func (s *SQLiteStore) ListStageRuns(workflowID string) ([]domain.StageRun, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	rows, err := s.db.Query(`SELECT
		id, workflow_run_id, stage_name, display_name, status, attempt, owner_type,
		agent_session_id, started_at, ended_at, result_summary, artifacts_json, rule_report_id,
		sort_order, created_at, updated_at
	FROM stage_runs
	WHERE workflow_run_id = ?
	ORDER BY sort_order ASC, created_at ASC`, strings.TrimSpace(workflowID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]domain.StageRun, 0)
	for rows.Next() {
		item, err := scanStageRun(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *item)
	}
	return list, rows.Err()
}

// GetStageRunByName loads one stage run by workflow and stage name.
func (s *SQLiteStore) GetStageRunByName(workflowID, stageName string) (*domain.StageRun, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	row := s.db.QueryRow(`SELECT
		id, workflow_run_id, stage_name, display_name, status, attempt, owner_type,
		agent_session_id, started_at, ended_at, result_summary, artifacts_json, rule_report_id,
		sort_order, created_at, updated_at
	FROM stage_runs
	WHERE workflow_run_id = ? AND stage_name = ?`,
		strings.TrimSpace(workflowID), strings.TrimSpace(stageName))
	return scanStageRun(row)
}

// UpdateStageRun persists stage run fields.
func (s *SQLiteStore) UpdateStageRun(input domain.StageRun) (*domain.StageRun, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	input.UpdatedAt = time.Now()
	result, err := s.db.Exec(`UPDATE stage_runs
		SET display_name = ?, status = ?, attempt = ?, owner_type = ?, agent_session_id = ?,
		    started_at = ?, ended_at = ?, result_summary = ?, artifacts_json = ?, rule_report_id = ?,
		    sort_order = ?, updated_at = ?
		WHERE id = ?`,
		strings.TrimSpace(input.DisplayName),
		strings.TrimSpace(input.Status),
		input.Attempt,
		strings.TrimSpace(input.OwnerType),
		strings.TrimSpace(input.AgentSessionID),
		toNullableUnixMilli(input.StartedAt),
		toNullableUnixMilli(input.EndedAt),
		strings.TrimSpace(input.ResultSummary),
		jsonString(input.Artifacts),
		strings.TrimSpace(input.RuleReportID),
		input.Order,
		toUnixMilli(input.UpdatedAt),
		strings.TrimSpace(input.ID),
	)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrNotFound
	}
	row := s.db.QueryRow(`SELECT
		id, workflow_run_id, stage_name, display_name, status, attempt, owner_type,
		agent_session_id, started_at, ended_at, result_summary, artifacts_json, rule_report_id,
		sort_order, created_at, updated_at
	FROM stage_runs WHERE id = ?`, strings.TrimSpace(input.ID))
	return scanStageRun(row)
}

// GetStageRun loads one stage run by ID.
func (s *SQLiteStore) GetStageRun(stageRunID string) (*domain.StageRun, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	row := s.db.QueryRow(`SELECT
		id, workflow_run_id, stage_name, display_name, status, attempt, owner_type,
		agent_session_id, started_at, ended_at, result_summary, artifacts_json, rule_report_id,
		sort_order, created_at, updated_at
	FROM stage_runs WHERE id = ?`, strings.TrimSpace(stageRunID))
	item, err := scanStageRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return item, err
}

// BulkUpdateTaskStatus updates all task items for a workflow that match fromStatus to toStatus.
func (s *SQLiteStore) BulkUpdateTaskStatus(workflowID, fromStatus, toStatus string) error {
	if s == nil || s.db == nil {
		return errors.New("sqlite store is not initialized")
	}
	now := toUnixMilli(time.Now())
	_, err := s.db.Exec(`UPDATE task_items SET status = ?, updated_at = ?
		WHERE workflow_run_id = ? AND status = ?`,
		strings.TrimSpace(toStatus), now,
		strings.TrimSpace(workflowID), strings.TrimSpace(fromStatus))
	return err
}

// ReplaceTaskItems replaces workflow tasks for one stage.
func (s *SQLiteStore) ReplaceTaskItems(workflowID, stageRunID string, items []domain.TaskItem) error {
	if s == nil || s.db == nil {
		return errors.New("sqlite store is not initialized")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM task_items WHERE workflow_run_id = ? AND stage_run_id = ?`,
		strings.TrimSpace(workflowID), strings.TrimSpace(stageRunID)); err != nil {
		return err
	}
	for _, item := range items {
		now := time.Now()
		if item.ID == "" {
			item.ID = newPrefixedID("task")
		}
		if item.CreatedAt.IsZero() {
			item.CreatedAt = now
		}
		if item.UpdatedAt.IsZero() {
			item.UpdatedAt = now
		}
		if _, err := tx.Exec(`INSERT INTO task_items(
			id, workflow_run_id, stage_run_id, parent_task_id, title, description, scope, required,
			status, owner_session_id, depends_on_json, evidence_artifact_id, created_at, updated_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			item.ID,
			strings.TrimSpace(item.WorkflowRunID),
			strings.TrimSpace(item.StageRunID),
			strings.TrimSpace(item.ParentTaskID),
			strings.TrimSpace(item.Title),
			strings.TrimSpace(item.Description),
			strings.TrimSpace(item.Scope),
			boolToInt(item.Required),
			strings.TrimSpace(item.Status),
			strings.TrimSpace(item.OwnerSessionID),
			jsonString(item.DependsOn),
			strings.TrimSpace(item.EvidenceArtifactID),
			toUnixMilli(item.CreatedAt),
			toUnixMilli(item.UpdatedAt),
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListTaskItems lists all tasks for one workflow.
func (s *SQLiteStore) ListTaskItems(workflowID string) ([]domain.TaskItem, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	rows, err := s.db.Query(`SELECT
		id, workflow_run_id, stage_run_id, parent_task_id, title, description, scope,
		required, status, owner_session_id, depends_on_json, evidence_artifact_id, created_at, updated_at
	FROM task_items
	WHERE workflow_run_id = ?
	ORDER BY created_at ASC, id ASC`, strings.TrimSpace(workflowID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]domain.TaskItem, 0)
	for rows.Next() {
		item, err := scanTaskItem(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *item)
	}
	return list, rows.Err()
}

// UpsertDecisionRequest inserts or updates decision request by id.
func (s *SQLiteStore) UpsertDecisionRequest(input domain.DecisionRequest) (*domain.DecisionRequest, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	now := time.Now()
	if input.ID == "" {
		input.ID = newPrefixedID("dec")
	}
	if input.CreatedAt.IsZero() {
		input.CreatedAt = now
	}
	_, err := s.db.Exec(`INSERT INTO decision_requests(
		id, workflow_run_id, stage_run_id, request_type, title, question, context, options_json,
		recommended_option, blocking, status, decision, decider, created_at, resolved_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		request_type = excluded.request_type,
		title = excluded.title,
		question = excluded.question,
		context = excluded.context,
		options_json = excluded.options_json,
		recommended_option = excluded.recommended_option,
		blocking = excluded.blocking,
		status = excluded.status,
		decision = excluded.decision,
		decider = excluded.decider,
		resolved_at = excluded.resolved_at`,
		input.ID,
		strings.TrimSpace(input.WorkflowRunID),
		strings.TrimSpace(input.StageRunID),
		strings.TrimSpace(input.RequestType),
		strings.TrimSpace(input.Title),
		strings.TrimSpace(input.Question),
		strings.TrimSpace(input.Context),
		jsonString(input.Options),
		strings.TrimSpace(input.RecommendedOption),
		boolToInt(input.Blocking),
		defaultIfEmpty(input.Status, domain.DecisionStatusPending),
		strings.TrimSpace(input.Decision),
		strings.TrimSpace(input.Decider),
		toUnixMilli(input.CreatedAt),
		toNullableUnixMilli(input.ResolvedAt),
	)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRow(`SELECT
		id, workflow_run_id, stage_run_id, request_type, title, question, context, options_json,
		recommended_option, blocking, status, decision, decider, created_at, resolved_at
	FROM decision_requests WHERE id = ?`, input.ID)
	return scanDecisionRequest(row)
}

// ListDecisionRequests lists workflow decisions.
func (s *SQLiteStore) ListDecisionRequests(workflowID string) ([]domain.DecisionRequest, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	query := `SELECT
		id, workflow_run_id, stage_run_id, request_type, title, question, context, options_json,
		recommended_option, blocking, status, decision, decider, created_at, resolved_at
	FROM decision_requests`
	args := make([]any, 0, 1)
	if strings.TrimSpace(workflowID) != "" {
		query += `
	WHERE workflow_run_id = ?`
		args = append(args, strings.TrimSpace(workflowID))
	}
	query += `
	ORDER BY created_at DESC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]domain.DecisionRequest, 0)
	for rows.Next() {
		item, err := scanDecisionRequest(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *item)
	}
	return list, rows.Err()
}

// ResolveDecisionRequest updates one decision.
func (s *SQLiteStore) ResolveDecisionRequest(id string, input domain.DecisionResolutionInput) (*domain.DecisionRequest, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	now := time.Now()
	result, err := s.db.Exec(`UPDATE decision_requests
		SET status = ?, decision = ?, decider = ?, resolved_at = ?
		WHERE id = ?`,
		domain.DecisionStatusResolved,
		strings.TrimSpace(input.Decision),
		strings.TrimSpace(input.Decider),
		toUnixMilli(now),
		strings.TrimSpace(id),
	)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrNotFound
	}
	row := s.db.QueryRow(`SELECT
		id, workflow_run_id, stage_run_id, request_type, title, question, context, options_json,
		recommended_option, blocking, status, decision, decider, created_at, resolved_at
	FROM decision_requests WHERE id = ?`, strings.TrimSpace(id))
	return scanDecisionRequest(row)
}

// UpsertReviewGate inserts or updates one review gate.
func (s *SQLiteStore) UpsertReviewGate(input domain.ReviewGate) (*domain.ReviewGate, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	now := time.Now()
	if input.ID == "" {
		input.ID = newPrefixedID("gate")
	}
	if input.CreatedAt.IsZero() {
		input.CreatedAt = now
	}
	_, err := s.db.Exec(`INSERT INTO review_gates(
		id, workflow_run_id, stage_name, gate_type, status, reviewer, decision, comment,
		title, description, blocking_items_json, created_at, resolved_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(workflow_run_id, stage_name, gate_type) DO UPDATE SET
		status = excluded.status,
		reviewer = excluded.reviewer,
		decision = excluded.decision,
		comment = excluded.comment,
		title = excluded.title,
		description = excluded.description,
		blocking_items_json = excluded.blocking_items_json,
		resolved_at = excluded.resolved_at`,
		input.ID,
		strings.TrimSpace(input.WorkflowRunID),
		strings.TrimSpace(input.StageName),
		strings.TrimSpace(input.GateType),
		strings.TrimSpace(input.Status),
		strings.TrimSpace(input.Reviewer),
		strings.TrimSpace(input.Decision),
		strings.TrimSpace(input.Comment),
		strings.TrimSpace(input.Title),
		strings.TrimSpace(input.Description),
		jsonString(input.BlockingItems),
		toUnixMilli(input.CreatedAt),
		toNullableUnixMilli(input.ResolvedAt),
	)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRow(`SELECT
		id, workflow_run_id, stage_name, gate_type, status, reviewer, decision, comment,
		title, description, blocking_items_json, created_at, resolved_at
	FROM review_gates
	WHERE workflow_run_id = ? AND stage_name = ? AND gate_type = ?`,
		strings.TrimSpace(input.WorkflowRunID), strings.TrimSpace(input.StageName), strings.TrimSpace(input.GateType))
	return scanReviewGate(row)
}

// ListReviewGates lists review gates filtered by status or workflow.
func (s *SQLiteStore) ListReviewGates(status, workflowID string) ([]domain.ReviewGate, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	query := `SELECT
		id, workflow_run_id, stage_name, gate_type, status, reviewer, decision, comment,
		title, description, blocking_items_json, created_at, resolved_at
	FROM review_gates`
	args := make([]any, 0, 2)
	where := make([]string, 0, 2)
	if strings.TrimSpace(status) != "" {
		where = append(where, "status = ?")
		args = append(args, strings.TrimSpace(status))
	}
	if strings.TrimSpace(workflowID) != "" {
		where = append(where, "workflow_run_id = ?")
		args = append(args, strings.TrimSpace(workflowID))
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY created_at DESC"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]domain.ReviewGate, 0)
	for rows.Next() {
		item, err := scanReviewGate(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *item)
	}
	return list, rows.Err()
}

// UpdateReviewGate updates one review gate.
func (s *SQLiteStore) UpdateReviewGate(id string, input domain.ReviewGateUpdateInput) (*domain.ReviewGate, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	status := strings.TrimSpace(input.Status)
	decision := strings.TrimSpace(input.Decision)
	resolvedAt := any(nil)
	if status == domain.ReviewGateStatusApproved || status == domain.ReviewGateStatusRejected || status == domain.ReviewGateStatusWaived {
		resolvedAt = toUnixMilli(time.Now())
	}
	result, err := s.db.Exec(`UPDATE review_gates
		SET status = ?, decision = ?, reviewer = ?, comment = ?, resolved_at = ?
		WHERE id = ?`,
		status,
		decision,
		strings.TrimSpace(input.Reviewer),
		strings.TrimSpace(input.Comment),
		resolvedAt,
		strings.TrimSpace(id),
	)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrNotFound
	}
	row := s.db.QueryRow(`SELECT
		id, workflow_run_id, stage_name, gate_type, status, reviewer, decision, comment,
		title, description, blocking_items_json, created_at, resolved_at
	FROM review_gates WHERE id = ?`, strings.TrimSpace(id))
	return scanReviewGate(row)
}

// UpsertArtifact inserts or updates one artifact by workflow/stage/type/path.
func (s *SQLiteStore) UpsertArtifact(input domain.Artifact) (*domain.Artifact, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	now := time.Now()
	if input.ID == "" {
		input.ID = newPrefixedID("art")
	}
	if input.Version <= 0 {
		input.Version = 1
	}
	if input.CreatedAt.IsZero() {
		input.CreatedAt = now
	}
	if input.UpdatedAt.IsZero() {
		input.UpdatedAt = now
	}
	_, err := s.db.Exec(`INSERT INTO artifacts(
		id, project_id, requirement_id, workflow_run_id, stage_run_id, artifact_type, title, path,
		version, status, source, content_hash, created_at, updated_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		title = excluded.title,
		path = excluded.path,
		version = excluded.version,
		status = excluded.status,
		source = excluded.source,
		content_hash = excluded.content_hash,
		updated_at = excluded.updated_at`,
		input.ID,
		strings.TrimSpace(input.ProjectID),
		strings.TrimSpace(input.RequirementID),
		strings.TrimSpace(input.WorkflowRunID),
		strings.TrimSpace(input.StageRunID),
		strings.TrimSpace(input.ArtifactType),
		strings.TrimSpace(input.Title),
		strings.TrimSpace(input.Path),
		input.Version,
		strings.TrimSpace(input.Status),
		strings.TrimSpace(input.Source),
		strings.TrimSpace(input.ContentHash),
		toUnixMilli(input.CreatedAt),
		toUnixMilli(input.UpdatedAt),
	)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRow(`SELECT
		id, project_id, requirement_id, workflow_run_id, stage_run_id, artifact_type, title, path,
		version, status, source, content_hash, created_at, updated_at
	FROM artifacts WHERE id = ?`, strings.TrimSpace(input.ID))
	return scanArtifact(row)
}

// ListArtifacts lists artifacts filtered by workflow.
func (s *SQLiteStore) ListArtifacts(workflowID string) ([]domain.Artifact, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	query := `SELECT
		id, project_id, requirement_id, workflow_run_id, stage_run_id, artifact_type, title, path,
		version, status, source, content_hash, created_at, updated_at
	FROM artifacts`
	args := make([]any, 0, 1)
	if strings.TrimSpace(workflowID) != "" {
		query += `
	WHERE workflow_run_id = ?`
		args = append(args, strings.TrimSpace(workflowID))
	}
	query += `
	ORDER BY created_at ASC, id ASC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]domain.Artifact, 0)
	for rows.Next() {
		item, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *item)
	}
	return list, rows.Err()
}

// GetArtifact returns one artifact by id.
func (s *SQLiteStore) GetArtifact(artifactID string) (*domain.Artifact, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	row := s.db.QueryRow(`SELECT
		id, project_id, requirement_id, workflow_run_id, stage_run_id, artifact_type, title, path,
		version, status, source, content_hash, created_at, updated_at
	FROM artifacts WHERE id = ?`, strings.TrimSpace(artifactID))
	item, err := scanArtifact(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

// UpsertCodeSnapshot inserts or updates one code snapshot by workflow/stage/type.
func (s *SQLiteStore) UpsertCodeSnapshot(input domain.CodeSnapshot) (*domain.CodeSnapshot, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	now := time.Now()
	if input.ID == "" {
		input.ID = newPrefixedID("snap")
	}
	if input.CreatedAt.IsZero() {
		input.CreatedAt = now
	}
	_, err := s.db.Exec(`INSERT INTO code_snapshots(
		id, project_id, workflow_run_id, stage_run_id, snapshot_type, git_commit, git_branch,
		workspace_revision, file_count, created_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(workflow_run_id, stage_run_id, snapshot_type) DO UPDATE SET
		project_id = excluded.project_id,
		git_commit = excluded.git_commit,
		git_branch = excluded.git_branch,
		workspace_revision = excluded.workspace_revision,
		file_count = excluded.file_count,
		created_at = excluded.created_at`,
		input.ID,
		strings.TrimSpace(input.ProjectID),
		strings.TrimSpace(input.WorkflowRunID),
		strings.TrimSpace(input.StageRunID),
		strings.TrimSpace(input.SnapshotType),
		strings.TrimSpace(input.GitCommit),
		strings.TrimSpace(input.GitBranch),
		strings.TrimSpace(input.WorkspaceRevision),
		input.FileCount,
		toUnixMilli(input.CreatedAt),
	)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRow(`SELECT
		id, project_id, workflow_run_id, stage_run_id, snapshot_type, git_commit, git_branch,
		workspace_revision, file_count, created_at
	FROM code_snapshots WHERE workflow_run_id = ? AND stage_run_id = ? AND snapshot_type = ?`,
		strings.TrimSpace(input.WorkflowRunID), strings.TrimSpace(input.StageRunID), strings.TrimSpace(input.SnapshotType))
	return scanCodeSnapshot(row)
}

// ListCodeSnapshots lists workflow snapshots.
func (s *SQLiteStore) ListCodeSnapshots(workflowID string) ([]domain.CodeSnapshot, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	query := `SELECT
		id, project_id, workflow_run_id, stage_run_id, snapshot_type, git_commit, git_branch,
		workspace_revision, file_count, created_at
	FROM code_snapshots`
	args := make([]any, 0, 1)
	if strings.TrimSpace(workflowID) != "" {
		query += `
	WHERE workflow_run_id = ?`
		args = append(args, strings.TrimSpace(workflowID))
	}
	query += `
	ORDER BY created_at ASC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]domain.CodeSnapshot, 0)
	for rows.Next() {
		item, err := scanCodeSnapshot(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *item)
	}
	return list, rows.Err()
}

// GetCodeSnapshotByScope loads snapshot by workflow/stage/type.
func (s *SQLiteStore) GetCodeSnapshotByScope(workflowID, stageRunID, snapshotType string) (*domain.CodeSnapshot, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	row := s.db.QueryRow(`SELECT
		id, project_id, workflow_run_id, stage_run_id, snapshot_type, git_commit, git_branch,
		workspace_revision, file_count, created_at
	FROM code_snapshots
	WHERE workflow_run_id = ? AND stage_run_id = ? AND snapshot_type = ?`,
		strings.TrimSpace(workflowID), strings.TrimSpace(stageRunID), strings.TrimSpace(snapshotType))
	return scanCodeSnapshot(row)
}

// UpsertChangeSet inserts or updates a change set by workflow/stage/scope.
func (s *SQLiteStore) UpsertChangeSet(input domain.ChangeSet) (*domain.ChangeSet, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	now := time.Now()
	if input.ID == "" {
		input.ID = newPrefixedID("chg")
	}
	if input.CreatedAt.IsZero() {
		input.CreatedAt = now
	}
	_, err := s.db.Exec(`INSERT INTO change_sets(
		id, project_id, workflow_run_id, stage_run_id, base_snapshot_id, target_snapshot_id,
		change_scope, summary, file_stats_json, files_json, patch_artifact_id, created_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(workflow_run_id, stage_run_id, change_scope) DO UPDATE SET
		project_id = excluded.project_id,
		base_snapshot_id = excluded.base_snapshot_id,
		target_snapshot_id = excluded.target_snapshot_id,
		summary = excluded.summary,
		file_stats_json = excluded.file_stats_json,
		files_json = excluded.files_json,
		patch_artifact_id = excluded.patch_artifact_id,
		created_at = excluded.created_at`,
		input.ID,
		strings.TrimSpace(input.ProjectID),
		strings.TrimSpace(input.WorkflowRunID),
		strings.TrimSpace(input.StageRunID),
		strings.TrimSpace(input.BaseSnapshotID),
		strings.TrimSpace(input.TargetSnapshotID),
		strings.TrimSpace(input.ChangeScope),
		strings.TrimSpace(input.Summary),
		jsonObjectString(input.FileStats),
		jsonString(input.Files),
		strings.TrimSpace(input.PatchArtifactID),
		toUnixMilli(input.CreatedAt),
	)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRow(`SELECT
		id, project_id, workflow_run_id, stage_run_id, base_snapshot_id, target_snapshot_id,
		change_scope, summary, file_stats_json, files_json, patch_artifact_id, created_at
	FROM change_sets
	WHERE workflow_run_id = ? AND stage_run_id = ? AND change_scope = ?`,
		strings.TrimSpace(input.WorkflowRunID), strings.TrimSpace(input.StageRunID), strings.TrimSpace(input.ChangeScope))
	return scanChangeSet(row)
}

// ListChangeSets lists workflow change sets.
func (s *SQLiteStore) ListChangeSets(workflowID string) ([]domain.ChangeSet, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	query := `SELECT
		id, project_id, workflow_run_id, stage_run_id, base_snapshot_id, target_snapshot_id,
		change_scope, summary, file_stats_json, files_json, patch_artifact_id, created_at
	FROM change_sets`
	args := make([]any, 0, 1)
	if strings.TrimSpace(workflowID) != "" {
		query += `
	WHERE workflow_run_id = ?`
		args = append(args, strings.TrimSpace(workflowID))
	}
	query += `
	ORDER BY created_at DESC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]domain.ChangeSet, 0)
	for rows.Next() {
		item, err := scanChangeSet(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *item)
	}
	return list, rows.Err()
}

// GetChangeSet loads one change set by id.
func (s *SQLiteStore) GetChangeSet(id string) (*domain.ChangeSet, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	row := s.db.QueryRow(`SELECT
		id, project_id, workflow_run_id, stage_run_id, base_snapshot_id, target_snapshot_id,
		change_scope, summary, file_stats_json, files_json, patch_artifact_id, created_at
	FROM change_sets WHERE id = ?`, strings.TrimSpace(id))
	return scanChangeSet(row)
}

func scanWorkflowRun(row scanner) (*domain.WorkflowRun, error) {
	var item domain.WorkflowRun
	var startedAt int64
	var endedAt sql.NullInt64
	var createdAt int64
	var updatedAt int64
	err := row.Scan(
		&item.ID,
		&item.ProjectID,
		&item.ProjectName,
		&item.RequirementID,
		&item.RequirementTitle,
		&item.Status,
		&item.CurrentStage,
		&item.TriggerMode,
		&item.RiskLevel,
		&startedAt,
		&endedAt,
		&item.LastError,
		&item.ResumeFromStage,
		&item.Progress,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return nil, translateNoRows(err)
	}
	item.StartedAt = fromUnixMilli(startedAt)
	item.EndedAt = nullableUnixMilliToTime(endedAt)
	item.CreatedAt = fromUnixMilli(createdAt)
	item.UpdatedAt = fromUnixMilli(updatedAt)
	return &item, nil
}

func scanStageRun(row scanner) (*domain.StageRun, error) {
	var item domain.StageRun
	var startedAt sql.NullInt64
	var endedAt sql.NullInt64
	var artifactsRaw string
	var createdAt int64
	var updatedAt int64
	err := row.Scan(
		&item.ID,
		&item.WorkflowRunID,
		&item.StageName,
		&item.DisplayName,
		&item.Status,
		&item.Attempt,
		&item.OwnerType,
		&item.AgentSessionID,
		&startedAt,
		&endedAt,
		&item.ResultSummary,
		&artifactsRaw,
		&item.RuleReportID,
		&item.Order,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return nil, translateNoRows(err)
	}
	item.StartedAt = nullableUnixMilliToTime(startedAt)
	item.EndedAt = nullableUnixMilliToTime(endedAt)
	item.Artifacts = decodeJSONList[string](artifactsRaw)
	item.CreatedAt = fromUnixMilli(createdAt)
	item.UpdatedAt = fromUnixMilli(updatedAt)
	return &item, nil
}

func scanTaskItem(row scanner) (*domain.TaskItem, error) {
	var item domain.TaskItem
	var required int64
	var dependsOnRaw string
	var createdAt int64
	var updatedAt int64
	err := row.Scan(
		&item.ID,
		&item.WorkflowRunID,
		&item.StageRunID,
		&item.ParentTaskID,
		&item.Title,
		&item.Description,
		&item.Scope,
		&required,
		&item.Status,
		&item.OwnerSessionID,
		&dependsOnRaw,
		&item.EvidenceArtifactID,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return nil, translateNoRows(err)
	}
	item.Required = intToBool(required)
	item.DependsOn = decodeJSONList[string](dependsOnRaw)
	item.CreatedAt = fromUnixMilli(createdAt)
	item.UpdatedAt = fromUnixMilli(updatedAt)
	return &item, nil
}

func scanDecisionRequest(row scanner) (*domain.DecisionRequest, error) {
	var item domain.DecisionRequest
	var blocking int64
	var optionsRaw string
	var createdAt int64
	var resolvedAt sql.NullInt64
	err := row.Scan(
		&item.ID,
		&item.WorkflowRunID,
		&item.StageRunID,
		&item.RequestType,
		&item.Title,
		&item.Question,
		&item.Context,
		&optionsRaw,
		&item.RecommendedOption,
		&blocking,
		&item.Status,
		&item.Decision,
		&item.Decider,
		&createdAt,
		&resolvedAt,
	)
	if err != nil {
		return nil, translateNoRows(err)
	}
	item.Options = decodeJSONList[domain.DecisionOption](optionsRaw)
	item.Blocking = intToBool(blocking)
	item.CreatedAt = fromUnixMilli(createdAt)
	item.ResolvedAt = nullableUnixMilliToTime(resolvedAt)
	return &item, nil
}

func scanReviewGate(row scanner) (*domain.ReviewGate, error) {
	var item domain.ReviewGate
	var blockingRaw string
	var createdAt int64
	var resolvedAt sql.NullInt64
	err := row.Scan(
		&item.ID,
		&item.WorkflowRunID,
		&item.StageName,
		&item.GateType,
		&item.Status,
		&item.Reviewer,
		&item.Decision,
		&item.Comment,
		&item.Title,
		&item.Description,
		&blockingRaw,
		&createdAt,
		&resolvedAt,
	)
	if err != nil {
		return nil, translateNoRows(err)
	}
	item.BlockingItems = decodeJSONList[string](blockingRaw)
	item.CreatedAt = fromUnixMilli(createdAt)
	item.ResolvedAt = nullableUnixMilliToTime(resolvedAt)
	return &item, nil
}

func scanArtifact(row scanner) (*domain.Artifact, error) {
	var item domain.Artifact
	var createdAt int64
	var updatedAt int64
	err := row.Scan(
		&item.ID,
		&item.ProjectID,
		&item.RequirementID,
		&item.WorkflowRunID,
		&item.StageRunID,
		&item.ArtifactType,
		&item.Title,
		&item.Path,
		&item.Version,
		&item.Status,
		&item.Source,
		&item.ContentHash,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return nil, translateNoRows(err)
	}
	item.CreatedAt = fromUnixMilli(createdAt)
	item.UpdatedAt = fromUnixMilli(updatedAt)
	return &item, nil
}

func scanCodeSnapshot(row scanner) (*domain.CodeSnapshot, error) {
	var item domain.CodeSnapshot
	var createdAt int64
	err := row.Scan(
		&item.ID,
		&item.ProjectID,
		&item.WorkflowRunID,
		&item.StageRunID,
		&item.SnapshotType,
		&item.GitCommit,
		&item.GitBranch,
		&item.WorkspaceRevision,
		&item.FileCount,
		&createdAt,
	)
	if err != nil {
		return nil, translateNoRows(err)
	}
	item.CreatedAt = fromUnixMilli(createdAt)
	return &item, nil
}

func scanChangeSet(row scanner) (*domain.ChangeSet, error) {
	var item domain.ChangeSet
	var fileStatsRaw string
	var filesRaw string
	var createdAt int64
	err := row.Scan(
		&item.ID,
		&item.ProjectID,
		&item.WorkflowRunID,
		&item.StageRunID,
		&item.BaseSnapshotID,
		&item.TargetSnapshotID,
		&item.ChangeScope,
		&item.Summary,
		&fileStatsRaw,
		&filesRaw,
		&item.PatchArtifactID,
		&createdAt,
	)
	if err != nil {
		return nil, translateNoRows(err)
	}
	item.FileStats = decodeJSONObject[domain.ChangeSetFileStats](fileStatsRaw)
	item.Files = decodeJSONList[domain.WorkflowFileChange](filesRaw)
	item.CreatedAt = fromUnixMilli(createdAt)
	return &item, nil
}

func defaultIfEmpty(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
