package format

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// LastSeenShort returns formatted time like "Active now", "3:04 PM MST" or "Jan 2"
func LastSeenShort(lastSeen *timestamppb.Timestamp, isOnline bool) string {
	if isOnline {
		return "Active now"
	}

	if lastSeen == nil {
		return "-"
	}

	lastSeenTime := lastSeen.AsTime().Local()
	now := time.Now()

	// If today or yesterday, show time
	if now.Sub(lastSeenTime) < 24*time.Hour {
		return lastSeenTime.Format("3:04 PM MST")
	}

	// Otherwise show date
	return lastSeenTime.Format("Jan 2")
}

// LastSeenFull returns full timestamp for hover text
func LastSeenFull(lastSeen *timestamppb.Timestamp) string {
	if lastSeen == nil {
		return ""
	}
	return lastSeen.AsTime().Local().Format("January 2, 2006 at 3:04:05 PM MST")
}

// LastSeenShortFromTime returns formatted time for a regular time.Time pointer
func LastSeenShortFromTime(lastSeen *time.Time, isOnline bool) string {
	if isOnline {
		return "Connected"
	}

	if lastSeen == nil {
		return "-"
	}

	lastSeenLocal := lastSeen.Local()
	now := time.Now()

	// If today or yesterday, show time
	if now.Sub(lastSeenLocal) < 24*time.Hour {
		return lastSeenLocal.Format("3:04 PM MST")
	}

	// Otherwise show date
	return lastSeenLocal.Format("Jan 2")
}

// LastSeenFullFromTime returns full timestamp from a regular time.Time pointer
func LastSeenFullFromTime(lastSeen *time.Time, isOnline bool) string {
	if isOnline {
		return "Currently connected"
	}

	if lastSeen == nil {
		return ""
	}

	return lastSeen.Local().Format("January 2, 2006 at 3:04:05 PM MST")
}
