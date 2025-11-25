package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Headscale struct {
		AgentTags   []string `yaml:"agent_tags"`
		AgentUserID uint64   `yaml:"agent_userid"`
		APIHostPort string   `yaml:"api_hostport"`
		APIKey      string   `yaml:"api_key"`
		ServerURL   string   `yaml:"server_url"`
	} `yaml:"headscale"`

	Listeners ListenersConfig `yaml:"listeners"`
}

// ListenersConfig holds all listener configurations
type ListenersConfig struct {
	Tailscale *TailscaleListener `yaml:"tailscale,omitempty"`
	HTTP      *HTTPListener      `yaml:"http,omitempty"`
}

// TailscaleListener configures the tsnet listener with WhoIs authentication
type TailscaleListener struct {
	Port          int      `yaml:"port,omitempty"`           // Default: 80
	AdminUserIDs  []uint64 `yaml:"admin_user_ids,omitempty"` // Required if listener configured
	AdminUserTags []string `yaml:"admin_user_tags,omitempty"`
}

// HTTPListener configures an HTTP listener with OIDC authentication
type HTTPListener struct {
	ListenAddr string      `yaml:"listen_addr,omitempty"` // Default: ":8080"
	OIDC       *OIDCConfig `yaml:"oidc,omitempty"`        // Required if HTTP listener configured
}

// OIDCConfig holds OIDC/OAuth2 authentication settings
type OIDCConfig struct {
	ProviderURL     string        `yaml:"provider_url"`               // Required
	ClientID        string        `yaml:"client_id"`                  // Required
	ClientSecret    string        `yaml:"client_secret"`              // Required
	RedirectURL     string        `yaml:"redirect_url"`               // Required
	AdminEmails     []string      `yaml:"admin_emails"`               // Required
	Scopes          []string      `yaml:"scopes,omitempty"`           // Default: ["openid", "profile", "email"]
	SessionSecret   string        `yaml:"session_secret"`             // Required
	SessionDuration time.Duration `yaml:"session_duration,omitempty"` // Default: 24h
}

// Load reads and parses the configuration file
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}

	// Set defaults for listener config
	cfg.setListenerDefaults()

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

// setListenerDefaults sets reasonable defaults for listener configuration
func (c *Config) setListenerDefaults() {
	// Tailscale listener defaults
	if c.Listeners.Tailscale != nil {
		if c.Listeners.Tailscale.Port == 0 {
			c.Listeners.Tailscale.Port = 80
		}
	}

	// HTTP listener defaults
	if c.Listeners.HTTP != nil {
		if c.Listeners.HTTP.ListenAddr == "" {
			c.Listeners.HTTP.ListenAddr = ":8080"
		}

		// OIDC defaults
		if c.Listeners.HTTP.OIDC != nil {
			if len(c.Listeners.HTTP.OIDC.Scopes) == 0 {
				c.Listeners.HTTP.OIDC.Scopes = []string{"openid", "profile", "email"}
			}
			if c.Listeners.HTTP.OIDC.SessionDuration == 0 {
				c.Listeners.HTTP.OIDC.SessionDuration = 24 * time.Hour
			}
		}
	}
}

// Validate checks that all required configuration fields are present and valid
func (c *Config) Validate() error {
	// Check agent_userid
	if c.Headscale.AgentUserID == 0 {
		return fmt.Errorf("headscale.agent_userid is required and must be greater than 0")
	}

	// Check api_hostport
	if c.Headscale.APIHostPort == "" {
		return fmt.Errorf("headscale.api_hostport is required (e.g., 'localhost:50443')")
	}
	// Basic validation: should contain a colon for host:port format
	if !strings.Contains(c.Headscale.APIHostPort, ":") {
		return fmt.Errorf("headscale.api_hostport must be in 'host:port' format (got: %q)", c.Headscale.APIHostPort)
	}

	// Check api_key
	if c.Headscale.APIKey == "" {
		return fmt.Errorf("headscale.api_key is required")
	}

	// Check server_url
	if c.Headscale.ServerURL == "" {
		return fmt.Errorf("headscale.server_url is required (e.g., 'https://headscale.example.com')")
	}
	// Validate URL format
	parsedURL, err := url.Parse(c.Headscale.ServerURL)
	if err != nil {
		return fmt.Errorf("headscale.server_url is not a valid URL: %w", err)
	}
	// URL should have a scheme (http/https)
	if parsedURL.Scheme == "" {
		return fmt.Errorf("headscale.server_url must include a scheme (http:// or https://)")
	}
	// URL should have a host
	if parsedURL.Host == "" {
		return fmt.Errorf("headscale.server_url must include a host")
	}

	// agent_tags is optional - no validation needed

	// Validate listener configuration
	if err := c.validateListeners(); err != nil {
		return err
	}

	return nil
}

// validateListeners validates the listener configuration
func (c *Config) validateListeners() error {
	// At least one listener should be configured
	if c.Listeners.Tailscale == nil && c.Listeners.HTTP == nil {
		return nil // No listeners configured - that's okay (no auth)
	}

	// Validate Tailscale listener
	if c.Listeners.Tailscale != nil {
		if len(c.Listeners.Tailscale.AdminUserIDs) == 0 && len(c.Listeners.Tailscale.AdminUserTags) == 0 {
			return fmt.Errorf("listeners.tailscale is configured but neither admin_user_ids nor admin_user_tags is set")
		}
	}

	// Validate HTTP listener
	if c.Listeners.HTTP != nil {
		// HTTP listener requires OIDC config
		if c.Listeners.HTTP.OIDC == nil {
			return fmt.Errorf("listeners.http is configured but oidc section is missing")
		}

		oidc := c.Listeners.HTTP.OIDC

		// Validate required OIDC fields
		if oidc.ProviderURL == "" {
			return fmt.Errorf("listeners.http.oidc.provider_url is required")
		}
		if oidc.ClientID == "" {
			return fmt.Errorf("listeners.http.oidc.client_id is required")
		}
		if oidc.ClientSecret == "" {
			return fmt.Errorf("listeners.http.oidc.client_secret is required")
		}
		if oidc.RedirectURL == "" {
			return fmt.Errorf("listeners.http.oidc.redirect_url is required")
		}
		if len(oidc.AdminEmails) == 0 {
			return fmt.Errorf("listeners.http.oidc.admin_emails is required")
		}
		if oidc.SessionSecret == "" {
			return fmt.Errorf("listeners.http.oidc.session_secret is required (generate with: openssl rand -base64 32)")
		}
		if len(oidc.SessionSecret) < 32 {
			return fmt.Errorf("listeners.http.oidc.session_secret must be at least 32 characters for security")
		}

		// Validate URL formats
		_, err := url.Parse(oidc.ProviderURL)
		if err != nil {
			return fmt.Errorf("listeners.http.oidc.provider_url is not a valid URL: %w", err)
		}

		_, err = url.Parse(oidc.RedirectURL)
		if err != nil {
			return fmt.Errorf("listeners.http.oidc.redirect_url is not a valid URL: %w", err)
		}
	}

	return nil
}
