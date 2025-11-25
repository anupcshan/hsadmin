package models

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/anupcshan/hsadmin/internal/format"
	headscale "github.com/juanfont/headscale/gen/go/headscale/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
)

// Machine represents a node with combined data from Headscale and tsnet
type Machine struct {
	Node       *headscale.Node      // From Headscale API
	PeerStatus *ipnstate.PeerStatus // From tsnet Status()
	WhoIsNode  *tailcfg.Node        // From tsnet WhoIs() - has HostInfo with version, DERP latency, etc
	Online     bool
}

// DERPRegionLatency holds processed DERP region latency information
type DERPRegionLatency struct {
	RegionID   int
	RegionName string
	LatencyMS  float64
	Preferred  bool
}

// StatusText returns "Online" or "Offline"
func (m *Machine) StatusText() string {
	if m.Online {
		return "Online"
	}
	return "Offline"
}

// StatusDotClass returns CSS class for status indicator dot
func (m *Machine) StatusDotClass() string {
	if m.Online {
		return "bg-green-500"
	}
	return "bg-gray-400"
}

// LastSeenShort returns formatted time like "6:53 PM PST" or "Jan 2" for older
func (m *Machine) LastSeenShort() string {
	var lastSeen *timestamppb.Timestamp
	if m.Node != nil {
		lastSeen = m.Node.LastSeen
	}
	return format.LastSeenShort(lastSeen, m.Online)
}

// LastSeenFull returns full timestamp for hover text
func (m *Machine) LastSeenFull() string {
	if m.Node == nil {
		return ""
	}
	return format.LastSeenFull(m.Node.LastSeen)
}

// Hostname returns the machine hostname
func (m *Machine) Hostname() string {
	if m.Node != nil && m.Node.GivenName != "" {
		return m.Node.GivenName
	}
	if m.Node != nil && m.Node.Name != "" {
		return m.Node.Name
	}
	return "-"
}

// ID returns the node ID
func (m *Machine) ID() uint64 {
	if m.Node != nil {
		return m.Node.Id
	}
	return 0
}

// User returns the user/namespace
func (m *Machine) User() string {
	if m.Node != nil && m.Node.User != nil {
		return m.Node.User.Name
	}
	return "-"
}

// TailscaleIPs returns the list of Tailscale IPs
func (m *Machine) TailscaleIPs() []string {
	if m.Node == nil {
		return nil
	}
	return m.Node.IpAddresses
}

// PrimaryIP returns the first Tailscale IP (usually IPv4)
func (m *Machine) PrimaryIP() string {
	ips := m.TailscaleIPs()
	if len(ips) > 0 {
		return ips[0]
	}
	return "-"
}

// OS returns the operating system
func (m *Machine) OS() string {
	if m.WhoIsNode != nil && m.WhoIsNode.Hostinfo.Valid() {
		return m.WhoIsNode.Hostinfo.OS()
	}
	if m.PeerStatus != nil && m.PeerStatus.OS != "" {
		return m.PeerStatus.OS
	}
	return "-"
}

// TailscaleVersion returns the Tailscale client version
func (m *Machine) TailscaleVersion() string {
	if m.WhoIsNode != nil && m.WhoIsNode.Hostinfo.Valid() {
		return m.WhoIsNode.Hostinfo.IPNVersion()
	}
	return "-"
}

// Tags returns the node tags
func (m *Machine) Tags() []string {
	if m.Node != nil && len(m.Node.ForcedTags) > 0 {
		return m.Node.ForcedTags
	}
	return nil
}

// DERPRegionName returns the DERP region name/ID
func (m *Machine) DERPRegionName() string {
	if m.PeerStatus != nil && m.PeerStatus.CurAddr != "" {
		return m.PeerStatus.CurAddr
	}
	return "-"
}

// UsingRelay returns true if connection is relayed via DERP
func (m *Machine) UsingRelay() bool {
	if m.PeerStatus != nil {
		return m.PeerStatus.Relay != ""
	}
	return false
}

// ConnectionType returns "Direct" or "Relay"
func (m *Machine) ConnectionType() string {
	if m.UsingRelay() {
		return "Relay"
	}
	return "Direct"
}

// DERPLatency returns the DERP latency map (region -> latency in seconds)
func (m *Machine) DERPLatency() map[string]float64 {
	if m.WhoIsNode != nil && m.WhoIsNode.Hostinfo.Valid() {
		netInfo := m.WhoIsNode.Hostinfo.NetInfo()
		if netInfo.Valid() {
			return netInfo.DERPLatency().AsMap()
		}
	}
	return nil
}

// FormattedDERPLatency returns a formatted string of DERP latencies
func (m *Machine) FormattedDERPLatency() string {
	latencies := m.DERPLatency()
	if len(latencies) == 0 {
		return "No data"
	}

	result := ""
	for region, latency := range latencies {
		if result != "" {
			result += ", "
		}
		// latency is in seconds, convert to ms
		result += fmt.Sprintf("%s: %.0fms", region, latency*1000)
	}
	return result
}

// ProcessedDERPLatencies returns DERP latencies with region names looked up from the DERP map.
// It parses keys like "10-v4", "17-v4", "999-v4", "1-v6" to extract region IDs and protocols,
// groups by region, and chooses the lower latency between v4 and v6 for each region.
func (m *Machine) ProcessedDERPLatencies(derpMap *tailcfg.DERPMap) []DERPRegionLatency {
	rawLatencies := m.DERPLatency()
	if len(rawLatencies) == 0 {
		return nil
	}

	// Group latencies by region ID, tracking both v4 and v6
	type latencyPair struct {
		v4      *float64
		v6      *float64
		v4Count int
		v6Count int
	}
	regionLatencies := make(map[int]*latencyPair)

	for key, latency := range rawLatencies {
		// Parse key like "10-v4" or "17-v6"
		parts := strings.Split(key, "-")
		if len(parts) != 2 {
			continue
		}

		regionID, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}

		protocol := parts[1]
		if _, exists := regionLatencies[regionID]; !exists {
			regionLatencies[regionID] = &latencyPair{}
		}

		// Convert to milliseconds
		latencyMS := latency * 1000

		switch protocol {
		case "v4":
			if regionLatencies[regionID].v4 == nil || latencyMS < *regionLatencies[regionID].v4 {
				regionLatencies[regionID].v4 = &latencyMS
			}
			regionLatencies[regionID].v4Count++
		case "v6":
			if regionLatencies[regionID].v6 == nil || latencyMS < *regionLatencies[regionID].v6 {
				regionLatencies[regionID].v6 = &latencyMS
			}
			regionLatencies[regionID].v6Count++
		}
	}

	// Build result array, choosing the lower latency between v4 and v6
	var result []DERPRegionLatency
	for regionID, pair := range regionLatencies {
		// Get region name from DERP map
		regionName := fmt.Sprintf("%d", regionID)
		if derpMap != nil && derpMap.Regions != nil {
			if region, exists := derpMap.Regions[regionID]; exists {
				regionName = region.RegionName
			}
		}

		// Choose the lower latency
		var bestLatency float64
		if pair.v4 != nil && pair.v6 != nil {
			if *pair.v4 <= *pair.v6 {
				bestLatency = *pair.v4
			} else {
				bestLatency = *pair.v6
			}
		} else if pair.v4 != nil {
			bestLatency = *pair.v4
		} else if pair.v6 != nil {
			bestLatency = *pair.v6
		} else {
			continue
		}

		result = append(result, DERPRegionLatency{
			RegionID:   regionID,
			RegionName: regionName,
			LatencyMS:  bestLatency,
			Preferred:  false, // We'll determine this later based on lowest latency
		})
	}

	// Sort by latency (lowest first) and mark the best one as preferred
	if len(result) > 0 {
		sort.Slice(result, func(i, j int) bool {
			return result[i].LatencyMS < result[j].LatencyMS
		})
		result[0].Preferred = true
	}

	return result
}

// OSHostname returns the OS-reported hostname (separate from machine name)
func (m *Machine) OSHostname() string {
	if m.PeerStatus != nil && m.PeerStatus.HostName != "" {
		return m.PeerStatus.HostName
	}
	if m.WhoIsNode != nil && m.WhoIsNode.Hostinfo.Valid() {
		return m.WhoIsNode.Hostinfo.Hostname()
	}
	return "-"
}

// NodeKey returns the node's public key
func (m *Machine) NodeKey() string {
	if m.Node != nil && m.Node.NodeKey != "" {
		return m.Node.NodeKey
	}
	return "-"
}

// NodeKeyShort returns abbreviated node key for display
func (m *Machine) NodeKeyShort() string {
	key := m.NodeKey()
	if key == "-" || len(key) < 16 {
		return key
	}
	return key[:16] + "..."
}

// Created returns the creation timestamp
func (m *Machine) Created() string {
	if m.Node != nil && m.Node.CreatedAt != nil {
		return m.Node.CreatedAt.AsTime().Format("Jan 2, 2006 at 3:04 PM MST")
	}
	return "-"
}

// KeyExpiry returns the key expiration status
func (m *Machine) KeyExpiry() string {
	if m.Node == nil || m.Node.Expiry == nil {
		return "No expiry"
	}
	expiry := m.Node.Expiry.AsTime()
	if expiry.IsZero() || expiry.Year() > 9000 {
		return "No expiry"
	}
	return expiry.Format("Jan 2, 2006 at 3:04 PM MST")
}

// OSVersion returns the OS version string
func (m *Machine) OSVersion() string {
	if m.WhoIsNode != nil && m.WhoIsNode.Hostinfo.Valid() {
		return m.WhoIsNode.Hostinfo.OSVersion()
	}
	return "-"
}

// AutoUpdate returns whether auto-update is enabled
func (m *Machine) AutoUpdate() string {
	if m.WhoIsNode != nil && m.WhoIsNode.Hostinfo.Valid() {
		if m.WhoIsNode.Hostinfo.GoArch() != "" {
			// This is a heuristic - need to check actual field if available
			return "false"
		}
	}
	return "false"
}

// ReleaseTrack returns the Tailscale release track
func (m *Machine) ReleaseTrack() string {
	// This info may not be directly available - would need to check hostinfo
	return "stable"
}

// StateEncrypted returns whether state is encrypted
func (m *Machine) StateEncrypted() string {
	// This info may not be directly available
	return "false"
}

// ShortDomain returns the short MagicDNS name
func (m *Machine) ShortDomain() string {
	if m.WhoIsNode != nil && m.WhoIsNode.ComputedName != "" {
		return m.WhoIsNode.ComputedName
	}
	return m.Hostname()
}

// FullDomain returns the full MagicDNS name
func (m *Machine) FullDomain() string {
	if m.WhoIsNode != nil && m.WhoIsNode.ComputedNameWithHost != "" {
		return m.WhoIsNode.ComputedNameWithHost
	}
	return "-"
}

// Endpoints returns the list of endpoint addresses
func (m *Machine) Endpoints() []string {
	if m.WhoIsNode != nil {
		var endpoints []string
		for _, endpoint := range m.WhoIsNode.Endpoints {
			endpoints = append(endpoints, endpoint.String())
		}
		return endpoints
	}
	return nil
}

// ClientConnectivity returns network capability information
func (m *Machine) ClientConnectivity() map[string]string {
	result := make(map[string]string)

	if m.WhoIsNode != nil && m.WhoIsNode.Hostinfo.Valid() {
		netInfo := m.WhoIsNode.Hostinfo.NetInfo()
		if netInfo.Valid() {
			// Varies - MappingVariesByDestIP
			if varies, ok := netInfo.MappingVariesByDestIP().Get(); ok {
				result["Varies"] = boolToYesNo(varies)
			} else {
				result["Varies"] = "—"
			}

			// Hairpinning
			if hairpin, ok := netInfo.HairPinning().Get(); ok {
				result["Hairpinning"] = boolToYesNo(hairpin)
			} else {
				result["Hairpinning"] = "—"
			}

			// IPv6 - WorkingIPv6
			if ipv6, ok := netInfo.WorkingIPv6().Get(); ok {
				result["IPv6"] = boolToYesNo(ipv6)
			} else {
				result["IPv6"] = "—"
			}

			// UDP - WorkingUDP
			if udp, ok := netInfo.WorkingUDP().Get(); ok {
				result["UDP"] = boolToYesNo(udp)
			} else {
				result["UDP"] = "—"
			}

			// UPnP
			if upnp, ok := netInfo.UPnP().Get(); ok {
				result["UPnP"] = boolToYesNo(upnp)
			} else {
				result["UPnP"] = "—"
			}

			// PCP
			if pcp, ok := netInfo.PCP().Get(); ok {
				result["PCP"] = boolToYesNo(pcp)
			} else {
				result["PCP"] = "—"
			}

			// NAT-PMP
			if pmp, ok := netInfo.PMP().Get(); ok {
				result["NAT-PMP"] = boolToYesNo(pmp)
			} else {
				result["NAT-PMP"] = "—"
			}
		}
	}

	return result
}

// boolToYesNo converts bool to "Yes" or "No"
func boolToYesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

// ApprovedSubnets returns the list of approved subnet routes (excluding exit node routes)
func (m *Machine) ApprovedSubnets() []string {
	if m.Node == nil {
		return nil
	}

	var subnets []string
	for _, route := range m.Node.ApprovedRoutes {
		// Exclude exit node routes
		if route != "0.0.0.0/0" && route != "::/0" {
			subnets = append(subnets, route)
		}
	}
	return subnets
}

// AdvertisedSubnets returns the list of advertised (pending) subnet routes (excluding exit node routes)
func (m *Machine) AdvertisedSubnets() []string {
	if m.Node == nil {
		return nil
	}

	// Create a map of approved routes for quick lookup
	approvedMap := make(map[string]bool)
	for _, route := range m.Node.ApprovedRoutes {
		approvedMap[route] = true
	}

	// Find routes that are available but not approved
	var pending []string
	for _, route := range m.Node.AvailableRoutes {
		if !approvedMap[route] {
			// Exclude exit node routes
			if route != "0.0.0.0/0" && route != "::/0" {
				pending = append(pending, route)
			}
		}
	}
	return pending
}

// HasSubnetRoutes returns true if this machine has any subnet routes (approved or advertised), excluding exit node routes
func (m *Machine) HasSubnetRoutes() bool {
	return len(m.ApprovedSubnets()) > 0 || len(m.AdvertisedSubnets()) > 0
}

// ExitNodeApproved returns whether this machine is approved as an exit node AND currently advertising it
func (m *Machine) ExitNodeApproved() bool {
	if m.Node == nil {
		return false
	}

	// First check if it's approved
	hasApproved := false
	for _, route := range m.Node.ApprovedRoutes {
		if route == "0.0.0.0/0" || route == "::/0" {
			hasApproved = true
			break
		}
	}

	if !hasApproved {
		return false
	}

	// Then check if it's currently being advertised
	for _, route := range m.Node.AvailableRoutes {
		if route == "0.0.0.0/0" || route == "::/0" {
			return true
		}
	}

	return false
}

// ExitNodeAdvertised returns whether this machine has advertised itself as an exit node (pending approval)
func (m *Machine) ExitNodeAdvertised() bool {
	if m.Node == nil {
		return false
	}

	// Create a map of approved routes for quick lookup
	approvedMap := make(map[string]bool)
	for _, route := range m.Node.ApprovedRoutes {
		approvedMap[route] = true
	}

	// Check if exit node routes are advertised but not approved
	for _, route := range m.Node.AvailableRoutes {
		if (route == "0.0.0.0/0" || route == "::/0") && !approvedMap[route] {
			return true
		}
	}
	return false
}

// ExitNodeStatus returns the exit node status: "Allowed", "Awaiting approval", or ""
func (m *Machine) ExitNodeStatus() string {
	if m.ExitNodeApproved() {
		return "Allowed"
	}
	if m.ExitNodeAdvertised() {
		return "Awaiting approval"
	}
	return ""
}

// HasExitNode returns true if this machine has exit node capability (approved or advertised)
func (m *Machine) HasExitNode() bool {
	return m.ExitNodeApproved() || m.ExitNodeAdvertised()
}

// TagsString returns tags as a comma-separated string
func (m *Machine) TagsString() string {
	tags := m.Tags()
	if len(tags) == 0 {
		return ""
	}
	return strings.Join(tags, ", ")
}
