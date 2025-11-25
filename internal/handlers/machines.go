package handlers

import (
	"context"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/anupcshan/hsadmin/internal/auth"
	"github.com/anupcshan/hsadmin/internal/models"
	headscale "github.com/juanfont/headscale/gen/go/headscale/v1"
	"tailscale.com/client/local"
)

type MachinesHandler struct {
	templates       *template.Template
	headscaleClient headscale.HeadscaleServiceClient
	tsnetClient     *local.Client
}

func NewMachinesHandler(tmpl *template.Template, hsClient headscale.HeadscaleServiceClient, tsClient *local.Client) *MachinesHandler {
	return &MachinesHandler{
		templates:       tmpl,
		headscaleClient: hsClient,
		tsnetClient:     tsClient,
	}
}

func (h *MachinesHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := strings.ToLower(r.URL.Query().Get("query"))

	// Fetch all machines
	machines, err := h.FetchMachines(ctx)
	if err != nil {
		http.Error(w, "Failed to fetch machines: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Fetch users for the move dropdown
	usersResp, err := h.headscaleClient.ListUsers(ctx, &headscale.ListUsersRequest{})
	if err != nil {
		http.Error(w, "Failed to fetch users: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Filter by search query
	if query != "" {
		filtered := []*models.Machine{}
		for _, m := range machines {
			if strings.Contains(strings.ToLower(m.Hostname()), query) ||
				strings.Contains(strings.ToLower(m.User()), query) ||
				strings.Contains(strings.ToLower(m.PrimaryIP()), query) {
				filtered = append(filtered, m)
			}
		}
		machines = filtered
	}

	data := map[string]interface{}{
		"Active":   "machines",
		"Machines": machines,
		"Users":    usersResp.Users,
	}
	data = auth.AddUserToTemplateData(r, data)

	if err := h.templates.ExecuteTemplate(w, "machines.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// FetchMachines retrieves all machines from Headscale and enriches with tsnet data
//
// Performance note: This function makes WhoIs calls for each machine in a loop.
// This is intentional and acceptable because:
// - Remote API calls are optimized: Single ListNodes() call for all machines (not per-user)
// - WhoIs calls are to the LOCAL tsnet daemon (Unix socket/in-process), not remote Headscale
// - Local WhoIs calls are very cheap (microseconds) and don't require caching
// - WhoIs provides essential data: DERP latency, version info, HostInfo not available elsewhere
func (h *MachinesHandler) FetchMachines(ctx context.Context) ([]*models.Machine, error) {
	// Get all nodes from Headscale (User field omitted = get all)
	nodesResp, err := h.headscaleClient.ListNodes(ctx, &headscale.ListNodesRequest{})
	if err != nil {
		return nil, err
	}

	// Get peer status from tsnet
	status, err := h.tsnetClient.Status(ctx)
	if err != nil {
		return nil, err
	}

	// Collect all machines and enrich with local tsnet data
	var machines []*models.Machine
	for _, node := range nodesResp.Nodes {
		machine := &models.Machine{
			Node:   node,
			Online: node.Online, // Use Headscale's online status
		}

		// Try to find peer status and get WhoIs data
		if len(node.IpAddresses) > 0 {
			nodeIP := node.IpAddresses[0]

			// Check if this is the local tsnet client node
			// Compare against all IPs in status.Self
			isLocalNode := false
			if status.Self != nil {
				for _, selfIP := range status.Self.TailscaleIPs {
					if selfIP.String() == nodeIP {
						isLocalNode = true
						machine.PeerStatus = status.Self

						// WhoIs call to LOCAL tsnet daemon (Unix socket) - very cheap
						if whois, err := h.tsnetClient.WhoIs(ctx, nodeIP); err == nil {
							machine.WhoIsNode = whois.Node
						}
						break
					}
				}
			}

			// If not the local node, check peers
			if !isLocalNode {
				for _, peer := range status.Peer {
					for _, peerIP := range peer.TailscaleIPs {
						if peerIP.String() == nodeIP {
							machine.PeerStatus = peer

							// WhoIs call to LOCAL tsnet daemon (Unix socket) - very cheap
							if whois, err := h.tsnetClient.WhoIs(ctx, nodeIP); err == nil {
								machine.WhoIsNode = whois.Node
							}
							break
						}
					}
				}
			}
		}

		machines = append(machines, machine)
	}

	return machines, nil
}

func (h *MachinesHandler) Detail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract machine ID from URL path
	// Expecting /machines/{id}
	idStr := strings.TrimPrefix(r.URL.Path, "/machines/")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid machine ID", http.StatusBadRequest)
		return
	}

	// Fetch all machines
	machines, err := h.FetchMachines(ctx)
	if err != nil {
		http.Error(w, "Failed to fetch machines: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Find the requested machine
	var machine *models.Machine
	for _, m := range machines {
		if m.ID() == id {
			machine = m
			break
		}
	}

	if machine == nil {
		http.Error(w, "Machine not found", http.StatusNotFound)
		return
	}

	// Fetch DERP map for region name lookups
	derpMap, err := h.tsnetClient.CurrentDERPMap(ctx)
	if err != nil {
		// Log error but continue - we'll show region IDs instead of names
		derpMap = nil
	}

	// Get processed DERP latencies with region names
	processedLatencies := machine.ProcessedDERPLatencies(derpMap)

	data := map[string]interface{}{
		"Active":        "machines",
		"Machine":       machine,
		"DERPLatencies": processedLatencies,
	}
	data = auth.AddUserToTemplateData(r, data)

	if err := h.templates.ExecuteTemplate(w, "machine_detail.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Rename handles POST /machines/{id}/rename - renames a machine
func (h *MachinesHandler) Rename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Extract machine ID from URL path
	// Expecting /machines/{id}/rename
	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/machines/"), "/")
	if len(pathParts) < 2 {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	machineID, err := strconv.ParseUint(pathParts[0], 10, 64)
	if err != nil {
		http.Error(w, "Invalid machine ID: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Parse form data
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	newName := strings.TrimSpace(r.FormValue("new_name"))
	if newName == "" {
		http.Error(w, "New machine name is required", http.StatusBadRequest)
		return
	}

	// Rename machine via Headscale API
	_, err = h.headscaleClient.RenameNode(ctx, &headscale.RenameNodeRequest{
		NodeId:  machineID,
		NewName: newName,
	})
	if err != nil {
		http.Error(w, "Failed to rename machine: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Redirect back to machines list (HTMX will follow)
	http.Redirect(w, r, "/machines", http.StatusSeeOther)
}
