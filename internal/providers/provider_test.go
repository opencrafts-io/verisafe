package providers

import (
	"net/url"
	"slices"
	"testing"

	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func googleDescriptor(t *testing.T) Descriptor {
	t.Helper()
	d, ok := NewRegistry(nil).Get("google")
	require.True(t, ok)
	return d
}

func spotifyDescriptor(t *testing.T) Descriptor {
	t.Helper()
	d, ok := NewRegistry(nil).Get("spotify")
	require.True(t, ok)
	return d
}

func TestRegistry_GetIsCaseInsensitive(t *testing.T) {
	r := NewRegistry(nil)
	for _, name := range []string{"google", "GOOGLE", " Google "} {
		d, ok := r.Get(name)
		assert.True(t, ok, name)
		assert.Equal(t, "google", d.Name)
	}

	_, ok := r.Get("microsoft")
	assert.False(t, ok, "unregistered provider must not resolve")
}

func TestRegistry_Names(t *testing.T) {
	assert.Equal(t, []string{"apple", "google", "spotify"}, NewRegistry(nil).Names())
}

// A Registry is a pointer field on several handlers, and the login path calls
// Get on every request. A handler constructed without one must degrade to
// "provider unknown" rather than panicking mid-request.
func TestRegistry_NilReceiverDoesNotPanic(t *testing.T) {
	var r *Registry

	_, ok := r.Get("google")
	assert.False(t, ok)
	assert.Nil(t, r.Names())
	assert.Nil(t, r.LoginScopesFor("google"))
	assert.Empty(t, r.AvailableCapabilities())
}

// Guards a malformed descriptor — most usefully, a future Microsoft one.
func TestRegistry_DescriptorInvariants(t *testing.T) {
	for _, d := range descriptors {
		t.Run(d.Name, func(t *testing.T) {
			assert.Equal(t, d.Name, normalizeLower(d.Name), "name must be canonical lowercase")
			assert.NotNil(t, d.Normalize, "Normalize must be set or scope comparison breaks")
			assert.NotEmpty(t, d.ScopeDelimiter)
			assert.NotEmpty(t, d.Capabilities, "a provider with no capabilities is unusable")

			_, hasIdentity := d.Capabilities[CapabilityIdentity]
			assert.True(t, hasIdentity, "every provider must expose identity")

			// LoginScopes must be a subset of the capability union, otherwise
			// a scope is requested at login that no capability can describe
			// and the grant can never be reasoned about.
			var union []string
			for _, scopes := range d.Capabilities {
				union = append(union, scopes...)
			}
			assert.Empty(
				t, d.MissingScopes(union, d.LoginScopes),
				"LoginScopes must be a subset of the capability union",
			)

			if d.SupportsIncremental {
				assert.NotEmpty(t, d.AuthURL, "incremental flow needs an AuthURL")
				assert.NotEmpty(t, d.TokenURL, "incremental flow needs a TokenURL")
			}

			_, hasCreds := credentialsFor[d.Name]
			assert.True(t, hasCreds, "descriptor has no credentials lookup")
		})
	}
}

func TestNormalizeGoogle(t *testing.T) {
	cases := []struct{ in, want string }{
		{"email", googleScopeEmail},
		{"userinfo.email", googleScopeEmail},
		{"  email  ", googleScopeEmail},
		{"profile", googleScopeProfile},
		{"userinfo.profile", googleScopeProfile},
		{"openid", googleScopeOpenID},
		{googleScopeCalendar, googleScopeCalendar},
		{googleScopeCalendar + "/", googleScopeCalendar},
		{googleScopeEmail, googleScopeEmail},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, normalizeGoogle(tc.in), tc.in)
	}
}

func TestNormalizeSpotify(t *testing.T) {
	assert.Equal(t, "user-read-email", normalizeSpotify("  USER-READ-Email "))
	assert.Equal(t, "playlist-read-private", normalizeSpotify("playlist-read-private"))
}

// The highest-probability real bug this package prevents: Google accepts the
// short alias in a request but reports the full URL back, so a naive string
// comparison marks identity scopes as permanently missing and the broker
// demands re-authorization forever.
func TestMissingScopes_GoogleAliasAsymmetry(t *testing.T) {
	d := googleDescriptor(t)

	granted := []string{googleScopeEmail, googleScopeProfile}
	assert.Empty(
		t, d.MissingScopes(granted, []string{"email", "profile"}),
		"a granted userinfo URL must satisfy a request for its short alias",
	)

	assert.Equal(
		t,
		[]string{googleScopeCalendar},
		d.MissingScopes(granted, []string{"email", googleScopeCalendar}),
		"genuinely absent scopes must still be reported",
	)
}

func TestMissingScopes_EmptyCases(t *testing.T) {
	d := googleDescriptor(t)
	assert.Empty(t, d.MissingScopes([]string{googleScopeEmail}, nil))
	assert.Equal(
		t,
		[]string{googleScopeCalendar},
		d.MissingScopes(nil, []string{googleScopeCalendar}),
	)
}

func TestScopesFor(t *testing.T) {
	d := googleDescriptor(t)

	got, err := d.ScopesFor([]Capability{CapabilityCalendar})
	require.NoError(t, err)
	assert.Equal(t, []string{googleScopeCalendar}, got)

	got, err = d.ScopesFor([]Capability{CapabilityIdentity, CapabilityCalendar})
	require.NoError(t, err)
	assert.Equal(t, []string{googleScopeCalendar, googleScopeEmail, googleScopeProfile}, got)

	// Overlapping capabilities must not duplicate scopes.
	got, err = d.ScopesFor([]Capability{CapabilityIdentity, CapabilityIdentity})
	require.NoError(t, err)
	assert.Equal(t, []string{googleScopeEmail, googleScopeProfile}, got)
}

// An unknown capability must be a loud error. Returning an empty scope set
// would read downstream as "nothing needed, already granted".
func TestScopesFor_UnknownCapabilityErrors(t *testing.T) {
	d := googleDescriptor(t)

	_, err := d.ScopesFor([]Capability{CapabilityPlayback})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "playback")
	assert.Contains(t, err.Error(), "google")

	_, err = d.ScopesFor([]Capability{"not-a-capability"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not-a-capability")
}

func TestCapabilitiesFor(t *testing.T) {
	d := googleDescriptor(t)

	assert.Equal(
		t,
		[]Capability{CapabilityIdentity},
		d.CapabilitiesFor([]string{googleScopeEmail, googleScopeProfile}),
	)
	assert.Equal(
		t,
		[]Capability{CapabilityCalendar, CapabilityIdentity},
		d.CapabilitiesFor([]string{googleScopeEmail, googleScopeProfile, googleScopeCalendar}),
	)

	// A partial capability must not be reported as held.
	assert.Empty(t, d.CapabilitiesFor([]string{googleScopeEmail}))
}

func TestParseScopeString(t *testing.T) {
	d := googleDescriptor(t)

	assert.Equal(
		t,
		[]string{googleScopeCalendar, googleScopeEmail},
		d.ParseScopeString("email "+googleScopeCalendar),
	)
	assert.Nil(t, d.ParseScopeString(""))
	assert.Nil(t, d.ParseScopeString("   "))

	// Repeated delimiters and duplicates.
	assert.Equal(
		t,
		[]string{googleScopeEmail},
		d.ParseScopeString("email   email"),
	)

	s := spotifyDescriptor(t)
	assert.Equal(
		t,
		[]string{"user-read-email", "user-read-private"},
		s.ParseScopeString("user-read-private user-read-email"),
	)
}

func TestNormalizeAll_DeduplicatesAndSorts(t *testing.T) {
	d := googleDescriptor(t)
	got := d.NormalizeAll([]string{"profile", googleScopeProfile, "email", ""})
	assert.Equal(t, []string{googleScopeEmail, googleScopeProfile}, got)
}

// DecorateAuthURL runs on every single login. If it corrupts the state
// parameter, gothic's state comparison fails on callback and nobody can sign
// in — so pin the byte-for-byte round trip.
func TestDecorateAuthURL_PreservesState(t *testing.T) {
	d := googleDescriptor(t)

	// A realistic gothic state: base64.URLEncoding output, including the "="
	// padding that survives an encode/decode round trip as %3D.
	const state = "YXV0aC5wbGF0Zm9ybS52YWx1ZS53ZWJ8aHR0cHM6Ly9hcHAuZXhhbXBsZS5jb20vY2J8fHw="

	original := "https://accounts.google.com/o/oauth2/auth?" + url.Values{
		"client_id":     {"cid.apps.googleusercontent.com"},
		"redirect_uri":  {"https://verisafe.example.com/auth/google/callback"},
		"response_type": {"code"},
		"scope":         {"email profile"},
		"state":         {state},
	}.Encode()

	decorated, err := DecorateAuthURL(d, original)
	require.NoError(t, err)

	parsed, err := url.Parse(decorated)
	require.NoError(t, err)
	q := parsed.Query()

	assert.Equal(t, state, q.Get("state"), "state must survive re-encoding byte-for-byte")
	assert.Equal(t, "true", q.Get("include_granted_scopes"))
	assert.Equal(t, "offline", q.Get("access_type"))

	// Everything else must be untouched.
	assert.Equal(t, "cid.apps.googleusercontent.com", q.Get("client_id"))
	assert.Equal(t, "https://verisafe.example.com/auth/google/callback", q.Get("redirect_uri"))
	assert.Equal(t, "code", q.Get("response_type"))
	assert.Equal(t, "email profile", q.Get("scope"))
}

func TestDecorateAuthURL_NoParamsIsIdentity(t *testing.T) {
	d := Descriptor{Name: "bare"}
	const raw = "https://example.com/authorize?state=abc%3D"

	got, err := DecorateAuthURL(d, raw)
	require.NoError(t, err)
	assert.Equal(t, raw, got, "a provider with no extra params must not touch the URL at all")
}

func TestDecorateAuthURL_InvalidURL(t *testing.T) {
	_, err := DecorateAuthURL(googleDescriptor(t), "://not a url")
	assert.Error(t, err)
}

func TestOAuthConfig(t *testing.T) {
	cfg := &config.Config{}
	cfg.AuthenticationConfig.GoogleClientID = "cid"
	cfg.AuthenticationConfig.GoogleClientSecret = "secret"

	d := googleDescriptor(t)
	conf, err := d.OAuthConfig(cfg, "https://verisafe.example.com/oauth/google/callback", []string{googleScopeCalendar})
	require.NoError(t, err)

	assert.Equal(t, "cid", conf.ClientID)
	assert.Equal(t, "secret", conf.ClientSecret)
	assert.Equal(t, []string{googleScopeCalendar}, conf.Scopes)
	assert.Equal(t, d.AuthURL, conf.Endpoint.AuthURL)
	assert.Equal(t, d.TokenURL, conf.Endpoint.TokenURL)
}

func TestOAuthConfig_MissingCredentials(t *testing.T) {
	_, err := googleDescriptor(t).OAuthConfig(&config.Config{}, "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing a client id or secret")
}

func TestAuthCodeOptions_PerRequestOverridesDescriptor(t *testing.T) {
	d := googleDescriptor(t)

	opts := d.AuthCodeOptions(map[string]string{
		"prompt": "consent",
		// Same key as the descriptor sets, to prove per-request wins.
		"access_type": "online",
	})

	cfg := &config.Config{}
	cfg.AuthenticationConfig.GoogleClientID = "cid"
	cfg.AuthenticationConfig.GoogleClientSecret = "secret"
	conf, err := d.OAuthConfig(cfg, "https://verisafe.example.com/cb", nil)
	require.NoError(t, err)

	// AuthCodeURL is the only way to observe the options; the setter is
	// unexported.
	parsed, err := url.Parse(conf.AuthCodeURL("state123", opts...))
	require.NoError(t, err)
	q := parsed.Query()

	assert.Equal(t, "true", q.Get("include_granted_scopes"))
	assert.Equal(t, "consent", q.Get("prompt"))
	assert.Equal(t, "online", q.Get("access_type"), "per-request params must override the descriptor")
}

func TestLoginScopesFor_FlagSwitchesBetweenMinimalAndHistorical(t *testing.T) {
	cfg := &config.Config{}

	cfg.ProviderTokensConfig.MinimalLoginScopes = false
	broad := NewRegistry(cfg).LoginScopesFor("google")
	assert.Contains(t, broad, googleScopeCalendar,
		"with the flag off, logins must keep requesting the historical scope set")

	cfg.ProviderTokensConfig.MinimalLoginScopes = true
	minimal := NewRegistry(cfg).LoginScopesFor("google")
	assert.NotContains(t, minimal, googleScopeCalendar)
	assert.ElementsMatch(t, []string{googleScopeEmail, googleScopeProfile}, minimal)

	assert.Nil(t, NewRegistry(cfg).LoginScopesFor("nope"))
}

func TestAvailableCapabilities(t *testing.T) {
	got := NewRegistry(nil).AvailableCapabilities()
	assert.True(t, slices.Contains(got["google"], CapabilityCalendar))
	assert.True(t, slices.Contains(got["spotify"], CapabilityPlayback))
	assert.Equal(t, []Capability{CapabilityIdentity}, got["apple"])
}
