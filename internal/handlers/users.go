package handlers

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/anupcshan/hsadmin/internal/auth"
	"github.com/anupcshan/hsadmin/internal/models"
	headscale "github.com/juanfont/headscale/gen/go/headscale/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
	"tailscale.com/client/local"
)

type UsersHandler struct {
	templates       *template.Template
	headscaleClient headscale.HeadscaleServiceClient
	tsnetClient     *local.Client
	machinesHandler *MachinesHandler
}

func NewUsersHandler(tmpl *template.Template, hsClient headscale.HeadscaleServiceClient, tsClient *local.Client, machinesHandler *MachinesHandler) *UsersHandler {
	return &UsersHandler{
		templates:       tmpl,
		headscaleClient: hsClient,
		tsnetClient:     tsClient,
		machinesHandler: machinesHandler,
	}
}

// List handles GET /users - displays list of users with machine counts
func (h *UsersHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Fetch all users with machine counts
	users, err := h.fetchUsersWithMachineCounts(ctx)
	if err != nil {
		http.Error(w, "Failed to fetch users: "+err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"Active": "users",
		"Users":  users,
	}
	data = auth.AddUserToTemplateData(r, data)

	if err := h.templates.ExecuteTemplate(w, "users_list.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Create handles POST /users - creates a new user
func (h *UsersHandler) Create(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RenderErrorWithStatus(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Parse form data
	if err := r.ParseForm(); err != nil {
		RenderErrorWithStatus(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	userName := strings.TrimSpace(r.FormValue("name"))
	if userName == "" {
		RenderErrorWithStatus(w, "User name is required", http.StatusBadRequest)
		return
	}

	// Create user via Headscale API
	_, err := h.headscaleClient.CreateUser(ctx, &headscale.CreateUserRequest{
		Name: userName,
	})
	if err != nil {
		RenderError(w, "Failed to create user: "+err.Error())
		return
	}

	// Redirect back to users list
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

// Rename handles POST /users/{id}/rename - renames a user
func (h *UsersHandler) Rename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RenderErrorWithStatus(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Extract user ID from URL path
	// Expecting /users/{id}/rename
	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/users/"), "/")
	if len(pathParts) < 2 {
		RenderErrorWithStatus(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	userID, err := parseUserID(pathParts[0])
	if err != nil {
		RenderErrorWithStatus(w, "Invalid user ID: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Parse form data
	if err := r.ParseForm(); err != nil {
		RenderErrorWithStatus(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	newName := strings.TrimSpace(r.FormValue("new_name"))
	if newName == "" {
		RenderErrorWithStatus(w, "New user name is required", http.StatusBadRequest)
		return
	}

	// Rename user via Headscale API
	_, err = h.headscaleClient.RenameUser(ctx, &headscale.RenameUserRequest{
		OldId:   userID,
		NewName: newName,
	})
	if err != nil {
		RenderError(w, "Failed to rename user: "+err.Error())
		return
	}

	// Redirect back to users list (HTMX will follow)
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

// Delete handles DELETE /users/{id} - deletes a user
func (h *UsersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		RenderErrorWithStatus(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Extract user ID from URL path
	// Expecting /users/{id}/delete
	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/users/"), "/")
	if len(pathParts) < 2 {
		RenderErrorWithStatus(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	userID, err := parseUserID(pathParts[0])
	if err != nil {
		RenderErrorWithStatus(w, "Invalid user ID: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Delete user via Headscale API
	_, err = h.headscaleClient.DeleteUser(ctx, &headscale.DeleteUserRequest{
		Id: userID,
	})
	if err != nil {
		// Fetch users and render the page with an error
		users, fetchErr := h.fetchUsersWithMachineCounts(ctx)
		if fetchErr != nil {
			RenderError(w, "Failed to delete user: "+err.Error())
			return
		}

		data := map[string]interface{}{
			"Active":       "users",
			"Users":        users,
			"ErrorMessage": "Failed to delete user: " + err.Error(),
		}
		data = auth.AddUserToTemplateData(r, data)

		if err := h.templates.ExecuteTemplate(w, "users_list.html", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Redirect back to users list (HTMX will follow)
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

// CreatePreAuthKey handles POST /users/{id}/preauth-keys - creates a pre-auth key for a user
func (h *UsersHandler) CreatePreAuthKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RenderErrorWithStatus(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Extract user ID from URL path
	// Expecting /users/{id}/preauth-keys
	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/users/"), "/")
	if len(pathParts) < 2 {
		RenderErrorWithStatus(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	userID, err := parseUserID(pathParts[0])
	if err != nil {
		RenderErrorWithStatus(w, "Invalid user ID: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Parse form data for optional parameters
	if err := r.ParseForm(); err != nil {
		RenderErrorWithStatus(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Get optional parameters
	ephemeral := r.FormValue("ephemeral") == "true"
	reusable := r.FormValue("reusable") == "true"
	expirationHours := r.FormValue("expiration_hours")

	// Default expiration: 1 hour
	expiration := time.Now().Add(1 * time.Hour)
	if expirationHours != "" {
		if hours, err := time.ParseDuration(expirationHours + "h"); err == nil {
			expiration = time.Now().Add(hours)
		}
	}

	// Create pre-auth key via Headscale API
	keyResp, err := h.headscaleClient.CreatePreAuthKey(ctx, &headscale.CreatePreAuthKeyRequest{
		User:       userID,
		Ephemeral:  ephemeral,
		Reusable:   reusable,
		Expiration: timestamppb.New(expiration),
	})
	if err != nil {
		RenderError(w, "Failed to create pre-auth key: "+err.Error())
		return
	}

	// Return the key as HTML fragment for HTMX, JSON, or plain text
	if r.Header.Get("HX-Request") == "true" {
		// HTMX request - return HTML fragment
		w.Header().Set("Content-Type", "text/html")
		html := `<div>
			<label class="block text-sm font-medium text-gray-700 mb-1">Generated Key</label>
			<div class="flex gap-2">
				<input
					type="text"
					id="generatedKey"
					data-testid="preauth-key-output"
					value="` + keyResp.PreAuthKey.Key + `"
					readonly
					class="flex-grow px-4 py-2 border border-gray-300 rounded-md bg-gray-50 font-mono text-sm">
				<button
					type="button"
					onclick="copyToClipboard('` + keyResp.PreAuthKey.Key + `', this)"
					class="px-3 py-2 bg-gray-100 text-gray-700 rounded-md hover:bg-gray-200">
					Copy
				</button>
			</div>
		</div>`
		w.Write([]byte(html))
	} else if strings.Contains(r.Header.Get("Accept"), "application/json") {
		// JSON request
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"key":"` + keyResp.PreAuthKey.Key + `"}`))
	} else {
		// Plain text request (backward compatibility)
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(keyResp.PreAuthKey.Key))
	}
}

// fetchUsersWithMachineCounts retrieves all users and enriches them with machine data
// Uses MachinesHandler.FetchMachines to avoid N+1 queries (shared optimization with SSE handler)
func (h *UsersHandler) fetchUsersWithMachineCounts(ctx context.Context) ([]*models.User, error) {
	// Get all users from Headscale
	usersResp, err := h.headscaleClient.ListUsers(ctx, &headscale.ListUsersRequest{})
	if err != nil {
		return nil, err
	}

	// Get all machines in a single optimized call (shared with SSE handler)
	machines, err := h.machinesHandler.FetchMachines(ctx)
	if err != nil {
		return nil, err
	}

	// Group machines by user name for efficient lookup
	machinesByUser := make(map[string][]*models.Machine)
	for _, m := range machines {
		userName := m.User()
		machinesByUser[userName] = append(machinesByUser[userName], m)
	}

	// Build user list with machine counts and last seen info
	var users []*models.User
	for _, hsUser := range usersResp.Users {
		user := &models.User{
			HeadscaleUser:       hsUser,
			MachineCount:        0,
			LastSeenTime:        nil,
			HasConnectedMachine: false,
		}

		// Get machines for this user from the grouped map
		userMachines := machinesByUser[hsUser.Name]
		user.MachineCount = len(userMachines)

		// Find most recent activity across all user's machines
		for _, machine := range userMachines {
			// Check if this machine is online
			if machine.Online {
				user.HasConnectedMachine = true
			}

			// Track most recent LastSeen time
			if machine.Node != nil && machine.Node.LastSeen != nil {
				lastSeenTime := machine.Node.LastSeen.AsTime()
				if user.LastSeenTime == nil || lastSeenTime.After(*user.LastSeenTime) {
					user.LastSeenTime = &lastSeenTime
				}
			}
		}

		users = append(users, user)
	}

	return users, nil
}

// parseUserID parses a user ID string to uint64
func parseUserID(idStr string) (uint64, error) {
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid user ID format: %w", err)
	}
	return id, nil
}
