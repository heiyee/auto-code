package persistence

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"auto-code/internal/domain"
)

// CreateProject inserts a new project.
func (s *SQLiteStore) CreateProject(input domain.ProjectMutation) (*domain.Project, error) {
	now := time.Now()
	id := newPrefixedID("proj")
	automationPaused := 0
	if input.AutomationPaused {
		automationPaused = 1
	}
	if _, err := s.db.Exec(`INSERT INTO projects(id, name, repository, branch, work_dir, automation_paused, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		strings.TrimSpace(input.Name),
		strings.TrimSpace(input.Repository),
		strings.TrimSpace(input.Branch),
		strings.TrimSpace(input.WorkDir),
		automationPaused,
		toUnixMilli(now),
		toUnixMilli(now),
	); err != nil {
		return nil, err
	}
	return s.GetProject(id)
}

// UpdateProject updates mutable project fields.
func (s *SQLiteStore) UpdateProject(projectID string, input domain.ProjectMutation) (*domain.Project, error) {
	now := time.Now()
	automationPaused := 0
	if input.AutomationPaused {
		automationPaused = 1
	}
	result, err := s.db.Exec(`UPDATE projects
		SET name = ?, repository = ?, branch = ?, work_dir = ?, automation_paused = ?, updated_at = ?
		WHERE id = ?`,
		strings.TrimSpace(input.Name),
		strings.TrimSpace(input.Repository),
		strings.TrimSpace(input.Branch),
		strings.TrimSpace(input.WorkDir),
		automationPaused,
		toUnixMilli(now),
		strings.TrimSpace(projectID),
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
	return s.GetProject(projectID)
}

// DeleteProject removes a project and returns how many requirements/sessions were cascaded.
func (s *SQLiteStore) DeleteProject(projectID string) (domain.DeleteProjectStats, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return domain.DeleteProjectStats{}, errors.New("project id is required")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return domain.DeleteProjectStats{}, err
	}
	defer func() { _ = tx.Rollback() }()

	stats := domain.DeleteProjectStats{}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM requirements WHERE project_id = ?`, projectID).Scan(&stats.RequirementCount); err != nil {
		return domain.DeleteProjectStats{}, err
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM cli_sessions
		WHERE project_id = ?
		   OR requirement_id IN (SELECT id FROM requirements WHERE project_id = ?)`, projectID, projectID).Scan(&stats.CLISessionCount); err != nil {
		return domain.DeleteProjectStats{}, err
	}
	if _, err := tx.Exec(`DELETE FROM cli_sessions
		WHERE project_id = ?
		   OR requirement_id IN (SELECT id FROM requirements WHERE project_id = ?)`, projectID, projectID); err != nil {
		return domain.DeleteProjectStats{}, err
	}

	result, err := tx.Exec(`DELETE FROM projects WHERE id = ?`, projectID)
	if err != nil {
		return domain.DeleteProjectStats{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return domain.DeleteProjectStats{}, err
	}
	if affected == 0 {
		return domain.DeleteProjectStats{}, ErrNotFound
	}

	if err := tx.Commit(); err != nil {
		return domain.DeleteProjectStats{}, err
	}
	return stats, nil
}

// GetProject fetches one project by id.
func (s *SQLiteStore) GetProject(projectID string) (*domain.Project, error) {
	projectID = strings.TrimSpace(projectID)
	row := s.db.QueryRow(`SELECT id, name, repository, branch, work_dir, automation_paused, created_at, updated_at FROM projects WHERE id = ?`, projectID)
	return scanProject(row)
}

// ListProjectSummaries returns all projects with requirement counters.
func (s *SQLiteStore) ListProjectSummaries() ([]domain.ProjectSummary, error) {
	rows, err := s.db.Query(`SELECT
		p.id, p.name, p.repository, p.branch, p.work_dir, p.automation_paused, p.created_at, p.updated_at,
		COUNT(r.id) AS requirement_count,
		SUM(CASE WHEN r.status = ? THEN 1 ELSE 0 END) AS running_count,
		SUM(CASE WHEN r.status = ? THEN 1 ELSE 0 END) AS done_count
	FROM projects p
	LEFT JOIN requirements r ON r.project_id = p.id
	GROUP BY p.id
	ORDER BY p.created_at DESC`, domain.RequirementStatusRunning, domain.RequirementStatusDone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]domain.ProjectSummary, 0)
	for rows.Next() {
		var summary domain.ProjectSummary
		var automationPaused int64
		var createdAt int64
		var updatedAt int64
		var runningCount sql.NullInt64
		var doneCount sql.NullInt64
		if err := rows.Scan(
			&summary.ID,
			&summary.Name,
			&summary.Repository,
			&summary.Branch,
			&summary.WorkDir,
			&automationPaused,
			&createdAt,
			&updatedAt,
			&summary.RequirementCount,
			&runningCount,
			&doneCount,
		); err != nil {
			return nil, err
		}
		summary.AutomationPaused = automationPaused != 0
		summary.CreatedAt = fromUnixMilli(createdAt)
		summary.UpdatedAt = fromUnixMilli(updatedAt)
		summary.RunningRequirementCount = int(runningCount.Int64)
		summary.DoneRequirementCount = int(doneCount.Int64)
		list = append(list, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

// scanProject converts sql row to domain.Project.
func scanProject(row scanner) (*domain.Project, error) {
	var project domain.Project
	var automationPaused int64
	var createdAt int64
	var updatedAt int64
	if err := row.Scan(&project.ID, &project.Name, &project.Repository, &project.Branch, &project.WorkDir, &automationPaused, &createdAt, &updatedAt); err != nil {
		return nil, translateNoRows(err)
	}
	project.AutomationPaused = automationPaused != 0
	project.CreatedAt = fromUnixMilli(createdAt)
	project.UpdatedAt = fromUnixMilli(updatedAt)
	return &project, nil
}
