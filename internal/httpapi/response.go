package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Response is the unified envelope structure for all JSON HTTP APIs.
type Response[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data,omitempty"`
}

// EmptyData marks responses that intentionally return no data payload.
type EmptyData struct{}

// NewSuccess builds a successful response payload with code=0.
func NewSuccess[T any](message string, data T) Response[T] {
	if strings.TrimSpace(message) == "" {
		message = "ok"
	}
	return Response[T]{
		Code:    0,
		Message: message,
		Data:    data,
	}
}

// NewError builds a failed response payload with non-zero code.
func NewError(message string, code int) Response[EmptyData] {
	if strings.TrimSpace(message) == "" {
		message = "request failed"
	}
	if code == 0 {
		code = http.StatusInternalServerError
	}
	return Response[EmptyData]{
		Code:    code,
		Message: message,
	}
}

// WriteSuccess writes one successful API response to HTTP writer.
func WriteSuccess[T any](w http.ResponseWriter, httpStatus int, message string, data T) {
	Write(w, httpStatus, NewSuccess(message, data))
}

// WriteError writes one failed API response to HTTP writer.
func WriteError(w http.ResponseWriter, httpStatus int, code int, message string) {
	if code == 0 {
		code = httpStatus
	}
	Write(w, httpStatus, NewError(message, code))
}

// Write serializes one JSON payload with status code.
func Write(w http.ResponseWriter, httpStatus int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(payload)
}
