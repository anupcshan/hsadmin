# Integration Tests

This directory contains integration tests for hsadmin that run against a real Headscale instance in a container.

## Prerequisites

- Docker (daemon must be running)
- Go 1.25+

## Quick Start

```bash
# Run integration tests
go test ./test/integration

# Skip integration tests (fast unit tests only)
go test -short ./...

# Update golden files after UI changes
go test ./test/integration -update
```

That's it! Container management is fully automatic.

## How It Works

Tests use [dockertest](https://github.com/ory/dockertest) to automatically:
1. Start a Headscale container with embedded config
2. Bootstrap test user and API keys via Headscale CLI
3. Create a tsnet client that joins the network
4. Run your tests against the real Headscale instance
5. Clean up containers when done

All container lifecycle is managed in `TestMain()` - no manual setup required.

## Directory Structure

```
test/integration/
├── testenv.go                 # Container setup and lifecycle
├── golden_test.go             # Golden file tests with version matrix
├── config/
│   └── headscale.yaml         # Headscale config (embedded in testenv.go)
└── testdata/                  # Golden files
    ├── machines_list.golden.html      # Default golden file
    ├── machine_detail.golden.html     # Default golden file
    └── 0.27.0/                        # Version-specific overrides (optional)
        └── machines_list.golden.html  # Override for 0.27.0 only
```

## Golden Files

Golden files capture expected HTML output for regression testing.

### Version-Agnostic by Default

By default, all Headscale versions use the same golden files in `testdata/`. This keeps maintenance low.

### Version-Specific Overrides

If a specific Headscale version produces different output, declare it explicitly in `golden_test.go`:

```go
goldenOverrides = map[string]bool{
    "0.27.0/machines_list.golden.html": true,  // Only 0.27.0 differs
}
```

Now when you run `-update`:
- 0.27.0 writes to `testdata/0.27.0/machines_list.golden.html`
- Other versions write to `testdata/machines_list.golden.html`

### Updating Golden Files

When you intentionally change the UI:

```bash
go test ./test/integration -update
git diff testdata/  # Review changes
git add testdata/ && git commit -m "Update golden files"
```

The `-update` flag knows exactly where to write files based on the `goldenOverrides` map.

### Normalization

Dynamic content is normalized before comparison:
- Timestamps → `TIMESTAMP`
- "5 minutes ago" → `TIME_AGO`
- Latencies → `XXms`
- IPs → `100.64.X.X`, `fd7a:XXXX:XXXX`
- Node keys → `nodekey:XXXX...`

This prevents false failures from timing variance.

## Testing Different Headscale Versions

Tests automatically run against multiple versions defined in the `versions` array in `golden_test.go`. Each test creates subtests for each version (e.g., `TestMachinesList_Golden/0.27.0`, `TestMachinesList_Golden/0.26.0`).

To test against a new version, add it to the `versions` array.

## CI/CD Integration

The tests run automatically against all versions in the `versions` array. In CI, just run:

```bash
go test ./test/integration
```

This will test all versions sequentially in subtests.

## Troubleshooting

### Docker connection error

```bash
# Ensure Docker daemon is running
docker ps

# Check Docker socket permissions
sudo usermod -aG docker $USER
```

### Container won't start

Tests automatically retry container startup. Check:
- Docker daemon is running
- Ports 18080, 50443, 19090 are available
- Sufficient Docker resources

### Tests fail intermittently

The health check waits for Headscale to be fully ready. If you still see flakes:
- Check Docker performance
- Increase retry timeout in `pool.Retry()`

### Debug container issues

Runtime artifacts are written to `.testoutput/` for inspection:

```bash
cat .testoutput/hsadmin.yaml  # Check generated config
cat .testoutput/api-key       # View API key
```

## Design Rationale

### Why dockertest instead of scripts?

- ✅ Tests work from any directory (finds repo root automatically)
- ✅ No manual setup/teardown - fully automatic
- ✅ Embedded config - no path dependencies
- ✅ Better Go integration (retry logic, structured errors)
- ✅ Simpler - just `go test`

### Why containers?

- ✅ Isolated from development environment
- ✅ Clean state every run (tmpfs storage)
- ✅ Version matrix testing
- ✅ CI/CD ready
- ✅ Real Headscale (no mocking)

### Why golden files instead of browser E2E?

- ✅ Faster (no browser startup)
- ✅ More reliable (no Selenium flakiness)
- ✅ Easier to review (git diff shows HTML changes)
- ✅ Tests structure and content
- ❌ Doesn't test JavaScript (use manual testing)

### Why real Headscale instead of mocks?

Per testing strategy: "I'd veer away from mocking any of these interactions since the API may not be very stable or well documented. Testing against the real thing is super important."
