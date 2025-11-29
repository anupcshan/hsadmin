package integration

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anupcshan/hsadmin/internal/events"
	"github.com/anupcshan/hsadmin/internal/handlers"
	"github.com/anupcshan/hsadmin/internal/sets"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	headscale "github.com/juanfont/headscale/gen/go/headscale/v1"
	"github.com/ory/dockertest/v3"
	"github.com/stretchr/testify/require"
	"tailscale.com/tsnet"
)

// uiTestFixture holds common setup for UI tests
type uiTestFixture struct {
	testEnv   *TestEnv
	server    *httptest.Server
	serverURL string
	browser   *rod.Browser
	ctx       context.Context
}

// setupUITest creates a complete test fixture with Headscale, server, and browser
func setupUITest(t *testing.T) *uiTestFixture {
	t.Helper()

	// Set up test environment with Headscale
	testEnv := SetupTestEnv(t, "0.27.0")
	require.NoError(t, testEnv.WriteConfigFiles())
	t.Cleanup(testEnv.Teardown)

	// Start HTTP server
	server, serverURL := startTestServer(t, testEnv)
	t.Cleanup(server.Close)

	// Wait for at least one user to exist
	// Use longer timeout for CPU-loaded environments
	ctx, cancelFunc := context.WithTimeout(context.Background(), 300*time.Second)
	t.Cleanup(func() { cancelFunc() })
	require.Eventually(t, func() bool {
		usersResp, err := testEnv.GetHeadscaleClient().ListUsers(ctx, &headscale.ListUsersRequest{})
		return err == nil && len(usersResp.Users) > 0
	}, 10*time.Second, 500*time.Millisecond, "Timeout waiting for users to exist")

	// Set up Rod browser
	browser := setupBrowser(t, ctx)
	t.Cleanup(func() { browser.MustClose() })

	return &uiTestFixture{
		testEnv:   testEnv,
		server:    server,
		serverURL: serverURL,
		browser:   browser,
		ctx:       ctx,
	}
}

// TestUsersDropdownMenu_UI tests the dropdown menu with a real browser
func TestUsersDropdownMenu_UI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping UI test in short mode")
	}
	t.Parallel()

	fixture := setupUITest(t)

	// Navigate to users page
	page := fixture.browser.MustPage(fixture.serverURL + "/users")
	defer page.MustClose()

	// Wait for page to load
	page.MustWaitLoad()

	// Find the first user's menu button (which is a <summary> inside <details>)
	menuButton := page.MustElement(`[data-testid="user-menu-button"]`)
	menu := page.MustElement(`[data-testid="user-menu-dropdown"]`)

	// Click the menu button to open it
	menuButton.MustClick()

	// Wait a moment for the details element to open
	page.MustWaitIdle()

	// Verify menu items are present and visible (MustElement will fail if not found)
	menu.MustElement(`[data-testid="user-menu-rename"]`)
	menu.MustElement(`[data-testid="user-menu-preauth"]`)
	menu.MustElement(`[data-testid="user-menu-delete"]`)

	t.Log("✓ Dropdown menu works correctly - opens on click and shows menu items")
}

// TestUsersDropdownClickOutside_UI tests that clicking outside the dropdown closes it
func TestUsersDropdownClickOutside_UI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping UI test in short mode")
	}
	t.Parallel()

	fixture := setupUITest(t)

	// Navigate to users page with screenshot on failure
	page := SetupPageWithScreenshot(t, fixture.browser, fixture.serverURL+"/users")

	// The details element that contains the menu button - use selector, not stale reference
	detailsSelector := `details:has([data-testid="user-menu-button"])`

	// Verify the dropdown is initially closed
	require.False(t, IsDropdownOpen(page, detailsSelector), "Dropdown should be initially closed")

	// Click the menu button to open it
	ClickElement(t, page, `[data-testid="user-menu-button"]`)

	// Wait for dropdown to be open (use retry for SSE updates)
	WaitForDropdownOpen(t, page, detailsSelector, 5*time.Second)

	// Click outside the dropdown (click on the page header)
	ClickElement(t, page, "h1")

	// Verify the dropdown is now closed
	WaitForDropdownClosed(t, page, detailsSelector, 5*time.Second)

	t.Log("✓ Dropdown closes when clicking outside")
}

// TestRenameUser_UI tests the rename user functionality end-to-end
func TestRenameUser_UI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping UI test in short mode")
	}
	t.Parallel()

	fixture := setupUITest(t)

	// Navigate to users page with screenshot on failure
	page := SetupPageWithScreenshot(t, fixture.browser, fixture.serverURL+"/users")

	// Get the original username for verification
	userNameElement := page.MustElement(`[data-testid="user-display-name"]`)
	originalName := userNameElement.MustText()
	t.Logf("Original username: %s", originalName)

	// Open the dropdown menu, click "Rename user", and wait for modal
	OpenAndClickDropdownItem(t, page,
		`[data-testid="user-menu-button"]`,
		`[data-testid="user-menu-rename"]`,
		`[data-testid="rename-modal"]`)
	renameModal := page.MustElement(`[data-testid="rename-modal"]`)

	// Fill in the new name
	newName := "renamed-user"
	nameInput := renameModal.MustElement(`[data-testid="rename-input"]`)
	nameInput.MustSelectAllText().MustInput(newName)

	// Submit the form - HTMX will handle the request
	submitButton := renameModal.MustElement(`[data-testid="rename-submit"]`)
	submitButton.MustClick()

	// Wait for the modal to close (HTMX success handler closes it)
	WaitForElementToDisappear(t, page, `dialog[open]`, "", 15*time.Second)

	// Verify the user was renamed in the UI
	// userNameElement = page.MustElement(`[data-testid="user-display-name"]`)
	// updatedName := userNameElement.MustText()
	// require.Equal(t, newName, updatedName, "User should be renamed in the UI")

	// Verify via API that the rename actually happened
	usersResp, err := fixture.testEnv.GetHeadscaleClient().ListUsers(fixture.ctx, &headscale.ListUsersRequest{})
	require.NoError(t, err)
	require.Len(t, usersResp.Users, 1, "Should still have one user")
	require.Equal(t, newName, usersResp.Users[0].Name, "User should be renamed in Headscale")

	t.Logf("✓ User successfully renamed from '%s' to '%s'", originalName, newName)
}

// TestGeneratePreAuthKey_UI tests the preauth key generation functionality end-to-end
func TestGeneratePreAuthKey_UI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping UI test in short mode")
	}
	t.Parallel()

	fixture := setupUITest(t)

	// Navigate to users page
	page := fixture.browser.MustPage(fixture.serverURL + "/users")
	defer page.MustClose()
	page.MustWaitLoad()

	// Get the user ID for API verification
	usersResp, err := fixture.testEnv.GetHeadscaleClient().ListUsers(fixture.ctx, &headscale.ListUsersRequest{})
	require.NoError(t, err)
	require.Len(t, usersResp.Users, 1, "Should have exactly one user")
	userID := usersResp.Users[0].Id
	userName := usersResp.Users[0].Name

	// Open the dropdown menu, click "Generate pre-auth key", and wait for modal
	OpenAndClickDropdownItem(t, page,
		`[data-testid="user-menu-button"]`,
		`[data-testid="user-menu-preauth"]`,
		`[data-testid="preauth-modal"]`)
	preauthModal := page.MustElement(`[data-testid="preauth-modal"]`)

	// Verify form elements are present
	preauthModal.MustElement(`[data-testid="preauth-ephemeral"]`)
	preauthModal.MustElement(`[data-testid="preauth-reusable"]`)
	expirationInput := preauthModal.MustElement(`[data-testid="preauth-expiration"]`)

	// Change expiration to 2 hours
	expirationInput.MustSelectAllText().MustInput("2")

	// Click generate button
	generateButton := preauthModal.MustElement(`[data-testid="preauth-generate"]`)
	generateButton.MustClick()

	// Wait for HTMX to swap in the generated key
	WaitForVisible(t, page, `[data-testid="preauth-key-output"]`)
	keyOutput := page.MustElement(`[data-testid="preauth-key-output"]`)

	// Verify the key was generated (non-empty)
	generatedKey := keyOutput.MustProperty("value").String()
	require.NotEmpty(t, generatedKey, "Generated key should not be empty")
	require.Greater(t, len(generatedKey), 10, "Generated key should be a reasonable length")

	t.Logf("✓ Pre-auth key generated: %s", generatedKey)

	// Verify via API that the key actually exists
	keysResp, err := fixture.testEnv.GetHeadscaleClient().ListPreAuthKeys(fixture.ctx, &headscale.ListPreAuthKeysRequest{
		User: userID,
	})
	require.NoError(t, err)
	require.NotEmpty(t, keysResp.PreAuthKeys, "Should have at least one preauth key")

	// Find the generated key in the list
	var foundKey *headscale.PreAuthKey
	for _, key := range keysResp.PreAuthKeys {
		if key.Key == generatedKey {
			foundKey = key
			break
		}
	}
	require.NotNil(t, foundKey, "Generated key should exist in the API response")
	require.Equal(t, userName, foundKey.User.Name, "Key should belong to the correct user")

	t.Logf("✓ Pre-auth key verified in API for user '%s'", userName)

	// Close the modal
	closeButton := preauthModal.MustElement(`[data-testid="preauth-close"]`)
	closeButton.MustClick()
	preauthModal.MustWaitInvisible()
}

// TestDeleteUser_UI tests the delete user functionality end-to-end
func TestDeleteUser_UI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping UI test in short mode")
	}
	t.Parallel()

	fixture := setupUITest(t)

	// Create a second user specifically for deletion (with no nodes)
	createUserResp, err := fixture.testEnv.GetHeadscaleClient().CreateUser(fixture.ctx, &headscale.CreateUserRequest{
		Name: "user-to-delete",
	})
	require.NoError(t, err)
	userToDelete := createUserResp.User
	t.Logf("Created user for deletion: %s (ID: %d)", userToDelete.Name, userToDelete.Id)

	// Navigate to users page with screenshot on failure
	page := SetupPageWithScreenshot(t, fixture.browser, fixture.serverURL+"/users")

	// Verify we have 2 users in the UI
	userElements := page.MustElements(`[data-testid="user-display-name"]`)
	require.Len(t, userElements, 2, "Should have 2 users in the UI")

	t.Logf("Attempting to delete user: %s", userToDelete.Name)

	// Open the dropdown menu, click "Delete user", and wait for modal
	OpenAndClickDropdownItemInRowByText(t, page, userToDelete.Name,
		`[data-testid="user-menu-button"]`,
		`[data-testid="user-menu-delete"]`,
		`[data-testid="delete-modal"]`)
	deleteModal := page.MustElement(`[data-testid="delete-modal"]`)

	// Confirm deletion by clicking the submit button - HTMX will handle the request
	submitButton := deleteModal.MustElement(`[data-testid="delete-submit"]`)
	submitButton.MustClick()

	// Wait for the modal to close (HTMX success handler closes it)
	WaitForElementToDisappear(t, page, `dialog[open]`, "", 15*time.Second)

	// Verify the user was deleted via API
	usersResp, err := fixture.testEnv.GetHeadscaleClient().ListUsers(fixture.ctx, &headscale.ListUsersRequest{})
	require.NoError(t, err)
	require.Len(t, usersResp.Users, 1, "Should have one user left after deletion")
	require.Equal(t, "testuser", usersResp.Users[0].Name, "The remaining user should be testuser")

	t.Logf("✓ User '%s' successfully deleted", userToDelete.Name)
}

// TestDeleteUserWithMachines_UI tests that deleting a user with machines shows an error
func TestDeleteUserWithMachines_UI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping UI test in short mode")
	}
	t.Parallel()

	fixture := setupUITest(t)

	// Get the testuser by name
	usersResp, err := fixture.testEnv.GetHeadscaleClient().ListUsers(fixture.ctx, &headscale.ListUsersRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, usersResp.Users, "Should have at least one user")

	// Find testuser explicitly by name
	var testUser *headscale.User
	for _, u := range usersResp.Users {
		if u.Name == "testuser" {
			testUser = u
			break
		}
	}
	require.NotNil(t, testUser, "Should find testuser")

	// Wait for the tsnet client machine to register and belong to testuser
	require.Eventually(t, func() bool {
		nodesResp, err := fixture.testEnv.GetHeadscaleClient().ListNodes(fixture.ctx, &headscale.ListNodesRequest{
			User: testUser.Name,
		})
		return err == nil && len(nodesResp.Nodes) > 0
	}, 30*time.Second, 500*time.Millisecond, "Timeout waiting for testuser's machine to register")

	t.Logf("Attempting to delete user with machines: %s (ID: %d)", testUser.Name, testUser.Id)

	// Navigate to users page
	page := fixture.browser.MustPage(fixture.serverURL + "/users")
	defer page.MustClose()
	page.MustWaitLoad()

	// Open the dropdown menu, click "Delete user", and wait for modal
	OpenAndClickDropdownItemInRowByText(t, page, testUser.Name,
		`[data-testid="user-menu-button"]`,
		`[data-testid="user-menu-delete"]`,
		`[data-testid="delete-modal"]`)

	// Confirm deletion by clicking the submit button
	ClickElement(t, page, `[data-testid="delete-submit"]`)

	// Wait a moment for the request to complete
	time.Sleep(500 * time.Millisecond)

	// Check what actually happened - did the user get deleted?
	usersRespAfter, err := fixture.testEnv.GetHeadscaleClient().ListUsers(fixture.ctx, &headscale.ListUsersRequest{})
	require.NoError(t, err)

	userStillExists := false
	for _, u := range usersRespAfter.Users {
		if u.Id == testUser.Id {
			userStillExists = true
			break
		}
	}

	if userStillExists {
		t.Log("✓ Headscale prevented deletion - user still exists")

		// Wait for error alert to appear (embedded in rendered page)
		t.Log("Waiting for error alert to appear...")

		var alert *rod.Element
		require.Eventually(t, func() bool {
			errorAlert, err := page.Element("#error-alert")
			if err != nil {
				return false
			}
			innerAlert := errorAlert.MustElement("div[role='alert']")
			if innerAlert != nil {
				alert = innerAlert
				return true
			}
			return false
		}, 5*time.Second, 100*time.Millisecond, "Error alert should appear")

		// Get the error message text
		alertText := alert.MustText()

		// Verify it contains the expected error message
		require.Contains(t, alertText, "Failed to delete user", "Alert should contain error message")
		t.Logf("✓ Error alert displayed: %s", alertText)
	} else {
		t.Fatal("HSAdmin allowed deletion of user with machines - this means Headscale doesn't prevent this")
	}
}

// TestRenameMachine_UI tests the rename machine functionality end-to-end
func TestRenameMachine_UI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping UI test in short mode")
	}
	t.Parallel()

	fixture := setupUITest(t)

	// Wait for at least one machine to register (the tsnet client)
	var machineID uint64
	var originalName string
	require.Eventually(t, func() bool {
		nodesResp, err := fixture.testEnv.GetHeadscaleClient().ListNodes(fixture.ctx, &headscale.ListNodesRequest{})
		if err != nil || len(nodesResp.Nodes) == 0 {
			return false
		}
		// Get the first machine
		machineID = nodesResp.Nodes[0].Id
		originalName = nodesResp.Nodes[0].GivenName
		return true
	}, 10*time.Second, 500*time.Millisecond, "Timeout waiting for machine to register")

	t.Logf("Original machine name: %s (ID: %d)", originalName, machineID)

	// Navigate to machines page with screenshot on failure
	page := SetupPageWithScreenshot(t, fixture.browser, fixture.serverURL+"/machines")

	// Open dropdown menu, click "Rename machine", and wait for modal
	OpenAndClickDropdownItem(t, page,
		`[data-testid="machine-menu-button"]`,
		`[data-testid="machine-menu-rename"]`,
		`#renameMachineModal`)

	// Wait for JavaScript htmx.process() to complete by checking the attribute is set
	require.Eventually(t, func() bool {
		form, err := page.Element(`#renameMachineForm`)
		if err != nil || form == nil {
			return false
		}
		hxPost, err := form.Attribute("hx-post")
		if err != nil {
			return false
		}
		return hxPost != nil && *hxPost != ""
	}, 5*time.Second, 100*time.Millisecond, "Form hx-post attribute should be set by JavaScript")

	// Fill in the new name - query fresh from page to avoid stale references
	newName := "renamed-test-machine"
	page.MustElement(`#renameMachineNewName`).MustSelectAllText().MustInput(newName)

	// Submit the form and wait for modal to close
	ClickAndWaitForModalClose(t, page, `#renameMachineModal button[type="submit"]`, `#renameMachineModal`)

	t.Log("Modal closed, HTMX request completed")

	// Wait for the page to update with new content
	page.MustWaitLoad()

	// Verify the machine was renamed via API
	require.Eventually(t, func() bool {
		nodesResp, err := fixture.testEnv.GetHeadscaleClient().ListNodes(fixture.ctx, &headscale.ListNodesRequest{})
		if err != nil {
			return false
		}
		require.NotEmpty(t, nodesResp.Nodes, "Should have at least one machine")

		// Find our machine and verify the name changed
		var foundMachine *headscale.Node
		for _, node := range nodesResp.Nodes {
			if node.Id == machineID {
				foundMachine = node
				break
			}
		}
		if foundMachine == nil || foundMachine.GivenName != newName {
			return false
		}

		// Also verify in the UI - the page should show the new name
		pageText := page.MustElement("body").MustText()
		return strings.Contains(pageText, newName)
	}, 15*time.Second, 200*time.Millisecond, "Machine should be renamed in Headscale")

	t.Logf("✓ Machine successfully renamed from '%s' to '%s'", originalName, newName)
}

// TestMachinesDropdownClickOutside_UI tests that clicking outside the dropdown closes it
func TestMachinesDropdownClickOutside_UI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping UI test in short mode")
	}
	t.Parallel()

	fixture := setupUITest(t)

	// Wait for at least one machine to register (the tsnet client)
	require.Eventually(t, func() bool {
		nodesResp, err := fixture.testEnv.GetHeadscaleClient().ListNodes(fixture.ctx, &headscale.ListNodesRequest{})
		return err == nil && len(nodesResp.Nodes) > 0
	}, 10*time.Second, 500*time.Millisecond, "Timeout waiting for machine to register")

	// Navigate to machines page with screenshot on failure
	page := SetupPageWithScreenshot(t, fixture.browser, fixture.serverURL+"/machines")

	// The details element that contains the menu button - use selector, not stale reference
	detailsSelector := `details:has([data-testid="machine-menu-button"])`

	// Verify the dropdown is initially closed
	require.False(t, IsDropdownOpen(page, detailsSelector), "Dropdown should be initially closed")

	// Click the menu button to open it (retry for SSE updates)
	require.Eventually(t, func() bool {
		ClickElement(t, page, `[data-testid="machine-menu-button"]`)
		// Give a small moment for the dropdown to open
		time.Sleep(100 * time.Millisecond)
		return IsDropdownOpen(page, detailsSelector)
	}, 5*time.Second, 200*time.Millisecond, "Should be able to open dropdown")

	// Click outside the dropdown (click on the page header)
	ClickElement(t, page, "h1")

	// Verify the dropdown is now closed
	WaitForDropdownClosed(t, page, detailsSelector, 5*time.Second)

	t.Log("✓ Machines dropdown closes when clicking outside")
}

// TestExitNodeApproval_UI tests the exit node approval/rejection functionality end-to-end
func TestExitNodeApproval_UI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping UI test in short mode")
	}
	t.Parallel()

	fixture := setupUITest(t)

	// Start a tailscale client that will advertise as an exit node
	err := fixture.testEnv.StartTailscaleClient(t, "exit-node-test")
	require.NoError(t, err)

	// Wait for the machine to register
	var machineID uint64
	require.Eventually(t, func() bool {
		nodesResp, err := fixture.testEnv.GetHeadscaleClient().ListNodes(fixture.ctx, &headscale.ListNodesRequest{})
		if err != nil || len(nodesResp.Nodes) == 0 {
			return false
		}
		// Find our exit-node-test machine
		for _, node := range nodesResp.Nodes {
			if node.GivenName == "exit-node-test" {
				machineID = node.Id
				return true
			}
		}
		return false
	}, 30*time.Second, 500*time.Millisecond, "Timeout waiting for exit-node-test machine to register")

	t.Logf("Machine registered with ID: %d", machineID)

	// Get the container resource for the tailscale client
	var clientContainer *dockertest.Resource
	for _, container := range fixture.testEnv.tailscaleClients {
		if container != nil {
			clientContainer = container
			break
		}
	}
	require.NotNil(t, clientContainer, "Should have a tailscale client container")

	// Advertise as exit node using tailscale set command
	t.Log("Advertising exit node capability...")
	exitCode, err := clientContainer.Exec([]string{"tailscale", "set", "--advertise-exit-node"}, dockertest.ExecOptions{})
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "tailscale set command should succeed")

	// Wait for routes to be advertised
	require.Eventually(t, func() bool {
		nodeResp, err := fixture.testEnv.GetHeadscaleClient().GetNode(fixture.ctx, &headscale.GetNodeRequest{
			NodeId: machineID,
		})
		if err != nil {
			return false
		}
		// Check if exit node routes are in AvailableRoutes
		for _, route := range nodeResp.Node.AvailableRoutes {
			if route == "0.0.0.0/0" || route == "::/0" {
				t.Logf("Exit node routes advertised: %v", nodeResp.Node.AvailableRoutes)
				return true
			}
		}
		return false
	}, 10*time.Second, 500*time.Millisecond, "Timeout waiting for exit node routes to be advertised")

	// Navigate to machine detail page
	page := fixture.browser.MustPage(fixture.serverURL + "/machines/" + fmt.Sprint(machineID))
	defer page.MustClose()
	page.MustWaitLoad()

	// Verify "Awaiting approval" status is shown
	page.MustElement("body").MustText()
	require.Contains(t, page.MustElement("body").MustText(), "Awaiting approval", "Should show awaiting approval status")

	// Click "Approve" button (find it by the form action)
	t.Log("Clicking Approve button...")
	approveForm := page.MustElement(`form[action*="/routes/exit-node/approve"]`)
	approveButton := approveForm.MustElement(`button[type="submit"]`)
	wait := page.MustWaitNavigation()
	approveButton.MustClick()
	wait()

	// Wait for page to reload
	page.MustWaitLoad()

	// Verify exit node was approved via API
	nodeResp, err := fixture.testEnv.GetHeadscaleClient().GetNode(fixture.ctx, &headscale.GetNodeRequest{
		NodeId: machineID,
	})
	require.NoError(t, err)

	hasExitNodeApproved := false
	for _, route := range nodeResp.Node.ApprovedRoutes {
		if route == "0.0.0.0/0" || route == "::/0" {
			hasExitNodeApproved = true
			break
		}
	}
	require.True(t, hasExitNodeApproved, "Exit node routes should be approved in Headscale")

	// Verify "Allowed" status is shown in UI
	require.Contains(t, page.MustElement("body").MustText(), "Allowed", "Should show allowed status after approval")

	t.Log("✓ Exit node approved successfully")

	// Now test rejection
	t.Log("Clicking Reject button...")
	rejectForm := page.MustElement(`form[action*="/routes/exit-node/reject"]`)
	rejectButton := rejectForm.MustElement(`button[type="submit"]`)
	wait = page.MustWaitNavigation()
	rejectButton.MustClick()
	wait()

	// Wait for page to reload
	page.MustWaitLoad()

	// Verify exit node was rejected via API
	nodeResp, err = fixture.testEnv.GetHeadscaleClient().GetNode(fixture.ctx, &headscale.GetNodeRequest{
		NodeId: machineID,
	})
	require.NoError(t, err)

	hasExitNodeApproved = false
	for _, route := range nodeResp.Node.ApprovedRoutes {
		if route == "0.0.0.0/0" || route == "::/0" {
			hasExitNodeApproved = true
			break
		}
	}
	require.False(t, hasExitNodeApproved, "Exit node routes should be rejected in Headscale")

	// Verify "Awaiting approval" status is shown again
	require.Contains(t, page.MustElement("body").MustText(), "Awaiting approval", "Should show awaiting approval status after rejection")

	t.Log("✓ Exit node rejected successfully")
}

// TestSubnetRouteApproval_UI tests the subnet route approval/rejection functionality end-to-end
func TestSubnetRouteApproval_UI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping UI test in short mode")
	}
	t.Parallel()

	fixture := setupUITest(t)

	// Start a tailscale client that will advertise subnet routes
	err := fixture.testEnv.StartTailscaleClient(t, "subnet-router-test")
	require.NoError(t, err)

	// Wait for the machine to register
	var machineID uint64
	require.Eventually(t, func() bool {
		nodesResp, err := fixture.testEnv.GetHeadscaleClient().ListNodes(fixture.ctx, &headscale.ListNodesRequest{})
		if err != nil || len(nodesResp.Nodes) == 0 {
			return false
		}
		// Find our subnet-router-test machine
		for _, node := range nodesResp.Nodes {
			if node.GivenName == "subnet-router-test" {
				machineID = node.Id
				return true
			}
		}
		return false
	}, 30*time.Second, 500*time.Millisecond, "Timeout waiting for subnet-router-test machine to register")

	t.Logf("Machine registered with ID: %d", machineID)

	// Get the container resource for the tailscale client
	var clientContainer *dockertest.Resource
	for i := len(fixture.testEnv.tailscaleClients) - 1; i >= 0; i-- {
		container := fixture.testEnv.tailscaleClients[i]
		if container != nil {
			clientContainer = container
			break
		}
	}
	require.NotNil(t, clientContainer, "Should have a tailscale client container")

	// Advertise subnet routes using tailscale set command
	t.Log("Advertising subnet routes...")
	exitCode, err := clientContainer.Exec([]string{"tailscale", "set", "--advertise-routes=10.0.0.0/24,192.168.1.0/24"}, dockertest.ExecOptions{})
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "tailscale set command should succeed")

	// Wait for routes to be advertised
	require.Eventually(t, func() bool {
		nodeResp, err := fixture.testEnv.GetHeadscaleClient().GetNode(fixture.ctx, &headscale.GetNodeRequest{
			NodeId: machineID,
		})
		if err != nil {
			return false
		}
		// Check if subnet routes are in AvailableRoutes
		hasRoutes := false
		for _, route := range nodeResp.Node.AvailableRoutes {
			if route == "10.0.0.0/24" || route == "192.168.1.0/24" {
				hasRoutes = true
			}
		}
		if hasRoutes {
			t.Logf("Subnet routes advertised: %v", nodeResp.Node.AvailableRoutes)
			return true
		}
		return false
	}, 10*time.Second, 500*time.Millisecond, "Timeout waiting for subnet routes to be advertised")

	// Navigate to machine detail page
	page := fixture.browser.MustPage(fixture.serverURL + "/machines/" + fmt.Sprint(machineID))
	defer page.MustClose()
	page.MustWaitLoad()

	// Verify routes are shown in "Awaiting Approval" section
	bodyText := page.MustElement("body").MustText()
	require.Contains(t, bodyText, "10.0.0.0/24", "Should show first subnet route")
	require.Contains(t, bodyText, "192.168.1.0/24", "Should show second subnet route")

	// Find and click "Approve" button for the first route (10.0.0.0/24)
	t.Log("Approving first subnet route...")

	// Find all forms with action containing "/routes/subnets/approve"
	approveForms := page.MustElements(`form[action*="/routes/subnets/approve"]`)
	require.NotEmpty(t, approveForms, "Should have approve forms")

	// Click the first approve button
	firstApproveButton := approveForms[0].MustElement(`button[type="submit"]`)
	wait := page.MustWaitNavigation()
	firstApproveButton.MustClick()
	wait()

	// Wait for page to reload
	page.MustWaitLoad()

	// Verify one route was approved via API
	nodeResp, err := fixture.testEnv.GetHeadscaleClient().GetNode(fixture.ctx, &headscale.GetNodeRequest{
		NodeId: machineID,
	})
	require.NoError(t, err)

	require.Len(t, nodeResp.Node.ApprovedRoutes, 1, "Should have one approved route")
	require.Contains(t, nodeResp.Node.ApprovedRoutes, "10.0.0.0/24", "First route should be approved")

	t.Log("✓ First subnet route approved successfully")

	// Approve the second route
	t.Log("Approving second subnet route...")
	approveForms = page.MustElements(`form[action*="/routes/subnets/approve"]`)
	require.NotEmpty(t, approveForms, "Should still have approve forms for second route")

	secondApproveButton := approveForms[0].MustElement(`button[type="submit"]`)
	wait = page.MustWaitNavigation()
	secondApproveButton.MustClick()
	wait()

	// Wait for page to reload
	page.MustWaitLoad()

	// Verify both routes are now approved via API
	nodeResp, err = fixture.testEnv.GetHeadscaleClient().GetNode(fixture.ctx, &headscale.GetNodeRequest{
		NodeId: machineID,
	})
	require.NoError(t, err)

	require.Len(t, nodeResp.Node.ApprovedRoutes, 2, "Should have two approved routes")
	require.Contains(t, nodeResp.Node.ApprovedRoutes, "10.0.0.0/24", "First route should be approved")
	require.Contains(t, nodeResp.Node.ApprovedRoutes, "192.168.1.0/24", "Second route should be approved")

	t.Log("✓ Second subnet route approved successfully")

	// Now test rejection - reject the first route
	t.Log("Rejecting first subnet route...")
	rejectForms := page.MustElements(`form[action*="/routes/subnets/reject"]`)
	require.NotEmpty(t, rejectForms, "Should have reject forms")

	// Find the reject button for 10.0.0.0/24 (should be in the "Approved" section now)
	var firstRejectForm *rod.Element
	for _, form := range rejectForms {
		routeInput := form.MustElement(`input[name="route"]`)
		routeValue := routeInput.MustProperty("value").String()
		if routeValue == "10.0.0.0/24" {
			firstRejectForm = form
			break
		}
	}
	require.NotNil(t, firstRejectForm, "Should find reject form for first route")

	firstRejectButton := firstRejectForm.MustElement(`button[type="submit"]`)
	wait = page.MustWaitNavigation()
	firstRejectButton.MustClick()
	wait()

	// Wait for page to reload
	page.MustWaitLoad()

	// Verify first route was rejected via API
	nodeResp, err = fixture.testEnv.GetHeadscaleClient().GetNode(fixture.ctx, &headscale.GetNodeRequest{
		NodeId: machineID,
	})
	require.NoError(t, err)

	require.Len(t, nodeResp.Node.ApprovedRoutes, 1, "Should have one approved route remaining")
	require.Contains(t, nodeResp.Node.ApprovedRoutes, "192.168.1.0/24", "Second route should still be approved")
	require.NotContains(t, nodeResp.Node.ApprovedRoutes, "10.0.0.0/24", "First route should be rejected")

	t.Log("✓ First subnet route rejected successfully")
}

// TestMoveMachine_UI tests the move machine functionality end-to-end
func TestMoveMachine_UI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping UI test in short mode")
	}
	t.Parallel()

	fixture := setupUITest(t)

	// Create a second user to move the machine to
	createUserResp, err := fixture.testEnv.GetHeadscaleClient().CreateUser(fixture.ctx, &headscale.CreateUserRequest{
		Name: "targetuser",
	})
	require.NoError(t, err)
	targetUser := createUserResp.User
	t.Logf("Created target user: %s (ID: %d)", targetUser.Name, targetUser.Id)

	// Wait for at least one machine to exist (from the test setup)
	require.Eventually(t, func() bool {
		nodesResp, err := fixture.testEnv.GetHeadscaleClient().ListNodes(fixture.ctx, &headscale.ListNodesRequest{
			User: "testuser",
		})
		return err == nil && len(nodesResp.Nodes) > 0
	}, 60*time.Second, 500*time.Millisecond, "Timeout waiting for test machine to register")

	// Get the machine ID and current owner
	nodesResp, err := fixture.testEnv.GetHeadscaleClient().ListNodes(fixture.ctx, &headscale.ListNodesRequest{
		User: "testuser",
	})
	require.NoError(t, err)
	require.NotEmpty(t, nodesResp.Nodes, "Should have at least one machine")
	machine := nodesResp.Nodes[0]
	machineID := machine.Id
	originalOwner := machine.User.Name
	t.Logf("Machine ID: %d, Current owner: %s", machineID, originalOwner)

	// Navigate to machines page with screenshot on failure
	page := SetupPageWithScreenshot(t, fixture.browser, fixture.serverURL+"/machines")

	// Open the dropdown menu, click "Move to user", and wait for modal
	OpenAndClickDropdownItem(t, page,
		`[data-testid="machine-menu-button"]`,
		`[data-testid="machine-menu-move"]`,
		`[data-testid="move-modal"]`)
	moveModal := page.MustElement(`[data-testid="move-modal"]`)

	// Verify the current user is displayed
	currentUserField := moveModal.MustElement(`#moveMachineCurrentUser`)
	currentUserValue := currentUserField.MustProperty("value").String()
	require.Equal(t, originalOwner, currentUserValue, "Current user should match original owner")

	// Select the target user from dropdown by username (visible text)
	targetUserDropdown := moveModal.MustElement(`[data-testid="move-target-user"]`)
	targetUserDropdown.MustSelect(targetUser.Name)

	// Submit the form - HTMX will handle the request and swap the body
	submitButton := moveModal.MustElement(`[data-testid="move-submit"]`)
	submitButton.MustClick()

	// Wait for the modal to close (HTMX success handler closes it)
	// and for the page content to be updated with the new owner
	WaitForElementToDisappear(t, page, `dialog[open]`, "", 15*time.Second)

	// Verify the machine was moved via API
	require.Eventually(t, func() bool {
		nodeResp, err := fixture.testEnv.GetHeadscaleClient().GetNode(fixture.ctx, &headscale.GetNodeRequest{
			NodeId: machineID,
		})
		if err != nil {
			return false
		}
		return targetUser.Id == nodeResp.Node.User.Id && targetUser.Name == nodeResp.Node.User.Name
	}, 15*time.Second, 200*time.Millisecond, "Machine should be moved to target user")

	t.Logf("✓ Machine successfully moved from '%s' to '%s'", originalOwner, targetUser.Name)
}

// TestManageTags_UI tests the tag management functionality end-to-end
func TestManageTags_UI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping UI test in short mode")
	}
	t.Parallel()

	fixture := setupUITest(t)

	// Wait for at least one machine to exist (from the test setup)
	require.Eventually(t, func() bool {
		nodesResp, err := fixture.testEnv.GetHeadscaleClient().ListNodes(fixture.ctx, &headscale.ListNodesRequest{
			User: "testuser",
		})
		return err == nil && len(nodesResp.Nodes) > 0
	}, 60*time.Second, 500*time.Millisecond, "Timeout waiting for test machine to register")

	// Get the machine ID
	nodesResp, err := fixture.testEnv.GetHeadscaleClient().ListNodes(fixture.ctx, &headscale.ListNodesRequest{
		User: "testuser",
	})
	require.NoError(t, err)
	require.NotEmpty(t, nodesResp.Nodes, "Should have at least one machine")
	machine := nodesResp.Nodes[0]
	machineID := machine.Id
	t.Logf("Machine ID: %d", machineID)

	// Navigate to machines page with screenshot on failure
	page := SetupPageWithScreenshot(t, fixture.browser, fixture.serverURL+"/machines")

	// Open the dropdown menu, click "Manage tags", and wait for modal
	OpenAndClickDropdownItem(t, page,
		`[data-testid="machine-menu-button"]`,
		`[data-testid="machine-menu-tags"]`,
		`[data-testid="tags-modal"]`)
	tagsModal := page.MustElement(`[data-testid="tags-modal"]`)

	// Enter tags (comma-separated)
	tagsInput := tagsModal.MustElement(`[data-testid="tags-input"]`)
	testTags := "tag:production, tag:web, tag:critical"
	tagsInput.MustSelectAllText().MustInput(testTags)

	// Submit the form - HTMX will handle the request and swap the body
	submitButton := tagsModal.MustElement(`[data-testid="tags-submit"]`)
	submitButton.MustClick()

	// Wait for the modal to close (HTMX success handler closes it)
	WaitForElementToDisappear(t, page, `dialog[open]`, "", 15*time.Second)

	// Verify the tags were set via API - use Eventually to handle async persistence
	expectedTags := sets.FromSlice([]string{"tag:production", "tag:web", "tag:critical"})
	require.Eventually(t, func() bool {
		nodeResp, err := fixture.testEnv.GetHeadscaleClient().GetNode(fixture.ctx, &headscale.GetNodeRequest{
			NodeId: machineID,
		})
		if err != nil {
			return false
		}
		t.Logf("Tags: %v", nodeResp.Node.ForcedTags)
		return len(nodeResp.Node.ForcedTags) == 3 && expectedTags.EqualsSlice(nodeResp.Node.ForcedTags)
	}, 10*time.Second, 100*time.Millisecond, "Tags should be set correctly")

	// Get final state for logging
	nodeResp, err := fixture.testEnv.GetHeadscaleClient().GetNode(fixture.ctx, &headscale.GetNodeRequest{
		NodeId: machineID,
	})
	require.NoError(t, err)
	t.Logf("✓ Tags successfully set: %v", nodeResp.Node.ForcedTags)

	// Test clearing tags - reload the page first to get fresh state
	page.MustNavigate(fixture.serverURL + "/machines")
	page.MustWaitLoad()

	// Open the dropdown menu, click "Manage tags" again, and wait for modal
	OpenAndClickDropdownItem(t, page,
		`[data-testid="machine-menu-button"]`,
		`[data-testid="machine-menu-tags"]`,
		`[data-testid="tags-modal"]`)
	tagsModal = page.MustElement(`[data-testid="tags-modal"]`)

	// Clear all tags
	tagsInput = tagsModal.MustElement(`[data-testid="tags-input"]`)
	tagsInput.MustSelectAllText().MustInput("") // Clear input

	// Submit the form - HTMX will handle the request
	submitButton = tagsModal.MustElement(`[data-testid="tags-submit"]`)
	submitButton.MustClick()

	// Wait for the modal to close
	WaitForElementToDisappear(t, page, `dialog[open]`, "", 15*time.Second)

	// Verify the tags were cleared via API (don't wait for page load, just check API)
	require.Eventually(t, func() bool {
		nodeResp, err = fixture.testEnv.GetHeadscaleClient().GetNode(fixture.ctx, &headscale.GetNodeRequest{
			NodeId: machineID,
		})
		if err != nil {
			return false
		}
		return len(nodeResp.Node.ForcedTags) == 0
	}, 5*time.Second, 100*time.Millisecond, "Tags should be cleared")

	t.Log("✓ Tags successfully cleared")
}

// setupBrowser creates and configures a Rod browser for testing
func setupBrowser(t *testing.T, ctx context.Context) *rod.Browser {
	t.Helper()

	l := launcher.New().Headless(true).UserDataDir(filepath.Join(t.TempDir(), "browser"))
	// Disable sandbox in CI environments (GitHub Actions sets CI=true)
	if os.Getenv("CI") != "" {
		l = l.NoSandbox(true)
	}
	t.Cleanup(l.Cleanup)

	url := l.MustLaunch()
	browser := rod.New().ControlURL(url).MustConnect().Context(ctx)

	return browser
}

// startTestServer starts an HTTP server for UI testing
func startTestServer(t *testing.T, testEnv *TestEnv) (*httptest.Server, string) {
	t.Helper()

	headscaleClient := testEnv.GetHeadscaleClient()

	// Create tsnet client
	tsnetSrv := &tsnet.Server{
		Hostname:   "hsadmin-ui-test",
		Ephemeral:  true,
		AuthKey:    testEnv.PreAuthKey,
		ControlURL: testEnv.HeadscaleURL,
		Dir:        filepath.Join(t.TempDir(), "tsnet"),
	}

	t.Cleanup(func() {
		tsnetSrv.Close()
	})

	localClient, err := tsnetSrv.LocalClient()
	require.NoError(t, err, "Failed to create tsnet LocalClient")

	// Parse templates
	repoRoot, err := findRepoRoot()
	require.NoError(t, err, "Failed to find repo root")

	templatesPath := filepath.Join(repoRoot, "web", "templates", "*.html")
	funcMap := template.FuncMap{
		"sub": func(a, b int) int { return a - b },
		"mul": func(a, b float64) float64 { return a * b },
	}
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseGlob(templatesPath))

	// Create handlers
	machinesHandler := handlers.NewMachinesHandler(tmpl, headscaleClient, localClient)
	machineActionsHandler := handlers.NewMachineActionsHandler(tmpl, headscaleClient, localClient)
	usersHandler := handlers.NewUsersHandler(tmpl, headscaleClient, localClient, machinesHandler)

	// Setup SSE (matching main.go)
	broker := events.NewBroker()
	t.Cleanup(func() { broker.Close() })

	sseHandler := handlers.NewSSEHandler(tmpl, headscaleClient, localClient, broker, machinesHandler, usersHandler)

	// Start SSE polling loop
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go sseHandler.StartPolling(ctx)

	// Setup routes
	mux := http.NewServeMux()

	// Serve static files from filesystem (matching main.go's /static/ route)
	staticPath := filepath.Join(repoRoot, "web", "static")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticPath))))

	handlers.SetupRoutes(mux, machinesHandler, machineActionsHandler, usersHandler, sseHandler)

	// Create test server
	server := httptest.NewServer(mux)

	return server, server.URL
}

func TestDeleteMachine_UI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping UI test in short mode")
	}
	t.Parallel()

	fixture := setupUITest(t)

	// Create a separate test machine that we can safely delete
	err := fixture.testEnv.StartTailscaleClient(t, "test-machine-to-delete")
	require.NoError(t, err)

	// Wait for the new machine to register
	var machineID uint64
	var machineName string
	require.Eventually(t, func() bool {
		nodesResp, err := fixture.testEnv.GetHeadscaleClient().ListNodes(fixture.ctx, &headscale.ListNodesRequest{})
		if err != nil {
			return false
		}
		// Find the machine we just created
		for _, node := range nodesResp.Nodes {
			if node.GivenName == "test-machine-to-delete" {
				machineID = node.Id
				machineName = node.GivenName
				return true
			}
		}
		return false
	}, 60*time.Second, 500*time.Millisecond, "Timeout waiting for test machine to register")

	t.Logf("Machine to delete: %s (ID: %d)", machineName, machineID)

	// Navigate to machines page with screenshot on failure
	page := SetupPageWithScreenshot(t, fixture.browser, fixture.serverURL+"/machines")

	// Open the dropdown menu, click "Delete machine", and wait for modal
	OpenAndClickDropdownItemInRowByText(t, page, machineName,
		`[data-testid="machine-menu-button"]`,
		`[data-testid="machine-menu-delete"]`,
		`#deleteMachineModal`)
	deleteModal := page.MustElement(`#deleteMachineModal`)

	// Verify the modal shows the correct machine name
	machineNameField := deleteModal.MustElement(`#deleteMachineName`)
	actualName := machineNameField.MustProperty("value").String()
	require.Equal(t, machineName, actualName, "Modal should display the correct machine name")

	// Verify the warning message is present
	warningText := deleteModal.MustHTML()
	require.Contains(t, warningText, "cannot be undone", "Modal should show warning about permanent deletion")
	require.Contains(t, warningText, "permanently deleted", "Modal should mention permanent deletion")

	// Wait for JavaScript htmx.process() to complete by checking the attribute is set
	require.Eventually(t, func() bool {
		form, err := page.Element(`#deleteMachineForm`)
		if err != nil || form == nil {
			return false
		}
		hxPost, err := form.Attribute("hx-post")
		if err != nil {
			return false
		}
		expectedURL := "/machines/" + fmt.Sprint(machineID) + "/delete"
		return hxPost != nil && *hxPost == expectedURL
	}, 5*time.Second, 100*time.Millisecond, "Form hx-post attribute should be set to correct URL by JavaScript")

	// Verify the delete button is styled dangerously (red)
	submitButton := page.MustElement(`[data-testid="delete-submit"]`)
	require.Contains(t, submitButton.MustHTML(), "bg-red-600", "Delete button should have red background")

	// Click the "Delete Machine" button to actually delete it
	submitButton.MustClick()

	// Wait for the redirect back to machines list
	WaitForURL(t, page, fixture.serverURL+"/machines", 15*time.Second)

	// Verify the machine was actually deleted from Headscale
	require.Eventually(t, func() bool {
		nodesResp, err := fixture.testEnv.GetHeadscaleClient().ListNodes(fixture.ctx, &headscale.ListNodesRequest{})
		if err != nil {
			return false
		}
		// Check that the deleted machine is not in the list
		for _, node := range nodesResp.Nodes {
			if node.Id == machineID {
				return false // Machine still exists, keep waiting
			}
		}
		return true // Machine not found, deletion successful
	}, 15*time.Second, 200*time.Millisecond, "Deleted machine should be removed from Headscale")

	t.Logf("✓ Machine successfully deleted from Headscale (ID: %d)", machineID)
}
