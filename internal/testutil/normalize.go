package testutil

import (
	"regexp"
	"strings"
)

// NormalizeHTML replaces variable content with stable placeholders for golden file comparison.
// This allows us to test HTML structure without false positives from timing variance.
func NormalizeHTML(html string) string {
	// Replace absolute timestamps (e.g., "Jan 2, 2006 at 3:04 PM MST", "November 7, 2025 at 6:44:55 AM UTC")
	html = regexp.MustCompile(`[A-Z][a-z]+\s+\d{1,2},\s+\d{4}\s+at\s+\d{1,2}:\d{2}(:\d{2})?\s+(AM|PM)\s+[A-Z]{3,4}`).
		ReplaceAllString(html, "TIMESTAMP")

	// Replace short date format (e.g., "Nov 8", "Jan 2", "Dec 31")
	html = regexp.MustCompile(`[A-Z][a-z]{2}\s+\d{1,2}`).
		ReplaceAllString(html, "DATE")

	// Replace short time format (e.g., "6:44 AM UTC")
	html = regexp.MustCompile(`\d{1,2}:\d{2}\s+(AM|PM)\s+[A-Z]{3,4}`).
		ReplaceAllString(html, "TIME")

	// Replace ISO timestamps (e.g., "2024-01-01T12:00:00Z")
	html = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z?`).
		ReplaceAllString(html, "TIMESTAMP")

	// Replace relative times (e.g., "5 minutes ago", "2 hours ago")
	html = regexp.MustCompile(`\d+\s+(second|minute|hour|day)s?\s+ago`).
		ReplaceAllString(html, "TIME_AGO")

	// Replace "Active now" and "Connected" status
	html = strings.ReplaceAll(html, "Active now", "ACTIVE_NOW")
	html = strings.ReplaceAll(html, "Connected", "CONNECTED")

	// Replace DERP latencies (e.g., "123ms", "45.6ms")
	html = regexp.MustCompile(`\d+(\.\d+)?\s*ms`).
		ReplaceAllString(html, "XXms")

	// Replace Tailscale IPv4 addresses (100.64.0.0/10 range)
	html = regexp.MustCompile(`100\.\d{1,3}\.\d{1,3}\.\d{1,3}`).
		ReplaceAllString(html, "100.64.X.X")

	// Replace Tailscale IPv6 addresses (fd7a: prefix)
	html = regexp.MustCompile(`fd7a:[0-9a-f:]+`).
		ReplaceAllString(html, "fd7a:XXXX:XXXX")

	// Replace endpoint IPv6:port combinations (e.g., [2601:647:6700:920::134f]:47864)
	html = regexp.MustCompile(`\[[0-9a-f:]+\]:\d+`).
		ReplaceAllString(html, "[IPV6]:PORT")

	// Replace endpoint IP:port combinations (any IPv4)
	html = regexp.MustCompile(`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}:\d+`).
		ReplaceAllString(html, "IP:PORT")

	// Replace node and user IDs (numeric)
	html = regexp.MustCompile(`>ID: \d+<`).
		ReplaceAllString(html, ">ID: NNN<")

	// Replace profile picture URLs (http/https URLs in img src)
	html = regexp.MustCompile(`<img src="https?://[^"]+"`).
		ReplaceAllString(html, `<img src="PROFILE_PIC_URL"`)

	// Replace node keys (base64-like strings starting with nodekey:)
	html = regexp.MustCompile(`nodekey:[a-f0-9]{64}`).
		ReplaceAllString(html, "nodekey:XXXX")

	// Replace abbreviated node keys (e.g., "nodekey:78da1abd...")
	html = regexp.MustCompile(`nodekey:[a-f0-9]{8,}\.\.\.`).
		ReplaceAllString(html, "nodekey:XXXX...")

	// Replace OS/kernel versions within data-os-version elements
	// Matches everything between data-os-version> and the closing <
	html = regexp.MustCompile(`(data-os-version[^>]*>)[^<]+`).
		ReplaceAllString(html, "${1}KERNEL_VERSION")

	// Replace Tailscale versions within data-ts-version elements
	// Matches everything between data-ts-version> and the closing <
	html = regexp.MustCompile(`(data-ts-version[^>]*>)[^<]+`).
		ReplaceAllString(html, "${1}TAILSCALE_VERSION")

	// Normalize whitespace (collapse multiple spaces/newlines into single space)
	// This makes golden files more stable against formatting changes
	html = regexp.MustCompile(`\s+`).ReplaceAllString(html, " ")

	// Trim leading/trailing whitespace
	html = strings.TrimSpace(html)

	return html
}
