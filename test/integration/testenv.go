package integration

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	headscale "github.com/juanfont/headscale/gen/go/headscale/v1"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"
)

//go:embed config/headscale.yaml
var headscaleConfig string

// TestEnv manages the Headscale container for integration tests
type TestEnv struct {
	pool             *dockertest.Pool
	container        *dockertest.Resource
	tailscaleClients []*dockertest.Resource // Additional tailscale client containers
	HeadscaleURL     string
	GRPCAddr         string
	APIKey           string
	PreAuthKey       string
	UserID           uint64
	grpcConn         *grpc.ClientConn
	headscaleClient  headscale.HeadscaleServiceClient
	configFile       string // Temp config file path
}

// findRepoRoot walks up the directory tree to find the repo root (containing go.mod)
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find repo root (go.mod)")
		}
		dir = parent
	}
}

// SetupTestEnv starts a Headscale container and sets up test data
func SetupTestEnv(t *testing.T, headscaleVersion string) *TestEnv {
	pool, err := dockertest.NewPool("")
	require.NoError(t, err)

	require.NoError(t, pool.Client.Ping())

	// Write embedded config to temp file
	tmpConfig, err := os.CreateTemp("", "headscale-*.yaml")
	require.NoError(t, err)

	if _, err := tmpConfig.WriteString(headscaleConfig); err != nil {
		tmpConfig.Close()
		os.Remove(tmpConfig.Name())
		require.NoError(t, err)
		return nil
	}
	tmpConfig.Close()

	// Start Headscale container
	log.Println("Starting Headscale container...")
	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "headscale/headscale",
		Tag:        headscaleVersion,
		Cmd:        []string{"serve"},
		Mounts: []string{
			fmt.Sprintf("%s:/etc/headscale/config.yaml:ro", tmpConfig.Name()),
		},
		ExposedPorts: []string{"8080", "50443", "9090"},
		PortBindings: map[docker.Port][]docker.PortBinding{
			"8080/tcp":  {{HostIP: "127.0.0.1"}},
			"50443/tcp": {{HostIP: "127.0.0.1"}},
			"9090/tcp":  {{HostIP: "127.0.0.1"}},
		},
		Env: []string{
			"HEADSCALE_LOG_LEVEL=warn",
		},
	}, func(config *docker.HostConfig) {
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{Name: "no"}
		// Use tmpfs for ephemeral state
		config.Tmpfs = map[string]string{
			"/var/lib/headscale": "mode=1777,size=100m",
		}
	})
	require.NoError(t, err)

	// Get the actual host:port that Docker assigned
	grpcHostPort := resource.GetHostPort("50443/tcp")
	httpHostPort := resource.GetHostPort("8080/tcp")

	env := &TestEnv{
		pool:         pool,
		container:    resource,
		HeadscaleURL: fmt.Sprintf("http://%s", httpHostPort),
		GRPCAddr:     grpcHostPort,
		configFile:   tmpConfig.Name(),
	}

	// Set expiration to 60 seconds
	resource.Expire(60)

	// Wait for Headscale to be ready (just check if gRPC port is accepting connections)
	log.Println("Waiting for Headscale to be ready...")
	if err := pool.Retry(func() error {
		conn, err := grpc.NewClient(
			env.GRPCAddr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			return err
		}
		conn.Close()

		exitCode, err := resource.Exec([]string{"headscale", "users", "list"}, dockertest.ExecOptions{})
		if err != nil {
			return err
		}

		if exitCode != 0 {
			return fmt.Errorf("bad exitcode %d", exitCode)
		}
		return nil
	}); err != nil {
		resource.Close()
		require.NoError(t, err)
		return nil
	}

	log.Println("Headscale is ready")

	// Create test user, API key, and pre-auth key
	if err := env.setupTestData(); err != nil {
		env.Teardown()
		require.NoError(t, err)
		return nil
	}

	return env
}

// setupTestData creates user, API key, and pre-auth key
func (e *TestEnv) setupTestData() error {
	ctx := context.Background()

	stdout := new(bytes.Buffer)
	log.Println("Creating API key...")
	_, err := e.container.Exec([]string{"headscale", "apikeys", "create"}, dockertest.ExecOptions{
		StdOut: stdout,
	})
	if err != nil {
		return fmt.Errorf("failed to create apikey: %w", err)
	}

	// Create test user

	e.APIKey = strings.TrimSpace(stdout.String())

	// Reconnect with API key authentication
	conn, err := grpc.NewClient(
		e.GRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(&apiKeyAuth{key: e.APIKey}),
	)
	if err != nil {
		return fmt.Errorf("failed to reconnect with API key: %w", err)
	}
	e.grpcConn = conn
	e.headscaleClient = headscale.NewHeadscaleServiceClient(conn)

	log.Println("Creating test user...")
	userResp, err := e.headscaleClient.CreateUser(ctx, &headscale.CreateUserRequest{
		Name: "testuser",
	})
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}
	e.UserID = userResp.User.Id

	// Create pre-auth key
	log.Println("Creating pre-auth key...")
	preAuthResp, err := e.headscaleClient.CreatePreAuthKey(ctx, &headscale.CreatePreAuthKeyRequest{
		User:       e.UserID,
		Reusable:   true,
		Ephemeral:  false,
		Expiration: timestamppb.New(time.Now().Add(1 * time.Hour)),
	})
	if err != nil {
		return fmt.Errorf("failed to create pre-auth key: %w", err)
	}
	e.PreAuthKey = preAuthResp.PreAuthKey.Key

	log.Printf("Test data ready (User ID: %d)", e.UserID)
	return nil
}

// GetHeadscaleClient returns an authenticated Headscale gRPC client
func (e *TestEnv) GetHeadscaleClient() headscale.HeadscaleServiceClient {
	return e.headscaleClient
}

// StartTailscaleClient starts a tailscale client container that connects to the Headscale server
func (e *TestEnv) StartTailscaleClient(t *testing.T, hostname string) error {
	log.Printf("Starting tailscale client container with hostname: %s", hostname)

	// Create a new pre-auth key for this client
	ctx := context.Background()
	preAuthResp, err := e.headscaleClient.CreatePreAuthKey(ctx, &headscale.CreatePreAuthKeyRequest{
		User:       e.UserID,
		Reusable:   false,
		Ephemeral:  false,
		Expiration: timestamppb.New(time.Now().Add(1 * time.Hour)),
	})
	if err != nil {
		return fmt.Errorf("failed to create pre-auth key for client: %w", err)
	}
	clientAuthKey := preAuthResp.PreAuthKey.Key

	// Run tailscale container
	// Get the headscale HTTP port
	headscaleHostPort := e.container.GetHostPort("8080/tcp")

	// Run tailscale container - connect to headscale via host
	resource, err := e.pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "tailscale/tailscale",
		Tag:        "latest",
		Hostname:   hostname,
		Env: []string{
			fmt.Sprintf("TS_AUTHKEY=%s", clientAuthKey),
			fmt.Sprintf("TS_HOSTNAME=%s", hostname),
			"TS_ACCEPT_DNS=false",
			"TS_USERSPACE=true",
			// Connect to headscale via the host's mapped port
			fmt.Sprintf("TS_EXTRA_ARGS=--login-server=http://host.docker.internal:%s", strings.Split(headscaleHostPort, ":")[1]),
		},
	}, func(config *docker.HostConfig) {
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{Name: "no"}
		config.CapAdd = []string{"NET_ADMIN"}
		// Add host.docker.internal mapping for Linux
		config.ExtraHosts = []string{"host.docker.internal:host-gateway"}
	})
	if err != nil {
		return fmt.Errorf("failed to start tailscale container: %w", err)
	}

	// Set expiration to match headscale container
	resource.Expire(60)

	// Track the container
	e.tailscaleClients = append(e.tailscaleClients, resource)

	// Wait for tailscale to connect (check if node appears in headscale)
	log.Printf("Waiting for %s to register with Headscale...", hostname)
	if err := e.pool.Retry(func() error {
		nodesResp, err := e.headscaleClient.ListNodes(ctx, &headscale.ListNodesRequest{})
		if err != nil {
			// If we get an error, log container logs for debugging
			return err
		}
		for _, node := range nodesResp.Nodes {
			if node.GivenName == hostname {
				log.Printf("Tailscale client %s registered successfully", hostname)
				return nil
			}
		}
		return fmt.Errorf("node %s not yet registered", hostname)
	}); err != nil {
		return fmt.Errorf("timeout waiting for tailscale client to register: %w", err)
	}

	return nil
}

// Teardown stops and removes the Headscale container
func (e *TestEnv) Teardown() {
	if e.grpcConn != nil {
		e.grpcConn.Close()
	}

	// Stop all tailscale client containers
	for _, client := range e.tailscaleClients {
		log.Println("Stopping tailscale client container...")
		if err := e.pool.Purge(client); err != nil {
			log.Printf("Warning: failed to purge tailscale client: %v", err)
		}
	}

	if e.container != nil {
		log.Println("Stopping Headscale container...")
		if err := e.pool.Purge(e.container); err != nil {
			log.Printf("Warning: failed to purge container: %v", err)
		}
	}

	// Clean up temp config file
	if e.configFile != "" {
		if err := os.Remove(e.configFile); err != nil {
			log.Printf("Warning: failed to remove temp config: %v", err)
		}
	}
}

// WriteConfigFiles writes test config files to .testoutput for debugging
func (e *TestEnv) WriteConfigFiles() error {
	repoRoot, err := findRepoRoot()
	if err != nil {
		return err
	}
	outputDir := filepath.Join(repoRoot, "test", "integration", ".testoutput")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}

	// Write API key
	if err := os.WriteFile(filepath.Join(outputDir, "api-key"), []byte(e.APIKey), 0644); err != nil {
		return err
	}

	// Write pre-auth key
	if err := os.WriteFile(filepath.Join(outputDir, "preauth-key"), []byte(e.PreAuthKey), 0644); err != nil {
		return err
	}

	// Write hsadmin config
	config := fmt.Sprintf(`headscale:
  agent_userid: %d
  api_hostport: %s
  api_key: %s
  server_url: %s
  agent_tags: []
`, e.UserID, e.GRPCAddr, e.APIKey, e.HeadscaleURL)

	return os.WriteFile(filepath.Join(outputDir, "hsadmin.yaml"), []byte(config), 0644)
}

// apiKeyAuth implements gRPC credentials for API key authentication
type apiKeyAuth struct {
	key string
}

func (a *apiKeyAuth) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + a.key}, nil
}

func (a *apiKeyAuth) RequireTransportSecurity() bool {
	return false // Test environment uses insecure gRPC
}
