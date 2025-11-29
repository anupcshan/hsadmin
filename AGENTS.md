# AGENTS.md - Guide for AI Agents Working on hsadmin

## Project Overview

**hsadmin** is a web-based admin interface for [Headscale](https://github.com/juanfont/headscale) (self-hosted Tailscale control server). The goal is to closely replicate the Tailscale admin console UI/UX.

**Tech Stack:**
- **Backend**: Go (standard library focused)
- **Templates**: Go `html/template` (no frontend framework)
- **Frontend**: HTMX for dynamic updates, Tailwind CSS (CDN) for styling
- **Network**: Runs as a tsnet node within the Tailscale network
- **API**: Headscale gRPC API + Tailscale LocalClient

**Project Status:** ~72% complete - functional for basic Headscale administration (view machines, manage users, approve routes, set tags).

## Essential Commands

### Build and Run

```bash
# Run the application (requires config file)
go run main.go -config hsadmin.yaml

# Build binary
go build -o hsadmin main.go

# Run with custom config
./hsadmin -config /path/to/config.yaml
```

**Config file format** (`hsadmin.yaml`):
```yaml
headscale:
  agent_userid: <user_id>              # REQUIRED: Headscale user ID for hsadmin agent (must be > 0)
  api_hostport: <host:port>            # REQUIRED: Headscale gRPC API endpoint (e.g., localhost:50443)
  api_key: <api_key>                   # REQUIRED: Headscale API key
  server_url: <https://url>            # REQUIRED: Headscale control server URL (must include http:// or https://)
  agent_tags: ["tag:hsadmin"]          # Optional: tags for hsadmin node
```

**Note**: All required fields are validated on startup. If any field is missing or invalid, the application will exit with a clear error message indicating which field needs to be fixed.

### Testing

```bash
# Run all tests (includes integration tests - requires Docker)
go test ./...

# Run only fast unit tests (skip integration tests)
go test -short ./...

# Run integration tests specifically
go test ./test/integration

# Update golden files after intentional UI changes
go test ./test/integration -update

# Run specific test
go test ./test/integration -run TestMachinesList_Golden

# Run with verbose output
go test -v ./test/integration
```

**Testing Requirements:**
- Docker daemon must be running for integration tests
- Integration tests automatically start Headscale containers
- Golden file tests compare normalized HTML output

### Development Workflow

```bash
# 1. Make code changes

# 2. Run unit tests (fast feedback)
go test -short ./...

# 3. Run integration tests (comprehensive)
go test ./test/integration

# 4. If you changed UI templates, update golden files
go test ./test/integration -update
git diff test/integration/testdata/  # Review HTML changes

# 5. Run the app locally to manually verify
go run main.go -config hsadmin.yaml
```

## Project Structure

```
/main.go                              # Entry point - sets up gRPC, tsnet, HTTP server
/hsadmin.yaml                         # Config file (not in git - user-specific)

/internal/
  /config/                            # YAML config loading
    config.go                         # Config struct and Load() function
  /models/                            # Data models with helper methods
    machine.go                        # Machine model (combines Headscale + tsnet data)
    machine_test.go                   # Unit tests for complex model logic
    user.go                           # User model
  /format/                            # Formatting utilities
    time.go                           # Timestamp formatting (LastSeenShort, etc.)
  /sets/                              # Generic set implementation
    set.go                            # Set[T comparable] interface and impl
  /handlers/                          # HTTP handlers (one file per domain)
    machines.go                       # Machine list, detail, rename
    machine_actions.go                # Route management (approve/reject routes)
    users.go                          # User CRUD, pre-auth keys
    alerts.go                         # Alert/toast notification rendering
  /testutil/                          # Test utilities
    normalize.go                      # HTML normalization for golden files

/web/
  /templates/                         # Go html/templates
    layout.html                       # Base layout (nav, styles, scripts)
    machines.html                     # Machine list view
    machine_detail.html               # Machine detail view (single page)
    users_list.html                   # User list view with modals

/test/
  /integration/                       # Integration tests
    testenv.go                        # Dockertest setup (Headscale containers)
    golden_test.go                    # Golden file tests (HTML snapshots)
    ui_test.go                        # Browser automation tests (Rod)
    README.md                         # Integration testing guide
    /config/                          # Test configs
      headscale.yaml                  # Embedded in testenv.go
    /testdata/                        # Golden file snapshots
      machines_list.golden.html       # Expected HTML for machines list
      machine_detail.golden.html      # Expected HTML for machine detail
      users_list.golden.html          # Expected HTML for users list
```

## Code Organization Patterns

### Handler Pattern

All handlers follow this structure:

```go
type MachinesHandler struct {
    templates       *template.Template
    headscaleClient headscale.HeadscaleServiceClient
    tsnetClient     *tailscale.LocalClient
}

func NewMachinesHandler(tmpl *template.Template, hsClient headscale.HeadscaleServiceClient, tsClient *tailscale.LocalClient) *MachinesHandler {
    return &MachinesHandler{...}
}

func (h *MachinesHandler) List(w http.ResponseWriter, r *http.Request) {
    // 1. Extract parameters from request
    // 2. Fetch data from Headscale API and/or tsnet
    // 3. Process data (filter, transform)
    // 4. Execute template with data
}
```

**Handlers are organized by domain:**
- `machines.go` - Machine viewing and basic operations
- `machine_actions.go` - Route management (approve/reject)
- `users.go` - User management and pre-auth keys
- `alerts.go` - Alert rendering utilities

### Data Model Pattern

The `Machine` model combines data from three sources:

```go
type Machine struct {
    Node       *headscale.Node      // From Headscale API (basic node info)
    PeerStatus *ipnstate.PeerStatus // From tsnet Status() (online/offline, connection type)
    WhoIsNode  *tailcfg.Node        // From tsnet WhoIs() (HostInfo, DERP latency, version)
    Online     bool
}
```

**Helper methods** provide clean access to data:
- Basic info: `Hostname()`, `ID()`, `User()`, `OS()`
- Timestamps: `Created()`, `LastSeenShort()`, `LastSeenFull()`
- Network: `TailscaleIPs()`, `PrimaryIP()`, `ConnectionType()`
- Routing: `ApprovedSubnets()`, `AdvertisedSubnets()`, `ExitNodeStatus()`
- DERP: `ProcessedDERPLatencies()` (complex logic - see tests)

**Key principles:**
- Models never do I/O - they only transform data
- Nil checks everywhere (peer might not be online)
- Complex logic (DERP latency, route filtering) lives in models, not templates
- Unit tests for complex methods only (not simple getters)

### Template Pattern

Templates use Go's `html/template` with HTMX for interactivity:

```html
<!-- Search with HTMX (auto-submit on keyup) -->
<input type="search"
       name="query"
       hx-get="/machines"
       hx-trigger="keyup changed delay:300ms"
       hx-target="#machines-list">

<!-- Dropdown menu (native HTML details/summary) -->
<details class="...">
    <summary data-testid="user-menu-button">⋮</summary>
    <div data-testid="user-menu-dropdown">
        <button>Action 1</button>
        <button>Action 2</button>
    </div>
</details>

<!-- Modal dialog (native HTML dialog element) -->
<dialog id="rename-modal">
    <form hx-post="/users/123/rename" hx-target="#alerts">
        <input name="new_name" required>
        <button type="submit">Rename</button>
    </form>
</dialog>
```

**Template conventions:**
- Use `data-testid` attributes for UI testing
- HTMX attributes for dynamic behavior
- Native HTML elements (details/summary for dropdowns, dialog for modals)
- Dark theme matching Tailscale's design
- Tailwind CSS classes for styling

### Testing Pattern

**Three types of tests:**

1. **Unit tests** (`internal/models/machine_test.go`):
   - Test complex logic only (DERP latency parsing, route filtering)
   - Skip simple getters and formatters
   - Fast, no external dependencies

2. **Golden file tests** (`test/integration/golden_test.go`):
   - Capture expected HTML output
   - Normalize dynamic content (timestamps, IPs, latencies)
   - Test against multiple Headscale versions
   - Catch UI regressions

3. **Browser automation tests** (`test/integration/ui_test.go`):
   - Use Rod (headless Chrome) for JavaScript interactions
   - Test dropdowns, modals, click-outside-to-close
   - Verify API calls were made correctly
   - Most comprehensive but slowest

**Golden file workflow:**
```bash
# Make UI changes
vim web/templates/machines.html

# Update golden files
go test ./test/integration -update

# Review what changed
git diff test/integration/testdata/

# If correct, commit
git add test/integration/testdata/
git commit -m "Update UI layout"
```

## Naming Conventions

### File naming
- Lowercase with underscores: `machine_actions.go`, `users_list.html`
- Test files: `*_test.go`
- Golden files: `*.golden.html`

### Go code
- Exported types/functions: `PascalCase`
- Unexported types/functions: `camelCase`
- Package names: lowercase single word (`config`, `handlers`, `models`)
- No stuttering: `models.Machine`, not `models.MachineModel`

### Templates
- Template names match filenames: `machines.html`, `machine_detail.html`
- Reusable components prefixed with `layout-`: `layout-header`, `layout-styles`
- Template data uses `map[string]interface{}` with capitalized keys: `"Machines"`, `"Users"`, `"Active"`

### HTTP endpoints
- RESTful-ish but not strict:
  - `GET /machines` - list
  - `GET /machines/:id` - detail
  - `POST /machines/:id/rename` - action
  - `POST /machines/:id/routes/exit-node/approve` - nested action
  - `POST /users` - create
  - `POST /users/:id/delete` - action (not DELETE verb)

### Test IDs
- `data-testid` uses kebab-case: `user-menu-button`, `machine-detail-header`
- Format: `{domain}-{component}-{element}`

## Important Patterns and Gotchas

### 1. Machine Data Fetching

Machines require data from multiple sources:

```go
// In handlers/machines.go
func (h *MachinesHandler) fetchMachines(ctx context.Context) ([]*models.Machine, error) {
    // 1. Get all users from Headscale
    usersResp, err := h.headscaleClient.ListUsers(ctx, &headscale.ListUsersRequest{})

    // 2. Get peer status from tsnet (who's online)
    status, err := h.tsnetClient.Status(ctx)

    // 3. For each user, get their nodes
    for _, user := range usersResp.Users {
        nodesResp, err := h.headscaleClient.ListNodes(ctx, &headscale.ListNodesRequest{
            User: user.Name,
        })

        // 4. For each node, get detailed info via WhoIs
        for _, node := range nodesResp.Nodes {
            machine := &models.Machine{Node: node, Online: false}

            // Match with peer status
            for _, peer := range status.Peer {
                if matches(peer, node) {
                    machine.PeerStatus = peer
                    machine.Online = true

                    // Get detailed WhoIs data
                    whoIs, _ := h.tsnetClient.WhoIs(ctx, peer.TailscaleIPs[0])
                    machine.WhoIsNode = whoIs.Node
                }
            }
        }
    }
}
```

**Why so complex?** Headscale API doesn't provide online status or DERP latency - must use tsnet LocalClient.

### 2. Route Management Logic

Exit nodes and subnet routes are stored together but displayed separately:

```go
// Approve exit node: add 0.0.0.0/0 and ::/0 to approved routes
// BUT preserve existing subnet routes
func (h *MachineActionsHandler) ApproveExitNode(w http.ResponseWriter, r *http.Request) {
    // 1. Get current node
    node, _ := h.headscaleClient.GetNode(ctx, &headscale.GetNodeRequest{NodeId: id})

    // 2. Keep existing subnet routes (non-exit-node routes)
    approvedRoutes := []string{}
    for _, route := range node.ApprovedRoutes {
        if route != "0.0.0.0/0" && route != "::/0" {
            approvedRoutes = append(approvedRoutes, route)
        }
    }

    // 3. Add exit node routes
    for _, route := range node.AvailableRoutes {
        if route == "0.0.0.0/0" || route == "::/0" {
            approvedRoutes = append(approvedRoutes, route)
        }
    }

    // 4. Update approved routes
    h.headscaleClient.SetApprovedRoutes(ctx, &headscale.SetApprovedRoutesRequest{
        NodeId: id,
        Routes: approvedRoutes,
    })
}
```

**Gotcha:** Rejecting exit node means removing 0.0.0.0/0 and ::/0 from approved routes, but keeping subnet routes.

### 3. DERP Latency Processing

DERP latencies come in a weird format and need processing:

```go
// WhoIs returns: DERPLatency = map[string]float64{"10-v4": 23.5, "10-v6": 25.1, "17-v4": 45.2}
// Must parse region IDs, group by region, choose min latency, sort, mark preferred

func (m *Machine) ProcessedDERPLatencies() []DERPRegionLatency {
    // See internal/models/machine.go and machine_test.go for full logic
    // 1. Parse "10-v4" to extract region ID 10
    // 2. Group by region, take min(v4, v6)
    // 3. Sort by latency
    // 4. Mark preferred (first in list)
}
```

**Complex logic is unit tested** - see `internal/models/machine_test.go`.

### 4. Template Execution

Templates are parsed once at startup:

```go
// main.go
funcMap := template.FuncMap{
    "sub": func(a, b int) int { return a - b },
    "mul": func(a, b float64) float64 { return a * b },
}
tmpl := template.Must(template.New("").Funcs(funcMap).ParseGlob("web/templates/*.html"))
```

**When editing templates:**
- Restart the server to see changes
- Or use a file watcher (not currently implemented)
- Golden tests will catch template errors

### 5. HTMX Patterns

Common HTMX patterns used:

```html
<!-- Auto-submit form on input change -->
<input hx-get="/machines" hx-trigger="keyup changed delay:300ms">

<!-- Replace content after POST -->
<button hx-post="/users/123/rename" hx-target="#alerts" hx-swap="innerHTML">

<!-- Out-of-band swap for notifications -->
<div id="alerts" hx-swap-oob="true">
    <div class="alert">Success!</div>
</div>

<!-- Redirect after action (using HTTP 303) -->
http.Redirect(w, r, "/machines/123", http.StatusSeeOther)
```

### 6. Test Normalization

Golden files normalize dynamic content to prevent false failures:

```go
// internal/testutil/normalize.go
func NormalizeHTML(html string) string {
    // Timestamps → "TIMESTAMP"
    // "5 minutes ago" → "TIME_AGO"
    // "123ms" → "XXms"
    // "100.64.0.1" → "100.64.X.X"
    // "fd7a:..." → "fd7a:XXXX:XXXX"
    // "nodekey:..." → "nodekey:XXXX"
}
```

**When golden tests fail:**
1. Check if your change was intentional
2. If yes, run `go test ./test/integration -update`
3. Review the diff carefully
4. Commit updated golden files

### 7. Integration Test Environment

Tests automatically manage Headscale containers:

```go
// test/integration/testenv.go
func SetupTestEnv(t *testing.T, headscaleVersion string) *TestEnv {
    // 1. Start Headscale container with dockertest
    // 2. Wait for gRPC to be ready
    // 3. Bootstrap API key and test user
    // 4. Return TestEnv with clients
    // Cleanup happens automatically via t.Cleanup()
}
```

**Test versions:** Tests run against multiple Headscale versions defined in `versions` array.

**Adding containers:** Use `env.AddTailscaleClient()` for testing routes.

## Key Dependencies

### Production

- `tailscale.com/tsnet` - Tailscale network integration
- `github.com/juanfont/headscale/gen/go/headscale/v1` - Headscale gRPC API
- `google.golang.org/grpc` - gRPC client
- `gopkg.in/yaml.v3` - YAML config parsing

### Testing

- `github.com/stretchr/testify` - Test assertions (`require.NoError`, etc.)
- `github.com/ory/dockertest/v3` - Docker container management for tests
- `github.com/go-rod/rod` - Browser automation for UI tests

### Frontend (CDN)

- HTMX 1.9.x - Dynamic HTML updates
- Tailwind CSS 3.x - Styling
- No build step, no npm, no webpack

## Common Tasks

### Adding a New Page

1. **Create template** in `web/templates/`:
```html
<!-- web/templates/my_page.html -->
{{template "layout.html" .}}
{{define "content"}}
    <h1>My Page</h1>
    <!-- content here -->
{{end}}
```

2. **Create handler** in `internal/handlers/`:
```go
// internal/handlers/my_page.go
func (h *MyHandler) Show(w http.ResponseWriter, r *http.Request) {
    data := map[string]interface{}{
        "Active": "my_page",  // For nav highlighting
        "Items": fetchItems(),
    }
    h.templates.ExecuteTemplate(w, "my_page.html", data)
}
```

3. **Add route** in `main.go`:
```go
myHandler := handlers.NewMyHandler(tmpl, headscaleClient, localClient)
mux.HandleFunc("/my-page", myHandler.Show)
```

4. **Add nav item** in `web/templates/layout.html`:
```html
<a href="/my-page" class="{{if eq .Active "my_page"}}active{{end}}">
    My Page
</a>
```

5. **Add golden test** in `test/integration/golden_test.go`

### Adding a Model Method

1. **Add method** in `internal/models/machine.go`:
```go
func (m *Machine) MyMethod() string {
    if m.Node == nil {
        return ""
    }
    // logic here
}
```

2. **If complex logic**, add unit test in `internal/models/machine_test.go`:
```go
func TestMachine_MyMethod(t *testing.T) {
    tests := []struct {
        name string
        machine *Machine
        want string
    }{
        // test cases
    }
    // ...
}
```

3. **Use in templates**:
```html
<div>{{.Machine.MyMethod}}</div>
```

### Adding a Machine Action

1. **Add handler** in `internal/handlers/machine_actions.go`:
```go
func (h *MachineActionsHandler) MyAction(w http.ResponseWriter, r *http.Request) {
    machineID, _ := extractMachineID(r.URL.Path)
    // Call Headscale API
    // Redirect back
    http.Redirect(w, r, "/machines/"+strconv.FormatUint(machineID, 10), http.StatusSeeOther)
}
```

2. **Add route** in `main.go`:
```go
mux.HandleFunc("/machines/", func(w http.ResponseWriter, r *http.Request) {
    if strings.HasSuffix(path, "/my-action") {
        machineActionsHandler.MyAction(w, r)
    }
    // ...
})
```

3. **Add UI** in `web/templates/machine_detail.html` or `machines.html`:
```html
<form hx-post="/machines/{{.Machine.ID}}/my-action" hx-target="#alerts">
    <button type="submit">Do Action</button>
</form>
```

4. **Add browser automation test** in `test/integration/ui_test.go`

## Testing Strategy

See [TESTING_STRATEGY.md](TESTING_STRATEGY.md) for full details.

**Key points:**
- Unit tests for complex model logic only
- Golden files for HTML regression testing
- Browser automation (Rod) for JavaScript interactions
- Integration tests use real Headscale (no mocks)
- Test against multiple Headscale versions

**When to write tests:**
- Complex logic (DERP parsing, route filtering) → Unit test
- UI changes → Update golden files
- Interactive features (dropdowns, modals) → Browser automation test
- Simple getters and formatters → No test needed

## Development Guidelines

### Code Style

- **Keep it simple:** Standard library over dependencies
- **No clever code:** Explicit is better than implicit
- **Nil checks everywhere:** Network data can be missing
- **Early returns:** Reduce nesting
- **Handler → Model → Template:** Keep logic in models, not handlers

### Error Handling

```go
// Handlers: return HTTP errors
if err != nil {
    http.Error(w, "Failed to fetch data: "+err.Error(), http.StatusInternalServerError)
    return
}

// Models: return empty/default values (no I/O, no errors)
func (m *Machine) Hostname() string {
    if m.Node == nil {
        return "-"
    }
    return m.Node.Name
}
```

### Template Data

Always use `map[string]interface{}` with clear keys:

```go
data := map[string]interface{}{
    "Active":   "machines",     // Current page for nav
    "Machines": machines,        // Main content
    "Users":    users,           // Dropdown data
    "Query":    searchQuery,     // Search state
}
```

### HTMX Responses

```go
// Success with redirect
http.Redirect(w, r, "/machines/123", http.StatusSeeOther)

// Success with notification
w.Header().Set("HX-Trigger", "notification")
// Or render alert template with hx-swap-oob

// Error
http.Error(w, "Failed: "+err.Error(), http.StatusBadRequest)
```

### Go Best Practices

**Always follow these Go practices before committing:**

```bash
# Format all Go code (required before commit)
go fmt ./...

# Run static analysis to catch common mistakes
go vet ./...

# Run comprehensive linting (uses go tool to ensure consistent version)
go tool golangci-lint run

# Run tests with race detector (critical for concurrent code)
go test -race -short ./...
go test -race ./test/integration
```

**Why these matter:**
- `go fmt` ensures consistent code style across the project
- `go vet` catches common mistakes like printf format mismatches, unreachable code, etc.
- `go tool golangci-lint` runs multiple linters in parallel (gofmt, govet, staticcheck, etc.) - catches more issues than go vet alone. Using `go tool` ensures consistent version across all developers without requiring separate installation.
- `-race` flag detects race conditions in concurrent code (tsnet and gRPC are concurrent)

**Race detector is especially important** because:
- This project uses concurrent code (gRPC, HTTP server, tsnet)
- Race conditions can cause subtle bugs that only appear in production
- The `-race` flag adds minimal overhead during testing

### Testing Changes

**Before committing:**
1. Format code: `go fmt ./...`
2. Run static analysis: `go vet ./...`
3. Run linting: `go tool golangci-lint run`
4. Run unit tests with race detection: `go test -race -short ./...`
5. Run integration tests with race detection: `go test -race ./test/integration`
6. If templates changed, update golden files: `go test ./test/integration -update`
7. Manually test in browser
8. Review golden file diffs

## Documentation

- **IMPLEMENTATION.md** - Project roadmap, feature tracking, architecture
- **TESTING_STRATEGY.md** - Testing philosophy and approach
- **test/integration/README.md** - Integration testing guide
- **AGENTS.md** (this file) - Day-to-day development guide

## Troubleshooting

### "Template not found"

- Check template name matches filename: `machines.html`
- Check template is parsed: `template.ParseGlob("web/templates/*.html")`
- Restart server after template changes

### Integration tests fail

- Check Docker daemon is running: `docker ps`
- Check ports available: 18080, 50443, 19090
- Check test output in `.testoutput/` directory
- Try running single test: `go test -v ./test/integration -run TestName`

### Golden files fail

- If intentional change: `go test ./test/integration -update`
- Review diff: `git diff test/integration/testdata/`
- Check normalization didn't break: view raw golden file
- Try regenerating all golden files

### HTMX not working

- Check browser console for errors
- Check HTMX attributes are correct: `hx-get`, `hx-post`, `hx-target`
- Check server returns correct content type: `text/html`
- Check HTMX CDN loaded in `layout.html`

### Machine data missing

- Check tsnet client is connected
- Check WhoIs was called successfully
- Check nil checks in model methods
- Some fields only available when machine is online

## Future Work

See [IMPLEMENTATION.md](IMPLEMENTATION.md) for roadmap. Key missing features:

- **Node expiration and deletion** (Phase 6)
- **ACL editor** (Phase 7)
- **Real-time updates via SSE** (Phase 8)
- **Loading states and polish** (Phase 10)

## Getting Help

1. Check existing docs: IMPLEMENTATION.md, TESTING_STRATEGY.md
2. Read similar code (handlers, models, tests)
3. Check git history: `git log --follow <file>`
4. Look at Headscale API docs: https://github.com/juanfont/headscale
5. Look at Tailscale LocalClient docs: https://pkg.go.dev/tailscale.com

## Quick Reference

```bash
# Run app
go run main.go -config hsadmin.yaml

# Format code (always run before commit)
go fmt ./...

# Static analysis (always run before commit)
go vet ./...

# Linting (recommended before commit)
go tool golangci-lint run

# Test everything
go test ./...

# Test with race detection (recommended before commit)
go test -race -short ./...
go test -race ./test/integration

# Test fast (unit only)
go test -short ./...

# Test integration only
go test ./test/integration

# Update golden files
go test ./test/integration -update

# Run specific test
go test -v ./test/integration -run TestMachinesList

# Build
go build -o hsadmin main.go

# Run built binary
./hsadmin -config hsadmin.yaml
```

**Key files to know:**
- `main.go` - Entry point, server setup
- `internal/handlers/machines.go` - Machine list and detail
- `internal/models/machine.go` - Machine data model
- `web/templates/layout.html` - Base layout
- `test/integration/testenv.go` - Test environment setup

**When making changes:**
1. Understand existing patterns
2. Follow naming conventions
3. Add tests if needed
4. Update golden files if UI changed
5. Test manually in browser
6. Update IMPLEMENTATION.md if new feature

---

**Remember:** This is a Go project with minimal dependencies. Keep it simple, follow existing patterns, and test against real Headscale.
