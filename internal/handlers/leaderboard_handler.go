package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/middleware/pagination"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

type LeaderBoardHandler struct {
	Logger *slog.Logger
}

func (lh *LeaderBoardHandler) RegisterLeaderBoardHandlers(cfg *config.Config, router *http.ServeMux) {
	router.Handle("GET /leaderboard/global", middleware.CreateStack(
		middleware.IsAuthenticated(cfg, lh.Logger),
	)(http.HandlerFunc(lh.GetGlobalLeaderBoard)))
	router.Handle("GET /leaderboard/global/{user}", middleware.CreateStack(
		middleware.IsAuthenticated(cfg, lh.Logger),
	)(http.HandlerFunc(lh.GetGlobalUserRank)))

}

func (lh *LeaderBoardHandler) GetGlobalUserRank(w http.ResponseWriter, r *http.Request) {

	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		lh.Logger.Error("Error while processing request", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	tx, err := conn.Begin(r.Context())
	if err != nil {
		lh.Logger.Error("Failed to start transaction", slog.Any("error", err))
		http.Error(w, `{"error":"Cannot process your request at the moment"}`, http.StatusInternalServerError)
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
		lh.Logger.Error("Failed to retrieve leaderboard", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We couldn't provide the global leaderboard at the moment",
		})
		return
	}
	json.NewEncoder(w).Encode(leaderboardRank)
}

// Returns the global leaderboard using the limit offset scheme
func (lh *LeaderBoardHandler) GetGlobalLeaderBoard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		lh.Logger.Error("Error while processing request", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	tx, err := conn.Begin(r.Context())
	if err != nil {
		lh.Logger.Error("Failed to start transaction", slog.Any("error", err))
		http.Error(w, `{"error":"Cannot process your request at the moment"}`, http.StatusInternalServerError)
		return
	}

	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	// Parse pagination params
	pageParams := pagination.ParsePageParams(r)

	totalCount, err := repo.GetGlobalLeaderBoardCount(r.Context())
	if err != nil {
		lh.Logger.Error("Failed to get leaderboard count", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We couldn't provide the global leaderboard at the moment",
		})
		return
	}

	leaderboard, err := repo.GetLeaderboard(r.Context(), repository.GetLeaderboardParams{
		Limit:  int32(pageParams.PageSize),
		Offset: int32(pageParams.Offset),
	})

	if err != nil {
		lh.Logger.Error("Failed to retrieve leaderboard", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We couldn't provide the global leaderboard at the moment",
		})
		return
	}

	response := pagination.BuildPaginatedResponse(r, totalCount, leaderboard, pageParams)
	json.NewEncoder(w).Encode(response)
}
