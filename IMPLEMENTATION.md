# Headscale Admin Dashboard - Implementation Plan

## Project Overview
Build a full-featured web admin interface for Headscale (self-hosted Tailscale control server) that **closely replicates the Tailscale admin console UI/UX**. Built with Go, standard Go `html/template`, HTMX, and Server-Sent Events for real-time updates. The application runs as a tsnet node within the Tailscale network.

## Progress Summary

**Overall Completion:** ~87% complete (9/11 phases complete, 1 partial, 1 not started)

| Phase | Status | Completion |
|-------|--------|------------|
| Phase 1: Core Infrastructure | ✅ Complete | 100% |
| Phase 2: Base Layout & Navigation | ✅ Complete | 100% |
| Phase 3: Machines List View | ✅ Complete | 100% |
| Phase 4: Machine Detail View | ✅ Complete | 100% |
| Phase 5: User Management | ✅ Complete | 100% |
| Phase 6: Node Actions | ✅ Complete | 100% |
| Phase 7: ACL Editor | ⏸️ Not Started | 0% |
| Phase 8: Real-time Updates (SSE) | ✅ Complete | 100% |
| Phase 9: Testing Infrastructure | ✅ Complete | 100% |
| Phase 10: Polish & Production | 🔄 Partial | 30% |
| Phase 11: Authentication & Authorization | ✅ Complete | 100% |

**The application is production-ready** with comprehensive authentication, real-time updates, and full machine/user management. Missing: ACL editor.

## Code Quality & Technical Debt

**Overall Code Quality: A- (Excellent with minor issues)**

The codebase follows Go best practices with clean architecture, comprehensive testing, and proper error handling. A comprehensive code review identified the following items for tracking:

### 🔴 High Priority Issues

| Issue | Location | Status | Notes |
|-------|----------|--------|-------|
| ~~SSE Handler Race Condition~~ | ~~sse.go:47-48~~ | ✅ Fixed | State maps moved to local variables in polling loop - no concurrent access possible |
| ~~WhoIs N+1 Calls~~ | ~~machines.go:107,123~~ | ✅ Documented | WhoIs calls are to local tsnet daemon (Unix socket/in-process), not remote - very cheap, documented in code |
| ~~N+1 Query Pattern (Users)~~ | ~~users.go:276-320~~ | ✅ Fixed | Refactored to use shared MachinesHandler.FetchMachines() - reduces API calls and deduplicates code |
| ~~Config Validation Missing~~ | ~~config.go:20-32~~ | ✅ Fixed | Added comprehensive validation with clear error messages - validates all required fields on startup |

### 🟡 Medium Priority Issues

| Issue | Location | Status | Notes |
|-------|----------|--------|-------|
| Aggressive SSE Polling | sse.go:67 | 📝 Needs Discussion | 500ms polling - already optimized in Phase 8, may be intentional |
| Missing Context Timeouts | main.go:64, machines.go:80 | ⏸️ To Fix | API calls should use context.WithTimeout |
| Code Duplication | machine_actions.go | ⏸️ To Fix | Similar patterns across action handlers |
| Regex Compilation | testutil/normalize.go | ⏸️ To Fix | Compile regexes at package init for performance |

### 🟢 Low Priority Issues

| Issue | Location | Status | Notes |
|-------|----------|--------|-------|
| ~~Inefficient Bubble Sort~~ | ~~machine.go:277-284~~ | ✅ Fixed | Replaced with idiomatic sort.Slice - more efficient and cleaner |
| Startup Initialization Structure | main.go:64-73 | 📋 Future | Not a real leak (log.Fatal kills process), but should structure initialization better and limit log.Fatal calls |
| Route Matching Robustness | routes.go:18-38 | 📋 Future | Consider proper router library |
| CSRF Protection | handlers/*.go | 📋 Future | Mitigated by Tailscale network, defense-in-depth for later |
| HTML in Go Code | users.go:245-263 | 📋 Future | Move to template partial |

### Legend
- 🔴 High Priority - Should address before production deployment with >50 machines
- 🟡 Medium Priority - Improve over time
- 🟢 Low Priority - Nice to have
- 🔍 Investigating - Verifying if issue exists
- 📝 Needs Discussion - Decision needed on approach
- ⏸️ To Fix - Confirmed issue, needs implementation
- 📋 Future - Tracked for future enhancement

## Current State

### Core Infrastructure ✅
- ✅ Parses YAML configuration for Headscale connection
- ✅ Connects to Headscale gRPC API with TLS and API key authentication
- ✅ Spawns a tsnet agent that joins the network
- ✅ HTTP server with template rendering and routing
- ✅ Data models with comprehensive helper methods (using WhoIs for detailed info)
- ✅ Structured package organization (handlers, models, config, format)

### UI/Styling ✅
- ✅ Dark theme matching Tailscale's visual design
- ✅ Horizontal navigation with active state indicators
- ✅ Reusable layout components (layout-header, layout-styles)
- ✅ Tailwind CSS via CDN
- ✅ Custom CSS for Tailscale-specific styling
- ✅ Copy buttons with JavaScript
- ✅ Status badges for Connected/Offline, Exit Node, Subnets
- ✅ Alert/toast notification system with HTMX out-of-band swaps
- ✅ Dropdown menus with click-outside-to-close functionality
- ✅ Modal dialogs for user actions

### Machines Management ✅
- ✅ Machines list view with status indicators
- ✅ HTMX-powered search (filter by hostname, IP, user)
- ✅ Machine detail view with comprehensive information:
  - All basic machine info (creator, name, OS hostname, OS, version, ID, node key, created, key expiry)
  - ATTRIBUTES section (node configuration details)
  - CLIENT CONNECTIVITY section (network capabilities: hairpinning, IPv6, UDP, UPnP, PCP, NAT-PMP)
  - ADDRESSES section (Tailscale IPs, short/full domains, endpoints)
  - DERP latency with sorted regions and preferred indicator
  - Subnets section (approved and awaiting approval routes with per-route approve/reject buttons)
  - Routing Settings section (exit node status with approve/reject buttons)
- ✅ Rename machine functionality with modal UI
- ✅ Dropdown menu on machine rows with actions
- ✅ Route management functionality:
  - Approve/reject exit node capability
  - Approve/reject individual subnet routes
  - Visual status indicators for route states
- ✅ Machine actions (via dropdown menu):
  - Move node to different user with user selection dropdown
  - Manage tags with comma-separated input (set/edit/clear)

### User Management ✅ (Phase 5 Complete)
- ✅ Users list view with machine counts
- ✅ Create user form with HTMX
- ✅ Rename user with modal dialog and API integration
- ✅ Delete user with confirmation modal and API integration
- ✅ Generate pre-auth keys with options (ephemeral, reusable, expiration)
- ✅ Dropdown menu on user rows with all actions
- ✅ Navigation item for "Users"

### Testing Infrastructure ✅ (Phase 9 Complete)
- ✅ Integration tests with real Headscale using dockertest
- ✅ Support for multiple Headscale versions (0.27.0+)
- ✅ Golden file tests for UI regression detection
- ✅ Browser automation tests (Rod) covering:
  - Dropdown menu interactions
  - Click-outside-to-close behavior
  - User rename with API verification
  - User delete with API verification
  - Pre-auth key generation with API verification
  - Machine rename with API verification
  - Exit node approval/rejection with real Tailscale containers and API verification
  - Subnet route approval/rejection with real Tailscale containers and API verification
  - Move node to different user with API verification
  - Tag management (set/clear tags) with API verification
- ✅ Tailscale client containers for route testing with `tailscale set` commands
- ✅ Parallel test execution
- ✅ Test utilities for normalization and fixtures

### What's Working Now 🚀
The application is **production-ready** for Headscale management:
- **View all machines** with search, status indicators, and detailed information
- **Real-time updates** via Server-Sent Events (500ms polling)
  - Live machine status changes (online/offline)
  - New machines appear automatically
  - Deleted machines disappear automatically
  - Route changes and tag updates reflected immediately
- **Manage users** - create, rename, delete users
- **Generate pre-auth keys** with custom settings (ephemeral, reusable, expiration)
- **Rename machines** via dropdown actions
- **Route management** - approve/reject exit node capability and subnet routes
  - Individual subnet route approval/rejection with per-route buttons
  - Exit node approval/rejection with visual status indicators
- **Machine actions** - move nodes to different users, manage tags, expire keys, delete machines
  - Move node to different user with dropdown selector
  - Set/edit/clear tags with comma-separated input
  - Expire machine keys (forces re-authentication)
  - Delete machines with confirmation modal and permanent deletion warning
- **Dark UI** matching Tailscale's design with responsive layout
- **Comprehensive testing** with golden file tests and browser automation (including SSE, route management and machine actions)

### What's Next 📋

**Priority features for full feature parity:**
1. **ACL Editor** - Web-based policy editor with syntax highlighting (Phase 7 - not started)
2. **Polish & Production** - Loading states, Dockerfile, documentation (Phase 10 - partial)

**Config file structure** (`hsadmin.yaml`):
```yaml
headscale:
  agent_userid: <user_id>
  api_hostport: <host:port>
  api_key: <api_key>
  server_url: <https://url>
  agent_tags: ["tag:hsadmin"]  # Optional

# Auth configuration (Phase 11 - implemented)
auth:
  # WhoIs-based auth (Tailscale network access)
  whois_enabled: true
  admin_user_ids: [1, 5, 10]
  admin_user_tags: ["tag:admin"]  # Optional

  # OIDC auth (external access via Funnel/HTTP)
  oidc_enabled: false
  oidc_provider_url: "https://accounts.google.com"
  oidc_client_id: "your-client-id"
  oidc_client_secret: "your-client-secret"
  oidc_redirect_url: "https://hsadmin.example.com/auth/callback"
  oidc_admin_emails: ["admin@example.com"]
  session_secret: "your-random-secret-here"
  session_duration: 24h
```

See `hsadmin.yaml.example` for a complete configuration example with detailed comments.

## Technology Stack
- **Backend**: Go (stdlib only, no unnecessary dependencies)
- **Templates**: Standard Go `html/template`
- **Frontend**: HTMX for dynamic updates
- **Real-time**: Server-Sent Events (SSE) for live updates
- **Styling**: Tailwind CSS (matching Tailscale's design system)
- **Authentication**: Tailscale network access (tsnet-based)

## UI/UX Design Goal
**Replicate the Tailscale admin console** (https://login.tailscale.com/admin/machines) as closely as possible:
- Clean, modern interface with Tailscale's visual design language
- Similar layout, navigation, and component styling
- Consistent color scheme (Tailscale's blues, grays, status colors)
- Same information hierarchy and organization
- Responsive design matching their mobile/desktop layouts

**Key UI elements to replicate**:
- Top navigation bar with logo/branding
- Horizontal navigation (Machines, Users, Access Controls)
- Machine list view with status indicators
- Search/filter bar
- Machine detail view with comprehensive information
- Status badges and pills
- Loading states and transitions

## Required Features

### 1. Machines List View (Primary Dashboard)
Replicate the Tailscale machines page:
- **List/Grid view toggle** (start with list)
- **Machine cards/rows** showing:
  - Online/offline status (green/gray dot)
  - Hostname (clickable to detail view)
  - User/owner
  - Tailscale IPs (IPv4, IPv6)
  - Last seen (relative time: "5 minutes ago", "Active now")
  - OS icon and version
  - Tags (if any)
  - Three-dot menu for actions (future: disable, delete, etc.)
- **Top bar**:
  - Search box (filter by hostname, IP, user)
  - Filter dropdowns (status, user, tags)
  - Refresh button
  - "Add machine" or relevant actions
- **Real-time updates**: Status changes, new machines, last-seen updates via SSE
- **Sorting**: By hostname, last seen, status, IP

### 2. Machine Detail View (Modal/Slide-over Panel)
When clicking a machine, show detailed view similar to Tailscale's:
- **Overview tab**:
  - Full machine information
  - Status and connection quality
  - IP addresses (Tailscale + external)
  - OS details, Tailscale version
  - Created/last seen timestamps
  - Machine key, node key (abbreviated)
  - Tags and expiry info
  - User/owner details

- **Network tab** (Network Analytics Integration):
  - **DERP Relay Information**:
    - Current DERP region in use
    - Latency to all DERP servers (table/list)
    - Home DERP preference
  - **Connection Information**:
    - Direct connection vs DERP relayed
    - Local LAN IP
    - External IP
    - Connection type indicators
  - **Peer Connections** (if available):
    - Which peers this machine connects to
    - Connection type for each peer (direct/relayed)
    - Latency to peers
  - **Network Health**:
    - Packet statistics
    - Connection quality indicators
    - NAT traversal info

- **Activity tab** (future/optional):
  - Connection history
  - Event log

### 3. User Management Page
**Fully supported by Headscale API** (CreateUser, RenameUser, DeleteUser, ListUsers):
- List all users with machine counts
- Create new user form
- Rename user action
- Delete user action (with confirmation)
- User detail view showing machines belonging to that user
- Integrate with PreAuth key generation (per-user keys)

### 4. Node Actions (Machine Management)
**Machine detail view with actions** using supported API methods:
- **Route Management** ✅ IMPLEMENTED:
  - ✅ Approve/reject advertised subnet routes (SetApprovedRoutes)
  - ✅ Approve/reject exit node capability (SetApprovedRoutes)
  - ✅ Visual feedback for approval state changes
  - ✅ Per-route approve/reject buttons
- **Node Operations**:
  - ✅ Rename node (RenameNode)
  - ✅ Move node to different user (MoveNode)
  - ✅ Set/edit tags (SetTags)
  - ✅ Delete node (DeleteNode) with confirmation modal
  - ✅ Expire node key (ExpireNode) with confirmation modal
- **Action buttons**:
  - Dropdown menu on machine list rows
  - Direct action buttons on machine detail page for routes
  - Modals for rename/confirmation actions
  - Toast notifications for success/errors

### 5. Access Controls (ACL) Page
Replicate Tailscale's ACL editor:
- Code editor with syntax highlighting (JSON/HuJSON)
- Current policy display
- Edit mode with validation
- "Save" and "Cancel" buttons
- Warning modal before applying changes
- ACL test/validation tool (check if rule allows X to Y)
- Version history (optional, if Headscale supports)

## Architecture & Data Flow

```
┌─────────────────────────────────────────────────────┐
│              Browser (HTMX + Minimal JS)             │
│  - HTML rendered from Go templates                   │
│  - HTMX for dynamic updates                          │
│  - SSE for real-time data                            │
└───────────────────┬─────────────────────────────────┘
                    │ HTTP/SSE
┌───────────────────▼─────────────────────────────────┐
│         Go HTTP Server (tsnet :80)                   │
│  - Routes: /, /machines, /machines/:id, /users, /acl│
│  - html/template rendering                           │
│  - SSE endpoint: /events                             │
│  - API endpoints: POST /users, POST /machines/*, etc │
└──────────┬─────────────────────┬────────────────────┘
           │                     │
           │ gRPC                │ tsnet LocalClient
           ▼                     ▼
┌──────────────────┐    ┌───────────────────┐
│ Headscale API    │    │  Tailscale Peers  │
│ - ListUsers      │    │  - Status()       │
│ - CreateUser     │    │  - WhoIs()        │
│ - DeleteUser     │    │  - DERP latency   │
│ - ListNodes      │    │  - Peer details   │
│ - SetApprovedRoutes │  └───────────────────┘
│ - DeleteNode     │
│ - RenameNode     │
│ - MoveNode       │
│ - GetPolicy      │
│ - SetPolicy      │
└──────────────────┘
```

## Project Structure (Current)

```
/main.go                        # Entry point (at root)
/internal/
  /config/
    config.go                   # Config loading and validation
  /models/
    machine.go                  # Machine data model with helper methods
    machine_test.go             # Unit tests for machine model
    user.go                     # User data model
  /format/
    time.go                     # Time formatting utilities
  /handlers/
    machines.go                 # Machine list, detail, and rename handlers
    machine_actions.go          # Route management (approve/reject exit nodes and subnets)
    users.go                    # User management handlers (CRUD + PreAuth)
    alerts.go                   # Alert/toast notification rendering
    sse.go                      # SSE handler with polling and change detection
  /events/
    broker.go                   # SSE event broker (hub pattern)
  /sets/
    set.go                      # Generic set implementation
  /testutil/
    normalize.go                # Test normalization utilities
/web/
  /templates/
    layout.html                 # Base layout with nav, styles, alert container
    machines.html               # Machines list view
    machine_detail.html         # Machine detail view
    users_list.html             # Users list view with modals
  /static/
    /css/                       # (Empty - using Tailwind CDN)
    /js/                        # (Empty - minimal inline JS in templates)
/test/
  /integration/
    testenv.go                  # Test environment setup with dockertest
    golden_test.go              # Golden file tests for UI regression
    ui_test.go                  # Browser automation tests with Rod
    /testdata/
      /golden/                  # Golden file snapshots
```

**Note:** The implementation uses direct client calls and template-based HTTP handlers for simplicity and performance. SSE provides real-time updates without requiring a separate data collection service or in-memory store.

## Implementation Plan

### Phase 1: Project Restructuring & Core Infrastructure ✅
- [x] Reorganize code into structured packages
- [x] Create config package for YAML loading
- [x] Create data models with helper methods
- [x] Set up HTTP server with routing
- [x] Template parsing and rendering
- [x] Create headscale client wrapper (using directly - no wrapper needed)
- [x] Create tsnet agent manager (managed in main)
- [ ] Build data collection service (background polling) - deferred to Phase 8
- [ ] Implement in-memory store with thread-safe access - deferred to Phase 8
- [ ] Build SSE event broker (hub pattern) - deferred to Phase 8
- [ ] Add basic middleware (logging, recovery) - deferred to Phase 8

### Phase 2: Base Layout & Navigation ✅
- [x] Create base HTML template matching Tailscale's layout
- [x] Add header with logo/title (with Tailscale-style dots)
- [x] Implement horizontal navigation (not sidebar)
- [x] Add Tailwind CSS (CDN)
- [x] Create custom CSS for Tailscale-specific styling
- [x] Add navigation state management with underline indicator
- [x] Alert/toast container for notifications
- [ ] Implement responsive mobile menu (future enhancement)

### Phase 3: Machines List View ✅ (Core Complete)
- [x] Create machines.html template
- [x] Implement `/` and `/machines` handlers
- [x] Build machine row component in template
- [x] Add status dot indicators
- [x] Implement search functionality with HTMX (hostname, IP, user)
- [x] Add empty states
- [x] Add dropdown menu on machine rows
- [x] Implement rename machine action
- [x] Click-outside-to-close for dropdowns
- [ ] Add filter dropdowns (status, user, tags) - future enhancement
- [ ] Implement column sorting - future enhancement
- [ ] Connect SSE for live status updates - Phase 8
- [ ] Add loading states - Phase 10

### Phase 4: Machine Detail View ✅
- [x] Create machine_detail.html template (single page, not tabs - matching Tailscale UI)
- [x] Implement `/machines/:id` handler
- [x] Build Machine Details section with two-column layout (matching Tailscale)
- [x] Display basic machine info (creator, name, OS, version, IDs, last seen)
- [x] Add OS hostname field (separate from machine name)
- [x] Add Node key with abbreviated display and copy button
- [x] Add Created timestamp and Key expiry status
- [x] Add ATTRIBUTES section (node:os, node:osVersion, node:tsAutoUpdate, node:tsReleaseTrack, node:tsStateEncrypted, node:tsVersion)
- [x] Add CLIENT CONNECTIVITY section (Varies, Hairpinning, IPv6, UDP, UPnP, PCP, NAT-PMP)
- [x] Add Short domain and Full domain to ADDRESSES section
- [x] Add Endpoints (IP:port list) to ADDRESSES section
- [x] Show connection type (direct/relay)
- [x] Display DERP latency under "Relays" subsection with region names
- [x] Show Tailscale IP addresses in "Addresses" section
- [x] Add breadcrumb navigation (All Machines / IP)
- [x] Status indicators and badges (Connected/Offline, Exit Node/Subnets)
- [x] Template refactored to use reusable layout components (layout-header, layout-styles)
- [x] Add copy buttons with JavaScript for Machine name, ID, Node key, IPs, domains
- [x] Add Subnets section (Approved and Awaiting Approval routes)
- [x] Add Routing Settings section (Exit Node status with Allowed/Awaiting approval indicators)
- [x] Separate exit node routes from subnet routes in display
- [ ] Connect real-time updates for detail view - Phase 8

### Phase 5: User Management Page ✅ COMPLETE
- [x] Create users.go handler file
- [x] Create users_list.html template
- [x] Implement GET /users handler (list all users)
- [x] Display user list with machine counts
- [x] Add "Create User" form with HTMX
- [x] Implement POST /users (CreateUser API)
- [x] Add rename user action with modal dialog (RenameUser API)
- [x] Add delete user action with confirmation modal (DeleteUser API)
- [x] Integrate PreAuth key generation per user with modal dialog
- [x] Add navigation item for "Users"
- [x] Dropdown menu on user rows with all actions
- [x] Click-outside-to-close for dropdowns
- [x] Alert notifications for success/error states
- [x] Browser automation tests (Rod) for rename, delete, and pre-auth key generation with API verification
- [x] Golden file tests for UI regression detection
- [ ] Add user filtering to machines list (filter by user parameter) - future enhancement
- [ ] Add dropdown button on user row to navigate to prefiltered machines list - future enhancement

### Phase 6: Node Actions (Machine Management) ✅ COMPLETE
- [x] Implement POST /machines/:id/rename (RenameNode)
- [x] Add rename action to machine dropdown menu
- [x] Add confirmation modal for rename
- [x] Browser automation tests for rename functionality
- [x] Create machine_actions.go handler file for route management actions
- [x] Implement POST /machines/:id/routes/exit-node/approve (SetApprovedRoutes)
- [x] Implement POST /machines/:id/routes/exit-node/reject (SetApprovedRoutes)
- [x] Implement POST /machines/:id/routes/subnets/approve (SetApprovedRoutes)
- [x] Implement POST /machines/:id/routes/subnets/reject (SetApprovedRoutes)
- [x] Add UI for approving/rejecting subnet routes on machine detail page
- [x] Add UI for approving/rejecting exit node capability
- [x] Browser automation tests for exit node approval/rejection
- [x] Browser automation tests for subnet route approval/rejection
- [x] Implement POST /machines/:id/move (MoveNode) with user dropdown
- [x] Add UI for moving node to different user with modal
- [x] Browser automation tests for move node
- [x] Implement POST /machines/:id/tags (SetTags)
- [x] Add UI for managing tags with comma-separated input
- [x] Browser automation tests for tag management
- [x] Implement POST /machines/:id/delete (DeleteNode)
- [x] Add delete button to machine dropdown menu (red styled for danger)
- [x] Add confirmation modal with warning for delete action
- [x] Browser automation test verifying end-to-end deletion from Headscale
- [x] Implement POST /machines/:id/expire (ExpireNode)
- [x] Add expire button to machine dropdown menu (yellow styled for warning)
- [x] Add confirmation modal with re-authentication warning for expire action
- [x] Browser automation test verifying end-to-end key expiration
- [x] Update machine detail view after actions - handled by SSE

### Phase 7: ACL Editor Page
- [ ] Create acl.go handler file
- [ ] Create acl.html template
- [ ] Integrate code editor (Monaco or CodeMirror)
- [ ] Implement GET /acl handler (GetPolicy API)
- [ ] Implement POST /acl handler (SetPolicy API)
- [ ] Add JSON validation
- [ ] Build confirmation modal with diff
- [ ] Add success/error notifications
- [ ] Add navigation item for "Access Controls"

### Phase 8: Real-time Updates & Data Infrastructure ✅ COMPLETE
- [x] Build SSE event broker (hub pattern in internal/events/broker.go)
- [x] Implement SSE handler with optimized polling (internal/handlers/sse.go)
- [x] Connect SSE for live status updates on machines list
- [x] Connect real-time updates for users list
- [x] **Performance optimizations:**
  - Eliminated duplicate API calls (broadcastMachinesTableUpdate now accepts data as params)
  - Simplified FetchMachines (ListNodes without user filter → single call for all machines)
  - Refactored user polling to derive machine counts from already-fetched machines
  - **Result: 91% reduction in API calls** (from 22 calls/poll → 2 calls/poll with 10 users)
  - Enabled 500ms polling (10x faster than initial 5s) with same API load as original
- [x] Implement 500ms polling for responsive updates
- [x] Add change detection for machines (online status, routes, tags, new/deleted)
- [x] Add change detection for users (new/deleted, machine counts)
- [x] Pure detection functions (detectMachineChanges, detectUserChanges) with short-circuit logic
- [x] Browser automation tests for SSE functionality (4 test scenarios)
- [x] State tracking to prevent unnecessary broadcasts
- [x] Client connection management with automatic cleanup
- [ ] Build data collection service - not needed (direct polling is efficient)
- [ ] Implement in-memory store - not needed (direct API calls work well)
- [ ] Add basic middleware (logging, recovery) - deferred to Phase 10

**SSE Architecture:** Coordinated polling that fetches all data once per poll cycle, derives counts from existing data, uses pure change detection functions, and only broadcasts when changes are detected. Polling only occurs when clients are connected.

### Phase 9: Testing Infrastructure ✅ COMPLETE
- [x] Testing strategy documented in TESTING_STRATEGY.md
- [x] Unit tests for complex model logic (machine models)
- [x] Integration tests with real Headscale (dockertest + version matrix)
- [x] Golden file tests for UI regression detection (machines, machine detail, users)
- [x] Browser automation tests (Rod) covering:
  - User management: rename, delete, pre-auth key generation
  - Machine management: rename, move to user, tag management
  - Route management: exit node approval/rejection, subnet route approval/rejection
  - SSE real-time updates: machine addition, deletion, status changes, multiple changes
  - Dropdown menu interactions
  - Click-outside-to-close behavior
  - Modal dialogs
  - API verification for all actions
- [x] Tailscale client containers for route testing with `tailscale set` commands
- [x] Parallel test execution for performance
- [x] Test utilities and helpers (normalization, fixtures)
- [x] Support for multiple Headscale versions
- [ ] Expand test coverage for future features (ACL editor, additional node actions)

### Phase 10: Polish & Production Readiness 🔄 PARTIAL
- [x] Alert notifications for actions (HTMX out-of-band swaps)
- [x] Error handling for API failures
- [x] Auto-dismissing toasts with animations
- [x] Basic responsive design (container, grid layouts)
- [ ] Loading states and skeleton screens
- [ ] Enhanced mobile responsive design (collapsible nav, touch-friendly)
- [ ] Accessibility improvements (ARIA labels, keyboard navigation)
- [ ] Performance optimization (template caching, gzip compression)
- [ ] Add structured logging (replace fmt.Printf with proper logger)
- [ ] Graceful shutdown handling (context cancellation)
- [ ] Create Dockerfile and container image
- [ ] Write comprehensive documentation (README, setup guide, API docs)

### Phase 11: Authentication & Authorization ✅ COMPLETE
**✅ Production-ready authentication implemented with dual auth support**

#### Dual Authentication Strategy
Smart middleware that supports both authentication methods has been implemented:

**1. WhoIs-based Auth (Tailscale Network Access)**
- [x] Create auth middleware that intercepts all HTTP requests
- [x] Extract remote address from request (connection peer info)
- [x] Call `tsnetClient.WhoIs(ctx, remoteAddr)` to identify connecting user
- [x] Extract user ID from WhoIs response
- [x] Check if user ID is in allowed admin list from config
- [x] Return 403 Forbidden with clear message if unauthorized
- [x] Add request context with authenticated user info for audit logging
- [x] Support for admin user tags in addition to user IDs

**2. OIDC-based Auth (External/Funnel Access)**
- [x] Add OIDC configuration to config file (provider URL, client ID, client secret)
- [x] Implement OAuth2/OIDC flow (authorization code grant)
- [x] Create `/auth/login` endpoint to initiate OIDC flow
- [x] Create `/auth/callback` endpoint to handle OIDC callback
- [x] Store session tokens in secure HTTP-only cookies
- [x] Implement session validation middleware
- [x] Add user email claim validation against admin allow-list
- [x] Create `/auth/logout` endpoint to clear session
- [x] Add login page UI for unauthenticated users
- [x] Compatible with popular OIDC providers (Google, Okta, Keycloak, Authentik)

**3. Smart Middleware Logic**
- [x] Detection logic: Try WhoIs first, fall back to OIDC session
- [x] If WhoIs succeeds and user is admin → allow request
- [x] If WhoIs fails or returns non-admin → check OIDC session
- [x] If OIDC session valid and user is admin → allow request
- [x] If both fail → redirect to login page (or 403 if API request)
- [x] Distinguish between HTML requests (redirect) and API/HTMX requests (403 JSON)
- [x] Configuration flags to enable/disable each auth method

**4. Configuration Structure**
```yaml
headscale:
  agent_userid: 1
  api_hostport: localhost:50443
  api_key: test-key
  server_url: https://headscale.example.com

auth:
  # WhoIs-based auth (for Tailscale network access)
  whois_enabled: true
  admin_user_ids: [1, 5, 10]  # Headscale user IDs allowed to admin

  # OIDC auth (for external access via Funnel or direct HTTP)
  oidc_enabled: false  # Optional, for external access
  oidc_provider_url: https://accounts.google.com
  oidc_client_id: your-client-id
  oidc_client_secret: your-client-secret
  oidc_redirect_url: https://hsadmin.example.com/auth/callback
  oidc_admin_emails: [admin@example.com, ops@example.com]  # Email claim validation
  session_secret: random-256-bit-secret  # For cookie encryption
  session_duration: 24h
```

**4. Security Considerations**
- [x] CSRF protection for OIDC flow (state parameter validation)
- [x] Secure session cookie attributes (HttpOnly, Secure, SameSite)
- [x] Audit logging for all authentication attempts (success/failure)
- [x] Token expiration and session duration configuration
- [ ] Session token rotation on privilege escalation (future enhancement)
- [ ] Rate limiting on auth endpoints to prevent brute force (future enhancement)
- [ ] Support for PKCE (Proof Key for Code Exchange) in OIDC flow (future enhancement)

**5. Testing**
- [x] Unit tests for auth middleware logic
- [x] Test public path bypass (login, callback, logout)
- [x] Test user context handling and template data enrichment
- [ ] Integration tests with mock OIDC provider (future enhancement)
- [ ] Test WhoIs auth with multiple Headscale users (integration test needed)
- [ ] Test fallback from WhoIs to OIDC (integration test needed)
- [ ] Browser automation tests for login/logout flow (future enhancement)

**6. UI/UX**
- [x] Login page with OIDC provider button (when OIDC enabled)
- [x] User indicator in nav bar showing authenticated user
- [x] Auth method badge (SSO vs Tailscale)
- [x] Logout button in user menu (for OIDC sessions)
- [x] Clear error messages for auth failures ("Not authorized", "Session expired")
- [x] Responsive user menu (mobile-friendly)

**Implementation Files:**
- `internal/auth/auth.go` - Main auth middleware and WhoIs authentication
- `internal/auth/oidc.go` - OIDC authenticator and session management
- `internal/auth/handlers.go` - HTTP handlers for login/callback/logout
- `internal/auth/auth_test.go` - Unit tests for auth middleware
- `internal/config/config.go` - Auth configuration structure and validation
- `web/templates/login.html` - Login page template
- `web/templates/layout.html` - User indicator in nav bar
- `hsadmin.yaml.example` - Complete configuration example with comments
- `main.go` - Auth middleware integration
- [ ] Graceful degradation when auth provider is unavailable

**8. Documentation**
- [ ] Setup guide for WhoIs-only deployment (simple)
- [ ] Setup guide for OIDC configuration (with popular providers)
- [ ] Security best practices documentation
- [ ] Troubleshooting guide for auth issues

**Priority:** 🔴 **CRITICAL** - Must complete before any production deployment with multiple users

**Dependencies:** None (can be implemented immediately)

**Estimated effort:** ~3-5 days for comprehensive implementation with both auth methods

## Visual Design Checklist
Match these Tailscale UI elements:
- [x] Color palette (dark theme with grays, blues, green for online, gray for offline)
- [x] Typography (system font stack via Tailwind CSS)
- [x] Status indicators (colored dots + text)
- [x] Status badges (Connected/Offline, Exit Node, Subnets)
- [x] Card/row hover states (table rows, buttons, menu items)
- [x] Button styles (copy buttons with hover states)
- [x] Form input styling (text inputs, checkboxes, selects)
- [x] Modal/dialog styling (HTML5 dialog elements with dark theme)
- [x] Navigation active states (underline indicator)
- [x] Dropdown menus (details/summary with positioning)
- [x] Alert/toast notifications (slide-in animations, auto-dismiss)
- [x] Empty states ("No machines found", "This machine does not expose any routes")
- [x] Responsive breakpoints (container, grid layouts)
- [x] Copy buttons with visual feedback
- [ ] Info icons with tooltips - future enhancement
- [ ] Loading spinners - future enhancement

## Key Implementation Details

### Go Templates Pattern
```go
// Parse templates once at startup
templates := template.Must(template.ParseGlob("web/templates/*.html"))

// Handler
func (h *Handler) MachinesList(w http.ResponseWriter, r *http.Request) {
    machines := h.store.GetMachines()

    data := struct {
        Machines []*models.Machine
        Query    string
    }{
        Machines: machines,
        Query:    r.URL.Query().Get("q"),
    }

    templates.ExecuteTemplate(w, "machines_list.html", data)
}
```

### HTMX Example for Live Search
```html
<input type="search"
       name="q"
       placeholder="Search machines..."
       hx-get="/machines"
       hx-trigger="keyup changed delay:300ms"
       hx-target="#machine-list"
       hx-select="#machine-list">

<div id="machine-list">
    {{range .Machines}}
        {{template "machine_row.html" .}}
    {{end}}
</div>
```

### SSE Integration
```html
<div hx-ext="sse" sse-connect="/events" sse-swap="machine-update">
    <!-- Updates triggered by SSE events -->
</div>
```

### Data Models
```go
type Machine struct {
    Node       *headscale.Node      // From Headscale API
    PeerStatus *ipnstate.PeerStatus // From tsnet Status()
    WhoIsNode  *tailcfg.Node        // From tsnet WhoIs() - has HostInfo with version, DERP latency, etc
    Online     bool
}

// Helper methods include:
// - Basic info: Hostname(), ID(), User(), OS(), OSHostname(), TailscaleVersion()
// - Timestamps: Created(), LastSeenShort(), LastSeenFull(), KeyExpiry()
// - Network: TailscaleIPs(), PrimaryIP(), ConnectionType(), Endpoints()
// - Domains: ShortDomain(), FullDomain()
// - Keys: NodeKey(), NodeKeyShort()
// - Attributes: OSVersion(), AutoUpdate(), ReleaseTrack(), StateEncrypted()
// - Connectivity: ClientConnectivity() (returns map of network capabilities)
// - DERP: DERPLatency(), ProcessedDERPLatencies()
// - Routing: ApprovedSubnets(), AdvertisedSubnets(), HasSubnetRoutes()
// - Exit Node: ExitNodeApproved(), ExitNodeAdvertised(), HasExitNode(), ExitNodeStatus()
// - Status: StatusText(), StatusDotClass()
```

### Collector Service Pattern
```go
type Collector struct {
    headscaleClient *headscale.Client
    tsnetClient     *tsnet.LocalClient
    store           *store.Store
    eventBroker     *events.Broker
    interval        time.Duration
}

func (c *Collector) Run(ctx context.Context) {
    ticker := time.NewTicker(c.interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            c.collect()
        }
    }
}
```

## Authentication
- Server only accessible on Tailscale network (tsnet)
- Optionally use WhoIs to check client identity
- Could add ACL checks (only allow certain Tailscale users)

## Testing

See [TESTING_STRATEGY.md](TESTING_STRATEGY.md) for overall approach and [test/integration/README.md](test/integration/README.md) for details.

## Dependencies
Using Go stdlib only where possible:
- `html/template` - templating
- `net/http` - HTTP server and routing
- Existing: `tailscale.com/tsnet` - Tailscale network
- Existing: `github.com/juanfont/headscale/gen/go/headscale/v1` - Headscale API
- Existing: `google.golang.org/grpc` - gRPC client
- Existing: `gopkg.in/yaml.v3` - YAML config
- Testing: `github.com/ory/dockertest/v3` - container management for integration tests

Frontend (CDN):
- HTMX - dynamic HTML updates
- Tailwind CSS - styling
- Optional: Monaco Editor or CodeMirror for ACL editor

## Notes
- Keep dependencies minimal
- Use Go stdlib where possible
- Focus on replicating Tailscale's UI/UX closely
- Real-time updates are crucial for good UX
- Network analytics integrated into machine detail view (not separate section)

## API Limitations & Design Decisions
**DNS Management is NOT feasible**: Headscale does not expose any DNS configuration methods via its gRPC API. DNS settings (MagicDNS, nameservers, search domains) can only be configured through Headscale's YAML configuration file. This is a fundamental limitation of Headscale's architecture.

**Fully Supported Features** (via Headscale gRPC API):
- User Management: CreateUser, RenameUser, DeleteUser, ListUsers
- Node Actions: SetApprovedRoutes, DeleteNode, RenameNode, MoveNode, SetTags, ExpireNode
- PreAuth Keys: CreatePreAuthKey, ListPreAuthKeys, ExpirePreAuthKey
- ACL Management: GetPolicy, SetPolicy
- API Key Management: CreateApiKey, ListApiKeys, ExpireApiKey, DeleteApiKey

**Priority Order** (based on API support and usefulness):
1. **User Management** - Fundamental for organizing machines and users
2. **Node Actions** - Makes machine management actionable (approve routes, delete nodes, etc.)
3. **ACL Editor** - Crucial for access control and security policies
4. **Real-time Updates** - Important for UX but technically complex with SSE
