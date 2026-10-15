package leaderboard

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

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
	router.Handle("GET /leaderboard/global/{user}/around", middleware.CreateStack(
		middleware.IsAuthenticated(lh.Cfg, lh.DB, lh.Cacher, lh.Logger),
	)(core.AppHandler(lh.GetLeaderboardAroundUser)))
}

type LeaderboardAroundResponse struct {
	UserID       uuid.UUID                                `json:"user_id"`
	UserPosition int64                                    `json:"user_position"`
	TotalUsers   int64                                    `json:"total_users"`
	Results      []repository.GetLeaderboardAroundUserRow `json:"results"`
}

// GetLeaderboardAroundUser godoc
//
// @Summary      Get a leaderboard window around a user
// @Description  Returns up to 50 users, centered on the requested account when possible. The user's own position is included in the response.
// @Tags         leaderboard
// @Produce      json
// @Param        user   path   string true  "Account ID"
// @Param        limit  query  int    false "Number of users to return (default 20, maximum 50)"
// @Success      200 {object} LeaderboardAroundResponse
// @Failure      400 {object} core.APIError "Invalid user id or limit"
// @Failure      404 {object} core.APIError "User is not on the leaderboard"
// @Failure      500 {object} core.APIError "Failed to fetch leaderboard"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /leaderboard/global/{user}/around [get]
func (lh *LeaderBoardHandler) GetLeaderboardAroundUser(
	w http.ResponseWriter,
	r *http.Request,
) error {
	userID, err := uuid.Parse(r.PathValue("user"))
	if err != nil {
		return core.Public(core.ErrInvalidInput, msgInvalidUserID)
	}

	windowSize := 20
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		parsed, parseErr := strconv.Atoi(rawLimit)
		if parseErr != nil || parsed < 1 {
			return core.Public(core.ErrInvalidInput, msgInvalidLimit)
		}
		windowSize = parsed
	}
	if windowSize > 50 {
		windowSize = 50
	}

	conn, err := lh.DB.Acquire(r.Context())
	if err != nil {
		lh.Logger.Error("Error while processing request", slog.Any("error", err))
		return core.Public(core.ErrInternal, msgInternalServer)
	}

	var rows []repository.GetLeaderboardAroundUserRow
	if err := core.WithTransaction(r.Context(), conn, func(tx pgx.Tx) error {
		var queryErr error
		rows, queryErr = lh.svc(tx).Around(r.Context(), userID, int32(windowSize))
		if queryErr != nil {
			return queryErr
		}
		return nil
	}); err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return core.Public(core.ErrNotFound, msgLeaderboardUserNotFound)
		}
		lh.Logger.Error("Failed to retrieve leaderboard window", slog.Any("error", err))
		return core.Fallback(err, core.ErrInternal, msgLeaderboardFailed)
	}

	response := LeaderboardAroundResponse{
		UserID:     userID,
		TotalUsers: rows[0].TotalUsers,
		Results:    rows,
	}
	for _, row := range rows {
		if row.ID == userID {
			response.UserPosition = row.Position
			break
		}
	}

	core.WriteJSON(w, http.StatusOK, response)
	return nil
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
