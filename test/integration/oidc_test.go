package integration

import (
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/anupcshan/hsadmin/internal/auth"
	"github.com/anupcshan/hsadmin/internal/config"
	"github.com/oauth2-proxy/mockoidc"
	"github.com/stretchr/testify/require"
)

// TestOIDC_EndToEnd tests the complete OIDC authentication flow:
// 1. User accesses protected resource -> redirected to /auth/login
// 2. /auth/login redirects to OIDC provider (with PKCE challenge)
// 3. OIDC provider redirects back to /auth/callback (with code)
// 4. /auth/callback exchanges code for token (with PKCE verifier)
// 5. Session cookie is set
// 6. User can access protected resources
// 7. User logs out -> session cleared
func TestOIDC_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Setup mock OIDC provider
	mockOIDC, err := mockoidc.Run()
	require.NoError(t, err)
	defer mockOIDC.Shutdown()

	// Configure mock OIDC provider with a test user
	testUser := mockoidc.MockUser{
		Email:             "admin@example.com",
		EmailVerified:     true,
		Subject:           "test-user-123",
		PreferredUsername: "testadmin",
	}
	mockOIDC.QueueUser(&testUser)

	// Create config for hsadmin with OIDC
	// Note: We'll need to update the redirect URL after starting the server
	cfg := &config.Config{
		Listeners: config.ListenersConfig{
			HTTP: &config.HTTPListener{
				ListenAddr: "127.0.0.1:0", // Random available port
				OIDC: &config.OIDCConfig{
					ProviderURL:     mockOIDC.Issuer(),
					ClientID:        mockOIDC.ClientID,
					ClientSecret:    mockOIDC.ClientSecret,
					RedirectURL:     "http://placeholder/auth/callback", // Temporary, will be updated
					AdminEmails:     []string{"admin@example.com"},
					SessionSecret:   "test-session-secret-32-bytes-long",
					SessionDuration: 24 * time.Hour,
					Scopes:          []string{"openid", "profile", "email"},
				},
			},
		},
	}

	// Start hsadmin HTTP server with OIDC
	server, authMiddleware := startHTTPServerWithConfig(t, cfg)
	defer server.Close()

	// Update redirect URL now that we know the server address
	// We need to update the oauth2Config in the OIDC authenticator
	cfg.Listeners.HTTP.OIDC.RedirectURL = server.URL + "/auth/callback"
	oidcAuth := authMiddleware.GetOIDCAuth()
	oidcAuth.UpdateRedirectURL(server.URL + "/auth/callback")

	// Create HTTP client with cookie jar (to maintain session)
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Don't follow redirects automatically - we want to inspect them
			return http.ErrUseLastResponse
		},
	}

	t.Run("login flow", func(t *testing.T) {
		// Step 1: Try to access protected resource (/machines)
		// Should redirect to /auth/login
		resp, err := client.Get(server.URL + "/machines")
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusSeeOther, resp.StatusCode, "Should redirect to login")
		loginURL := resp.Header.Get("Location")
		require.Contains(t, loginURL, "/auth/login", "Should redirect to /auth/login")

		// Step 2: Follow redirect to /auth/login
		// Should immediately redirect to OIDC provider with PKCE challenge
		resp, err = client.Get(server.URL + loginURL)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusSeeOther, resp.StatusCode, "Should redirect to OIDC provider")
		oidcAuthURL := resp.Header.Get("Location")
		require.Contains(t, oidcAuthURL, mockOIDC.Issuer(), "Should redirect to mock OIDC provider")
		require.Contains(t, oidcAuthURL, "code_challenge=", "Should include PKCE challenge")
		require.Contains(t, oidcAuthURL, "code_challenge_method=S256", "Should use S256 PKCE method")
		require.Contains(t, oidcAuthURL, "state=", "Should include state parameter")

		// Verify PKCE cookies were set
		// Note: httptest.Server sets Secure=true cookies, but serves over HTTP
		// Cookie jar won't store these, so we need to manually extract from headers
		var stateCookie, verifierCookie *http.Cookie
		for _, cookieStr := range resp.Header["Set-Cookie"] {
			// Parse each Set-Cookie header
			header := http.Header{}
			header.Add("Set-Cookie", cookieStr)
			cookies := (&http.Response{Header: header}).Cookies()
			for _, c := range cookies {
				if c.Name == "oidc_state" {
					stateCookie = c
				}
				if c.Name == "oidc_verifier" {
					verifierCookie = c
				}
			}
		}
		require.NotNil(t, stateCookie, "State cookie should be set")
		require.NotNil(t, verifierCookie, "Verifier cookie should be set")
		require.NotEmpty(t, stateCookie.Value, "State cookie should have a value")
		require.NotEmpty(t, verifierCookie.Value, "Verifier cookie should have a value")

		// Manually add cookies to jar for subsequent requests
		// Remove Secure flag so they work with HTTP test server
		stateCookie.Secure = false
		verifierCookie.Secure = false
		serverURL := mustParseURL(server.URL)
		jar.SetCookies(serverURL, []*http.Cookie{stateCookie, verifierCookie})

		// Step 3: Follow redirect to mock OIDC provider
		// Since we QueueUser'd a user, mockoidc will automatically authenticate
		// and redirect back to our callback with an auth code
		resp, err = client.Get(oidcAuthURL)
		require.NoError(t, err)
		defer resp.Body.Close()

		// mockoidc should redirect back to /auth/callback with code
		// mockoidc uses 302 (Found) instead of 303 (See Other)
		require.Equal(t, http.StatusFound, resp.StatusCode, "mockoidc should redirect back to callback")
		callbackURL := resp.Header.Get("Location")
		require.Contains(t, callbackURL, "/auth/callback", "Should redirect to callback")
		require.Contains(t, callbackURL, "code=", "Should include auth code")

		// Step 4: Follow redirect to /auth/callback
		// hsadmin will exchange the code for a token with mockoidc
		resp, err = client.Get(callbackURL)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Should redirect to /machines after successful login
		require.Equal(t, http.StatusSeeOther, resp.StatusCode, "Should redirect after successful auth")
		location := resp.Header.Get("Location")
		require.Equal(t, "/machines", location, "Should redirect to /machines")

		// Verify session cookie was set
		// Extract from Set-Cookie headers (same as state/verifier cookies)
		var sessionCookie *http.Cookie
		for _, cookieStr := range resp.Header["Set-Cookie"] {
			header := http.Header{}
			header.Add("Set-Cookie", cookieStr)
			cookies := (&http.Response{Header: header}).Cookies()
			for _, c := range cookies {
				if c.Name == "hsadmin_session" {
					sessionCookie = c
				}
			}
		}
		require.NotNil(t, sessionCookie, "Session cookie should be set")
		require.NotEmpty(t, sessionCookie.Value, "Session cookie should have a value")
		require.True(t, sessionCookie.HttpOnly, "Session cookie should be HttpOnly")
		require.True(t, sessionCookie.Secure, "Session cookie should be Secure")

		// Add to jar for subsequent requests (remove Secure flag)
		sessionCookie.Secure = false
		jar.SetCookies(serverURL, []*http.Cookie{sessionCookie})

		// Step 5: Access protected resource with session cookie
		// Now we can follow redirects since we have a valid session
		client.CheckRedirect = nil
		resp, err = client.Get(server.URL + "/machines")
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode, "Should successfully access /machines with valid session")
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		bodyStr := string(body)
		require.Contains(t, bodyStr, "Machines", "Response should contain machines page")
		require.Contains(t, bodyStr, testUser.Email, "Response should show logged-in user email")
	})

	t.Run("logout", func(t *testing.T) {
		// User should still be logged in from previous test
		// Disable automatic redirect following to verify logout redirect
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}

		// Access logout endpoint
		resp, err := client.Get(server.URL + "/auth/logout")
		require.NoError(t, err)
		defer resp.Body.Close()

		// Should redirect to login page
		require.Equal(t, http.StatusSeeOther, resp.StatusCode, "Should redirect after logout")
		require.Equal(t, "/auth/login", resp.Header.Get("Location"), "Should redirect to login")

		// Verify session cookie was cleared
		// Check Set-Cookie header for deletion cookie (MaxAge=-1)
		var deletionCookie *http.Cookie
		for _, cookieStr := range resp.Header["Set-Cookie"] {
			header := http.Header{}
			header.Add("Set-Cookie", cookieStr)
			cookies := (&http.Response{Header: header}).Cookies()
			for _, c := range cookies {
				if c.Name == "hsadmin_session" {
					deletionCookie = c
				}
			}
		}
		// Cookie should be present with MaxAge=-1 to delete it
		require.NotNil(t, deletionCookie, "Session deletion cookie should be sent")
		require.Equal(t, -1, deletionCookie.MaxAge, "Session cookie should be expired")

		// Try to access protected resource again - should be denied
		// (CheckRedirect already disabled above)
		resp, err = client.Get(server.URL + "/machines")
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusSeeOther, resp.StatusCode, "Should redirect to login after logout")
		require.Contains(t, resp.Header.Get("Location"), "/auth/login", "Should redirect to login")
	})
}

// TestOIDC_UnauthorizedEmail tests that users with non-admin emails are rejected
func TestOIDC_UnauthorizedEmail(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Setup mock OIDC provider
	mockOIDC, err := mockoidc.Run()
	require.NoError(t, err)
	defer mockOIDC.Shutdown()

	// Queue a user with unauthorized email
	unauthorizedUser := mockoidc.MockUser{
		Email:             "unauthorized@example.com", // Not in admin list
		EmailVerified:     true,
		Subject:           "unauthorized-user",
		PreferredUsername: "unauthorizeduser",
	}
	mockOIDC.QueueUser(&unauthorizedUser)

	// Create config with only admin@example.com authorized
	cfg := &config.Config{
		Listeners: config.ListenersConfig{
			HTTP: &config.HTTPListener{
				ListenAddr: "127.0.0.1:0",
				OIDC: &config.OIDCConfig{
					ProviderURL:     mockOIDC.Issuer(),
					ClientID:        mockOIDC.ClientID,
					ClientSecret:    mockOIDC.ClientSecret,
					RedirectURL:     "http://placeholder/auth/callback", // Temporary, will be updated
					AdminEmails:     []string{"admin@example.com"},      // Only this email is authorized
					SessionSecret:   "test-session-secret-32-bytes-long",
					SessionDuration: 24 * time.Hour,
					Scopes:          []string{"openid", "profile", "email"},
				},
			},
		},
	}

	server, authMiddleware := startHTTPServerWithConfig(t, cfg)
	defer server.Close()

	cfg.Listeners.HTTP.OIDC.RedirectURL = server.URL + "/auth/callback"
	oidcAuth := authMiddleware.GetOIDCAuth()
	oidcAuth.UpdateRedirectURL(server.URL + "/auth/callback")

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Don't follow redirects automatically
			return http.ErrUseLastResponse
		},
	}

	// Try to authenticate with unauthorized user
	// Start by accessing /auth/login
	resp, err := client.Get(server.URL + "/auth/login")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Extract state and verifier cookies from Set-Cookie headers
	var stateCookie, verifierCookie *http.Cookie
	for _, cookieStr := range resp.Header["Set-Cookie"] {
		header := http.Header{}
		header.Add("Set-Cookie", cookieStr)
		cookies := (&http.Response{Header: header}).Cookies()
		for _, c := range cookies {
			if c.Name == "oidc_state" {
				stateCookie = c
			}
			if c.Name == "oidc_verifier" {
				verifierCookie = c
			}
		}
	}
	require.NotNil(t, stateCookie, "State cookie should be set")
	require.NotNil(t, verifierCookie, "Verifier cookie should be set")

	// Manually add cookies to jar (remove Secure flag for HTTP test server)
	stateCookie.Secure = false
	verifierCookie.Secure = false
	serverURL := mustParseURL(server.URL)
	jar.SetCookies(serverURL, []*http.Cookie{stateCookie, verifierCookie})

	// Follow to OIDC provider
	oidcAuthURL := resp.Header.Get("Location")
	resp, err = client.Get(oidcAuthURL)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Follow back to callback
	callbackURL := resp.Header.Get("Location")
	resp, err = client.Get(callbackURL)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should be rejected with unauthorized status
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode, "Unauthorized user should be rejected")
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "not authorized", "Response should indicate user is not authorized")
}

// startHTTPServerWithConfig starts an hsadmin HTTP server for testing
func startHTTPServerWithConfig(t *testing.T, cfg *config.Config) (*httptest.Server, *auth.Middleware) {
	t.Helper()

	// Load templates
	repoRoot, err := findRepoRoot()
	require.NoError(t, err)
	templatesPath := filepath.Join(repoRoot, "web", "templates", "*.html")
	funcMap := template.FuncMap{
		"sub": func(a, b int) int { return a - b },
		"mul": func(a, b float64) float64 { return a * b },
	}
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseGlob(templatesPath))

	// Create auth middleware (no tsnet client needed for OIDC-only tests)
	authMiddleware := auth.NewMiddleware(cfg, nil)

	// Create a simple test mux with protected routes
	mux := http.NewServeMux()

	// Auth routes (public)
	if authMiddleware.GetOIDCAuth() != nil {
		authHandlers := auth.NewAuthHandlers(authMiddleware.GetOIDCAuth(), tmpl)
		mux.HandleFunc("/auth/login", authHandlers.ShowLoginPage)
		mux.HandleFunc("/auth/callback", authHandlers.HandleCallback)
		mux.HandleFunc("/auth/logout", authHandlers.HandleLogout)
	}

	// Protected routes
	mux.HandleFunc("/machines", func(w http.ResponseWriter, r *http.Request) {
		user := auth.GetUser(r)
		if user == nil {
			http.Error(w, "No user in context", http.StatusInternalServerError)
			return
		}
		// Return simple response with user info
		fmt.Fprintf(w, "<html><body><h1>Machines</h1><p>User: %s</p></body></html>", user.Email)
	})

	// Wrap with auth middleware
	handler := authMiddleware.RequireAuth(mux)

	// Create test server
	server := httptest.NewServer(handler)
	return server, authMiddleware
}

// mustParseURL parses a URL or panics
func mustParseURL(rawURL string) *url.URL {
	u, err := url.Parse(rawURL)
	if err != nil {
		panic(err)
	}
	return u
}
