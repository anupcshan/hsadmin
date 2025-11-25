package handlers

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/anupcshan/hsadmin/internal/events"
	"github.com/anupcshan/hsadmin/internal/models"
	"github.com/anupcshan/hsadmin/internal/sets"
	headscale "github.com/juanfont/headscale/gen/go/headscale/v1"
	"tailscale.com/client/local"
)

// MachineState represents the state of a machine for change detection
type MachineState struct {
	ID              uint64
	Online          bool
	ApprovedRoutes  sets.Set[string]
	ExitNodeEnabled bool
	Tags            sets.Set[string]
}

// UserState represents the state of a user for change detection
type UserState struct {
	ID           uint64
	Name         string
	MachineCount int
}

// SSEHandler handles Server-Sent Events for real-time updates
type SSEHandler struct {
	templates       *template.Template
	headscaleClient headscale.HeadscaleServiceClient
	tsnetClient     *local.Client
	broker          *events.Broker
	machinesHandler *MachinesHandler
	usersHandler    *UsersHandler
	pollInterval    time.Duration
}

// NewSSEHandler creates a new SSE handler
func NewSSEHandler(
	tmpl *template.Template,
	hsClient headscale.HeadscaleServiceClient,
	tsClient *local.Client,
	broker *events.Broker,
	machinesHandler *MachinesHandler,
	usersHandler *UsersHandler,
) *SSEHandler {
	return &SSEHandler{
		templates:       tmpl,
		headscaleClient: hsClient,
		tsnetClient:     tsClient,
		broker:          broker,
		machinesHandler: machinesHandler,
		usersHandler:    usersHandler,
		pollInterval:    500 * time.Millisecond,
	}
}

// HandleSSE handles SSE connections
func (h *SSEHandler) HandleSSE(w http.ResponseWriter, r *http.Request) {
	log.Printf("SSE: New client connected from %s", r.RemoteAddr)

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Subscribe to events
	clientChan := h.broker.Subscribe()
	defer func() {
		h.broker.Unsubscribe(clientChan)
		log.Printf("SSE: Client disconnected from %s", r.RemoteAddr)
	}()

	log.Printf("SSE: Client subscribed, total clients: %d", h.broker.ClientCount())

	// Get flusher for streaming
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send initial connection message
	fmt.Fprintf(w, "data: {\"type\":\"connected\"}\n\n")
	flusher.Flush()
	log.Printf("SSE: Sent connection confirmation to client")

	// Stream events to client
	for {
		select {
		case event := <-clientChan:
			log.Printf("SSE: Broadcasting event type=%s to client", event.Type)
			// Send event
			fmt.Fprintf(w, "event: %s\n", event.Type)
			// Split HTML into multiple data lines for proper SSE format
			for _, line := range strings.Split(event.HTML, "\n") {
				fmt.Fprintf(w, "data: %s\n", line)
			}
			fmt.Fprintf(w, "\n")
			flusher.Flush()
			log.Printf("SSE: Event sent and flushed")

		case <-r.Context().Done():
			// Client disconnected
			log.Printf("SSE: Client context done")
			return
		}
	}
}

// StartPolling starts the polling loop for change detection
// State is kept local to this function to prevent any possibility of concurrent access
func (h *SSEHandler) StartPolling(ctx context.Context) {
	ticker := time.NewTicker(h.pollInterval)
	defer ticker.Stop()

	log.Printf("SSE: Starting polling loop (interval: %v)", h.pollInterval)

	// State tracking for change detection - local to this goroutine
	lastMachineStates := make(map[uint64]*MachineState)
	lastUserStates := make(map[uint64]*UserState)

	for {
		select {
		case <-ticker.C:
			// Only poll if there are connected clients
			clientCount := h.broker.ClientCount()
			if clientCount == 0 {
				continue
			}

			// Fetch all data once
			machines, err := h.machinesHandler.FetchMachines(ctx)
			if err != nil {
				log.Printf("SSE: Error fetching machines: %v", err)
				continue
			}

			usersResp, err := h.headscaleClient.ListUsers(ctx, &headscale.ListUsersRequest{})
			if err != nil {
				log.Printf("SSE: Error fetching users: %v", err)
				continue
			}

			// Derive machine counts per user
			machineCounts := countMachinesPerUser(machines)

			// Check for machine changes and update state
			lastMachineStates = h.pollMachineChanges(ctx, machines, usersResp.Users, lastMachineStates)

			// Check for user changes and update state
			lastUserStates = h.pollUserChanges(ctx, usersResp.Users, machineCounts, lastUserStates)

		case <-ctx.Done():
			log.Printf("SSE: Polling loop stopped")
			return
		}
	}
}

// detectMachineChanges compares states and returns true on first change detected
func detectMachineChanges(current, previous map[uint64]*MachineState) bool {
	// Check for new machines
	for id := range current {
		if _, exists := previous[id]; !exists {
			log.Printf("SSE: Machine changes detected (new machine %d)", id)
			return true
		}
	}

	// Check for deleted machines
	for id := range previous {
		if _, exists := current[id]; !exists {
			log.Printf("SSE: Machine changes detected (deleted machine %d)", id)
			return true
		}
	}

	// Check for changes in existing machines
	for id, curr := range current {
		prev := previous[id] // Safe because new machines already returned above

		if curr.Online != prev.Online {
			log.Printf("SSE: Machine changes detected (machine %d online: %v -> %v)", id, prev.Online, curr.Online)
			return true
		}

		if !curr.ApprovedRoutes.Equals(prev.ApprovedRoutes) ||
			curr.ExitNodeEnabled != prev.ExitNodeEnabled {
			log.Printf("SSE: Machine changes detected (machine %d routes changed)", id)
			return true
		}

		if !curr.Tags.Equals(prev.Tags) {
			log.Printf("SSE: Machine changes detected (machine %d tags changed)", id)
			return true
		}
	}

	return false
}

// pollMachineChanges detects changes in machine state and broadcasts updates if changed
// Returns the new state map to be used in the next poll cycle
func (h *SSEHandler) pollMachineChanges(ctx context.Context, machines []*models.Machine, users []*headscale.User, previousStates map[uint64]*MachineState) map[uint64]*MachineState {
	// Build current state map
	currentStates := make(map[uint64]*MachineState)
	for _, m := range machines {
		currentStates[m.ID()] = &MachineState{
			ID:              m.ID(),
			Online:          m.Online,
			ApprovedRoutes:  sets.FromSlice(m.ApprovedSubnets()),
			ExitNodeEnabled: m.ExitNodeStatus() == "Approved",
			Tags:            sets.FromSlice(m.Tags()),
		}
	}

	// Detect and broadcast changes
	if detectMachineChanges(currentStates, previousStates) {
		h.broadcastMachinesTableUpdate(ctx, machines, users)
	}

	return currentStates
}

// detectUserChanges compares states and returns true on first change detected
func detectUserChanges(current, previous map[uint64]*UserState) bool {
	// Check for new or deleted users
	if len(current) != len(previous) {
		log.Printf("SSE: User changes detected (count: %d -> %d)", len(previous), len(current))
		return true
	}

	// Check for changes in existing users
	for id, curr := range current {
		prev, exists := previous[id]
		if !exists || curr.Name != prev.Name || curr.MachineCount != prev.MachineCount {
			log.Printf("SSE: User changes detected (user %d)", id)
			return true
		}
	}

	return false
}

// pollUserChanges detects changes in user state and broadcasts updates if changed
// Returns the new state map to be used in the next poll cycle
func (h *SSEHandler) pollUserChanges(ctx context.Context, users []*headscale.User, machineCounts map[string]int, previousStates map[uint64]*UserState) map[uint64]*UserState {
	// Build current state map
	currentStates := make(map[uint64]*UserState)
	for _, user := range users {
		currentStates[user.Id] = &UserState{
			ID:           user.Id,
			Name:         user.Name,
			MachineCount: machineCounts[user.Name],
		}
	}

	// Detect and broadcast changes
	if detectUserChanges(currentStates, previousStates) {
		h.broadcastUsersTableUpdate(ctx, users)
	}

	return currentStates
}

// broadcastMachinesTableUpdate sends a full machine table update
func (h *SSEHandler) broadcastMachinesTableUpdate(ctx context.Context, machines []*models.Machine, users []*headscale.User) {
	log.Printf("SSE: Broadcasting machines table update (%d machines)", len(machines))

	var buf bytes.Buffer
	data := map[string]interface{}{
		"Machines": machines,
		"Users":    users,
	}

	if err := h.templates.ExecuteTemplate(&buf, "machines-table", data); err != nil {
		log.Printf("SSE: Error rendering machines table: %v", err)
		return
	}

	html := buf.String()
	log.Printf("SSE: Generated HTML update (%d bytes)", len(html))

	h.broker.Broadcast(events.Event{
		Type: "machinesTable",
		HTML: html,
	})

	log.Printf("SSE: Broadcast complete")
}

// broadcastUsersTableUpdate sends a full user table update
func (h *SSEHandler) broadcastUsersTableUpdate(ctx context.Context, users []*headscale.User) {
	log.Printf("SSE: Broadcasting users table update (%d users)", len(users))

	var buf bytes.Buffer
	data := map[string]interface{}{
		"Users": users,
	}

	if err := h.templates.ExecuteTemplate(&buf, "users-table", data); err != nil {
		log.Printf("SSE: Error rendering users table: %v", err)
		return
	}

	html := fmt.Sprintf(`<div id="users-table" hx-swap-oob="true">%s</div>`, buf.String())

	h.broker.Broadcast(events.Event{
		Type: "usersTable",
		HTML: html,
	})
}

// Helper functions

func countMachinesPerUser(machines []*models.Machine) map[string]int {
	counts := make(map[string]int)
	for _, m := range machines {
		counts[m.User()]++
	}
	return counts
}
