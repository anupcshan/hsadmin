package models

import (
	"testing"

	headscale "github.com/juanfont/headscale/gen/go/headscale/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

// TestProcessedDERPLatencies_EmptyData tests handling of machines with no DERP latency data
func TestProcessedDERPLatencies_EmptyData(t *testing.T) {
	tests := []struct {
		name    string
		machine *Machine
		derpMap *tailcfg.DERPMap
		want    []DERPRegionLatency
	}{
		{
			name:    "nil machine",
			machine: nil,
			derpMap: nil,
			want:    nil,
		},
		{
			name:    "machine with no WhoIsNode",
			machine: &Machine{},
			derpMap: nil,
			want:    nil,
		},
		{
			name: "machine with WhoIsNode but no latency data",
			machine: &Machine{
				WhoIsNode: &tailcfg.Node{},
			},
			derpMap: nil,
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result []DERPRegionLatency
			if tt.machine != nil {
				result = tt.machine.ProcessedDERPLatencies(tt.derpMap)
			}
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestProcessedDERPLatencies_Parsing tests the core parsing and processing logic
// We test this by mocking the DERPLatency() return value via a wrapper
func TestProcessedDERPLatencies_Parsing(t *testing.T) {
	tests := []struct {
		name         string
		rawLatencies map[string]float64
		derpMap      *tailcfg.DERPMap
		wantCount    int
		wantFirst    DERPRegionLatency // Check first (lowest latency) entry
	}{
		{
			name: "single region IPv4 only",
			rawLatencies: map[string]float64{
				"10-v4": 0.025, // 25ms
			},
			derpMap: &tailcfg.DERPMap{
				Regions: map[int]*tailcfg.DERPRegion{
					10: {RegionID: 10, RegionName: "New York"},
				},
			},
			wantCount: 1,
			wantFirst: DERPRegionLatency{
				RegionID:   10,
				RegionName: "New York",
				LatencyMS:  25.0,
				Preferred:  true,
			},
		},
		{
			name: "single region with both IPv4 and IPv6 - chooses lower",
			rawLatencies: map[string]float64{
				"10-v4": 0.030, // 30ms
				"10-v6": 0.025, // 25ms - should be chosen
			},
			derpMap: &tailcfg.DERPMap{
				Regions: map[int]*tailcfg.DERPRegion{
					10: {RegionID: 10, RegionName: "New York"},
				},
			},
			wantCount: 1,
			wantFirst: DERPRegionLatency{
				RegionID:   10,
				RegionName: "New York",
				LatencyMS:  25.0, // Should pick v6 (lower)
				Preferred:  true,
			},
		},
		{
			name: "multiple regions - sorted by latency, lowest marked preferred",
			rawLatencies: map[string]float64{
				"10-v4": 0.050, // 50ms
				"17-v4": 0.015, // 15ms - should be first (preferred)
				"20-v4": 0.030, // 30ms - should be second
			},
			derpMap: &tailcfg.DERPMap{
				Regions: map[int]*tailcfg.DERPRegion{
					10: {RegionID: 10, RegionName: "New York"},
					17: {RegionID: 17, RegionName: "London"},
					20: {RegionID: 20, RegionName: "Tokyo"},
				},
			},
			wantCount: 3,
			wantFirst: DERPRegionLatency{
				RegionID:   17,
				RegionName: "London",
				LatencyMS:  15.0,
				Preferred:  true, // Lowest latency
			},
		},
		{
			name: "missing DERP map - uses region ID as name",
			rawLatencies: map[string]float64{
				"10-v4": 0.025,
			},
			derpMap:   nil, // No DERP map provided
			wantCount: 1,
			wantFirst: DERPRegionLatency{
				RegionID:   10,
				RegionName: "10", // Falls back to ID as string
				LatencyMS:  25.0,
				Preferred:  true,
			},
		},
		{
			name: "malformed keys are skipped",
			rawLatencies: map[string]float64{
				"10-v4":       0.025, // Valid
				"invalid":     0.050, // No dash - should be skipped
				"abc-v4":      0.030, // Non-numeric region - should be skipped
				"10":          0.020, // No protocol - should be skipped
				"10-v4-extra": 0.015, // Too many parts - should be skipped
			},
			derpMap: &tailcfg.DERPMap{
				Regions: map[int]*tailcfg.DERPRegion{
					10: {RegionID: 10, RegionName: "New York"},
				},
			},
			wantCount: 1, // Only the valid "10-v4" entry
			wantFirst: DERPRegionLatency{
				RegionID:   10,
				RegionName: "New York",
				LatencyMS:  25.0,
				Preferred:  true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a machine with DERP latency data
			// We'll need to construct this through the actual tailcfg types
			m := createMachineWithDERPLatency(t, tt.rawLatencies)

			result := m.ProcessedDERPLatencies(tt.derpMap)

			require.Len(t, result, tt.wantCount, "unexpected number of regions")
			if tt.wantCount > 0 {
				assert.Equal(t, tt.wantFirst, result[0], "first (preferred) region mismatch")
			}
		})
	}
}

// TestExitNode tests exit node detection logic
func TestExitNode(t *testing.T) {
	tests := []struct {
		name            string
		approvedRoutes  []string
		availableRoutes []string
		wantApproved    bool
		wantAdvertised  bool
		wantStatus      string
		wantHasExitNode bool
	}{
		{
			name:            "no exit node routes",
			approvedRoutes:  []string{"10.0.0.0/24"},
			availableRoutes: []string{"10.0.0.0/24"},
			wantApproved:    false,
			wantAdvertised:  false,
			wantStatus:      "",
			wantHasExitNode: false,
		},
		{
			name:            "exit node approved and advertised (IPv4)",
			approvedRoutes:  []string{"0.0.0.0/0"},
			availableRoutes: []string{"0.0.0.0/0"},
			wantApproved:    true,
			wantAdvertised:  false,
			wantStatus:      "Allowed",
			wantHasExitNode: true,
		},
		{
			name:            "exit node approved and advertised (IPv6)",
			approvedRoutes:  []string{"::/0"},
			availableRoutes: []string{"::/0"},
			wantApproved:    true,
			wantAdvertised:  false,
			wantStatus:      "Allowed",
			wantHasExitNode: true,
		},
		{
			name:            "exit node approved but not currently advertised",
			approvedRoutes:  []string{"0.0.0.0/0"},
			availableRoutes: []string{}, // Not advertising anymore
			wantApproved:    false,      // Requires BOTH approved AND advertised
			wantAdvertised:  false,
			wantStatus:      "",
			wantHasExitNode: false,
		},
		{
			name:            "exit node advertised but not approved",
			approvedRoutes:  []string{},
			availableRoutes: []string{"0.0.0.0/0"},
			wantApproved:    false,
			wantAdvertised:  true,
			wantStatus:      "Awaiting approval",
			wantHasExitNode: true,
		},
		{
			name:            "exit node with both IPv4 and IPv6 approved",
			approvedRoutes:  []string{"0.0.0.0/0", "::/0"},
			availableRoutes: []string{"0.0.0.0/0", "::/0"},
			wantApproved:    true,
			wantAdvertised:  false,
			wantStatus:      "Allowed",
			wantHasExitNode: true,
		},
		{
			name:            "exit node with subnet routes mixed in",
			approvedRoutes:  []string{"10.0.0.0/24", "0.0.0.0/0"},
			availableRoutes: []string{"10.0.0.0/24", "0.0.0.0/0", "192.168.0.0/24"},
			wantApproved:    true,
			wantAdvertised:  false,
			wantStatus:      "Allowed",
			wantHasExitNode: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := createMachineWithRoutes(tt.approvedRoutes, tt.availableRoutes)

			assert.Equal(t, tt.wantApproved, m.ExitNodeApproved(), "ExitNodeApproved mismatch")
			assert.Equal(t, tt.wantAdvertised, m.ExitNodeAdvertised(), "ExitNodeAdvertised mismatch")
			assert.Equal(t, tt.wantStatus, m.ExitNodeStatus(), "ExitNodeStatus mismatch")
			assert.Equal(t, tt.wantHasExitNode, m.HasExitNode(), "HasExitNode mismatch")
		})
	}
}

// createMachineWithDERPLatency is a test helper to create a Machine with mocked DERP latency data
// This is complex because we need to set up the tailcfg view types properly
func createMachineWithDERPLatency(t *testing.T, latencies map[string]float64) *Machine {
	// Create NetInfo with DERP latencies
	netInfo := &tailcfg.NetInfo{
		DERPLatency: latencies,
	}

	// Create the underlying Hostinfo structure
	hostinfo := &tailcfg.Hostinfo{
		NetInfo: netInfo,
	}

	return &Machine{
		WhoIsNode: &tailcfg.Node{
			Hostinfo: hostinfo.View(),
		},
	}
}

// TestSubnetRoutes tests subnet route detection and filtering logic
func TestSubnetRoutes(t *testing.T) {
	tests := []struct {
		name                  string
		approvedRoutes        []string
		availableRoutes       []string
		wantApprovedSubnets   []string
		wantAdvertisedSubnets []string
		wantHasSubnetRoutes   bool
	}{
		{
			name:                  "no routes at all",
			approvedRoutes:        []string{},
			availableRoutes:       []string{},
			wantApprovedSubnets:   nil,
			wantAdvertisedSubnets: nil,
			wantHasSubnetRoutes:   false,
		},
		{
			name:                  "only exit node routes (should be excluded)",
			approvedRoutes:        []string{"0.0.0.0/0", "::/0"},
			availableRoutes:       []string{"0.0.0.0/0", "::/0"},
			wantApprovedSubnets:   nil,
			wantAdvertisedSubnets: nil,
			wantHasSubnetRoutes:   false,
		},
		{
			name:                  "approved subnet routes only",
			approvedRoutes:        []string{"10.0.0.0/24", "192.168.1.0/24"},
			availableRoutes:       []string{"10.0.0.0/24", "192.168.1.0/24"},
			wantApprovedSubnets:   []string{"10.0.0.0/24", "192.168.1.0/24"},
			wantAdvertisedSubnets: nil,
			wantHasSubnetRoutes:   true,
		},
		{
			name:                  "advertised subnet routes awaiting approval",
			approvedRoutes:        []string{},
			availableRoutes:       []string{"10.0.0.0/24", "192.168.1.0/24"},
			wantApprovedSubnets:   nil,
			wantAdvertisedSubnets: []string{"10.0.0.0/24", "192.168.1.0/24"},
			wantHasSubnetRoutes:   true,
		},
		{
			name:                  "mix of approved and pending subnet routes",
			approvedRoutes:        []string{"10.0.0.0/24"},
			availableRoutes:       []string{"10.0.0.0/24", "192.168.1.0/24"},
			wantApprovedSubnets:   []string{"10.0.0.0/24"},
			wantAdvertisedSubnets: []string{"192.168.1.0/24"},
			wantHasSubnetRoutes:   true,
		},
		{
			name:                  "subnet routes mixed with exit node routes",
			approvedRoutes:        []string{"10.0.0.0/24", "0.0.0.0/0"},
			availableRoutes:       []string{"10.0.0.0/24", "0.0.0.0/0", "192.168.1.0/24", "::/0"},
			wantApprovedSubnets:   []string{"10.0.0.0/24"},    // Exit node routes excluded
			wantAdvertisedSubnets: []string{"192.168.1.0/24"}, // Exit node and approved routes excluded
			wantHasSubnetRoutes:   true,
		},
		{
			name:                  "IPv6 subnet routes",
			approvedRoutes:        []string{"fd00::/64"},
			availableRoutes:       []string{"fd00::/64", "fd01::/64"},
			wantApprovedSubnets:   []string{"fd00::/64"},
			wantAdvertisedSubnets: []string{"fd01::/64"},
			wantHasSubnetRoutes:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := createMachineWithRoutes(tt.approvedRoutes, tt.availableRoutes)

			assert.Equal(t, tt.wantApprovedSubnets, m.ApprovedSubnets(), "ApprovedSubnets mismatch")
			assert.Equal(t, tt.wantAdvertisedSubnets, m.AdvertisedSubnets(), "AdvertisedSubnets mismatch")
			assert.Equal(t, tt.wantHasSubnetRoutes, m.HasSubnetRoutes(), "HasSubnetRoutes mismatch")
		})
	}
}

// createMachineWithRoutes is a test helper to create a Machine with route configuration
func createMachineWithRoutes(approved, available []string) *Machine {
	return &Machine{
		Node: &headscale.Node{
			ApprovedRoutes:  approved,
			AvailableRoutes: available,
		},
	}
}
