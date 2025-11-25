package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/anupcshan/hsadmin/internal/config"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OIDCAuthenticator handles OIDC authentication
type OIDCAuthenticator struct {
	config       *config.Config
	provider     *oidc.Provider
	oauth2Config oauth2.Config
	verifier     *oidc.IDTokenVerifier
	sessions     *SessionStore
}

// SessionData holds session information
type SessionData struct {
	Email     string
	Name      string
	ExpiresAt time.Time
}

// NewOIDCAuthenticator creates a new OIDC authenticator
func NewOIDCAuthenticator(cfg *config.Config) (*OIDCAuthenticator, error) {
	if cfg.Listeners.HTTP == nil || cfg.Listeners.HTTP.OIDC == nil {
		return nil, fmt.Errorf("OIDC is not configured")
	}

	oidcCfg := cfg.Listeners.HTTP.OIDC
	ctx := context.Background()

	// Discover OIDC provider configuration
	provider, err := oidc.NewProvider(ctx, oidcCfg.ProviderURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
	}

	// Configure OAuth2
	oauth2Config := oauth2.Config{
		ClientID:     oidcCfg.ClientID,
		ClientSecret: oidcCfg.ClientSecret,
		RedirectURL:  oidcCfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       oidcCfg.Scopes,
	}

	// Create ID token verifier
	verifier := provider.Verifier(&oidc.Config{
		ClientID: oidcCfg.ClientID,
	})

	return &OIDCAuthenticator{
		config:       cfg,
		provider:     provider,
		oauth2Config: oauth2Config,
		verifier:     verifier,
		sessions:     NewSessionStore(oidcCfg.SessionSecret),
	}, nil
}

// AuthenticateFromSession checks if the request has a valid OIDC session
func (o *OIDCAuthenticator) AuthenticateFromSession(r *http.Request) (*User, error) {
	// Get session cookie
	cookie, err := r.Cookie("hsadmin_session")
	if err != nil {
		return nil, fmt.Errorf("no session cookie: %w", err)
	}

	// Verify and decode session
	session, err := o.sessions.Decode(cookie.Value)
	if err != nil {
		return nil, fmt.Errorf("invalid session: %w", err)
	}

	// Check if session is expired
	if time.Now().After(session.ExpiresAt) {
		return nil, fmt.Errorf("session expired")
	}

	// Check if user email is authorized
	if !o.isAuthorizedEmail(session.Email) {
		return nil, fmt.Errorf("user %s is not authorized", session.Email)
	}

	// Create authenticated user
	user := &User{
		Email:  session.Email,
		Name:   session.Name,
		Method: "oidc",
	}

	return user, nil
}

// UpdateRedirectURL updates the OAuth2 redirect URL after initialization
// This is useful for testing where the server URL is not known until after startup
func (o *OIDCAuthenticator) UpdateRedirectURL(redirectURL string) {
	o.oauth2Config.RedirectURL = redirectURL
}

// GetAuthURL returns the OAuth2 authorization URL with PKCE
// Returns the auth URL and the PKCE verifier (to be stored for callback)
func (o *OIDCAuthenticator) GetAuthURLWithVerifier(state string) (string, string) {
	// Generate PKCE verifier
	// Per RFC 7636: verifier = high-entropy cryptographic random string
	verifier := oauth2.GenerateVerifier()

	// Use PKCE (Proof Key for Code Exchange) for enhanced security
	// S256ChallengeOption automatically computes SHA-256 challenge from verifier
	authURL := o.oauth2Config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))

	return authURL, verifier
}

// HandleCallback processes the OIDC callback and creates a session
func (o *OIDCAuthenticator) HandleCallback(ctx context.Context, code string, verifier string) (*SessionData, error) {
	// Exchange code for token (with PKCE verifier if provided)
	var oauth2Token *oauth2.Token
	var err error
	if verifier != "" {
		// Use PKCE verifier in token exchange
		oauth2Token, err = o.oauth2Config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	} else {
		// No PKCE
		oauth2Token, err = o.oauth2Config.Exchange(ctx, code)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to exchange token: %w", err)
	}

	// Extract ID token
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("no id_token in token response")
	}

	// Verify ID token
	idToken, err := o.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("failed to verify ID token: %w", err)
	}

	// Extract claims
	var claims struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to parse claims: %w", err)
	}

	// Check if user is authorized
	if !o.isAuthorizedEmail(claims.Email) {
		return nil, fmt.Errorf("user %s is not authorized", claims.Email)
	}

	// Create session
	session := &SessionData{
		Email:     claims.Email,
		Name:      claims.Name,
		ExpiresAt: time.Now().Add(o.config.Listeners.HTTP.OIDC.SessionDuration),
	}

	log.Printf("OIDC auth successful: email=%s, name=%s", claims.Email, claims.Name)
	return session, nil
}

// CreateSessionCookie creates a session cookie
func (o *OIDCAuthenticator) CreateSessionCookie(session *SessionData) (*http.Cookie, error) {
	encoded, err := o.sessions.Encode(session)
	if err != nil {
		return nil, err
	}

	return &http.Cookie{
		Name:     "hsadmin_session",
		Value:    encoded,
		Path:     "/",
		HttpOnly: true,
		Secure:   true, // Requires HTTPS
		SameSite: http.SameSiteLaxMode,
		Expires:  session.ExpiresAt,
	}, nil
}

// ClearSessionCookie returns a cookie that clears the session
func (o *OIDCAuthenticator) ClearSessionCookie() *http.Cookie {
	return &http.Cookie{
		Name:     "hsadmin_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1, // Delete immediately
	}
}

// isAuthorizedEmail checks if an email is in the admin list
func (o *OIDCAuthenticator) isAuthorizedEmail(email string) bool {
	if o.config.Listeners.HTTP == nil || o.config.Listeners.HTTP.OIDC == nil {
		return false
	}

	for _, adminEmail := range o.config.Listeners.HTTP.OIDC.AdminEmails {
		if email == adminEmail {
			return true
		}
	}
	return false
}

// GenerateState generates a random state parameter for CSRF protection
func GenerateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// SessionStore handles encrypted session storage
type SessionStore struct {
	secret []byte
}

// NewSessionStore creates a new session store
func NewSessionStore(secret string) *SessionStore {
	// Derive a 32-byte key from the secret using SHA-256
	hash := sha256.Sum256([]byte(secret))
	return &SessionStore{
		secret: hash[:],
	}
}

// Encode encrypts and encodes session data using AES-GCM
func (s *SessionStore) Encode(session *SessionData) (string, error) {
	// Marshal session to JSON
	plaintext, err := json.Marshal(session)
	if err != nil {
		return "", err
	}

	// Create AES cipher
	block, err := aes.NewCipher(s.secret)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt and authenticate
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)

	// Encode to base64 for cookie storage
	encoded := base64.URLEncoding.EncodeToString(ciphertext)
	return encoded, nil
}

// Decode decrypts and decodes session data using AES-GCM
func (s *SessionStore) Decode(encoded string) (*SessionData, error) {
	// Decode base64
	ciphertext, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("invalid base64: %w", err)
	}

	// Create AES cipher
	block, err := aes.NewCipher(s.secret)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Check minimum length
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	// Extract nonce and ciphertext
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]

	// Decrypt and verify
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}

	// Unmarshal JSON
	var session SessionData
	if err := json.Unmarshal(plaintext, &session); err != nil {
		return nil, fmt.Errorf("invalid session data: %w", err)
	}

	return &session, nil
}
