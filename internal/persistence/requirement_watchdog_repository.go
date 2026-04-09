package persistence

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"auto-code/internal/domain"
)

// CreateRequirementWatchdogEvent inserts one watchdog event record.
func (s *SQLiteStore) CreateRequirementWatchdogEvent(input domain.RequirementWatchdogEvent) (*domain.RequirementWatchdogEvent, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	now := time.Now()
	if input.CreatedAt.IsZero() {
		input.CreatedAt = now
	}
	if strings.TrimSpace(input.ID) == "" {
		input.ID = newPrefixedID("wdg")
	}
	if _, err := s.db.Exec(`INSERT INTO requirement_watchdog_events(
			id, requirement_id, session_id, trigger_kind, trigger_reason, action, status, detail, created_at, finished_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		strings.TrimSpace(input.ID),
		strings.TrimSpace(input.RequirementID),
		strings.TrimSpace(input.SessionID),
		strings.TrimSpace(input.TriggerKind),
		strings.TrimSpace(input.TriggerReason),
		strings.TrimSpace(input.Action),
		strings.TrimSpace(input.Status),
		strings.TrimSpace(input.Detail),
		toUnixMilli(input.CreatedAt),
		toNullableUnixMilli(input.FinishedAt),
	); err != nil {
		return nil, err
	}
	return s.GetRequirementWatchdogEvent(input.ID)
}

// FinishRequirementWatchdogEvent updates result fields for one watchdog event.
func (s *SQLiteStore) FinishRequirementWatchdogEvent(eventID, status, detail string, finishedAt time.Time) (*domain.RequirementWatchdogEvent, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return nil, errors.New("event id is required")
	}
	if finishedAt.IsZero() {
		finishedAt = time.Now()
	}
	result, err := s.db.Exec(`UPDATE requirement_watchdog_events
		SET status = ?, detail = ?, finished_at = ?
		WHERE id = ?`,
		strings.TrimSpace(status),
		strings.TrimSpace(detail),
		toUnixMilli(finishedAt),
		eventID,
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
	return s.GetRequirementWatchdogEvent(eventID)
}

// GetRequirementWatchdogEvent loads one watchdog event by id.
func (s *SQLiteStore) GetRequirementWatchdogEvent(eventID string) (*domain.RequirementWatchdogEvent, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	row := s.db.QueryRow(`SELECT
			id, requirement_id, session_id, trigger_kind, trigger_reason, action, status, detail, created_at, finished_at
		FROM requirement_watchdog_events
		WHERE id = ?`,
		strings.TrimSpace(eventID),
	)
	return scanRequirementWatchdogEvent(row)
}

// GetLatestRequirementWatchdogEvent returns newest watchdog event for one requirement.
func (s *SQLiteStore) GetLatestRequirementWatchdogEvent(requirementID string) (*domain.RequirementWatchdogEvent, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	row := s.db.QueryRow(`SELECT
			id, requirement_id, session_id, trigger_kind, trigger_reason, action, status, detail, created_at, finished_at
		FROM requirement_watchdog_events
		WHERE requirement_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT 1`,
		strings.TrimSpace(requirementID),
	)
	return scanRequirementWatchdogEvent(row)
}

// GetLatestRequirementWatchdogEventByStatus returns the newest watchdog event for one requirement filtered by status.
func (s *SQLiteStore) GetLatestRequirementWatchdogEventByStatus(requirementID, status string) (*domain.RequirementWatchdogEvent, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite store is not initialized")
	}
	row := s.db.QueryRow(`SELECT
			id, requirement_id, session_id, trigger_kind, trigger_reason, action, status, detail, created_at, finished_at
		FROM requirement_watchdog_events
		WHERE requirement_id = ? AND status = ?
		ORDER BY created_at DESC, id DESC
		LIMIT 1`,
		strings.TrimSpace(requirementID),
		strings.TrimSpace(status),
	)
	return scanRequirementWatchdogEvent(row)
}

// CountRequirementWatchdogRetryAttempts returns how many resend/rebuild watchdog actions were already recorded.
func (s *SQLiteStore) CountRequirementWatchdogRetryAttempts(requirementID string) (int, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("sqlite store is not initialized")
	}
	requirementID = strings.TrimSpace(requirementID)
	if requirementID == "" {
		return 0, errors.New("requirement id is required")
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*)
		FROM requirement_watchdog_events
		WHERE requirement_id = ?
		  AND action IN (?, ?)
		  AND status IN (?, ?, ?)`,
		requirementID,
		domain.RequirementNoResponseActionResendRequirement,
		domain.RequirementNoResponseActionCloseAndResendRequirement,
		domain.RequirementWatchdogEventStatusPending,
		domain.RequirementWatchdogEventStatusSucceeded,
		domain.RequirementWatchdogEventStatusFailed,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count requirement watchdog retry attempts: %w", err)
	}
	return count, nil
}

func scanRequirementWatchdogEvent(row scanner) (*domain.RequirementWatchdogEvent, error) {
	var item domain.RequirementWatchdogEvent
	var createdAt int64
	var finishedAt sql.NullInt64
	if err := row.Scan(
		&item.ID,
		&item.RequirementID,
		&item.SessionID,
		&item.TriggerKind,
		&item.TriggerReason,
		&item.Action,
		&item.Status,
		&item.Detail,
		&createdAt,
		&finishedAt,
	); err != nil {
		return nil, translateNoRows(err)
	}
	item.CreatedAt = fromUnixMilli(createdAt)
	item.FinishedAt = nullableUnixMilliToTime(finishedAt)
	return &item, nil
}
