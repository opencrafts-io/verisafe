package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/opencrafts-io/verisafe/internal/handlers/account"
	"github.com/opencrafts-io/verisafe/internal/handlers/activity"
	"github.com/opencrafts-io/verisafe/internal/handlers/device"
	"github.com/opencrafts-io/verisafe/internal/handlers/institution"
	"github.com/opencrafts-io/verisafe/internal/handlers/leaderboard"
	"github.com/opencrafts-io/verisafe/internal/handlers/oauth"
	"github.com/opencrafts-io/verisafe/internal/handlers/permission"
	"github.com/opencrafts-io/verisafe/internal/handlers/role"
	"github.com/opencrafts-io/verisafe/internal/handlers/servicetoken"
	"github.com/opencrafts-io/verisafe/internal/handlers/social"
	"github.com/opencrafts-io/verisafe/internal/handlers/streak"
	"github.com/stretchr/testify/require"
)

// The one thing a large handler refactor can break with nothing else failing
// is the route table itself: a dropped RegisterHandlers call, a renamed path,
// a method silently changed. This locks the table against a golden file.
//
// Handlers are constructed zero-valued on purpose. Registration only closes
// over its dependencies, so no pool, cache or event bus is needed to enumerate
// what each handler registers. The auth handler is excluded because
// NewAuthenticator mutates goth's package-level state and needs real provider
// credentials; its six routes are covered by internal/auth's own tests.

func testHandlers() []VerisafeHandler {
	return []VerisafeHandler{
		&account.AccountHandler{},
		&servicetoken.ServiceTokenHandler{},
		&social.SocialHandler{},
		&role.RoleHandler{},
		&permission.PermissionHandler{},
		&institution.InstitutionHandler{},
		&leaderboard.LeaderBoardHandler{},
		&activity.ActivityHandler{},
		&streak.StreakHandler{},
		&device.DeviceHandler{},
		&oauth.OAuthBrokerHandler{},
		&oauth.OAuthScopeHandler{},
	}
}

// recordingRouter captures the patterns passed to it. This is the reason
// RegisterHandlers takes core.Router rather than *http.ServeMux: the standard
// mux does not expose what has been registered on it.
type recordingRouter struct{ patterns []string }

func (r *recordingRouter) Handle(pattern string, _ http.Handler) {
	r.patterns = append(r.patterns, pattern)
}

func (r *recordingRouter) HandleFunc(
	pattern string,
	_ func(http.ResponseWriter, *http.Request),
) {
	r.patterns = append(r.patterns, pattern)
}

func routeTable(t *testing.T) []string {
	t.Helper()

	rec := &recordingRouter{}
	require.NotPanics(t, func() { registerAll(rec, testHandlers()) })

	out := append([]string{}, rec.patterns...)
	sort.Strings(out)
	return out
}

func TestRouteTableMatchesGolden(t *testing.T) {
	got := routeTable(t)

	golden := filepath.Join("testdata", "routes.golden")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(
			t,
			os.WriteFile(golden, []byte(strings.Join(got, "\n")+"\n"), 0o644),
		)
		t.Log("golden file updated")
	}

	want, err := os.ReadFile(golden)
	require.NoError(t, err, "run with UPDATE_GOLDEN=1 to create the golden file")

	require.Equal(
		t,
		strings.Split(strings.TrimSpace(string(want)), "\n"),
		got,
		"the route table changed. If that was intended, re-run with "+
			"UPDATE_GOLDEN=1 and review the diff carefully -- every entry "+
			"here is a live URL.",
	)
}

// Registering without panicking is weaker than the routes actually resolving,
// so drive the real mux buildRouter produces and require a match for each.
func TestBuildRouterResolvesEveryRoute(t *testing.T) {
	router := buildRouter(testHandlers())

	for _, entry := range routeTable(t) {
		method, pattern, ok := strings.Cut(entry, " ")
		require.True(t, ok, "malformed route entry %q", entry)

		// Path parameters need a concrete value to match against.
		path := pattern
		for {
			open := strings.Index(path, "{")
			if open == -1 {
				break
			}
			closed := strings.Index(path[open:], "}")
			require.NotEqual(t, -1, closed, "unbalanced brace in %q", pattern)
			path = path[:open] + "x" + path[open+closed+1:]
		}

		req := httptest.NewRequest(method, path, nil)
		h, matched := router.Handler(req)
		require.NotEmpty(t, matched, "no route matched %s %s", method, path)
		require.NotNil(t, h)
	}
}

// A pattern registered twice makes http.ServeMux panic at boot. Registering
// every handler into one mux here means that failure shows up in CI rather
// than on the first request after a deploy.
func TestNoDuplicateRoutePatterns(t *testing.T) {
	require.NotPanics(t, func() { buildRouter(testHandlers()) })
}
