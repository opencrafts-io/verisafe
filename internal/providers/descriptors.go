package providers

import (
	"strings"

	"github.com/opencrafts-io/verisafe/internal/config"
)

// Google scope constants. Spelled out rather than inlined so the capability
// map below reads as a table and typos surface at compile time.
const (
	googleScopeEmail    = "https://www.googleapis.com/auth/userinfo.email"
	googleScopeProfile  = "https://www.googleapis.com/auth/userinfo.profile"
	googleScopeOpenID   = "openid"
	googleScopeCalendar = "https://www.googleapis.com/auth/calendar"
	googleScopeTasks    = "https://www.googleapis.com/auth/tasks"
)

// descriptors is the complete provider set.
//
// To add a provider: append a Descriptor here, add its credentials lookup to
// credentialsFor below, and add the client id/secret to internal/config. If it
// should also be a sign-in provider, register it with goth in
// internal/auth/auth.go. Nothing else in the codebase needs to change.
var descriptors = []Descriptor{
	{
		Name:     "google",
		AuthURL:  "https://accounts.google.com/o/oauth2/auth",
		TokenURL: "https://oauth2.googleapis.com/token",

		// Identity only. Calendar and Tasks are acquired incrementally, at the
		// point the user does something needing them, rather than being
		// demanded on the sign-in consent screen.
		LoginScopes: []string{googleScopeEmail, googleScopeProfile},

		Capabilities: map[Capability][]string{
			CapabilityIdentity: {googleScopeEmail, googleScopeProfile},
			CapabilityCalendar: {googleScopeCalendar},
			CapabilityTasks:    {googleScopeTasks},
		},

		SupportsRefresh:     true,
		SupportsIncremental: true,
		SupportsPKCE:        true,
		ReportsScope:        true,
		ScopeDelimiter:      " ",
		Normalize:           normalizeGoogle,

		ExtraAuthParams: map[string]string{
			// offline is what makes Google return a refresh token at all.
			"access_type": "offline",
			// Without this, a post-cutover login requesting only identity
			// would complete a *new*, narrower authorization and could
			// silently drop a user's existing calendar access. With it, the
			// issued tokens carry the union of old and newly-requested scopes.
			"include_granted_scopes": "true",
		},

		// What every login requested before incremental authorization existed.
		// Seeds presumed grants for pre-cutover accounts; see the backfill in
		// 20260801090000_add_oauth_grants.sql.
		HistoricalLoginScopes: []string{
			googleScopeEmail,
			googleScopeProfile,
			googleScopeCalendar,
			googleScopeTasks,
		},
	},
	{
		Name:     "spotify",
		AuthURL:  "https://accounts.spotify.com/authorize",
		TokenURL: "https://accounts.spotify.com/api/token",

		LoginScopes: []string{"user-read-email", "user-read-private"},

		Capabilities: map[Capability][]string{
			CapabilityIdentity: {"user-read-email", "user-read-private"},
			CapabilityPlayback: {
				"user-read-playback-state",
				"user-modify-playback-state",
				"user-read-currently-playing",
				"app-remote-control",
			},
			CapabilityPlaylist: {
				"playlist-read-private",
				"playlist-modify-private",
				"playlist-modify-public",
			},
			CapabilityLibrary: {
				"user-read-recently-played",
				"user-top-read",
				"user-follow-read",
				"user-follow-modify",
			},
		},

		SupportsRefresh:     true,
		SupportsIncremental: true,
		SupportsPKCE:        true,
		ReportsScope:        true,
		ScopeDelimiter:      " ",
		Normalize:           normalizeSpotify,

		HistoricalLoginScopes: []string{
			"user-read-playback-state",
			"user-modify-playback-state",
			"user-read-currently-playing",
			"user-read-recently-played",
			"user-top-read",
			"app-remote-control",
			"playlist-read-private",
			"playlist-modify-private",
			"playlist-modify-public",
			"user-follow-modify",
			"user-follow-read",
			"user-read-email",
			"user-read-private",
		},
	},
	{
		Name: "apple",

		// Apple is sign-in only. It never reports a scope field, so its grants
		// can never move from presumed to verified, and it does not support
		// additive consent — hence no incremental flow and no capabilities
		// beyond identity. Refresh tokens exist but are validated at most
		// daily and there is nothing to broker them for.
		LoginScopes: []string{"name", "email"},

		Capabilities: map[Capability][]string{
			CapabilityIdentity: {"name", "email"},
		},

		SupportsRefresh:     false,
		SupportsIncremental: false,
		SupportsPKCE:        false,
		ReportsScope:        false,
		ScopeDelimiter:      " ",
		Normalize:           normalizeLower,

		HistoricalLoginScopes: []string{"name", "email"},
	},
}

// credentialsFor resolves each provider's client credentials from config.
var credentialsFor = map[string]clientCredentials{
	"google": func(c *config.Config) (string, string) {
		return c.AuthenticationConfig.GoogleClientID,
			c.AuthenticationConfig.GoogleClientSecret
	},
	"spotify": func(c *config.Config) (string, string) {
		return c.AuthenticationConfig.SpotifyClientID,
			c.AuthenticationConfig.SpotifyClientSecret
	},
	"apple": func(c *config.Config) (string, string) {
		// Apple's "secret" is a short-lived signed JWT generated per process
		// start (see auth.GenerateAppleClientSecret), not a static value, so
		// it is deliberately not resolvable here. Apple does not participate
		// in the incremental or broker flows.
		return c.AuthenticationConfig.AppleClientID, ""
	},
}

// normalizeGoogle canonicalizes a Google scope to its full URL form.
//
// This is load-bearing, not cosmetic. Google *accepts* the short aliases
// "email" and "profile" in an authorization request, but always *reports* them
// back as the full userinfo URLs in the token response. Comparing raw strings
// would mark "email" as permanently missing on every grant, and the broker
// would demand re-authorization forever.
func normalizeGoogle(s string) string {
	s = strings.TrimSpace(s)
	switch s {
	case "email", "userinfo.email":
		return googleScopeEmail
	case "profile", "userinfo.profile":
		return googleScopeProfile
	case "openid":
		return googleScopeOpenID
	}
	return strings.TrimSuffix(s, "/")
}

// normalizeSpotify canonicalizes a Spotify scope. Spotify uses bare lowercase
// words and is otherwise consistent.
func normalizeSpotify(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func normalizeLower(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
