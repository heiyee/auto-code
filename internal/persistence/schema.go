package persistence

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ensureSchema creates required tables and indexes with idempotent SQL.
func (s *SQLiteStore) ensureSchema() error {
	if s == nil || s.db == nil {
		return errors.New("sqlite store is not initialized")
	}

	stmts := []string{
		`PRAGMA foreign_keys = ON;`,
		`CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER NOT NULL PRIMARY KEY
		);`,
		`INSERT OR IGNORE INTO schema_version(version) VALUES (1);`,
		`CREATE TABLE IF NOT EXISTS projects (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			repository TEXT NOT NULL,
			branch TEXT NOT NULL,
			work_dir TEXT,
			automation_paused INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_projects_name ON projects(name);`,
		`CREATE TABLE IF NOT EXISTS requirements (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			sort_order INTEGER NOT NULL DEFAULT 0,
			title TEXT NOT NULL,
			description TEXT NOT NULL,
			status TEXT NOT NULL,
			execution_mode TEXT NOT NULL DEFAULT 'manual',
			cli_type TEXT NOT NULL DEFAULT '',
			auto_clear_session INTEGER NOT NULL DEFAULT 0,
			no_response_timeout_minutes INTEGER NOT NULL DEFAULT 0,
			no_response_error_action TEXT NOT NULL DEFAULT 'none',
			no_response_idle_action TEXT NOT NULL DEFAULT 'none',
			requires_design_review INTEGER NOT NULL DEFAULT 0,
			requires_code_review INTEGER NOT NULL DEFAULT 0,
			requires_acceptance_review INTEGER NOT NULL DEFAULT 0,
			requires_release_approval INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			started_at INTEGER,
			ended_at INTEGER,
			prompt_dispatched_at INTEGER,
			prompt_replayed_at INTEGER,
			auto_retry_attempts INTEGER NOT NULL DEFAULT 0,
			last_auto_retry_reason TEXT NOT NULL DEFAULT '',
			retry_budget_exhausted_at INTEGER,
			updated_at INTEGER NOT NULL,
			FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_requirements_project_id ON requirements(project_id);`,
		`CREATE INDEX IF NOT EXISTS idx_requirements_status ON requirements(status);`,
		`CREATE INDEX IF NOT EXISTS idx_requirements_project_status_mode ON requirements(project_id, status, execution_mode, created_at);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_requirements_project_running
			ON requirements(project_id)
			WHERE status = 'running';`,
		`CREATE TABLE IF NOT EXISTS requirement_watchdog_events (
			id TEXT PRIMARY KEY,
			requirement_id TEXT NOT NULL,
			session_id TEXT NOT NULL DEFAULT '',
			trigger_kind TEXT NOT NULL,
			trigger_reason TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL,
			status TEXT NOT NULL,
			detail TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			finished_at INTEGER,
			FOREIGN KEY(requirement_id) REFERENCES requirements(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_requirement_watchdog_requirement
			ON requirement_watchdog_events(requirement_id, created_at DESC);`,
		`CREATE TABLE IF NOT EXISTS cli_sessions (
			id TEXT PRIMARY KEY,
			cli_type TEXT NOT NULL,
			profile TEXT NOT NULL,
			agent_id TEXT NOT NULL UNIQUE,
			project_id TEXT,
			requirement_id TEXT,
			work_dir TEXT NOT NULL,
			session_state TEXT NOT NULL,
			launch_mode TEXT,
			process_pid INTEGER NOT NULL DEFAULT 0,
			process_pgid INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			last_active_at INTEGER NOT NULL,
			FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE,
			FOREIGN KEY(requirement_id) REFERENCES requirements(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_cli_sessions_type ON cli_sessions(cli_type);`,
		`CREATE INDEX IF NOT EXISTS idx_cli_sessions_state ON cli_sessions(session_state);`,
		`CREATE INDEX IF NOT EXISTS idx_cli_sessions_project ON cli_sessions(project_id);`,
		`CREATE INDEX IF NOT EXISTS idx_cli_sessions_requirement ON cli_sessions(requirement_id);`,
		`CREATE INDEX IF NOT EXISTS idx_cli_sessions_profile ON cli_sessions(profile);`,
		`CREATE TABLE IF NOT EXISTS workflow_runs (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			requirement_id TEXT NOT NULL UNIQUE,
			status TEXT NOT NULL,
			current_stage TEXT NOT NULL,
			trigger_mode TEXT NOT NULL,
			risk_level TEXT NOT NULL DEFAULT 'medium',
			started_at INTEGER NOT NULL,
			ended_at INTEGER,
			last_error TEXT NOT NULL DEFAULT '',
			resume_from_stage TEXT NOT NULL DEFAULT '',
			progress INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE,
			FOREIGN KEY(requirement_id) REFERENCES requirements(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_runs_project ON workflow_runs(project_id);`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_runs_status ON workflow_runs(status);`,
		`CREATE TABLE IF NOT EXISTS stage_runs (
			id TEXT PRIMARY KEY,
			workflow_run_id TEXT NOT NULL,
			stage_name TEXT NOT NULL,
			display_name TEXT NOT NULL,
			status TEXT NOT NULL,
			attempt INTEGER NOT NULL DEFAULT 0,
			owner_type TEXT NOT NULL,
			agent_session_id TEXT NOT NULL DEFAULT '',
			started_at INTEGER,
			ended_at INTEGER,
			result_summary TEXT NOT NULL DEFAULT '',
			artifacts_json TEXT NOT NULL DEFAULT '[]',
			rule_report_id TEXT NOT NULL DEFAULT '',
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			FOREIGN KEY(workflow_run_id) REFERENCES workflow_runs(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_stage_runs_workflow ON stage_runs(workflow_run_id, sort_order);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_stage_runs_workflow_name ON stage_runs(workflow_run_id, stage_name);`,
		`CREATE TABLE IF NOT EXISTS task_items (
			id TEXT PRIMARY KEY,
			workflow_run_id TEXT NOT NULL,
			stage_run_id TEXT NOT NULL,
			parent_task_id TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL,
			description TEXT NOT NULL,
			scope TEXT NOT NULL,
			required INTEGER NOT NULL DEFAULT 1,
			status TEXT NOT NULL,
			owner_session_id TEXT NOT NULL DEFAULT '',
			depends_on_json TEXT NOT NULL DEFAULT '[]',
			evidence_artifact_id TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			FOREIGN KEY(workflow_run_id) REFERENCES workflow_runs(id) ON DELETE CASCADE,
			FOREIGN KEY(stage_run_id) REFERENCES stage_runs(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_task_items_workflow ON task_items(workflow_run_id);`,
		`CREATE INDEX IF NOT EXISTS idx_task_items_stage ON task_items(stage_run_id);`,
		`CREATE TABLE IF NOT EXISTS decision_requests (
			id TEXT PRIMARY KEY,
			workflow_run_id TEXT NOT NULL,
			stage_run_id TEXT NOT NULL DEFAULT '',
			request_type TEXT NOT NULL,
			title TEXT NOT NULL,
			question TEXT NOT NULL,
			context TEXT NOT NULL DEFAULT '',
			options_json TEXT NOT NULL DEFAULT '[]',
			recommended_option TEXT NOT NULL DEFAULT '',
			blocking INTEGER NOT NULL DEFAULT 1,
			status TEXT NOT NULL,
			decision TEXT NOT NULL DEFAULT '',
			decider TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			resolved_at INTEGER,
			FOREIGN KEY(workflow_run_id) REFERENCES workflow_runs(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_decision_requests_workflow ON decision_requests(workflow_run_id);`,
		`CREATE TABLE IF NOT EXISTS review_gates (
			id TEXT PRIMARY KEY,
			workflow_run_id TEXT NOT NULL,
			stage_name TEXT NOT NULL,
			gate_type TEXT NOT NULL,
			status TEXT NOT NULL,
			reviewer TEXT NOT NULL DEFAULT '',
			decision TEXT NOT NULL DEFAULT '',
			comment TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			blocking_items_json TEXT NOT NULL DEFAULT '[]',
			created_at INTEGER NOT NULL,
			resolved_at INTEGER,
			FOREIGN KEY(workflow_run_id) REFERENCES workflow_runs(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_review_gates_workflow ON review_gates(workflow_run_id);`,
		`CREATE INDEX IF NOT EXISTS idx_review_gates_status ON review_gates(status);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_review_gates_scope ON review_gates(workflow_run_id, stage_name, gate_type);`,
		`CREATE TABLE IF NOT EXISTS artifacts (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			requirement_id TEXT NOT NULL,
			workflow_run_id TEXT NOT NULL,
			stage_run_id TEXT NOT NULL DEFAULT '',
			artifact_type TEXT NOT NULL,
			title TEXT NOT NULL,
			path TEXT NOT NULL,
			version INTEGER NOT NULL DEFAULT 1,
			status TEXT NOT NULL,
			source TEXT NOT NULL,
			content_hash TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE,
			FOREIGN KEY(requirement_id) REFERENCES requirements(id) ON DELETE CASCADE,
			FOREIGN KEY(workflow_run_id) REFERENCES workflow_runs(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_artifacts_workflow ON artifacts(workflow_run_id);`,
		`CREATE INDEX IF NOT EXISTS idx_artifacts_stage ON artifacts(stage_run_id);`,
		`CREATE INDEX IF NOT EXISTS idx_artifacts_type ON artifacts(artifact_type);`,
		`CREATE TABLE IF NOT EXISTS code_snapshots (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			workflow_run_id TEXT NOT NULL,
			stage_run_id TEXT NOT NULL DEFAULT '',
			snapshot_type TEXT NOT NULL,
			git_commit TEXT NOT NULL DEFAULT '',
			git_branch TEXT NOT NULL DEFAULT '',
			workspace_revision TEXT NOT NULL DEFAULT '',
			file_count INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE,
			FOREIGN KEY(workflow_run_id) REFERENCES workflow_runs(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_code_snapshots_workflow ON code_snapshots(workflow_run_id, created_at);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_code_snapshots_scope ON code_snapshots(workflow_run_id, stage_run_id, snapshot_type);`,
		`CREATE TABLE IF NOT EXISTS change_sets (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			workflow_run_id TEXT NOT NULL,
			stage_run_id TEXT NOT NULL DEFAULT '',
			base_snapshot_id TEXT NOT NULL,
			target_snapshot_id TEXT NOT NULL,
			change_scope TEXT NOT NULL,
			summary TEXT NOT NULL,
			file_stats_json TEXT NOT NULL DEFAULT '{}',
			files_json TEXT NOT NULL DEFAULT '[]',
			patch_artifact_id TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE,
			FOREIGN KEY(workflow_run_id) REFERENCES workflow_runs(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_change_sets_workflow ON change_sets(workflow_run_id, created_at);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_change_sets_scope ON change_sets(workflow_run_id, stage_run_id, change_scope);`,
		`CREATE TABLE IF NOT EXISTS rule_packs (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			scope TEXT NOT NULL,
			version TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			blocking INTEGER NOT NULL DEFAULT 0,
			source_type TEXT NOT NULL,
			source_ref TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS rule_execution_reports (
			id TEXT PRIMARY KEY,
			workflow_run_id TEXT NOT NULL,
			stage_run_id TEXT NOT NULL DEFAULT '',
			rule_pack_id TEXT NOT NULL,
			rule_pack_name TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			score INTEGER,
			blocking_violations INTEGER NOT NULL DEFAULT 0,
			non_blocking_violations INTEGER NOT NULL DEFAULT 0,
			output_path TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			FOREIGN KEY(workflow_run_id) REFERENCES workflow_runs(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS workflow_events (
			id TEXT PRIMARY KEY,
			workflow_run_id TEXT NOT NULL,
			stage_run_id TEXT NOT NULL DEFAULT '',
			event_type TEXT NOT NULL,
			payload_json TEXT NOT NULL DEFAULT '{}',
			created_at INTEGER NOT NULL,
			FOREIGN KEY(workflow_run_id) REFERENCES workflow_runs(id) ON DELETE CASCADE
		);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("ensure schema failed: %w", err)
		}
	}
	if err := s.ensureCLISessionProjectColumn(); err != nil {
		return err
	}
	if err := s.ensureCLISessionProcessColumns(); err != nil {
		return err
	}
	if err := s.ensureRequirementAutomationColumns(); err != nil {
		return err
	}
	if err := s.ensureProjectAutomationPauseColumn(); err != nil {
		return err
	}
	if err := s.ensureRequirementSortOrderColumn(); err != nil {
		return err
	}
	if err := s.ensureRequirementSessionFields(); err != nil {
		return err
	}
	if err := s.ensureRequirementWorkflowPolicyColumns(); err != nil {
		return err
	}
	if err := s.ensureRequirementWatchdogFields(); err != nil {
		return err
	}
	return nil
}

// ensureRequirementSessionFields upgrades requirements table with cli_type and auto_clear_session columns.
func (s *SQLiteStore) ensureRequirementSessionFields() error {
	if s == nil || s.db == nil {
		return errors.New("sqlite store is not initialized")
	}

	cliTypeExists, err := s.tableColumnExists("requirements", "cli_type")
	if err != nil {
		return fmt.Errorf("inspect requirements.cli_type: %w", err)
	}
	if !cliTypeExists {
		if _, err := s.db.Exec(`ALTER TABLE requirements ADD COLUMN cli_type TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add requirements.cli_type failed: %w", err)
		}
	}

	autoClearExists, err := s.tableColumnExists("requirements", "auto_clear_session")
	if err != nil {
		return fmt.Errorf("inspect requirements.auto_clear_session: %w", err)
	}
	if !autoClearExists {
		if _, err := s.db.Exec(`ALTER TABLE requirements ADD COLUMN auto_clear_session INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add requirements.auto_clear_session failed: %w", err)
		}
	}

	return nil
}

func (s *SQLiteStore) ensureRequirementWorkflowPolicyColumns() error {
	if s == nil || s.db == nil {
		return errors.New("sqlite store is not initialized")
	}

	columns := []struct {
		name       string
		definition string
	}{
		{name: "requires_design_review", definition: `ALTER TABLE requirements ADD COLUMN requires_design_review INTEGER NOT NULL DEFAULT 0`},
		{name: "requires_code_review", definition: `ALTER TABLE requirements ADD COLUMN requires_code_review INTEGER NOT NULL DEFAULT 0`},
		{name: "requires_acceptance_review", definition: `ALTER TABLE requirements ADD COLUMN requires_acceptance_review INTEGER NOT NULL DEFAULT 0`},
		{name: "requires_release_approval", definition: `ALTER TABLE requirements ADD COLUMN requires_release_approval INTEGER NOT NULL DEFAULT 0`},
	}

	for _, column := range columns {
		exists, err := s.tableColumnExists("requirements", column.name)
		if err != nil {
			return fmt.Errorf("inspect requirements.%s: %w", column.name, err)
		}
		if exists {
			continue
		}
		if _, err := s.db.Exec(column.definition); err != nil {
			return fmt.Errorf("add requirements.%s failed: %w", column.name, err)
		}
	}

	return nil
}

// ensureRequirementWatchdogFields upgrades requirements table with watchdog policy columns.
func (s *SQLiteStore) ensureRequirementWatchdogFields() error {
	if s == nil || s.db == nil {
		return errors.New("sqlite store is not initialized")
	}

	type requirementColumn struct {
		name       string
		definition string
	}
	columns := []requirementColumn{
		{name: "no_response_timeout_minutes", definition: `ALTER TABLE requirements ADD COLUMN no_response_timeout_minutes INTEGER NOT NULL DEFAULT 0`},
		{name: "no_response_error_action", definition: `ALTER TABLE requirements ADD COLUMN no_response_error_action TEXT NOT NULL DEFAULT 'none'`},
		{name: "no_response_idle_action", definition: `ALTER TABLE requirements ADD COLUMN no_response_idle_action TEXT NOT NULL DEFAULT 'none'`},
	}

	for _, column := range columns {
		exists, err := s.tableColumnExists("requirements", column.name)
		if err != nil {
			return fmt.Errorf("inspect requirements.%s: %w", column.name, err)
		}
		if exists {
			continue
		}
		if _, err := s.db.Exec(column.definition); err != nil {
			return fmt.Errorf("add requirements.%s failed: %w", column.name, err)
		}
	}

	if _, err := s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_requirements_project_running
		ON requirements(project_id)
		WHERE status = 'running'`); err != nil {
		return fmt.Errorf("create running requirement unique index failed: %w", err)
	}

	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS requirement_watchdog_events (
		id TEXT PRIMARY KEY,
		requirement_id TEXT NOT NULL,
		session_id TEXT NOT NULL DEFAULT '',
		trigger_kind TEXT NOT NULL,
		trigger_reason TEXT NOT NULL DEFAULT '',
		action TEXT NOT NULL,
		status TEXT NOT NULL,
		detail TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL,
		finished_at INTEGER,
		FOREIGN KEY(requirement_id) REFERENCES requirements(id) ON DELETE CASCADE
	)`); err != nil {
		return fmt.Errorf("create requirement_watchdog_events failed: %w", err)
	}

	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_requirement_watchdog_requirement
		ON requirement_watchdog_events(requirement_id, created_at DESC)`); err != nil {
		return fmt.Errorf("create requirement_watchdog_events index failed: %w", err)
	}

	return nil
}

// ensureCLISessionProjectColumn upgrades legacy cli_sessions table by adding project_id metadata.
func (s *SQLiteStore) ensureCLISessionProjectColumn() error {
	if s == nil || s.db == nil {
		return errors.New("sqlite store is not initialized")
	}
	exists, err := s.tableColumnExists("cli_sessions", "project_id")
	if err != nil {
		return fmt.Errorf("inspect cli_sessions columns: %w", err)
	}
	if !exists {
		if _, err := s.db.Exec(`ALTER TABLE cli_sessions ADD COLUMN project_id TEXT`); err != nil {
			return fmt.Errorf("add cli_sessions.project_id failed: %w", err)
		}
	}
	if _, err := s.db.Exec(`UPDATE cli_sessions
		SET project_id = (
			SELECT r.project_id
			FROM requirements r
			WHERE r.id = cli_sessions.requirement_id
		)
		WHERE (project_id IS NULL OR TRIM(project_id) = '')
		  AND requirement_id IS NOT NULL
		  AND TRIM(requirement_id) <> ''`); err != nil {
		return fmt.Errorf("backfill cli_sessions.project_id failed: %w", err)
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_cli_sessions_project ON cli_sessions(project_id)`); err != nil {
		return fmt.Errorf("ensure idx_cli_sessions_project failed: %w", err)
	}
	return nil
}

// ensureCLISessionProcessColumns upgrades legacy cli_sessions table by adding process metadata columns.
func (s *SQLiteStore) ensureCLISessionProcessColumns() error {
	if s == nil || s.db == nil {
		return errors.New("sqlite store is not initialized")
	}
	processPIDExists, err := s.tableColumnExists("cli_sessions", "process_pid")
	if err != nil {
		return fmt.Errorf("inspect cli_sessions.process_pid: %w", err)
	}
	if !processPIDExists {
		if _, err := s.db.Exec(`ALTER TABLE cli_sessions ADD COLUMN process_pid INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add cli_sessions.process_pid failed: %w", err)
		}
	}
	processPGIDExists, err := s.tableColumnExists("cli_sessions", "process_pgid")
	if err != nil {
		return fmt.Errorf("inspect cli_sessions.process_pgid: %w", err)
	}
	if !processPGIDExists {
		if _, err := s.db.Exec(`ALTER TABLE cli_sessions ADD COLUMN process_pgid INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add cli_sessions.process_pgid failed: %w", err)
		}
	}
	return nil
}

// ensureRequirementAutomationColumns upgrades requirements table with automation metadata.
func (s *SQLiteStore) ensureRequirementAutomationColumns() error {
	if s == nil || s.db == nil {
		return errors.New("sqlite store is not initialized")
	}

	executionModeExists, err := s.tableColumnExists("requirements", "execution_mode")
	if err != nil {
		return fmt.Errorf("inspect requirements.execution_mode: %w", err)
	}
	if !executionModeExists {
		if _, err := s.db.Exec(`ALTER TABLE requirements ADD COLUMN execution_mode TEXT NOT NULL DEFAULT 'manual'`); err != nil {
			return fmt.Errorf("add requirements.execution_mode failed: %w", err)
		}
	}
	if _, err := s.db.Exec(`UPDATE requirements
		SET execution_mode = 'manual'
		WHERE execution_mode IS NULL OR TRIM(execution_mode) = ''`); err != nil {
		return fmt.Errorf("backfill requirements.execution_mode failed: %w", err)
	}

	promptDispatchedExists, err := s.tableColumnExists("requirements", "prompt_dispatched_at")
	if err != nil {
		return fmt.Errorf("inspect requirements.prompt_dispatched_at: %w", err)
	}
	if !promptDispatchedExists {
		if _, err := s.db.Exec(`ALTER TABLE requirements ADD COLUMN prompt_dispatched_at INTEGER`); err != nil {
			return fmt.Errorf("add requirements.prompt_dispatched_at failed: %w", err)
		}
	}
	promptReplayedExists, err := s.tableColumnExists("requirements", "prompt_replayed_at")
	if err != nil {
		return fmt.Errorf("inspect requirements.prompt_replayed_at: %w", err)
	}
	if !promptReplayedExists {
		if _, err := s.db.Exec(`ALTER TABLE requirements ADD COLUMN prompt_replayed_at INTEGER`); err != nil {
			return fmt.Errorf("add requirements.prompt_replayed_at failed: %w", err)
		}
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_requirements_project_status_mode ON requirements(project_id, status, execution_mode, created_at)`); err != nil {
		return fmt.Errorf("ensure idx_requirements_project_status_mode failed: %w", err)
	}
	automationColumns := []struct {
		name       string
		definition string
	}{
		{name: "auto_retry_attempts", definition: `ALTER TABLE requirements ADD COLUMN auto_retry_attempts INTEGER NOT NULL DEFAULT 0`},
		{name: "last_auto_retry_reason", definition: `ALTER TABLE requirements ADD COLUMN last_auto_retry_reason TEXT NOT NULL DEFAULT ''`},
		{name: "retry_budget_exhausted_at", definition: `ALTER TABLE requirements ADD COLUMN retry_budget_exhausted_at INTEGER`},
	}
	for _, column := range automationColumns {
		exists, err := s.tableColumnExists("requirements", column.name)
		if err != nil {
			return fmt.Errorf("inspect requirements.%s: %w", column.name, err)
		}
		if exists {
			continue
		}
		if _, err := s.db.Exec(column.definition); err != nil {
			return fmt.Errorf("add requirements.%s failed: %w", column.name, err)
		}
	}
	return nil
}

// ensureRequirementSortOrderColumn upgrades requirements table with explicit ordering metadata.
func (s *SQLiteStore) ensureRequirementSortOrderColumn() error {
	if s == nil || s.db == nil {
		return errors.New("sqlite store is not initialized")
	}

	exists, err := s.tableColumnExists("requirements", "sort_order")
	if err != nil {
		return fmt.Errorf("inspect requirements.sort_order: %w", err)
	}
	if !exists {
		if _, err := s.db.Exec(`ALTER TABLE requirements ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add requirements.sort_order failed: %w", err)
		}
	}

	if err := s.normalizeRequirementSortOrders(); err != nil {
		return fmt.Errorf("normalize requirements.sort_order failed: %w", err)
	}

	if _, err := s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_requirements_project_sort_order
		ON requirements(project_id, sort_order)`); err != nil {
		return fmt.Errorf("ensure idx_requirements_project_sort_order failed: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ensureProjectAutomationPauseColumn() error {
	if s == nil || s.db == nil {
		return errors.New("sqlite store is not initialized")
	}
	exists, err := s.tableColumnExists("projects", "automation_paused")
	if err != nil {
		return fmt.Errorf("inspect projects.automation_paused: %w", err)
	}
	if exists {
		return nil
	}
	if _, err := s.db.Exec(`ALTER TABLE projects ADD COLUMN automation_paused INTEGER NOT NULL DEFAULT 0`); err != nil {
		return fmt.Errorf("add projects.automation_paused failed: %w", err)
	}
	return nil
}

func (s *SQLiteStore) normalizeRequirementSortOrders() error {
	rows, err := s.db.Query(`SELECT DISTINCT project_id FROM requirements`)
	if err != nil {
		return err
	}
	defer rows.Close()

	projectIDs := make([]string, 0)
	for rows.Next() {
		var projectID string
		if err := rows.Scan(&projectID); err != nil {
			return err
		}
		projectID = strings.TrimSpace(projectID)
		if projectID != "" {
			projectIDs = append(projectIDs, projectID)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, projectID := range projectIDs {
		needsNormalize, err := s.requirementSortOrderNeedsNormalize(projectID)
		if err != nil {
			return err
		}
		if !needsNormalize {
			continue
		}
		if err := s.resequenceProjectRequirements(projectID); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) requirementSortOrderNeedsNormalize(projectID string) (bool, error) {
	row := s.db.QueryRow(`SELECT
		COUNT(*),
		COUNT(DISTINCT CASE WHEN sort_order > 0 THEN sort_order END),
		COALESCE(MIN(sort_order), 0)
	FROM requirements
	WHERE project_id = ?`, strings.TrimSpace(projectID))

	var total int
	var positiveDistinct int
	var minSortOrder int
	if err := row.Scan(&total, &positiveDistinct, &minSortOrder); err != nil {
		return false, err
	}
	if total == 0 {
		return false, nil
	}
	if minSortOrder <= 0 {
		return true, nil
	}
	if positiveDistinct != total {
		return true, nil
	}
	return false, nil
}

func (s *SQLiteStore) resequenceProjectRequirements(projectID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.Query(`SELECT id
		FROM requirements
		WHERE project_id = ?
		ORDER BY CASE WHEN sort_order > 0 THEN 0 ELSE 1 END, sort_order ASC, created_at ASC, id ASC`, strings.TrimSpace(projectID))
	if err != nil {
		return err
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for idx, id := range ids {
		if _, err := tx.Exec(`UPDATE requirements SET sort_order = ? WHERE id = ?`, idx+1, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// tableColumnExists reports whether one sqlite table has the specified column.
func (s *SQLiteStore) tableColumnExists(tableName, columnName string) (bool, error) {
	rows, err := s.db.Query(`PRAGMA table_info(` + tableName + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			typ        string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultVal, &pk); err != nil {
			return false, err
		}
		if strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(columnName)) {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}
