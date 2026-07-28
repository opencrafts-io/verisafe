// Package testsupport holds helpers shared by the handler packages' tests.
//
// It exists as a normal package rather than a _test.go file because the
// handler packages are siblings: after the split there is no single package
// a shared harness could live in and still be importable by all of them.
package testsupport

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/opencrafts-io/verisafe/internal/core"
	mockscore "github.com/opencrafts-io/verisafe/internal/core/mocks"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

// WireCase asserts the exact bytes a handler puts on the wire.
//
// Assertions are on the raw body string rather than with assert.JSONEq, and on
// Content-Type explicitly, because these tests exist to prove a refactor moved
// nothing observable. The trailing newline json.Encoder emits is part of what
// clients receive, so a change to it must fail here.
type WireCase struct {
	Name string

	// Handler is invoked directly rather than through a middleware stack.
	// Pass http.HandlerFunc for a handler that writes its own responses, or
	// core.AppHandler for one that returns an error; the assertions below are
	// on the bytes either way, which is what lets a case survive the move from
	// one form to the other unchanged.
	Handler http.Handler

	// Request is the request to serve.
	Request *http.Request

	// Authenticated mirrors production for routes behind IsAuthenticated,
	// which does w.Header().Add("Content-Type", "application/json") before the
	// handler runs. Without this a handler that never sets the header itself
	// would be recorded with Go's sniffed text/plain, baking in a golden that
	// does not match what the service actually returns.
	Authenticated bool

	WantStatus int
	// WantContentType of "" asserts the header is absent.
	WantContentType string
	// WantBody is compared exactly, including any trailing newline.
	WantBody string
}

func (c WireCase) Run(t *testing.T) {
	t.Helper()

	rr := httptest.NewRecorder()
	if c.Authenticated {
		rr.Header().Add("Content-Type", "application/json")
	}

	c.Handler.ServeHTTP(rr, c.Request)

	assert.Equal(t, c.WantStatus, rr.Code, "status code")
	assert.Equal(t, c.WantContentType, rr.Header().Get("Content-Type"), "Content-Type")
	assert.Equal(t, c.WantBody, rr.Body.String(), "response body bytes")
}

// RunWireCases runs each case as a subtest.
func RunWireCases(t *testing.T, cases []WireCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) { tc.Run(t) })
	}
}

// FailingAcquireDB returns a provider whose Acquire always fails, exercising
// the branch every handler runs when the pool is exhausted or the database is
// unreachable.
func FailingAcquireDB(t *testing.T, err error) core.IDBProvider {
	t.Helper()
	ctrl := gomock.NewController(t)
	db := mockscore.NewMockIDBProvider(ctrl)
	db.EXPECT().Acquire(gomock.Any()).Return(nil, err).AnyTimes()
	return db
}

// FailingBeginDB returns a provider that hands out a connection whose Begin
// fails. Release is still expected, because a handler that acquires a
// connection must return it to the pool whatever happens next.
func FailingBeginDB(t *testing.T, err error) core.IDBProvider {
	t.Helper()
	ctrl := gomock.NewController(t)

	conn := mockscore.NewMockIDBConnection(ctrl)
	conn.EXPECT().Begin(gomock.Any()).Return(nil, err).AnyTimes()
	conn.EXPECT().Release().AnyTimes()

	db := mockscore.NewMockIDBProvider(ctrl)
	db.EXPECT().Acquire(gomock.Any()).Return(conn, nil).AnyTimes()
	return db
}

// TxDB returns a provider whose connection begins a transaction backed by tx.
// Callers set the expectations they care about on tx.
func TxDB(t *testing.T, tx pgx.Tx) core.IDBProvider {
	t.Helper()
	ctrl := gomock.NewController(t)

	conn := mockscore.NewMockIDBConnection(ctrl)
	conn.EXPECT().Begin(gomock.Any()).Return(tx, nil).AnyTimes()
	conn.EXPECT().Release().AnyTimes()

	db := mockscore.NewMockIDBProvider(ctrl)
	db.EXPECT().Acquire(gomock.Any()).Return(conn, nil).AnyTimes()
	return db
}

// NewTx returns a transaction mock that accepts Rollback and Commit without
// the caller having to declare them. Handlers defer a rollback on every path,
// so an unexpected-call failure there would obscure the assertion under test.
func NewTx(t *testing.T) *mockscore.MockTx {
	t.Helper()
	tx := mockscore.NewMockTx(gomock.NewController(t))
	tx.EXPECT().Rollback(gomock.Any()).Return(nil).AnyTimes()
	return tx
}
