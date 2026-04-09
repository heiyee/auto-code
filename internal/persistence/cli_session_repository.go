package persistence

import (
	"errors"
	"strings"
	"time"

	"auto-code/internal/domain"
)

const cliSessionSelectColumns = `SELECT
	c.id,
	c.cli_type,
	c.profile,
	c.agent_id,
	COALESCE(c.project_id, COALESCE(r.project_id, ''), ''),
	COALESCE(p.name, ''),
	COALESCE(c.requirement_id, ''),
	COALESCE(r.title, ''),
	c.work_dir,
	c.session_state,
	COALESCE(c.launch_mode, ''),
	COALESCE(c.process_pid, 0),
	COALESCE(c.process_pgid, 0),
	c.created_at,
	c.last_active_at
FROM cli_sessions c
LEFT JOIN requirements r ON r.id = c.requirement_id
LEFT JOIN projects p ON p.id = COALESCE(c.project_id, r.project_id)`

// CreateCLISessionRecord inserts persisted metadata for one CLI session.
func (s *SQLiteStore) CreateCLISessionRecord(record domain.CLISessionRecord) error {
	if strings.TrimSpace(record.ID) == "" {
		return errors.New("session id is required")
	}
	if strings.TrimSpace(record.AgentID) == "" {
		return errors.New("agent id is required")
	}
	if strings.TrimSpace(record.CLIType) == "" {
		return errors.New("cli type is required")
	}
	if strings.TrimSpace(record.Profile) == "" {
		return errors.New("profile is required")
	}
	if strings.TrimSpace(record.WorkDir) == "" {
		return errors.New("work_dir is required")
	}
	if strings.TrimSpace(record.SessionState) == "" {
		record.SessionState = domain.CLISessionStateRunning
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}
	if record.LastActiveAt.IsZero() {
		record.LastActiveAt = record.CreatedAt
	}

	_, err := s.db.Exec(`INSERT INTO cli_sessions(
		id, cli_type, profile, agent_id, project_id, requirement_id, work_dir,
		session_state, launch_mode, process_pid, process_pgid, created_at, last_active_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID,
		record.CLIType,
		record.Profile,
		record.AgentID,
		nullIfEmpty(record.ProjectID),
		nullIfEmpty(record.RequirementID),
		record.WorkDir,
		record.SessionState,
		record.LaunchMode,
		record.ProcessPID,
		record.ProcessPGID,
		toUnixMilli(record.CreatedAt),
		toUnixMilli(record.LastActiveAt),
	)
	return err
}

// UpdateCLISessionState updates session state, launch mode and last active timestamp.
func (s *SQLiteStore) UpdateCLISessionState(sessionID, state, launchMode string, at time.Time) error {
	return s.UpdateCLISessionRuntime(sessionID, state, launchMode, 0, 0, at)
}

// UpdateCLISessionRuntime updates state/runtime metadata and last activity timestamp.
func (s *SQLiteStore) UpdateCLISessionRuntime(sessionID, state, launchMode string, processPID, processPGID int, at time.Time) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session id is required")
	}
	if at.IsZero() {
		at = time.Now()
	}
	result, err := s.db.Exec(`UPDATE cli_sessions
		SET session_state = ?, launch_mode = ?, process_pid = ?, process_pgid = ?, last_active_at = ?
		WHERE id = ?`,
		strings.TrimSpace(state),
		strings.TrimSpace(launchMode),
		processPID,
		processPGID,
		toUnixMilli(at),
		sessionID,
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

// UpdateCLISessionBinding updates project/requirement ownership metadata for one session.
func (s *SQLiteStore) UpdateCLISessionBinding(sessionID, projectID, requirementID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session id is required")
	}
	result, err := s.db.Exec(`UPDATE cli_sessions
		SET project_id = ?, requirement_id = ?
		WHERE id = ?`,
		nullIfEmpty(projectID),
		nullIfEmpty(requirementID),
		sessionID,
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

// TouchCLISession updates last_active_at to reflect recent interaction.
func (s *SQLiteStore) TouchCLISession(sessionID string, at time.Time) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session id is required")
	}
	if at.IsZero() {
		at = time.Now()
	}
	result, err := s.db.Exec(`UPDATE cli_sessions SET last_active_at = ? WHERE id = ?`, toUnixMilli(at), sessionID)
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

// DeleteCLISessionRecord removes one persisted CLI session record.
func (s *SQLiteStore) DeleteCLISessionRecord(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session id is required")
	}
	result, err := s.db.Exec(`DELETE FROM cli_sessions WHERE id = ?`, sessionID)
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

// GetCLISessionRecord reads one persisted CLI session by id.
func (s *SQLiteStore) GetCLISessionRecord(sessionID string) (*domain.CLISessionRecord, error) {
	row := s.db.QueryRow(cliSessionSelectColumns+` WHERE c.id = ?`, strings.TrimSpace(sessionID))
	return scanCLISessionRecord(row)
}

// ListCLISessionRecords returns all CLI sessions with optional cli type filter.
func (s *SQLiteStore) ListCLISessionRecords(cliType string) ([]domain.CLISessionRecord, error) {
	cliType = strings.TrimSpace(strings.ToLower(cliType))
	baseSQL := cliSessionSelectColumns
	args := make([]any, 0, 1)
	if cliType != "" {
		baseSQL += ` WHERE c.cli_type = ?`
		args = append(args, cliType)
	}
	baseSQL += ` ORDER BY c.created_at DESC`

	rows, err := s.db.Query(baseSQL, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]domain.CLISessionRecord, 0)
	for rows.Next() {
		record, err := scanCLISessionRecord(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

// ListCLISessionRecordsByRequirement returns all sessions linked to one requirement.
func (s *SQLiteStore) ListCLISessionRecordsByRequirement(requirementID string) ([]domain.CLISessionRecord, error) {
	rows, err := s.db.Query(
		cliSessionSelectColumns+` WHERE c.requirement_id = ? ORDER BY c.created_at DESC`,
		strings.TrimSpace(requirementID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]domain.CLISessionRecord, 0)
	for rows.Next() {
		record, err := scanCLISessionRecord(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

// ListCLISessionRecordsByProject returns all sessions linked to one project.
func (s *SQLiteStore) ListCLISessionRecordsByProject(projectID string) ([]domain.CLISessionRecord, error) {
	projectID = strings.TrimSpace(projectID)
	rows, err := s.db.Query(
		cliSessionSelectColumns+` WHERE COALESCE(c.project_id, r.project_id) = ? ORDER BY c.created_at DESC`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]domain.CLISessionRecord, 0)
	for rows.Next() {
		record, err := scanCLISessionRecord(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

// MaxCLISessionSequence returns the max numeric suffix from `cli-xxxxxx` session ids.
func (s *SQLiteStore) MaxCLISessionSequence() (uint64, error) {
	row := s.db.QueryRow(`SELECT COALESCE(MAX(CAST(SUBSTR(id, 5) AS INTEGER)), 0)
		FROM cli_sessions
		WHERE id GLOB 'cli-[0-9]*'`)
	var maxSeq int64
	if err := row.Scan(&maxSeq); err != nil {
		return 0, err
	}
	if maxSeq <= 0 {
		return 0, nil
	}
	return uint64(maxSeq), nil
}

// scanCLISessionRecord converts sql row to domain.CLISessionRecord.
func scanCLISessionRecord(row scanner) (*domain.CLISessionRecord, error) {
	var record domain.CLISessionRecord
	var createdAt int64
	var lastActiveAt int64
	if err := row.Scan(
		&record.ID,
		&record.CLIType,
		&record.Profile,
		&record.AgentID,
		&record.ProjectID,
		&record.ProjectName,
		&record.RequirementID,
		&record.RequirementTitle,
		&record.WorkDir,
		&record.SessionState,
		&record.LaunchMode,
		&record.ProcessPID,
		&record.ProcessPGID,
		&createdAt,
		&lastActiveAt,
	); err != nil {
		return nil, translateNoRows(err)
	}
	record.CreatedAt = fromUnixMilli(createdAt)
	record.LastActiveAt = fromUnixMilli(lastActiveAt)
	return &record, nil
}
