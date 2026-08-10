package leaderboard_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/handlers/leaderboard"
	"github.com/opencrafts-io/verisafe/internal/testsupport"
)

// Byte-exact characterisation of each branch, so the service extraction can be
// shown to have moved nothing observable. See the role handler's tests for the
// reasoning behind driving the handler directly and pre-seeding Content-Type.

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// This was the first migrated handler that used http.Error, which sets
// Content-Type: text/plain even though the body is JSON-shaped. ADR 0009
// accepts moving it to core.WriteError as a deliberate, one-time header
// change with the same status and body; this test now asserts
// application/json rather than the text/plain it asserted before this
// migration, and that line is the visible record of the change.
func TestGetGlobalLeaderBoard_ConnectionAcquisitionFailure(t *testing.T) {
	h := &leaderboard.LeaderBoardHandler{
		Logger: discardLogger(),
		DB:     testsupport.FailingAcquireDB(t, errors.New("pool exhausted")),
	}

	testsupport.WireCase{
		Handler:         core.AppHandler(h.GetGlobalLeaderBoard),
		Request:         httptest.NewRequest("GET", "/leaderboard/global", nil),
		WantStatus:      500,
		WantContentType: "application/json",
		WantBody:        `{"error":"internal server error"}` + "\n",
	}.Run(t)
}

// Begin failing gives a different message than Acquire failing (msgGeneric
// used elsewhere is a third string again) -- this endpoint has always
// distinguished the two, and core.InTx would have collapsed them, which is
// why the handler calls Acquire and WithTransaction separately.
func TestGetGlobalLeaderBoard_BeginFailureIsADistinctMessage(t *testing.T) {
	h := &leaderboard.LeaderBoardHandler{
		Logger: discardLogger(),
		DB: testsupport.FailingBeginDB(
			t, errors.New("server closed the connection"),
		),
	}

	testsupport.WireCase{
		Handler:         core.AppHandler(h.GetGlobalLeaderBoard),
		Request:         httptest.NewRequest("GET", "/leaderboard/global", nil),
		WantStatus:      500,
		WantContentType: "application/json",
		WantBody:        `{"error":"Cannot process your request at the moment"}` + "\n",
	}.Run(t)
}

func TestGetGlobalUserRank(t *testing.T) {
	t.Run("connection acquisition failure uses the acquire-specific message", func(t *testing.T) {
		h := &leaderboard.LeaderBoardHandler{
			Logger: discardLogger(),
			DB:     testsupport.FailingAcquireDB(t, errors.New("pool exhausted")),
		}

		req := httptest.NewRequest("GET", "/leaderboard/global/x", nil)
		req.SetPathValue("user", "6f1b6b1e-0000-4000-8000-000000000000")

		testsupport.WireCase{
			Handler:         core.AppHandler(h.GetGlobalUserRank),
			Request:         req,
			WantStatus:      500,
			WantContentType: "application/json",
			WantBody:        `{"error":"internal server error"}` + "\n",
		}.Run(t)
	})

	// The id is validated inside the transaction, after Begin succeeds, which
	// is the order this endpoint used before the extraction: a malformed id
	// still returns 400 rather than whatever Acquire/Begin would have failed
	// with had they run first.
	t.Run("invalid user id is rejected once the transaction is open", func(t *testing.T) {
		h := &leaderboard.LeaderBoardHandler{
			Logger: discardLogger(),
			DB:     testsupport.TxDB(t, testsupport.NewTx(t)),
		}

		req := httptest.NewRequest("GET", "/leaderboard/global/x", nil)
		req.SetPathValue("user", "not-a-uuid")

		testsupport.WireCase{
			Handler:         core.AppHandler(h.GetGlobalUserRank),
			Request:         req,
			WantStatus:      400,
			WantContentType: "application/json",
			WantBody:        `{"error":"invalid user id"}` + "\n",
		}.Run(t)
	})
}
