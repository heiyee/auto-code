package persistence

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

type scanner interface {
	Scan(dest ...any) error
}

// toUnixMilli converts time to unix milliseconds.
func toUnixMilli(ts time.Time) int64 {
	if ts.IsZero() {
		return 0
	}
	return ts.UnixMilli()
}

// toNullableUnixMilli converts optional time pointer to optional unix milliseconds.
func toNullableUnixMilli(ts *time.Time) any {
	if ts == nil || ts.IsZero() {
		return nil
	}
	return ts.UnixMilli()
}

// nullInt64Arg converts nullable int64 to an SQL argument.
func nullInt64Arg(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}

// fromUnixMilli converts unix milliseconds to time.Time.
func fromUnixMilli(v int64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(v)
}

// nullableUnixMilliToTime converts sql.NullInt64 to optional time pointer.
func nullableUnixMilliToTime(v sql.NullInt64) *time.Time {
	if !v.Valid || v.Int64 <= 0 {
		return nil
	}
	ts := time.UnixMilli(v.Int64)
	return &ts
}

// nullIfEmpty converts empty string into nil SQL value.
func nullIfEmpty(value string) any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

// translateNoRows converts sql.ErrNoRows into package-level not found error.
func translateNoRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
