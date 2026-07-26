// Package providers is the single source of truth about the third-party
// OAuth2 providers Verisafe integrates with: which scopes each one exposes,
// what those scopes are called on the wire, whether tokens can be refreshed,
// and how to talk to the token endpoint.
//
// # Why a registry
//
// Before this package, provider knowledge was scattered: scope lists lived as
// package-level slices in internal/auth, "google" was compared as a string in
// several places, and nothing recorded what a user had actually granted.
// Adding a provider meant touching every one of those sites.
//
// Here, a provider is one Descriptor literal in descriptors.go. Adding
// Microsoft is that literal plus client credentials in config — the broker,
// the refresh service, scope diffing, and the database schema need no changes.
// Nothing outside this package should switch on a provider name.
//
// # Capabilities vs scopes
//
// Callers ask for a Capability ("calendar"), never a raw scope string. The
// Descriptor maps that onto provider-specific scopes. This keeps the wire
// vocabulary out of API contracts, lets one capability expand to several
// scopes, and means a caller needing something unmapped has to add it here
// rather than smuggling an arbitrary scope through the broker.
//
// # Dependencies
//
// Pure data and pure functions over stdlib, golang.org/x/oauth2, and
// internal/config. No database, no HTTP handlers, no internal/auth — so it can
// be imported from internal/auth, internal/handlers, and internal/service
// alike without an import cycle.
package providers

import (
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/opencrafts-io/verisafe/internal/config"
	"golang.org/x/oauth2"
)

// Capability is an abstract, provider-independent permission a caller can ask
// for. It is the only permission vocabulary that crosses an API boundary.
type Capability string

const (
	// CapabilityIdentity is sign-in: who the user is. Always granted at login.
	CapabilityIdentity Capability = "identity"
	CapabilityCalendar Capability = "calendar"
	CapabilityTasks    Capability = "tasks"
	CapabilityPlayback Capability = "playback"
	CapabilityPlaylist Capability = "playlist"
	CapabilityLibrary  Capability = "library"
)

// Descriptor is the complete declarative definition of one OAuth2 provider.
type Descriptor struct {
	// Name is the canonical lowercase identifier used in URLs and stored in
	// oauth_grants.provider.
	Name string

	// AuthURL and TokenURL are the provider's OAuth2 endpoints. Empty AuthURL
	// means the provider is login-only (handled entirely by goth) and does not
	// participate in the incremental flow.
	AuthURL  string
	TokenURL string

	// LoginScopes are requested during sign-in. Kept minimal — anything beyond
	// identity should be acquired through the incremental flow, when the user
	// is doing the thing that needs it.
	LoginScopes []string

	// Capabilities maps a capability onto the provider-specific scope strings
	// that satisfy it. This is the only place the two vocabularies are bound.
	Capabilities map[Capability][]string

	// SupportsRefresh reports whether the provider issues refresh tokens that
	// can be exchanged for new access tokens.
	SupportsRefresh bool

	// SupportsIncremental reports whether the provider supports additive
	// consent — granting a new scope without discarding existing grants.
	SupportsIncremental bool

	// SupportsPKCE reports whether the authorization endpoint accepts
	// code_challenge/code_verifier.
	SupportsPKCE bool

	// ReportsScope reports whether the token endpoint returns a "scope" field.
	// This decides whether a grant can ever move from presumed to verified:
	// Google and Spotify report it, Apple does not.
	ReportsScope bool

	// ScopeDelimiter separates scopes in the provider's scope string.
	ScopeDelimiter string

	// Normalize maps a scope string onto its canonical form. Providers are
	// inconsistent about this — see normalizeGoogle for why it matters.
	Normalize func(string) string

	// ExtraAuthParams are added to every authorization URL for this provider,
	// e.g. access_type=offline and include_granted_scopes=true for Google.
	ExtraAuthParams map[string]string

	// HistoricalLoginScopes is what every pre-cutover login requested. It
	// seeds *presumed* grants for accounts that predate scope recording, and
	// is never treated as verified truth. nil means nothing may be presumed.
	HistoricalLoginScopes []string
}

// ClientCredentials resolves this provider's client id and secret from config.
// Kept as a func on the descriptor so config lookup is declarative too.
type clientCredentials func(*config.Config) (id, secret string)

// Registry holds every configured provider.
type Registry struct {
	byName map[string]Descriptor
	cfg    *config.Config
}

// NewRegistry builds the registry from the static descriptor set.
func NewRegistry(cfg *config.Config) *Registry {
	byName := make(map[string]Descriptor, len(descriptors))
	for _, d := range descriptors {
		byName[d.Name] = d
	}
	return &Registry{byName: byName, cfg: cfg}
}

// Get returns the descriptor for a provider name, case-insensitively.
func (r *Registry) Get(name string) (Descriptor, bool) {
	d, ok := r.byName[strings.ToLower(strings.TrimSpace(name))]
	return d, ok
}

// Names returns every registered provider name, sorted for stable output.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.byName))
	for name := range r.byName {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// LoginScopesFor returns the scopes to request at sign-in for a provider.
// When OAUTH_MINIMAL_LOGIN_SCOPES is off, the historical (broad) set is
// returned instead, so the rollout can be reversed with a restart.
func (r *Registry) LoginScopesFor(name string) []string {
	d, ok := r.Get(name)
	if !ok {
		return nil
	}
	if r.cfg != nil && !r.cfg.ProviderTokensConfig.MinimalLoginScopes &&
		len(d.HistoricalLoginScopes) > 0 {
		return slices.Clone(d.HistoricalLoginScopes)
	}
	return slices.Clone(d.LoginScopes)
}

// AvailableCapabilities maps each provider to the capabilities it exposes.
// Lets a settings UI render connect-this toggles generically — adding a
// provider grows the map with no client change.
func (r *Registry) AvailableCapabilities() map[string][]Capability {
	out := make(map[string][]Capability, len(r.byName))
	for name, d := range r.byName {
		out[name] = d.CapabilityNames()
	}
	return out
}

// CapabilityNames lists the capabilities this provider exposes, sorted.
func (d Descriptor) CapabilityNames() []Capability {
	out := make([]Capability, 0, len(d.Capabilities))
	for c := range d.Capabilities {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// ScopesFor resolves capabilities to normalized provider scopes. An unknown
// capability is an error naming it, rather than a silently empty scope set
// that would look like "already granted".
func (d Descriptor) ScopesFor(caps []Capability) ([]string, error) {
	var scopes []string
	for _, c := range caps {
		mapped, ok := d.Capabilities[c]
		if !ok {
			return nil, fmt.Errorf(
				"provider %q does not support capability %q (supported: %v)",
				d.Name,
				c,
				d.CapabilityNames(),
			)
		}
		scopes = append(scopes, mapped...)
	}
	return d.NormalizeAll(scopes), nil
}

// CapabilitiesFor is the reverse mapping: which capabilities are fully covered
// by the given granted scopes. Used for display, never for authorization.
func (d Descriptor) CapabilitiesFor(granted []string) []Capability {
	normalized := d.NormalizeAll(granted)

	var out []Capability
	for c, required := range d.Capabilities {
		if len(d.MissingScopes(normalized, required)) == 0 {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// NormalizeAll canonicalizes, de-duplicates, and sorts a scope set so that
// comparisons and stored values are stable regardless of input ordering.
func (d Descriptor) NormalizeAll(raw []string) []string {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))

	for _, s := range raw {
		n := d.normalize(s)
		if n == "" {
			continue
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}

	sort.Strings(out)
	return out
}

// ParseScopeString splits a provider-returned scope string. Providers vary in
// delimiter, and some pad with extra whitespace, so split on any run of the
// delimiter and normalize each element.
func (d Descriptor) ParseScopeString(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	delim := d.ScopeDelimiter
	if delim == "" {
		delim = " "
	}
	parts := strings.Split(s, delim)

	// Tolerate space-padded comma lists ("a, b") and repeated delimiters.
	cleaned := make([]string, 0, len(parts))
	for _, p := range parts {
		for _, q := range strings.Fields(p) {
			cleaned = append(cleaned, q)
		}
	}
	return d.NormalizeAll(cleaned)
}

// MissingScopes returns the required scopes absent from granted. Both sides
// are normalized first, which is what makes the alias asymmetry harmless —
// requesting "email" is satisfied by a granted ".../auth/userinfo.email".
func (d Descriptor) MissingScopes(granted, required []string) []string {
	have := make(map[string]struct{}, len(granted))
	for _, g := range granted {
		have[d.normalize(g)] = struct{}{}
	}

	var missing []string
	for _, r := range d.NormalizeAll(required) {
		if _, ok := have[r]; !ok {
			missing = append(missing, r)
		}
	}
	return missing
}

// normalize applies the provider's canonicalization, defaulting to a trim.
func (d Descriptor) normalize(s string) string {
	if d.Normalize == nil {
		return strings.TrimSpace(s)
	}
	return d.Normalize(s)
}

// OAuthConfig builds an oauth2.Config for this provider. Used for the
// incremental flow, code exchange, and refresh — the login path still runs
// through goth.
func (d Descriptor) OAuthConfig(
	cfg *config.Config,
	redirectURL string,
	scopes []string,
) (*oauth2.Config, error) {
	creds, ok := credentialsFor[d.Name]
	if !ok {
		return nil, fmt.Errorf("provider %q has no configured credentials", d.Name)
	}
	id, secret := creds(cfg)
	if id == "" || secret == "" {
		return nil, fmt.Errorf(
			"provider %q is missing a client id or secret in configuration",
			d.Name,
		)
	}

	return &oauth2.Config{
		ClientID:     id,
		ClientSecret: secret,
		RedirectURL:  redirectURL,
		Scopes:       scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  d.AuthURL,
			TokenURL: d.TokenURL,
		},
	}, nil
}

// AuthCodeOptions renders ExtraAuthParams plus any per-request overrides as
// oauth2 options. Per-request entries win.
func (d Descriptor) AuthCodeOptions(
	extra map[string]string,
) []oauth2.AuthCodeOption {
	merged := make(map[string]string, len(d.ExtraAuthParams)+len(extra))
	for k, v := range d.ExtraAuthParams {
		merged[k] = v
	}
	for k, v := range extra {
		merged[k] = v
	}

	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic, so tests can assert on the URL

	opts := make([]oauth2.AuthCodeOption, 0, len(keys))
	for _, k := range keys {
		opts = append(opts, oauth2.SetAuthURLParam(k, merged[k]))
	}
	return opts
}

// DecorateAuthURL adds this provider's ExtraAuthParams to an authorization URL
// that was built elsewhere.
//
// This exists because goth binds its auth-code options at provider
// construction and exposes no generic setter, so include_granted_scopes cannot
// be set through its API. The login path still uses goth, so the parameter is
// applied to the URL goth hands back.
//
// The query is re-encoded in the process, which includes the state parameter.
// That round-trips correctly (base64 "=" padding becomes "%3D" and decodes
// back via r.FormValue), and TestDecorateAuthURL_PreservesState pins it —
// this runs on every login, so a regression here is a total auth outage.
func DecorateAuthURL(d Descriptor, rawURL string) (string, error) {
	if len(d.ExtraAuthParams) == 0 {
		return rawURL, nil
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse authorization URL: %w", err)
	}

	q := u.Query()
	for k, v := range d.ExtraAuthParams {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	return u.String(), nil
}
