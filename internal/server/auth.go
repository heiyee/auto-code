package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"auto-code/internal/config"
)

const (
	sessionCookieName = "auto_code_session"
)

// SessionData holds the data stored in the session cookie.
type SessionData struct {
	Version   int    `json:"v"`
	Username  string `json:"u"`
	ExpiresAt int64  `json:"e"`
}

// AuthService provides authentication functionality.
type AuthService struct {
	enabled       bool
	username      string
	password      string
	sessionSecret []byte
	sessionMaxAge int
}

// NewAuthService creates a new authentication service.
func NewAuthService(cfg config.AuthConfig) *AuthService {
	return &AuthService{
		enabled:       cfg.Enabled,
		username:      cfg.Username,
		password:      cfg.Password,
		sessionSecret: cfg.SessionSecret,
		sessionMaxAge: cfg.SessionMaxAge,
	}
}

// Enabled returns whether authentication is enabled.
func (s *AuthService) Enabled() bool {
	return s != nil && s.enabled
}

// ValidateCredentials checks if the provided username and password are valid.
func (s *AuthService) ValidateCredentials(username, password string) bool {
	if !s.Enabled() {
		return false
	}
	if username != s.username {
		return false
	}
	// Compare password directly
	return hmac.Equal([]byte(password), []byte(s.password))
}

// CreateSession creates a signed session cookie value for the given username.
func (s *AuthService) CreateSession(username string) (string, error) {
	if !s.Enabled() {
		return "", fmt.Errorf("auth is not enabled")
	}

	session := SessionData{
		Version:   1,
		Username:  username,
		ExpiresAt: time.Now().Add(time.Duration(s.sessionMaxAge) * time.Second).Unix(),
	}

	data, err := json.Marshal(session)
	if err != nil {
		return "", fmt.Errorf("marshal session data: %w", err)
	}

	encoded := base64.URLEncoding.EncodeToString(data)
	signature := s.sign(encoded)

	return encoded + "." + signature, nil
}

// ValidateSession validates the session cookie value and returns the session data.
func (s *AuthService) ValidateSession(cookieValue string) (*SessionData, error) {
	if !s.Enabled() {
		return nil, fmt.Errorf("auth is not enabled")
	}

	parts := strings.Split(cookieValue, ".")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid session format")
	}

	encoded, signature := parts[0], parts[1]

	// Verify signature
	expectedSig := s.sign(encoded)
	if !hmac.Equal([]byte(signature), []byte(expectedSig)) {
		return nil, fmt.Errorf("invalid session signature")
	}

	// Decode session data
	data, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode session data: %w", err)
	}

	var session SessionData
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("unmarshal session data: %w", err)
	}

	// Check expiration
	if time.Now().Unix() > session.ExpiresAt {
		return nil, fmt.Errorf("session expired")
	}

	return &session, nil
}

// SetSessionCookie sets the session cookie on the response.
func (s *AuthService) SetSessionCookie(w http.ResponseWriter, r *http.Request, value string) {
	secure := false
	if r != nil {
		secure = r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   s.sessionMaxAge,
	})
}

// ClearSessionCookie clears the session cookie.
func (s *AuthService) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

// sign creates an HMAC-SHA256 signature of the data.
func (s *AuthService) sign(data string) string {
	h := hmac.New(sha256.New, s.sessionSecret)
	h.Write([]byte(data))
	return base64.URLEncoding.EncodeToString(h.Sum(nil))
}
