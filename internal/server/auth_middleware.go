package server

import (
	"net/http"
	"strings"
)

// publicPaths are paths that don't require authentication.
var publicPaths = map[string]bool{
	"/login":           true,
	"/api/auth/login":  true,
	"/api/auth/logout": true,
	"/api/auth/status": true,
}

// AuthMiddleware checks if the request has a valid session cookie.
// If authentication is disabled, the middleware passes through all requests.
func AuthMiddleware(authSvc *AuthService, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If auth is disabled, pass through
		if authSvc == nil || !authSvc.Enabled() {
			next.ServeHTTP(w, r)
			return
		}

		path := r.URL.Path

		// Allow the React frontend shell and its embedded assets to load before auth checks.
		if strings.HasPrefix(path, "/app") {
			next.ServeHTTP(w, r)
			return
		}

		// Allow public paths
		if publicPaths[path] {
			next.ServeHTTP(w, r)
			return
		}

		// Check session cookie
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			handleUnauthorized(w, r)
			return
		}

		session, err := authSvc.ValidateSession(cookie.Value)
		if err != nil {
			handleUnauthorized(w, r)
			return
		}

		// Add username to request context (optional, for future use)
		_ = session.Username

		next.ServeHTTP(w, r)
	})
}

// handleUnauthorized responds with 401 for API requests or redirects to login for page requests.
func handleUnauthorized(w http.ResponseWriter, r *http.Request) {
	// Check if it's an API request
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
		return
	}

	// Redirect to the React login route for page requests.
	http.Redirect(w, r, "/app/login", http.StatusFound)
}
