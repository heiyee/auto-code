package server

import (
	"encoding/json"
	"net/http"
)

// handleLoginPage preserves the legacy entry point by redirecting to the React login route.
func (a *App) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	// If auth is disabled, redirect to home
	if a.authSvc == nil || !a.authSvc.Enabled() {
		http.Redirect(w, r, "/app/", http.StatusFound)
		return
	}

	// Check if already logged in
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if _, err := a.authSvc.ValidateSession(cookie.Value); err == nil {
			http.Redirect(w, r, "/app/", http.StatusFound)
			return
		}
	}
	http.Redirect(w, r, "/app/login", http.StatusFound)
}

// handleAPILogin handles login API requests.
func (a *App) handleAPILogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// If auth is disabled, return success
	if a.authSvc == nil || !a.authSvc.Enabled() {
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
		return
	}

	if !a.authSvc.ValidateCredentials(req.Username, req.Password) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid credentials"})
		return
	}

	sessionValue, err := a.authSvc.CreateSession(req.Username)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to create session"})
		return
	}

	a.authSvc.SetSessionCookie(w, r, sessionValue)
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// handleAPILogout handles logout API requests.
func (a *App) handleAPILogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if a.authSvc != nil {
		a.authSvc.ClearSessionCookie(w)
	}

	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// handleAPIAuthStatus returns whether authentication is enabled.
func (a *App) handleAPIAuthStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	enabled := a.authSvc != nil && a.authSvc.Enabled()
	json.NewEncoder(w).Encode(map[string]bool{"auth_enabled": enabled})
}
