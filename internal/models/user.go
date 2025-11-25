package models

import (
	"fmt"
	"strings"
	"time"

	"github.com/anupcshan/hsadmin/internal/format"
	headscale "github.com/juanfont/headscale/gen/go/headscale/v1"
)

// User represents a Headscale user with machine count
type User struct {
	HeadscaleUser       *headscale.User
	MachineCount        int
	LastSeenTime        *time.Time // Most recent activity across all user's machines
	HasConnectedMachine bool       // Whether any machine is currently connected
}

// Name returns the user name
func (u *User) Name() string {
	if u.HeadscaleUser != nil {
		return u.HeadscaleUser.Name
	}
	return "-"
}

// ID returns the user ID as a string
func (u *User) ID() string {
	if u.HeadscaleUser != nil {
		return fmt.Sprintf("%d", u.HeadscaleUser.Id)
	}
	return "-"
}

// CreatedAt returns the formatted creation time
func (u *User) CreatedAt() string {
	if u.HeadscaleUser != nil && u.HeadscaleUser.CreatedAt != nil {
		return u.HeadscaleUser.CreatedAt.AsTime().Format("January 2, 2006")
	}
	return "-"
}

// CreatedAtShort returns the short formatted creation time
func (u *User) CreatedAtShort() string {
	if u.HeadscaleUser == nil || u.HeadscaleUser.CreatedAt == nil {
		return "-"
	}

	createdAt := u.HeadscaleUser.CreatedAt.AsTime()
	now := time.Now()

	// If within the last year, show "MMM DD"
	if now.Sub(createdAt) < 365*24*time.Hour {
		return createdAt.Format("Jan 2")
	}

	// Otherwise show year
	return createdAt.Format("Jan 2, 2006")
}

// ProfilePicURL returns the profile picture URL if available (from OIDC)
func (u *User) ProfilePicURL() string {
	if u.HeadscaleUser != nil {
		return u.HeadscaleUser.ProfilePicUrl
	}
	return ""
}

// HasProfilePic returns true if the user has a profile picture URL
func (u *User) HasProfilePic() bool {
	return u.ProfilePicURL() != ""
}

// DisplayName returns the display name if available, otherwise returns Name
func (u *User) DisplayName() string {
	if u.HeadscaleUser != nil && u.HeadscaleUser.DisplayName != "" {
		return u.HeadscaleUser.DisplayName
	}
	return u.Name()
}

// Initials returns the first letter of the username for avatar display (uppercased)
func (u *User) Initials() string {
	// Prefer DisplayName if available, otherwise use Name
	name := u.DisplayName()

	if name == "-" || len(name) == 0 {
		return "?"
	}

	// Return first character, uppercased
	return strings.ToUpper(name[0:1])
}

// Provider returns the authentication provider (e.g., "oidc", "")
func (u *User) Provider() string {
	if u.HeadscaleUser != nil {
		return u.HeadscaleUser.Provider
	}
	return ""
}

// HasProvider returns true if the user was created via an external provider (OIDC)
func (u *User) HasProvider() bool {
	return u.Provider() != ""
}

// ProviderBadge returns a display-friendly provider name for badges
func (u *User) ProviderBadge() string {
	provider := u.Provider()
	if provider == "" {
		return ""
	}
	// Capitalize provider name for display
	return strings.ToUpper(provider)
}

// LastSeenShort returns formatted time like "Connected", "3:04 PM MST" or "Jan 2" (based on user's machines)
func (u *User) LastSeenShort() string {
	return format.LastSeenShortFromTime(u.LastSeenTime, u.HasConnectedMachine)
}

// LastSeenFull returns full timestamp for hover text
func (u *User) LastSeenFull() string {
	return format.LastSeenFullFromTime(u.LastSeenTime, u.HasConnectedMachine)
}
