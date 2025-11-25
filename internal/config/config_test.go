package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: Config{
				Headscale: struct {
					AgentTags   []string `yaml:"agent_tags"`
					AgentUserID uint64   `yaml:"agent_userid"`
					APIHostPort string   `yaml:"api_hostport"`
					APIKey      string   `yaml:"api_key"`
					ServerURL   string   `yaml:"server_url"`
				}{
					AgentUserID: 1,
					APIHostPort: "localhost:50443",
					APIKey:      "test-api-key",
					ServerURL:   "https://headscale.example.com",
					AgentTags:   []string{"tag:hsadmin"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid config without tags",
			config: Config{
				Headscale: struct {
					AgentTags   []string `yaml:"agent_tags"`
					AgentUserID uint64   `yaml:"agent_userid"`
					APIHostPort string   `yaml:"api_hostport"`
					APIKey      string   `yaml:"api_key"`
					ServerURL   string   `yaml:"server_url"`
				}{
					AgentUserID: 1,
					APIHostPort: "localhost:50443",
					APIKey:      "test-api-key",
					ServerURL:   "http://localhost:8080",
				},
			},
			wantErr: false,
		},
		{
			name: "missing agent_userid",
			config: Config{
				Headscale: struct {
					AgentTags   []string `yaml:"agent_tags"`
					AgentUserID uint64   `yaml:"agent_userid"`
					APIHostPort string   `yaml:"api_hostport"`
					APIKey      string   `yaml:"api_key"`
					ServerURL   string   `yaml:"server_url"`
				}{
					AgentUserID: 0, // Invalid!
					APIHostPort: "localhost:50443",
					APIKey:      "test-api-key",
					ServerURL:   "https://headscale.example.com",
				},
			},
			wantErr: true,
			errMsg:  "agent_userid is required",
		},
		{
			name: "missing api_hostport",
			config: Config{
				Headscale: struct {
					AgentTags   []string `yaml:"agent_tags"`
					AgentUserID uint64   `yaml:"agent_userid"`
					APIHostPort string   `yaml:"api_hostport"`
					APIKey      string   `yaml:"api_key"`
					ServerURL   string   `yaml:"server_url"`
				}{
					AgentUserID: 1,
					APIHostPort: "", // Invalid!
					APIKey:      "test-api-key",
					ServerURL:   "https://headscale.example.com",
				},
			},
			wantErr: true,
			errMsg:  "api_hostport is required",
		},
		{
			name: "invalid api_hostport format",
			config: Config{
				Headscale: struct {
					AgentTags   []string `yaml:"agent_tags"`
					AgentUserID uint64   `yaml:"agent_userid"`
					APIHostPort string   `yaml:"api_hostport"`
					APIKey      string   `yaml:"api_key"`
					ServerURL   string   `yaml:"server_url"`
				}{
					AgentUserID: 1,
					APIHostPort: "localhost", // Invalid - missing port!
					APIKey:      "test-api-key",
					ServerURL:   "https://headscale.example.com",
				},
			},
			wantErr: true,
			errMsg:  "must be in 'host:port' format",
		},
		{
			name: "missing api_key",
			config: Config{
				Headscale: struct {
					AgentTags   []string `yaml:"agent_tags"`
					AgentUserID uint64   `yaml:"agent_userid"`
					APIHostPort string   `yaml:"api_hostport"`
					APIKey      string   `yaml:"api_key"`
					ServerURL   string   `yaml:"server_url"`
				}{
					AgentUserID: 1,
					APIHostPort: "localhost:50443",
					APIKey:      "", // Invalid!
					ServerURL:   "https://headscale.example.com",
				},
			},
			wantErr: true,
			errMsg:  "api_key is required",
		},
		{
			name: "missing server_url",
			config: Config{
				Headscale: struct {
					AgentTags   []string `yaml:"agent_tags"`
					AgentUserID uint64   `yaml:"agent_userid"`
					APIHostPort string   `yaml:"api_hostport"`
					APIKey      string   `yaml:"api_key"`
					ServerURL   string   `yaml:"server_url"`
				}{
					AgentUserID: 1,
					APIHostPort: "localhost:50443",
					APIKey:      "test-api-key",
					ServerURL:   "", // Invalid!
				},
			},
			wantErr: true,
			errMsg:  "server_url is required",
		},
		{
			name: "invalid server_url - no scheme",
			config: Config{
				Headscale: struct {
					AgentTags   []string `yaml:"agent_tags"`
					AgentUserID uint64   `yaml:"agent_userid"`
					APIHostPort string   `yaml:"api_hostport"`
					APIKey      string   `yaml:"api_key"`
					ServerURL   string   `yaml:"server_url"`
				}{
					AgentUserID: 1,
					APIHostPort: "localhost:50443",
					APIKey:      "test-api-key",
					ServerURL:   "headscale.example.com", // Invalid - no http:// or https://
				},
			},
			wantErr: true,
			errMsg:  "must include a scheme",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.errMsg)
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %q, want error containing %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestLoad_ValidConfig(t *testing.T) {
	// Create a temporary valid config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	validConfig := `headscale:
  agent_userid: 1
  api_hostport: localhost:50443
  api_key: test-api-key-123
  server_url: https://headscale.example.com
  agent_tags:
    - tag:hsadmin
`

	err := os.WriteFile(configPath, []byte(validConfig), 0644)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed with valid config: %v", err)
	}

	// Verify fields were loaded correctly
	if cfg.Headscale.AgentUserID != 1 {
		t.Errorf("AgentUserID = %d, want 1", cfg.Headscale.AgentUserID)
	}
	if cfg.Headscale.APIHostPort != "localhost:50443" {
		t.Errorf("APIHostPort = %q, want %q", cfg.Headscale.APIHostPort, "localhost:50443")
	}
	if cfg.Headscale.APIKey != "test-api-key-123" {
		t.Errorf("APIKey = %q, want %q", cfg.Headscale.APIKey, "test-api-key-123")
	}
	if cfg.Headscale.ServerURL != "https://headscale.example.com" {
		t.Errorf("ServerURL = %q, want %q", cfg.Headscale.ServerURL, "https://headscale.example.com")
	}
}

func TestLoad_InvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	invalidConfig := `headscale:
  agent_userid: 0
  api_hostport: localhost:50443
  api_key: test-api-key
  server_url: https://headscale.example.com
`

	err := os.WriteFile(configPath, []byte(invalidConfig), 0644)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	_, err = Load(configPath)
	if err == nil {
		t.Fatal("Load() expected error with invalid config, got nil")
	}

	if !strings.Contains(err.Error(), "invalid configuration") {
		t.Errorf("Load() error = %q, want error containing 'invalid configuration'", err.Error())
	}
	if !strings.Contains(err.Error(), "agent_userid") {
		t.Errorf("Load() error = %q, want error mentioning 'agent_userid'", err.Error())
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("Load() expected error with missing file, got nil")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	invalidYAML := `headscale:
  agent_userid: [invalid yaml structure
`

	err := os.WriteFile(configPath, []byte(invalidYAML), 0644)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	_, err = Load(configPath)
	if err == nil {
		t.Fatal("Load() expected error with invalid YAML, got nil")
	}
}
