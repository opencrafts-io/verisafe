package leaderboard

import (
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/middleware/pagination"
	"github.com/opencrafts-io/verisafe/internal/repository"
	leaderboardsvc "github.com/opencrafts-io/verisafe/internal/service/leaderboard"
)

type LeaderBoardHandler struct {
	Cacher core.Cacher
	DB     core.IDBProvider
	Cfg    *config.Config
	Logger *slog.Logger

	// Service builds a leaderboard service bound to the caller's transaction.
	// Left nil it falls back to the real implementation; see the role handler
	// for why this field is the testing seam.
	Service func(repository.Querier) leaderboardsvc.Service
}

func (lh *LeaderBoardHandler) svc(tx pgx.Tx) leaderboardsvc.Service {
	if lh.Service != nil {
		return lh.Service(repository.New(tx))
	}
	return leaderboardsvc.NewService(repository.New(tx))
}

func (lh *LeaderBoardHandler) RegisterHandlers(router core.Router) {
	router.Handle("GET /leaderboard/global", middleware.CreateStack(
		middleware.IsAuthenticated(lh.Cfg, lh.DB, lh.Cacher, lh.Logger),
	)(core.AppHandler(lh.GetGlobalLeaderBoard)))
	router.Handle("GET /leaderboard/global/{user}", middleware.CreateStack(
		middleware.IsAuthenticated(lh.Cfg, lh.DB, lh.Cacher, lh.Logger),
	)(core.AppHandler(lh.GetGlobalUserRank)))
}

// GetGlobalUserRank godoc
//
// @Summary      Get a given user's global leaderboard rank
// @Description  Note: any authenticated caller can look up any user's rank by id — there is no ownership check on this endpoint today.
// @Tags         leaderboard
// @Produce      json
// @Param        user  path  string  true  "Account ID"
// @Success      200  {object}  repository.AccountVibepointRank
// @Failure      400  {object}  core.APIError  "Invalid user id"
// @Failure      500  {object}  core.APIError  "Failed to fetch rank"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /leaderboard/global/{user} [get]
func (lh *LeaderBoardHandler) GetGlobalUserRank(
	w http.ResponseWriter,
	r *http.Request,
) error {
	// Acquire and Begin are called separately, not through core.InTx, because
	// this endpoint gives each of their failures a distinct message
	// (msgInternalServer vs msgCannotProcess) and InTx collapses both into one
	// bare sentinel. WithTransaction still owns conn.Release, so there is no
	// leak window between the two calls.
	conn, err := lh.DB.Acquire(r.Context())
	if err != nil {
		lh.Logger.Error("Error while processing request", slog.Any("error", err))
		return core.Public(core.ErrInternal, msgInternalServer)
	}

	var rank repository.AccountVibepointRank
	if err := core.WithTransaction(r.Context(), conn, func(tx pgx.Tx) error {
		// The id is validated here, inside the transaction, rather than
		// before acquiring one -- that is the order this endpoint used
		// before the extraction, and preserving it means a malformed id
		// still returns 400 rather than whatever Acquire would have
		// returned had it run first.
		id, err := uuid.Parse(r.PathValue("user"))
		if err != nil {
			return core.Public(core.ErrInvalidInput, msgInvalidUserID)
		}

		rank, err = lh.svc(tx).RankForUser(r.Context(), id)
		if err != nil {
			lh.Logger.Error(
				"Failed to retrieve leaderboard", slog.Any("error", err),
			)
			return core.Public(core.ErrInternal, msgLeaderboardFailed)
		}
		return nil
	}); err != nil {
		return core.Fallback(err, core.ErrInternal, msgCannotProcess)
	}

	core.WriteJSON(w, http.StatusOK, rank)
	return nil
}

// GetGlobalLeaderBoard godoc
//
// @Summary      Get the global leaderboard
// @Description  Returns the global vibepoint ranking across all accounts.
// @Description  Paginated with page/page_size; the response is a
// @Description  count/next/previous/results envelope.
// @Tags         leaderboard
// @Produce      json
// @Param        page       query  int  false  "Page number (default 1)"
// @Param        page_size  query  int  false  "Page size (default 10, max 100)"
// @Success      200  {object}  pagination.PaginatedResponse
// @Failure      500  {object}  core.APIError  "Failed to fetch leaderboard"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /leaderboard/global [get]
func (lh *LeaderBoardHandler) GetGlobalLeaderBoard(
	w http.ResponseWriter,
	r *http.Request,
) error {
	pageParams := pagination.ParsePageParams(r)

	// See GetGlobalUserRank for why Acquire and Begin are separated rather
	// than going through core.InTx: this endpoint gives each failure a
	// distinct message.
	conn, err := lh.DB.Acquire(r.Context())
	if err != nil {
		lh.Logger.Error("Error while processing request", slog.Any("error", err))
		return core.Public(core.ErrInternal, msgInternalServer)
	}

	var total int64
	var rows []repository.AccountVibepointRank
	if err := core.WithTransaction(r.Context(), conn, func(tx pgx.Tx) error {
		var err error
		total, rows, err = lh.svc(tx).Global(
			r.Context(),
			int32(pageParams.PageSize),
			int32(pageParams.Offset),
		)
		if err != nil {
			lh.Logger.Error(
				"Failed to retrieve leaderboard", slog.Any("error", err),
			)
			return core.Public(core.ErrInternal, msgLeaderboardFailed)
		}
		return nil
	}); err != nil {
		return core.Fallback(err, core.ErrInternal, msgCannotProcess)
	}

	core.WriteJSON(
		w,
		http.StatusOK,
		pagination.BuildPaginatedResponse(r, total, rows, pageParams),
	)
	return nil
}
