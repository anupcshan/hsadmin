package handlers

import (
	"html/template"
	"net/http"
	"strconv"
	"strings"

	headscale "github.com/juanfont/headscale/gen/go/headscale/v1"
	"tailscale.com/client/local"
)

type MachineActionsHandler struct {
	templates       *template.Template
	headscaleClient headscale.HeadscaleServiceClient
	tsnetClient     *local.Client
}

func NewMachineActionsHandler(tmpl *template.Template, hsClient headscale.HeadscaleServiceClient, tsClient *local.Client) *MachineActionsHandler {
	return &MachineActionsHandler{
		templates:       tmpl,
		headscaleClient: hsClient,
		tsnetClient:     tsClient,
	}
}

// ApproveExitNode handles POST /machines/{id}/routes/exit-node/approve
func (h *MachineActionsHandler) ApproveExitNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Extract machine ID from URL path
	machineID, err := extractMachineID(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid machine ID: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Get current node to fetch existing routes
	nodeResp, err := h.headscaleClient.GetNode(ctx, &headscale.GetNodeRequest{
		NodeId: machineID,
	})
	if err != nil {
		http.Error(w, "Failed to fetch node: "+err.Error(), http.StatusInternalServerError)
		return
	}

	node := nodeResp.Node

	// Build new approved routes list by adding exit node routes to existing approved routes
	approvedRoutes := make([]string, 0)

	// Add existing approved subnet routes (non-exit-node routes)
	for _, route := range node.ApprovedRoutes {
		if route != "0.0.0.0/0" && route != "::/0" {
			approvedRoutes = append(approvedRoutes, route)
		}
	}

	// Add exit node routes from available routes
	exitNodeFound := false
	for _, route := range node.AvailableRoutes {
		if route == "0.0.0.0/0" || route == "::/0" {
			approvedRoutes = append(approvedRoutes, route)
			exitNodeFound = true
		}
	}

	if !exitNodeFound {
		http.Error(w, "No exit node routes found to approve", http.StatusBadRequest)
		return
	}

	// Set approved routes via Headscale API
	_, err = h.headscaleClient.SetApprovedRoutes(ctx, &headscale.SetApprovedRoutesRequest{
		NodeId: machineID,
		Routes: approvedRoutes,
	})
	if err != nil {
		http.Error(w, "Failed to approve exit node: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Redirect back to machine detail page
	http.Redirect(w, r, "/machines/"+strconv.FormatUint(machineID, 10), http.StatusSeeOther)
}

// RejectExitNode handles POST /machines/{id}/routes/exit-node/reject
func (h *MachineActionsHandler) RejectExitNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Extract machine ID from URL path
	machineID, err := extractMachineID(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid machine ID: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Get current node to fetch existing routes
	nodeResp, err := h.headscaleClient.GetNode(ctx, &headscale.GetNodeRequest{
		NodeId: machineID,
	})
	if err != nil {
		http.Error(w, "Failed to fetch node: "+err.Error(), http.StatusInternalServerError)
		return
	}

	node := nodeResp.Node

	// Build new approved routes list by excluding exit node routes
	approvedRoutes := make([]string, 0)
	for _, route := range node.ApprovedRoutes {
		if route != "0.0.0.0/0" && route != "::/0" {
			approvedRoutes = append(approvedRoutes, route)
		}
	}

	// Set approved routes via Headscale API (without exit node routes)
	_, err = h.headscaleClient.SetApprovedRoutes(ctx, &headscale.SetApprovedRoutesRequest{
		NodeId: machineID,
		Routes: approvedRoutes,
	})
	if err != nil {
		http.Error(w, "Failed to reject exit node: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Redirect back to machine detail page
	http.Redirect(w, r, "/machines/"+strconv.FormatUint(machineID, 10), http.StatusSeeOther)
}

// ApproveSubnetRoute handles POST /machines/{id}/routes/subnets/approve
func (h *MachineActionsHandler) ApproveSubnetRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Extract machine ID from URL path
	machineID, err := extractMachineID(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid machine ID: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Parse form data to get the route
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	route := r.FormValue("route")
	if route == "" {
		http.Error(w, "Route parameter is required", http.StatusBadRequest)
		return
	}

	// Get current node to fetch existing routes
	nodeResp, err := h.headscaleClient.GetNode(ctx, &headscale.GetNodeRequest{
		NodeId: machineID,
	})
	if err != nil {
		http.Error(w, "Failed to fetch node: "+err.Error(), http.StatusInternalServerError)
		return
	}

	node := nodeResp.Node

	// Build new approved routes list by adding the subnet route
	approvedRoutes := make([]string, 0)

	// Add all existing approved routes
	approvedRoutes = append(approvedRoutes, node.ApprovedRoutes...)

	// Check if the route is in available routes and not already approved
	routeFound := false
	for _, r := range node.AvailableRoutes {
		if r == route {
			routeFound = true
			// Add it if not already in approved routes
			alreadyApproved := false
			for _, ar := range node.ApprovedRoutes {
				if ar == route {
					alreadyApproved = true
					break
				}
			}
			if !alreadyApproved {
				approvedRoutes = append(approvedRoutes, route)
			}
			break
		}
	}

	if !routeFound {
		http.Error(w, "Route not found in available routes", http.StatusBadRequest)
		return
	}

	// Set approved routes via Headscale API
	_, err = h.headscaleClient.SetApprovedRoutes(ctx, &headscale.SetApprovedRoutesRequest{
		NodeId: machineID,
		Routes: approvedRoutes,
	})
	if err != nil {
		http.Error(w, "Failed to approve subnet route: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Redirect back to machine detail page
	http.Redirect(w, r, "/machines/"+strconv.FormatUint(machineID, 10), http.StatusSeeOther)
}

// RejectSubnetRoute handles POST /machines/{id}/routes/subnets/reject
func (h *MachineActionsHandler) RejectSubnetRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Extract machine ID from URL path
	machineID, err := extractMachineID(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid machine ID: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Parse form data to get the route
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	route := r.FormValue("route")
	if route == "" {
		http.Error(w, "Route parameter is required", http.StatusBadRequest)
		return
	}

	// Get current node to fetch existing routes
	nodeResp, err := h.headscaleClient.GetNode(ctx, &headscale.GetNodeRequest{
		NodeId: machineID,
	})
	if err != nil {
		http.Error(w, "Failed to fetch node: "+err.Error(), http.StatusInternalServerError)
		return
	}

	node := nodeResp.Node

	// Build new approved routes list by excluding the specified subnet route
	approvedRoutes := make([]string, 0)
	for _, r := range node.ApprovedRoutes {
		if r != route {
			approvedRoutes = append(approvedRoutes, r)
		}
	}

	// Set approved routes via Headscale API (without the rejected route)
	_, err = h.headscaleClient.SetApprovedRoutes(ctx, &headscale.SetApprovedRoutesRequest{
		NodeId: machineID,
		Routes: approvedRoutes,
	})
	if err != nil {
		http.Error(w, "Failed to reject subnet route: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Redirect back to machine detail page
	http.Redirect(w, r, "/machines/"+strconv.FormatUint(machineID, 10), http.StatusSeeOther)
}

// MoveNode handles POST /machines/{id}/move - moves a machine to a different user
func (h *MachineActionsHandler) MoveNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Extract machine ID from URL path
	machineID, err := extractMachineID(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid machine ID: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Parse form data to get the target user ID
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	targetUserIDStr := strings.TrimSpace(r.FormValue("target_user"))
	if targetUserIDStr == "" {
		http.Error(w, "Target user is required", http.StatusBadRequest)
		return
	}

	targetUserID, err := strconv.ParseUint(targetUserIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid user ID: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Move node via Headscale API
	_, err = h.headscaleClient.MoveNode(ctx, &headscale.MoveNodeRequest{
		NodeId: machineID,
		User:   targetUserID,
	})
	if err != nil {
		http.Error(w, "Failed to move node: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Redirect back to machines list
	http.Redirect(w, r, "/machines", http.StatusSeeOther)
}

// SetTags handles POST /machines/{id}/tags - sets tags on a machine
func (h *MachineActionsHandler) SetTags(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Extract machine ID from URL path
	machineID, err := extractMachineID(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid machine ID: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Parse form data to get the tags
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	tagsInput := strings.TrimSpace(r.FormValue("tags"))

	// Parse comma-separated tags into array
	// Initialize with empty slice (not nil) to properly clear tags
	tags := []string{}
	if tagsInput != "" {
		// Split by comma and trim whitespace from each tag
		for _, tag := range strings.Split(tagsInput, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				tags = append(tags, tag)
			}
		}
	}

	// Set tags via Headscale API
	_, err = h.headscaleClient.SetTags(ctx, &headscale.SetTagsRequest{
		NodeId: machineID,
		Tags:   tags,
	})
	if err != nil {
		http.Error(w, "Failed to set tags: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Redirect back to machines list
	http.Redirect(w, r, "/machines", http.StatusSeeOther)
}

// DeleteNode handles POST /machines/{id}/delete - permanently deletes a machine
func (h *MachineActionsHandler) DeleteNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Extract machine ID from URL path
	machineID, err := extractMachineID(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid machine ID: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Delete node via Headscale API
	_, err = h.headscaleClient.DeleteNode(ctx, &headscale.DeleteNodeRequest{
		NodeId: machineID,
	})
	if err != nil {
		http.Error(w, "Failed to delete node: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Redirect back to machines list
	http.Redirect(w, r, "/machines", http.StatusSeeOther)
}

// ExpireNode handles POST /machines/{id}/expire - expires a machine's key
func (h *MachineActionsHandler) ExpireNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Extract machine ID from URL path
	machineID, err := extractMachineID(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid machine ID: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Expire node via Headscale API
	_, err = h.headscaleClient.ExpireNode(ctx, &headscale.ExpireNodeRequest{
		NodeId: machineID,
	})
	if err != nil {
		http.Error(w, "Failed to expire node: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Redirect back to machines list
	http.Redirect(w, r, "/machines", http.StatusSeeOther)
}

// Helper function to extract machine ID from URL path
func extractMachineID(path string) (uint64, error) {
	// Path format: /machines/{id}/routes/exit-node/approve or reject
	// or /machines/{id}/routes/subnets/approve or reject
	// or /machines/{id}/move
	// or /machines/{id}/tags
	// or /machines/{id}/delete
	// or /machines/{id}/expire
	parts := strings.Split(strings.TrimPrefix(path, "/machines/"), "/")
	if len(parts) < 1 {
		return 0, http.ErrNotSupported
	}
	return strconv.ParseUint(parts[0], 10, 64)
}
