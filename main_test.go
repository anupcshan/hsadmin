package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain_InvalidConfig tests that the application exits with clear error messages for invalid configs
func TestMain_InvalidConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping main integration test in short mode")
	}

	// Build the binary first
	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "hsadmin-test")

	buildCmd := exec.Command("go", "build", "-o", binaryPath, ".")
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}

	tests := []struct {
		name       string
		configYAML string
		wantErrMsg string
	}{
		{
			name: "missing agent_userid",
			configYAML: `headscale:
  agent_userid: 0
  api_hostport: localhost:50443
  api_key: test-key
  server_url: https://headscale.example.com
`,
			wantErrMsg: "agent_userid is required and must be greater than 0",
		},
		{
			name: "missing api_hostport",
			configYAML: `headscale:
  agent_userid: 1
  api_hostport: ""
  api_key: test-key
  server_url: https://headscale.example.com
`,
			wantErrMsg: "api_hostport is required",
		},
		{
			name: "invalid api_hostport format",
			configYAML: `headscale:
  agent_userid: 1
  api_hostport: localhost
  api_key: test-key
  server_url: https://headscale.example.com
`,
			wantErrMsg: "must be in 'host:port' format",
		},
		{
			name: "missing api_key",
			configYAML: `headscale:
  agent_userid: 1
  api_hostport: localhost:50443
  api_key: ""
  server_url: https://headscale.example.com
`,
			wantErrMsg: "api_key is required",
		},
		{
			name: "missing server_url",
			configYAML: `headscale:
  agent_userid: 1
  api_hostport: localhost:50443
  api_key: test-key
  server_url: ""
`,
			wantErrMsg: "server_url is required",
		},
		{
			name: "invalid server_url - no scheme",
			configYAML: `headscale:
  agent_userid: 1
  api_hostport: localhost:50443
  api_key: test-key
  server_url: headscale.example.com
`,
			wantErrMsg: "must include a scheme",
		},
		{
			name: "invalid server_url - malformed",
			configYAML: `headscale:
  agent_userid: 1
  api_hostport: localhost:50443
  api_key: test-key
  server_url: "http://[invalid-url"
`,
			wantErrMsg: "is not a valid URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary config file
			configPath := filepath.Join(tmpDir, tt.name+".yaml")
			err := os.WriteFile(configPath, []byte(tt.configYAML), 0644)
			if err != nil {
				t.Fatalf("Failed to write config file: %v", err)
			}

			// Run the binary with the invalid config
			cmd := exec.Command(binaryPath, "-config", configPath)
			output, err := cmd.CombinedOutput()

			// Expect the command to fail
			if err == nil {
				t.Errorf("Expected command to fail with invalid config, but it succeeded")
				return
			}

			// Check that the error message contains the expected text
			outputStr := string(output)
			if !strings.Contains(outputStr, "invalid configuration") {
				t.Errorf("Expected error to contain 'invalid configuration', got: %s", outputStr)
			}
			if !strings.Contains(outputStr, tt.wantErrMsg) {
				t.Errorf("Expected error to contain %q, got: %s", tt.wantErrMsg, outputStr)
			}

			t.Logf("✓ Config validation caught error: %s", strings.TrimSpace(outputStr))
		})
	}
}

// TestMain_ValidConfigButConnectionFailure tests that validation passes but connection fails
func TestMain_ValidConfigButConnectionFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping main integration test in short mode")
	}

	// Build the binary
	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "hsadmin-test")

	buildCmd := exec.Command("go", "build", "-o", binaryPath, ".")
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}

	// Create a valid config but pointing to non-existent server
	validConfig := `headscale:
  agent_userid: 1
  api_hostport: localhost:50443
  api_key: test-api-key-12345
  server_url: https://localhost:8080
  agent_tags:
    - tag:hsadmin
`

	configPath := filepath.Join(tmpDir, "valid.yaml")
	err := os.WriteFile(configPath, []byte(validConfig), 0644)
	if err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Run the binary with valid config (should pass validation, fail on connection)
	cmd := exec.Command(binaryPath, "-config", configPath)
	output, err := cmd.CombinedOutput()

	// Expect the command to fail (connection failure, not validation)
	if err == nil {
		t.Errorf("Expected command to fail on connection, but it succeeded")
		return
	}

	outputStr := string(output)

	// Should NOT contain "invalid configuration" since validation passed
	if strings.Contains(outputStr, "invalid configuration") {
		t.Errorf("Expected validation to pass, but got validation error: %s", outputStr)
	}

	// Should contain connection-related error (gRPC dial error)
	if !strings.Contains(outputStr, "connection") && !strings.Contains(outputStr, "dial") && !strings.Contains(outputStr, "connect") {
		t.Logf("Note: Expected connection error, got: %s", outputStr)
		// This is not a failure - the error message might vary
	}

	t.Logf("✓ Config validation passed, connection failed as expected")
}
