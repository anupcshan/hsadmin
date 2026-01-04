package integration

import (
	"fmt"
	"strings"
	"testing"
	"time"

	headscale "github.com/juanfont/headscale/gen/go/headscale/v1"
	"github.com/ory/dockertest/v3"
	"github.com/stretchr/testify/require"
)

// TestSSE_MachineAddition tests that new machines appear via SSE without page refresh
func TestSSE_MachineAddition(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping SSE test in short mode")
	}
	t.Parallel()

	fixture := setupUITest(t)

	// Navigate to machines page with screenshot on failure
	page := SetupPageWithScreenshot(t, fixture.browser, fixture.serverURL+"/machines")

	// Get initial machine count (use helper to avoid panics)
	initialCount := CountElements(page, "tr[id^='machine-']")
	t.Logf("Initial machine count: %d", initialCount)

	// Add a new tailscale client (this should trigger SSE update)
	hostname := fmt.Sprintf("test-machine-%d", time.Now().Unix())
	t.Logf("Adding new machine: %s", hostname)
	err := fixture.testEnv.StartTailscaleClient(t, hostname)
	require.NoError(t, err, "Failed to start tailscale client")

	// Wait for SSE update to add the machine to the UI
	WaitForElementToContainText(t, page, "tr[id^='machine-']", hostname, 15*time.Second)
	t.Logf("✓ Machine %s appeared via SSE without page refresh", hostname)

	// Verify final count
	finalCount := CountElements(page, "tr[id^='machine-']")
	require.Equal(t, initialCount+1, finalCount, "Should have one more machine")
}

// TestSSE_MachineStatusChange tests that machine status changes (online/offline) are reflected via SSE
func TestSSE_MachineStatusChange(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping SSE test in short mode")
	}
	t.Parallel()

	fixture := setupUITest(t)
	ctx := fixture.ctx

	// Add a machine first
	hostname := fmt.Sprintf("status-test-%d", time.Now().Unix())
	t.Logf("Adding machine: %s", hostname)
	err := fixture.testEnv.StartTailscaleClient(t, hostname)
	require.NoError(t, err)

	// Wait for machine to be registered and get its container
	var nodeID uint64
	var container *dockertest.Resource
	require.Eventually(t, func() bool {
		nodesResp, err := fixture.testEnv.GetHeadscaleClient().ListNodes(ctx, &headscale.ListNodesRequest{})
		if err != nil {
			return false
		}
		for _, node := range nodesResp.Nodes {
			if node.GivenName == hostname {
				nodeID = node.Id
				// Find the container for this machine
				for i := range fixture.testEnv.tailscaleClients {
					if fixture.testEnv.tailscaleClients[i].Container.Config.Hostname == hostname {
						container = fixture.testEnv.tailscaleClients[i]
						return true
					}
				}
			}
		}
		return false
	}, 30*time.Second, 500*time.Millisecond, "Machine should be registered")
	require.NotNil(t, container, "Container should be found")

	// Navigate to machines page with screenshot on failure
	page := SetupPageWithScreenshot(t, fixture.browser, fixture.serverURL+"/machines")

	// Find the machine in the UI and wait for it to show as Connected
	// Use longer timeout since machine needs to connect and SSE needs to propagate
	statusElementID := fmt.Sprintf("machine-%d-status", nodeID)
	require.Eventually(t, func() bool {
		machines := GetElements(page, "tr[id^='machine-']")
		for _, machine := range machines {
			machineHTML := GetElementHTML(machine)
			if strings.Contains(machineHTML, hostname) {
				elem, err := machine.Element(fmt.Sprintf("#%s", statusElementID))
				if err != nil || elem == nil {
					continue
				}
				text, err := elem.Text()
				if err == nil && strings.Contains(text, "Connected") {
					return true
				}
			}
		}
		return false
	}, 60*time.Second, 1*time.Second, "Machine should appear as Connected")

	t.Log("✓ Machine initially shows as Connected in UI")

	// Take the machine offline using tailscale down
	t.Logf("Running 'tailscale down' in container")
	exitCode, err := container.Exec([]string{"tailscale", "down"}, dockertest.ExecOptions{})
	require.NoError(t, err, "Failed to execute tailscale down")
	require.Equal(t, 0, exitCode, "tailscale down should succeed")

	// First, wait for the API to reflect the machine as offline
	t.Log("Waiting for Headscale API to mark machine as offline...")
	require.Eventually(t, func() bool {
		nodeResp, err := fixture.testEnv.GetHeadscaleClient().GetNode(ctx, &headscale.GetNodeRequest{
			NodeId: nodeID,
		})
		if err != nil {
			return false
		}
		isOffline := !nodeResp.Node.Online
		if isOffline {
			t.Log("✓ API confirms machine is offline")
		}
		return isOffline
	}, 90*time.Second, 2*time.Second, "Machine should be marked offline in API")

	// Now wait for the UI to reflect the offline status via SSE
	// Note: We need to re-query the element each time because SSE may have replaced the table
	t.Log("Waiting for UI to reflect offline status via SSE...")
	require.Eventually(t, func() bool {
		// Re-query the status element using non-panic helpers
		machines := GetElements(page, "tr[id^='machine-']")
		for _, machine := range machines {
			machineHTML := GetElementHTML(machine)
			if strings.Contains(machineHTML, hostname) {
				elem, err := machine.Element(fmt.Sprintf("#%s", statusElementID))
				if err != nil || elem == nil {
					continue
				}
				statusText, err := elem.Text()
				if err != nil {
					continue
				}
				isOffline := !strings.Contains(statusText, "Connected")
				if isOffline {
					t.Logf("✓ UI shows offline status: %s", statusText)
				}
				return isOffline
			}
		}
		return false
	}, 20*time.Second, 1*time.Second, "UI should show offline status via SSE")

	t.Log("✓ Offline status change verified via SSE")

	// Note: We don't test bringing the machine back online because tailscale down causes
	// the containerboot wrapper to exit, which triggers AutoRemove and deletes the container.
	// The offline detection is the critical path for SSE updates.
}

// TestSSE_MachineDeletion tests that deleted machines disappear via SSE
func TestSSE_MachineDeletion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping SSE test in short mode")
	}
	t.Parallel()

	fixture := setupUITest(t)
	ctx := fixture.ctx

	// Add a machine first
	hostname := fmt.Sprintf("delete-test-%d", time.Now().Unix())
	t.Logf("Adding machine: %s", hostname)
	err := fixture.testEnv.StartTailscaleClient(t, hostname)
	require.NoError(t, err)

	// Wait for machine to be registered
	var nodeID uint64
	require.Eventually(t, func() bool {
		nodesResp, err := fixture.testEnv.GetHeadscaleClient().ListNodes(ctx, &headscale.ListNodesRequest{})
		if err != nil {
			return false
		}
		for _, node := range nodesResp.Nodes {
			if node.GivenName == hostname {
				nodeID = node.Id
				return true
			}
		}
		return false
	}, 30*time.Second, 500*time.Millisecond, "Machine should be registered")

	// Navigate to machines page with screenshot on failure
	page := SetupPageWithScreenshot(t, fixture.browser, fixture.serverURL+"/machines")

	// Wait for machine to appear in UI first
	WaitForElementToContainText(t, page, "tr[id^='machine-']", hostname, 15*time.Second)

	// Get initial count
	initialCount := CountElements(page, "tr[id^='machine-']")
	t.Logf("Machine count before deletion: %d", initialCount)

	// Delete the machine via API
	t.Logf("Deleting machine %s (ID: %d)", hostname, nodeID)
	_, err = fixture.testEnv.GetHeadscaleClient().DeleteNode(ctx, &headscale.DeleteNodeRequest{
		NodeId: nodeID,
	})
	require.NoError(t, err, "Failed to delete machine")

	// Wait for SSE update to remove the machine from the UI
	// Use longer timeout since SSE polling interval is ~5s and updates can take time to propagate
	WaitForElementToDisappear(t, page, "tr[id^='machine-']", hostname, 60*time.Second)

	// Verify final count
	finalCount := CountElements(page, "tr[id^='machine-']")
	require.Equal(t, initialCount-1, finalCount, "Should have one fewer machine")

	t.Logf("✓ Machine %s disappeared via SSE without page refresh", hostname)
}

// TestSSE_SubnetRouteChange tests that advertised subnet routes appear via SSE on machine detail page
func TestSSE_SubnetRouteChange(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping SSE test in short mode")
	}
	t.Parallel()

	fixture := setupUITest(t)
	ctx := fixture.ctx

	// Start a tailscale client
	hostname := fmt.Sprintf("subnet-sse-test-%d", time.Now().Unix())
	t.Logf("Starting tailscale client: %s", hostname)
	err := fixture.testEnv.StartTailscaleClient(t, hostname)
	require.NoError(t, err)

	// Wait for machine to be registered and get its ID and container
	var machineID uint64
	var clientContainer *dockertest.Resource
	require.Eventually(t, func() bool {
		nodesResp, err := fixture.testEnv.GetHeadscaleClient().ListNodes(ctx, &headscale.ListNodesRequest{})
		if err != nil {
			return false
		}
		for _, node := range nodesResp.Nodes {
			if node.GivenName == hostname {
				machineID = node.Id
				// Find the container
				for i := range fixture.testEnv.tailscaleClients {
					if fixture.testEnv.tailscaleClients[i].Container.Config.Hostname == hostname {
						clientContainer = fixture.testEnv.tailscaleClients[i]
						return true
					}
				}
			}
		}
		return false
	}, 30*time.Second, 500*time.Millisecond, "Machine should be registered")
	require.NotNil(t, clientContainer, "Container should be found")

	t.Logf("Machine registered with ID: %d", machineID)

	// Navigate to machine detail page BEFORE advertising routes
	page := SetupPageWithScreenshot(t, fixture.browser, fmt.Sprintf("%s/machines/%d", fixture.serverURL, machineID))

	// Verify the subnets section shows "does not expose any routes" initially
	require.Eventually(t, func() bool {
		bodyText, err := page.MustElement("body").Text()
		if err != nil {
			return false
		}
		return strings.Contains(bodyText, "does not expose any routes")
	}, 10*time.Second, 500*time.Millisecond, "Should show no routes initially")
	t.Log("✓ Page shows no routes initially")

	// Now advertise subnet routes (this should trigger SSE update)
	t.Log("Advertising subnet routes via tailscale set...")
	exitCode, err := clientContainer.Exec(
		[]string{"tailscale", "set", "--advertise-routes=10.99.0.0/24,172.16.0.0/16"},
		dockertest.ExecOptions{},
	)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "tailscale set command should succeed")

	// Wait for routes to be reflected in Headscale API
	require.Eventually(t, func() bool {
		nodeResp, err := fixture.testEnv.GetHeadscaleClient().GetNode(ctx, &headscale.GetNodeRequest{
			NodeId: machineID,
		})
		if err != nil {
			return false
		}
		for _, route := range nodeResp.Node.AvailableRoutes {
			if route == "10.99.0.0/24" {
				t.Logf("Routes now in API: %v", nodeResp.Node.AvailableRoutes)
				return true
			}
		}
		return false
	}, 10*time.Second, 500*time.Millisecond, "Routes should be in API")

	// Wait for SSE to update the subnets section (WITHOUT page refresh)
	// The routes should appear in the "Awaiting Approval" section
	t.Log("Waiting for SSE to update subnets section...")
	require.Eventually(t, func() bool {
		// Query the subnets section specifically
		subnetsSection, err := page.Element(fmt.Sprintf("#machine-subnets-%d", machineID))
		if err != nil || subnetsSection == nil {
			return false
		}
		sectionText, err := subnetsSection.Text()
		if err != nil {
			return false
		}
		has1099 := strings.Contains(sectionText, "10.99.0.0/24")
		has172 := strings.Contains(sectionText, "172.16.0.0/16")
		if has1099 && has172 {
			t.Log("✓ Both routes appeared in subnets section via SSE")
			return true
		}
		return false
	}, 20*time.Second, 1*time.Second, "Routes should appear in subnets section via SSE")

	// Verify "does not expose any routes" is no longer shown
	bodyText, err := page.MustElement("body").Text()
	require.NoError(t, err)
	require.NotContains(t, bodyText, "does not expose any routes",
		"Should no longer show 'no routes' message after SSE update")

	t.Log("✓ Subnet routes appeared via SSE without page refresh")
}

// TestSSE_MultipleChanges tests that multiple rapid changes are handled correctly
func TestSSE_MultipleChanges(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping SSE test in short mode")
	}
	t.Parallel()

	fixture := setupUITest(t)

	// Navigate to machines page with screenshot on failure
	page := SetupPageWithScreenshot(t, fixture.browser, fixture.serverURL+"/machines")

	// Get initial count
	initialCount := CountElements(page, "tr[id^='machine-']")
	t.Logf("Initial machine count: %d", initialCount)

	// Add multiple machines rapidly
	numMachines := 3
	hostnames := make([]string, numMachines)

	for i := 0; i < numMachines; i++ {
		hostname := fmt.Sprintf("multi-test-%d-%d", time.Now().Unix(), i)
		hostnames[i] = hostname
		t.Logf("Adding machine %d/%d: %s", i+1, numMachines, hostname)

		err := fixture.testEnv.StartTailscaleClient(t, hostname)
		require.NoError(t, err, "Failed to start tailscale client %d", i)
	}

	// Wait for all machines to appear in UI (use non-panic helpers)
	require.Eventually(t, func() bool {
		machines := GetElements(page, "tr[id^='machine-']")
		currentCount := len(machines)
		t.Logf("Current machine count: %d (expecting %d)", currentCount, initialCount+numMachines)

		// Check all hostnames are present
		foundCount := 0
		for _, hostname := range hostnames {
			for _, machine := range machines {
				machineHTML := GetElementHTML(machine)
				if strings.Contains(machineHTML, hostname) {
					foundCount++
					break
				}
			}
		}

		t.Logf("Found %d/%d machines", foundCount, numMachines)
		return foundCount == numMachines
	}, 30*time.Second, 2*time.Second, "All machines should appear via SSE")

	// Verify final count
	finalCount := CountElements(page, "tr[id^='machine-']")
	require.Equal(t, initialCount+numMachines, finalCount,
		"Should have %d more machines", numMachines)

	t.Logf("✓ All %d machines appeared via SSE", numMachines)
}
