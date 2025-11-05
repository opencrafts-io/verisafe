package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

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

	tx, _ := conn.Begin(r.Context())
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

	leaderboard, err := repo.GetLeaderboard(r.Context(), repository.GetLeaderboardParams{Limit: 100, Offset: 0})

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
