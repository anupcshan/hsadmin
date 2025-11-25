package auth

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
)

// AuthHandlers handles OIDC authentication HTTP endpoints
type AuthHandlers struct {
	authenticator *OIDCAuthenticator
	templates     *template.Template
}

// NewAuthHandlers creates new auth handlers
func NewAuthHandlers(auth *OIDCAuthenticator, tmpl *template.Template) *AuthHandlers {
	return &AuthHandlers{
		authenticator: auth,
		templates:     tmpl,
	}
}

// ShowLoginPage initiates OIDC login by redirecting to the provider
func (h *AuthHandlers) ShowLoginPage(w http.ResponseWriter, r *http.Request) {
	// Generate CSRF state token
	state, err := GenerateState()
	if err != nil {
		http.Error(w, "Failed to generate state token", http.StatusInternalServerError)
		return
	}

	// Get authorization URL with PKCE verifier
	authURL, verifier := h.authenticator.GetAuthURLWithVerifier(state)
	http.SetCookie(w, &http.Cookie{
		Name:     "oidc_state",
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   300, // 5 minutes
	})

	// Store PKCE verifier in cookie (for token exchange)
	http.SetCookie(w, &http.Cookie{
		Name:     "oidc_verifier",
		Value:    verifier,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   300, // 5 minutes
	})

	// Redirect to OIDC provider
	http.Redirect(w, r, authURL, http.StatusSeeOther)
}

// HandleCallback handles the OIDC callback from the provider
func (h *AuthHandlers) HandleCallback(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()

	// Get state from query params
	state := r.URL.Query().Get("state")
	if state == "" {
		log.Printf("OIDC callback failed: missing state parameter")
		http.Error(w, "Missing state parameter", http.StatusBadRequest)
		return
	}

	// Validate CSRF state
	stateCookie, err := r.Cookie("oidc_state")
	if err != nil {
		log.Printf("OIDC callback failed: state cookie not found - possible CSRF attack")
		http.Error(w, "Invalid state parameter - possible CSRF attack", http.StatusBadRequest)
		return
	}

	if stateCookie.Value != state {
		log.Printf("OIDC callback failed: state mismatch - possible CSRF attack")
		http.Error(w, "Invalid state parameter - possible CSRF attack", http.StatusBadRequest)
		return
	}

	// Get PKCE verifier from cookie
	verifierCookie, err := r.Cookie("oidc_verifier")
	verifier := ""
	if err == nil {
		verifier = verifierCookie.Value
	} else {
		log.Printf("OIDC callback: warning - no PKCE verifier cookie found")
	}

	// Clear state and verifier cookies
	http.SetCookie(w, &http.Cookie{
		Name:     "oidc_state",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "oidc_verifier",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	// Get authorization code
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Missing authorization code", http.StatusBadRequest)
		return
	}

	// Check for error from provider
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		errDesc := r.URL.Query().Get("error_description")
		log.Printf("OIDC error: %s - %s", errParam, errDesc)
		http.Error(w, fmt.Sprintf("Authentication failed: %s", errDesc), http.StatusUnauthorized)
		return
	}

	// Exchange code for token and create session (with PKCE verifier if available)
	session, err := h.authenticator.HandleCallback(ctx, code, verifier)
	if err != nil {
		log.Printf("OIDC callback failed: %v", err)
		http.Error(w, "Authentication failed: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Create session cookie
	sessionCookie, err := h.authenticator.CreateSessionCookie(session)
	if err != nil {
		log.Printf("Failed to create session cookie: %v", err)
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	// Set session cookie
	http.SetCookie(w, sessionCookie)

	// Redirect to main page
	http.Redirect(w, r, "/machines", http.StatusSeeOther)
}

// HandleLogout handles user logout
func (h *AuthHandlers) HandleLogout(w http.ResponseWriter, r *http.Request) {
	// Clear session cookie
	http.SetCookie(w, h.authenticator.ClearSessionCookie())

	// Redirect to login page
	http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
}
