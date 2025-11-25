package auth

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/anupcshan/hsadmin/internal/config"
	"tailscale.com/client/local"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const (
	// UserContextKey is the context key for storing authenticated user info
	UserContextKey contextKey = "authenticated_user"
)

// User represents an authenticated user
type User struct {
	ID     uint64   // Headscale user ID (for WhoIs auth)
	Email  string   // Email (for OIDC auth)
	Name   string   // Display name
	Method string   // Auth method used: "whois" or "oidc"
	Tags   []string // User tags (for WhoIs auth)
}

// Middleware handles authentication for all HTTP requests
type Middleware struct {
	config      *config.Config
	tsnetClient *local.Client
	oidcAuth    *OIDCAuthenticator // Will implement in next step
}

// NewMiddleware creates a new auth middleware
func NewMiddleware(cfg *config.Config, tsnetClient *local.Client) *Middleware {
	m := &Middleware{
		config:      cfg,
		tsnetClient: tsnetClient,
	}

	// Initialize OIDC authenticator if HTTP listener with OIDC is configured
	if cfg.Listeners.HTTP != nil && cfg.Listeners.HTTP.OIDC != nil {
		oidc, err := NewOIDCAuthenticator(cfg)
		if err != nil {
			log.Printf("Warning: Failed to initialize OIDC authenticator: %v", err)
			log.Printf("OIDC authentication will not be available")
		} else {
			m.oidcAuth = oidc
		}
	}

	return m
}

// RequireAuth is the middleware function that enforces authentication
func (m *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If no auth is configured, allow all requests
		if m.config.Listeners.Tailscale == nil && m.config.Listeners.HTTP == nil {
			next.ServeHTTP(w, r)
			return
		}

		// Skip auth for certain public paths
		if m.isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		var user *User
		var err error

		// Try WhoIs authentication first (if Tailscale listener configured)
		if m.config.Listeners.Tailscale != nil {
			user, err = m.authenticateWithWhoIs(r)
			if err == nil && user != nil {
				// WhoIs auth succeeded
				ctx := context.WithValue(r.Context(), UserContextKey, user)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			// WhoIs failed or returned nil, try OIDC if available
		}

		// Try OIDC authentication (if HTTP listener configured and WhoIs failed)
		if m.oidcAuth != nil {
			user, err = m.oidcAuth.AuthenticateFromSession(r)
			if err == nil && user != nil {
				// OIDC auth succeeded
				ctx := context.WithValue(r.Context(), UserContextKey, user)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		// Both auth methods failed
		m.handleUnauthorized(w, r)
	})
}

// authenticateWithWhoIs attempts to authenticate using Tailscale WhoIs
func (m *Middleware) authenticateWithWhoIs(r *http.Request) (*User, error) {
	// Get the remote address (connection peer)
	remoteAddr := r.RemoteAddr
	if remoteAddr == "" {
		return nil, fmt.Errorf("no remote address")
	}

	// For requests coming through tsnet, the RemoteAddr might be in format "ip:port"
	// Extract just the IP part
	if idx := strings.LastIndex(remoteAddr, ":"); idx != -1 {
		remoteAddr = remoteAddr[:idx]
	}

	// Call WhoIs to identify the connecting user
	whoIs, err := m.tsnetClient.WhoIs(r.Context(), remoteAddr)
	if err != nil {
		return nil, fmt.Errorf("whois lookup failed: %w", err)
	}

	if whoIs.Node == nil || whoIs.UserProfile == nil {
		return nil, fmt.Errorf("whois returned incomplete data")
	}

	// Extract user ID and tags
	// Note: Headscale doesn't populate UserProfile the same way Tailscale does
	// We need to get the user ID from the Node's User field
	var userID uint64
	if whoIs.Node.User != 0 {
		userID = uint64(whoIs.Node.User)
	} else {
		return nil, fmt.Errorf("could not determine user ID from whois")
	}

	// Get tags from the node
	var tags []string
	if whoIs.Node.Tags != nil {
		tags = whoIs.Node.Tags
	}

	// Check if user is authorized
	if !m.isAuthorizedUser(userID, tags) {
		return nil, fmt.Errorf("user %d is not authorized", userID)
	}

	// Create authenticated user
	user := &User{
		ID:     userID,
		Name:   whoIs.Node.Hostinfo.Hostname(),
		Method: "whois",
		Tags:   tags,
	}

	log.Printf("WhoIs auth successful: user_id=%d, hostname=%s", userID, user.Name)
	return user, nil
}

// isAuthorizedUser checks if a user is authorized based on ID or tags
func (m *Middleware) isAuthorizedUser(userID uint64, tags []string) bool {
	// No Tailscale listener configured means no WhoIs auth
	if m.config.Listeners.Tailscale == nil {
		return false
	}

	// Check if user ID is in the admin list
	for _, adminID := range m.config.Listeners.Tailscale.AdminUserIDs {
		if userID == adminID {
			return true
		}
	}

	// Check if user has any of the admin tags
	for _, userTag := range tags {
		for _, adminTag := range m.config.Listeners.Tailscale.AdminUserTags {
			if userTag == adminTag {
				return true
			}
		}
	}

	return false
}

// isPublicPath checks if a path should skip authentication
func (m *Middleware) isPublicPath(path string) bool {
	publicPaths := []string{
		"/auth/login",
		"/auth/callback",
		"/auth/logout",
	}

	for _, publicPath := range publicPaths {
		if path == publicPath || strings.HasPrefix(path, publicPath+"/") {
			return true
		}
	}

	return false
}

// handleUnauthorized handles requests that failed authentication
func (m *Middleware) handleUnauthorized(w http.ResponseWriter, r *http.Request) {
	// Check if this is an API/HTMX request or a browser request
	acceptHeader := r.Header.Get("Accept")
	isHTMXRequest := r.Header.Get("HX-Request") == "true"

	if isHTMXRequest || strings.Contains(acceptHeader, "application/json") {
		// API/HTMX request - return 403 JSON
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error": "Unauthorized", "message": "You do not have permission to access this resource"}`))
		return
	}

	// Browser request - redirect to login if HTTP listener with OIDC is configured
	if m.config.Listeners.HTTP != nil && m.oidcAuth != nil {
		http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
		return
	}

	// No OIDC available - show error page
	w.WriteHeader(http.StatusForbidden)
	w.Write([]byte(`
<!DOCTYPE html>
<html>
<head>
	<title>Unauthorized</title>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-gray-900 text-gray-100 min-h-screen flex items-center justify-center">
	<div class="max-w-md p-8 bg-gray-800 rounded-lg shadow-xl text-center">
		<h1 class="text-2xl font-bold text-red-400 mb-4">Unauthorized</h1>
		<p class="text-gray-300 mb-4">You do not have permission to access this resource.</p>
		<p class="text-gray-400 text-sm">Only authorized administrators can access hsadmin.</p>
	</div>
</body>
</html>
	`))
}

// GetUser retrieves the authenticated user from the request context
func GetUser(r *http.Request) *User {
	user, ok := r.Context().Value(UserContextKey).(*User)
	if !ok {
		return nil
	}
	return user
}

// GetOIDCAuth returns the OIDC authenticator (may be nil if OIDC is not enabled)
func (m *Middleware) GetOIDCAuth() *OIDCAuthenticator {
	return m.oidcAuth
}

// AddUserToTemplateData adds the authenticated user to template data
// Returns a new map with User added (or existing map if no user)
func AddUserToTemplateData(r *http.Request, data map[string]interface{}) map[string]interface{} {
	user := GetUser(r)
	if user != nil {
		data["User"] = user
	}
	return data
}
