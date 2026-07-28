package leaderboard

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/middleware/pagination"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

type LeaderBoardHandler struct {
	Cacher core.Cacher
	DB     core.IDBProvider
	Cfg    *config.Config
	Logger *slog.Logger
}

func (lh *LeaderBoardHandler) RegisterHandlers(
	router *http.ServeMux,
) {
	router.Handle("GET /leaderboard/global", middleware.CreateStack(
		middleware.IsAuthenticated(lh.Cfg, lh.DB, lh.Cacher, lh.Logger),
	)(http.HandlerFunc(lh.GetGlobalLeaderBoard)))
	router.Handle("GET /leaderboard/global/{user}", middleware.CreateStack(
		middleware.IsAuthenticated(lh.Cfg, lh.DB, lh.Cacher, lh.Logger),
	)(http.HandlerFunc(lh.GetGlobalUserRank)))
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
) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		lh.Logger.Error(
			"Error while processing request",
			slog.Any("error", err),
		)
		http.Error(
			w,
			`{"error":"internal server error"}`,
			http.StatusInternalServerError,
		)
		return
	}

	tx, err := conn.Begin(r.Context())
	if err != nil {
		lh.Logger.Error("Failed to start transaction", slog.Any("error", err))
		http.Error(
			w,
			`{"error":"Cannot process your request at the moment"}`,
			http.StatusInternalServerError,
		)
		return
	}
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	idStr := r.PathValue("user")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, `{"error":"invalid user id"}`, http.StatusBadRequest)
		return
	}

	leaderboardRank, err := repo.GetLeaderBoardRankForUser(r.Context(), id)
	if err != nil {
		lh.Logger.Error(
			"Failed to retrieve leaderboard",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We couldn't provide the global leaderboard at the moment",
		})
		return
	}
	json.NewEncoder(w).Encode(leaderboardRank)
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
) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		lh.Logger.Error(
			"Error while processing request",
			slog.Any("error", err),
		)
		http.Error(
			w,
			`{"error":"internal server error"}`,
			http.StatusInternalServerError,
		)
		return
	}

	tx, err := conn.Begin(r.Context())
	if err != nil {
		lh.Logger.Error("Failed to start transaction", slog.Any("error", err))
		http.Error(
			w,
			`{"error":"Cannot process your request at the moment"}`,
			http.StatusInternalServerError,
		)
		return
	}

	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	// Parse pagination params
	pageParams := pagination.ParsePageParams(r)

	totalCount, err := repo.GetGlobalLeaderBoardCount(r.Context())
	if err != nil {
		lh.Logger.Error(
			"Failed to get leaderboard count",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We couldn't provide the global leaderboard at the moment",
		})
		return
	}

	leaderboard, err := repo.GetLeaderboard(
		r.Context(),
		repository.GetLeaderboardParams{
			Limit:  int32(pageParams.PageSize),
			Offset: int32(pageParams.Offset),
		},
	)
	if err != nil {
		lh.Logger.Error(
			"Failed to retrieve leaderboard",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We couldn't provide the global leaderboard at the moment",
		})
		return
	}

	response := pagination.BuildPaginatedResponse(
		r,
		totalCount,
		leaderboard,
		pageParams,
	)
	json.NewEncoder(w).Encode(response)
}
