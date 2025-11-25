package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anupcshan/hsadmin/internal/config"
	"github.com/stretchr/testify/require"
)

func TestMiddleware_NoAuthConfigured(t *testing.T) {
	// Setup: No auth configured
	cfg := &config.Config{}
	middleware := NewMiddleware(cfg, nil)

	// Create a test handler that should be called
	called := false
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with auth middleware
	wrappedHandler := middleware.RequireAuth(testHandler)

	// Make request
	req := httptest.NewRequest("GET", "/machines", nil)
	rec := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(rec, req)

	// Assert: Handler should be called since no auth is configured
	require.True(t, called, "Handler should be called when no auth is configured")
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestMiddleware_PublicPaths(t *testing.T) {
	// Setup: Auth enabled
	cfg := &config.Config{
		Listeners: config.ListenersConfig{
			Tailscale: &config.TailscaleListener{
				AdminUserIDs: []uint64{1},
			},
		},
	}
	middleware := NewMiddleware(cfg, nil)

	// Create a test handler
	called := false
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware.RequireAuth(testHandler)

	tests := []struct {
		name string
		path string
	}{
		{"login page", "/auth/login"},
		{"callback", "/auth/callback"},
		{"logout", "/auth/logout"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called = false
			req := httptest.NewRequest("GET", tt.path, nil)
			rec := httptest.NewRecorder()
			wrappedHandler.ServeHTTP(rec, req)

			// Public paths should be allowed through
			require.True(t, called, "Handler should be called for public path: %s", tt.path)
			require.Equal(t, http.StatusOK, rec.Code)
		})
	}
}

func TestAddUserToTemplateData(t *testing.T) {
	tests := []struct {
		name     string
		user     *User
		data     map[string]interface{}
		expected map[string]interface{}
	}{
		{
			name: "with user",
			user: &User{
				ID:     1,
				Name:   "Test User",
				Method: "whois",
			},
			data: map[string]interface{}{
				"Active": "machines",
			},
			expected: map[string]interface{}{
				"Active": "machines",
				"User": &User{
					ID:     1,
					Name:   "Test User",
					Method: "whois",
				},
			},
		},
		{
			name: "without user",
			user: nil,
			data: map[string]interface{}{
				"Active": "machines",
			},
			expected: map[string]interface{}{
				"Active": "machines",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a request with user in context (or not)
			req := httptest.NewRequest("GET", "/machines", nil)
			if tt.user != nil {
				ctx := context.WithValue(req.Context(), UserContextKey, tt.user)
				req = req.WithContext(ctx)
			}

			// Call the function
			result := AddUserToTemplateData(req, tt.data)

			// Assert
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestGetUser(t *testing.T) {
	tests := []struct {
		name     string
		user     *User
		expected *User
	}{
		{
			name: "with user in context",
			user: &User{
				ID:     1,
				Name:   "Test User",
				Method: "whois",
			},
			expected: &User{
				ID:     1,
				Name:   "Test User",
				Method: "whois",
			},
		},
		{
			name:     "without user in context",
			user:     nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create request with or without user
			req := httptest.NewRequest("GET", "/machines", nil)
			if tt.user != nil {
				ctx := context.WithValue(req.Context(), UserContextKey, tt.user)
				req = req.WithContext(ctx)
			}

			// Get user
			result := GetUser(req)

			// Assert
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestSessionStore_EncryptionDecryption(t *testing.T) {
	secret := "test-secret-key-that-is-long-enough-for-security"
	store := NewSessionStore(secret)

	tests := []struct {
		name    string
		session *SessionData
	}{
		{
			name: "basic session",
			session: &SessionData{
				Email:     "test@example.com",
				Name:      "Test User",
				ExpiresAt: time.Now().Add(24 * time.Hour),
			},
		},
		{
			name: "session with special characters",
			session: &SessionData{
				Email:     "user+tag@example.com",
				Name:      "Test User 123 !@#$%",
				ExpiresAt: time.Now().Add(1 * time.Hour),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode
			encoded, err := store.Encode(tt.session)
			require.NoError(t, err)
			require.NotEmpty(t, encoded)

			// Encoded value should not contain plaintext email
			require.NotContains(t, encoded, tt.session.Email)

			// Decode
			decoded, err := store.Decode(encoded)
			require.NoError(t, err)
			require.Equal(t, tt.session.Email, decoded.Email)
			require.Equal(t, tt.session.Name, decoded.Name)
			require.WithinDuration(t, tt.session.ExpiresAt, decoded.ExpiresAt, time.Second)
		})
	}
}

func TestSessionStore_InvalidDecryption(t *testing.T) {
	secret := "test-secret-key-that-is-long-enough-for-security"
	store := NewSessionStore(secret)

	tests := []struct {
		name    string
		encoded string
	}{
		{
			name:    "invalid base64",
			encoded: "not-valid-base64!!!",
		},
		{
			name:    "too short",
			encoded: "YWJj", // "abc" in base64
		},
		{
			name:    "tampered data",
			encoded: "dGhpcyBpcyBub3QgYSB2YWxpZCBlbmNyeXB0ZWQgc2Vzc2lvbg==",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.Decode(tt.encoded)
			require.Error(t, err)
		})
	}
}

func TestSessionStore_DifferentSecrets(t *testing.T) {
	secret1 := "secret-key-1-that-is-long-enough"
	secret2 := "secret-key-2-that-is-different"

	store1 := NewSessionStore(secret1)
	store2 := NewSessionStore(secret2)

	session := &SessionData{
		Email:     "test@example.com",
		Name:      "Test User",
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}

	// Encode with store1
	encoded, err := store1.Encode(session)
	require.NoError(t, err)

	// Decoding with store2 (different secret) should fail
	_, err = store2.Decode(encoded)
	require.Error(t, err)

	// Decoding with store1 (same secret) should succeed
	decoded, err := store1.Decode(encoded)
	require.NoError(t, err)
	require.Equal(t, session.Email, decoded.Email)
}
