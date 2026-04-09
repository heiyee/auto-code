package persistence

import (
	"database/sql"
	"encoding/json"
)

func jsonString(value any) string {
	bytes, err := json.Marshal(value)
	if err != nil {
		return "[]"
	}
	return string(bytes)
}

func jsonObjectString(value any) string {
	bytes, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(bytes)
}

func decodeJSONList[T any](raw string) []T {
	if raw == "" {
		return nil
	}
	var items []T
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil
	}
	return items
}

func decodeJSONObject[T any](raw string) T {
	var value T
	if raw == "" {
		return value
	}
	_ = json.Unmarshal([]byte(raw), &value)
	return value
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func intToBool(v int64) bool {
	return v != 0
}

func nullString(raw string) sql.NullString {
	if raw == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: raw, Valid: true}
}
