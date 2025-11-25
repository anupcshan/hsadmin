# Testing Strategy for Headscale Admin Dashboard

## Overview
This document outlines the pragmatic testing approach for hsadmin - a web-based admin interface for Headscale. The strategy prioritizes **real-world integration testing** over mocking, with focused unit tests for complex logic only.

## Core Principles

1. **UI correctness is critical** - Users will click buttons that mutate state (delete nodes, create keys)
2. **Test against real Headscale** - The API is the source of truth, not mocks
3. **Version compatibility matters** - Test against multiple Headscale versions (v0.23-v0.27)
4. **Golden files for HTML** - Catch UI regressions without browser automation
5. **Minimize unit tests** - Only for genuinely complex logic with edge cases

## Testing Priorities

### Priority 1: Unit Tests for Complex Model Logic

**Test ONLY these complex methods** in `internal/models/machine.go`:

#### 1. DERP Latency Processing (100+ lines, critical for diagnostics)
- `ProcessedDERPLatencies()` - Parsing, grouping, sorting logic
  - Parse keys like "10-v4", "17-v6" to extract region IDs
  - Group by region, choose min latency between v4/v6
  - Sort by latency and mark preferred region
  - Handle edge cases: malformed keys, missing DERP map, empty data

#### 2. Exit Node Logic (business critical for upcoming mutations)
- `ExitNodeApproved()` - Must verify both approved AND advertised
- `ExitNodeAdvertised()` - Diff logic between available and approved routes
- `ExitNodeStatus()` - Drives UI display and button states ("Allowed", "Awaiting approval", "")
- `HasExitNode()` - Boolean aggregation

#### 3. Subnet Route Logic (same criticality)
- `ApprovedSubnets()` - Filter out exit node routes (0.0.0.0/0, ::/0)
- `AdvertisedSubnets()` - Diff logic with exit node filtering
- `HasSubnetRoutes()` - Boolean aggregation

**SKIP unit tests for**:
- Simple getters (Hostname, ID, User, etc.)
- Formatting methods (LastSeenShort, LastSeenFull) - covered by golden files
- Basic nil-checking and fallback logic

**Estimated effort**: 200-300 lines of tests, 2-3 hours

### Priority 2: Integration Tests with Real Headscale

Test against actual running Headscale instances, not mocks. The Headscale API may not be stable or well-documented, so testing against the real thing is essential.

#### Test Infrastructure
- Script to download and run specific Headscale versions
- Seed test data (machines with known states)
- Golden file generation and comparison

#### Test Matrix
Run tests against multiple Headscale versions:
- v0.23.0
- v0.24.0
- v0.25.0
- v0.26.0
- v0.27.0 (current)

This catches API compatibility issues early.

### Priority 3: Golden File Tests

Golden files capture the rendered HTML output for known inputs, allowing regression detection without browser automation.

#### Handling Timing Variance

Use normalization to handle dynamic content:

```go
// Normalize timestamps, latencies, IPs before comparison
func NormalizeHTML(html string) string {
    // Replace "5 minutes ago" → "TIME_AGO"
    // Replace "123ms" → "XXms"
    // Replace "100.64.0.1" → "100.64.X.X"
    // Normalize whitespace
}
```

#### What Golden Files Catch
✅ Structure changes (added/removed sections)
✅ CSS class changes (broken styling)
✅ Content changes (badges appear/disappear incorrectly)
✅ HTMX attribute changes (broken interactivity)
✅ Regressions (copy buttons missing, breadcrumbs gone)
✅ Version incompatibilities (Headscale API changes)

❌ Visual rendering (CSS layout) - still needs manual testing
❌ JavaScript runtime errors - still needs manual testing

#### Review Process
When golden files change:
1. Developer runs: `go test -tags=integration ./test/integration/... -update`
2. Review diff: `git diff test/integration/testdata/`
3. Manually verify HTML changes are intentional
4. Check structure still makes sense
5. Verify HTMX attributes still present
6. Commit updated golden files

### Priority 4: Manual Testing

Maintain a checklist for manual smoke testing:
- [ ] Machine list displays correctly
- [ ] Search filters machines
- [ ] Machine detail page shows all sections
- [ ] Copy buttons work (JavaScript)
- [ ] Status badges show correct colors
- [ ] DERP latencies sorted correctly
- [ ] When mutations are added: buttons perform correct actions

## What We're NOT Doing

### ❌ Comprehensive Mocking
- Don't mock Headscale API - test against real instances
- Don't mock tsnet - too much surface area, API changes frequently
- Mocking would give false confidence

### ❌ E2E Browser Automation
- Too expensive and flaky for current project size
- Golden files + manual testing is sufficient
- Can revisit if UI becomes much more complex

### ❌ Exhaustive Unit Tests
- Don't test simple getters
- Don't test formatting helpers
- Focus only on complex business logic

### ❌ Performance Testing
- Not needed unless scaling to 1000+ machines
- Can add later if performance issues emerge

## Test Infrastructure

### Directory Structure
```
test/
├── integration/
│   ├── testenv.go              # Container setup and lifecycle (dockertest)
│   ├── golden_test.go          # Golden file tests with version matrix
│   └── testdata/
│       ├── machines_list.golden.html       # Default golden files
│       ├── machine_detail.golden.html
│       └── 0.27.0/                         # Version-specific overrides (if needed)
│           └── machines_list.golden.html
└── internal/
    └── testutil/
        └── normalize.go        # HTML normalization utilities
```

### Dependencies

Add to `go.mod`:
```go
require (
    github.com/stretchr/testify v1.8.4  // Assertions
)
```

No mocking framework needed - testing against real services.

### CI/CD Integration

GitHub Actions workflow:
```yaml
name: Test

on: [push, pull_request]

jobs:
  unit:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.25'
      - name: Run unit tests
        run: go test ./internal/models/...

  integration:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
      - name: Run integration tests
        run: go test ./test/integration
```

Version matrix testing is built into the tests themselves using subtests. Use `go test -short` to skip integration tests.

## Development Workflow

### 1. Making Changes
```bash
# Make code changes

# Run unit tests (fast feedback - skips integration tests)
go test -short ./...

# Run integration tests (all versions)
go test ./test/integration
```

### 2. Updating Golden Files
```bash
# Regenerate golden files after intentional UI changes
go test ./test/integration -update

# Review changes
git diff test/integration/testdata/

# Commit if changes are correct
git add test/integration/testdata/
git commit -m "Update golden files for new layout"
```

### 3. Before PR
```bash
# Run full test suite (all tests including integration)
go test ./...

# Manual smoke test against your own Headscale
./hsadmin -config hsadmin.yaml
```

## Success Metrics

- **Coverage**: >80% for complex model methods (DERP, routing, exit node)
- **Test Speed**: Unit tests <1s, integration tests <30s per version
- **Flakiness**: Zero flaky tests (achieved by normalizing timing variance)
- **Maintenance**: Golden files require manual review but low ongoing effort
- **Real-world validation**: Tests run against actual Headscale, not mocks

## Implementation Phases

### Phase 1: Core Unit Tests ✅ CURRENT
- [ ] Unit tests for `ProcessedDERPLatencies()`
- [ ] Unit tests for exit node logic
- [ ] Unit tests for subnet route logic
- **Estimated**: 3-4 hours

### Phase 2: Integration Infrastructure
- [ ] Headscale setup script
- [ ] Seed data script
- [ ] Normalization utilities
- [ ] Golden test framework
- **Estimated**: 4-6 hours

### Phase 3: Golden File Baseline
- [ ] Generate golden files for v0.27
- [ ] Manual review and commit
- [ ] Add version matrix (v0.25, v0.26)
- **Estimated**: 2-3 hours

### Phase 4: CI/CD
- [ ] GitHub Actions workflow
- [ ] Branch protection rules
- **Estimated**: 1-2 hours

## Future Enhancements

When the tool grows more complex, consider:
- Playwright tests for critical JavaScript interactions
- Mutation testing (verify delete/create operations)
- Chaos testing (Headscale goes down mid-request)
- Performance benchmarks (1000+ machines)

## Conclusion

This testing strategy provides:
- ✅ High confidence in complex logic (unit tests)
- ✅ Regression protection (golden files)
- ✅ API compatibility validation (version matrix)
- ✅ Low maintenance burden (no mocks, no browser automation)
- ✅ Fast feedback loop (<5s for unit tests)
- ✅ Real-world validation (tests against actual Headscale)

True confidence comes from running hsadmin on your own Headscale network and iterating based on real issues discovered.
