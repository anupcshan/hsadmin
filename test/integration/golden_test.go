package integration

import (
	"context"
	"flag"
	"html/template"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/anupcshan/hsadmin/internal/handlers"
	"github.com/anupcshan/hsadmin/internal/testutil"
	headscale "github.com/juanfont/headscale/gen/go/headscale/v1"
	"github.com/stretchr/testify/require"
	"tailscale.com/client/local"
	"tailscale.com/tsnet"
)

var (
	update   = flag.Bool("update", false, "update golden files")
	versions = []string{"0.27.0", "0.26.0"}

	// goldenOverrides explicitly declares which golden files have version-specific overrides.
	// Key format: "version/filename" (e.g., "0.27.0/machines_list.golden.html")
	// If a version/filename is in this map, getGoldenPath will use the version-specific path.
	// Otherwise, it uses the default path in testdata/.
	goldenOverrides = map[string]bool{
		// "0.26.0/machine_detail.golden.html": true,
	}
)

// TestMachinesList_Golden tests the machines list view against a golden file.
// This test runs against a real Headscale instance in a container.
func TestMachinesList_Golden(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	for _, headscaleVersion := range versions {
		t.Run(headscaleVersion, func(t *testing.T) {
			t.Parallel()
			testEnv := SetupTestEnv(t, headscaleVersion)
			require.NoError(t, testEnv.WriteConfigFiles())
			defer testEnv.Teardown()

			handler, tsnetClient := setupRealHandlerWithClient(t, testEnv, "hsadmin-test")

			// Wait for at least one machine to register (the tsnet client)
			ctx := context.Background()
			require.Eventually(t, func() bool {
				nodesResp, err := testEnv.GetHeadscaleClient().ListNodes(ctx, &headscale.ListNodesRequest{})
				return err == nil && len(nodesResp.Nodes) > 0
			}, 10*time.Second, 500*time.Millisecond, "Timeout waiting for machine to register")

			// Start a separate tailscale client that should show as online
			require.NoError(t, testEnv.StartTailscaleClient(t, "test-client"))

			// Wait for the tailscale client to be visible as a peer in the tsnet client AND marked as online
			require.Eventually(t, func() bool {
				status, err := tsnetClient.Status(ctx)
				if err != nil {
					return false
				}
				// Check if we can see the test-client as a peer AND it's online
				for _, peer := range status.Peer {
					if peer.HostName == "test-client" && peer.Online {
						return true
					}
				}
				return false
			}, 15*time.Second, 500*time.Millisecond, "Timeout waiting for peer to be online")

			req := httptest.NewRequest("GET", "/machines", nil)
			w := httptest.NewRecorder()

			handler.List(w, req)

			if w.Code != 200 {
				t.Logf("Response body: %s", w.Body.String())
			}
			require.Equal(t, 200, w.Code, "expected 200 OK response")

			// Normalize HTML for comparison
			normalized := testutil.NormalizeHTML(w.Body.String())

			// Compare with golden file (version-specific if exists, otherwise default)
			goldenPath := getGoldenPath(t, headscaleVersion, "machines_list.golden.html")
			compareGolden(t, goldenPath, normalized)
		})
	}
}

// TestMachineDetail_Golden tests the machine detail view against a golden file.
// This test runs against a real Headscale instance in a container.
func TestMachineDetail_Golden(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	for _, headscaleVersion := range versions {
		t.Run(headscaleVersion, func(t *testing.T) {
			t.Parallel()
			testEnv := SetupTestEnv(t, headscaleVersion)
			require.NoError(t, testEnv.WriteConfigFiles())
			defer testEnv.Teardown()

			// Create handler with a unique hostname for this test
			handler, tsnetClient := setupRealHandlerWithClient(t, testEnv, "hsadmin-mdetail")

			// Wait for the tsnet client to register with Headscale (with retry)
			ctx := context.Background()
			var machineID uint64
			expectedHostname := "hsadmin-mdetail"

			require.Eventually(t, func() bool {
				nodesResp, err := testEnv.GetHeadscaleClient().ListNodes(ctx, &headscale.ListNodesRequest{})
				if err != nil {
					return false
				}
				// Find our machine by hostname
				for _, node := range nodesResp.Nodes {
					if node.GivenName == expectedHostname {
						machineID = node.Id
						return true
					}
				}
				return false
			}, 10*time.Second, 500*time.Millisecond, "Timeout waiting for machine to register")

			// Wait for the tsnet client to be fully connected (have IPs assigned)
			require.Eventually(t, func() bool {
				status, err := tsnetClient.Status(ctx)
				if err != nil {
					return false
				}
				return status.Self != nil && len(status.Self.TailscaleIPs) > 0
			}, 10*time.Second, 500*time.Millisecond, "Timeout waiting for tsnet client to have IPs assigned")

			// Request machine detail page
			req := httptest.NewRequest("GET", "/machines/"+strconv.FormatUint(machineID, 10), nil)
			w := httptest.NewRecorder()

			handler.Detail(w, req)

			if w.Code != 200 {
				t.Logf("Response body: %s", w.Body.String())
			}
			require.Equal(t, 200, w.Code, "expected 200 OK response")

			// Normalize HTML for comparison
			normalized := testutil.NormalizeHTML(w.Body.String())

			// Compare with golden file (version-specific if exists, otherwise default)
			goldenPath := getGoldenPath(t, headscaleVersion, "machine_detail.golden.html")
			compareGolden(t, goldenPath, normalized)
		})
	}
}

// TestUsersList_Golden tests the users list view against a golden file.
// This test runs against a real Headscale instance in a container.
func TestUsersList_Golden(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	for _, headscaleVersion := range versions {
		t.Run(headscaleVersion, func(t *testing.T) {
			t.Parallel()
			testEnv := SetupTestEnv(t, headscaleVersion)
			require.NoError(t, testEnv.WriteConfigFiles())
			defer testEnv.Teardown()

			handler, tsnetClient := setupRealUsersHandler(t, testEnv)

			// Wait for at least one user to exist (default user is created with setup)
			ctx := context.Background()
			require.Eventually(t, func() bool {
				usersResp, err := testEnv.GetHeadscaleClient().ListUsers(ctx, &headscale.ListUsersRequest{})
				return err == nil && len(usersResp.Users) > 0
			}, 10*time.Second, 500*time.Millisecond, "Timeout waiting for users to exist")

			// Wait for at least one machine to register (provides LastSeen data)
			require.Eventually(t, func() bool {
				nodesResp, err := testEnv.GetHeadscaleClient().ListNodes(ctx, &headscale.ListNodesRequest{})
				return err == nil && len(nodesResp.Nodes) > 0
			}, 10*time.Second, 500*time.Millisecond, "Timeout waiting for machine to register")

			// Wait for the tsnet client to be fully connected (have IPs assigned)
			require.Eventually(t, func() bool {
				status, err := tsnetClient.Status(ctx)
				if err != nil {
					return false
				}
				return status.Self != nil && len(status.Self.TailscaleIPs) > 0
			}, 10*time.Second, 500*time.Millisecond, "Timeout waiting for tsnet client to have IPs assigned")

			req := httptest.NewRequest("GET", "/users", nil)
			w := httptest.NewRecorder()

			handler.List(w, req)

			if w.Code != 200 {
				t.Logf("Response body: %s", w.Body.String())
			}
			require.Equal(t, 200, w.Code, "expected 200 OK response")

			// Normalize HTML for comparison
			normalized := testutil.NormalizeHTML(w.Body.String())

			// Compare with golden file (version-specific if exists, otherwise default)
			goldenPath := getGoldenPath(t, headscaleVersion, "users_list.golden.html")
			compareGolden(t, goldenPath, normalized)
		})
	}
}

// setupRealHandlerWithClient creates a handler and returns both the handler and tsnet client.
func setupRealHandlerWithClient(t *testing.T, testEnv *TestEnv, hostname string) (*handlers.MachinesHandler, *local.Client) {
	t.Helper()

	// Use global testEnv (set up by TestMain)
	headscaleClient := testEnv.GetHeadscaleClient()

	// Create tsnet client with specified hostname
	tsnetClient := setupTsnetClientWithHostname(t, testEnv.PreAuthKey, testEnv.HeadscaleURL, hostname)

	// Find repo root and parse templates
	repoRoot, err := findRepoRoot()
	require.NoError(t, err, "Failed to find repo root")

	templatesPath := filepath.Join(repoRoot, "web", "templates", "*.html")
	funcMap := template.FuncMap{
		"sub": func(a, b int) int { return a - b },
		"mul": func(a, b float64) float64 { return a * b },
	}
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseGlob(templatesPath))

	return handlers.NewMachinesHandler(tmpl, headscaleClient, tsnetClient), tsnetClient
}

// setupRealUsersHandler creates a users handler connected to the real test Headscale instance.
func setupRealUsersHandler(t *testing.T, testEnv *TestEnv) (*handlers.UsersHandler, *local.Client) {
	t.Helper()

	headscaleClient := testEnv.GetHeadscaleClient()

	// Create tsnet client for fetching machine status
	tsnetClient := setupTsnetClientWithHostname(t, testEnv.PreAuthKey, testEnv.HeadscaleURL, "hsadmin-users-test")

	// Find repo root and parse templates
	repoRoot, err := findRepoRoot()
	require.NoError(t, err, "Failed to find repo root")

	templatesPath := filepath.Join(repoRoot, "web", "templates", "*.html")
	funcMap := template.FuncMap{
		"sub": func(a, b int) int { return a - b },
		"mul": func(a, b float64) float64 { return a * b },
	}
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseGlob(templatesPath))

	// Create machines handler first (needed by users handler for deduplication)
	machinesHandler := handlers.NewMachinesHandler(tmpl, headscaleClient, tsnetClient)

	return handlers.NewUsersHandler(tmpl, headscaleClient, tsnetClient, machinesHandler), tsnetClient
}

// setupTsnetClientWithHostname creates and starts a tsnet client with a specific hostname.
func setupTsnetClientWithHostname(t *testing.T, preAuthKey, controlURL, hostname string) *local.Client {
	t.Helper()

	// Create tsnet server (ephemeral test node)
	tsnetSrv := &tsnet.Server{
		Hostname:   hostname,
		Ephemeral:  true,
		AuthKey:    preAuthKey,
		ControlURL: controlURL,
		Dir:        filepath.Join(t.TempDir(), "tsnet"), // Isolated state directory
	}

	// Clean up on test completion
	t.Cleanup(func() {
		tsnetSrv.Close()
	})

	// Get LocalClient
	localClient, err := tsnetSrv.LocalClient()
	require.NoError(t, err, "Failed to create tsnet LocalClient")

	return localClient
}

// getGoldenPath returns the path to a golden file.
// Uses version-specific path if explicitly declared in goldenOverrides map,
// otherwise uses the default path in testdata/.
func getGoldenPath(t *testing.T, version, filename string) string {
	t.Helper()

	repoRoot, err := findRepoRoot()
	require.NoError(t, err, "Failed to find repo root")

	// Check if this version/filename has an explicit override
	overrideKey := filepath.Join(version, filename)
	if goldenOverrides[overrideKey] {
		// Use version-specific path
		return filepath.Join(repoRoot, "test", "integration", "testdata", version, filename)
	}

	// Use default golden file
	return filepath.Join(repoRoot, "test", "integration", "testdata", filename)
}

// compareGolden compares the actual output with a golden file.
// If -update flag is set, it updates the golden file instead of comparing.
func compareGolden(t *testing.T, goldenPath string, actual string) {
	t.Helper()

	if *update {
		// Create directory if needed
		dir := filepath.Dir(goldenPath)
		err := os.MkdirAll(dir, 0755)
		require.NoError(t, err, "Failed to create golden file directory")

		// Write golden file
		err = os.WriteFile(goldenPath, []byte(actual), 0644)
		require.NoError(t, err, "Failed to write golden file")

		t.Logf("Updated golden file: %s", goldenPath)
		return
	}

	// Read golden file
	golden, err := os.ReadFile(goldenPath)
	if os.IsNotExist(err) {
		t.Fatalf("Golden file not found: %s\nRun with -update to create it:\n  go test -tags=integration -update", goldenPath)
	}
	require.NoError(t, err, "Failed to read golden file")

	// Compare
	require.Equal(t, string(golden), actual, "Golden file mismatch: %s\nRun with -update to update:\n  go test -tags=integration -update", goldenPath)
}
