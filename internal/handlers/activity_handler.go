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

type ActivityHandler struct {
	Logger *slog.Logger
}

func (ah *ActivityHandler) RegisterHadlers(cfg *config.Config, router *http.ServeMux) {
	router.Handle("POST /activity/add", middleware.CreateStack(
	middleware.IsAuthenticated(cfg, ah.Logger),
	)(http.HandlerFunc(ah.CreateActivity)))
	router.Handle("GET /activity/all", middleware.CreateStack(
	middleware.IsAuthenticated(cfg, ah.Logger),
	)(http.HandlerFunc(ah.GetAllActivities)))
	router.Handle("GET /activity/active", middleware.CreateStack(
	middleware.IsAuthenticated(cfg, ah.Logger),
	)(http.HandlerFunc(ah.GetAllActiveActivities)))
	router.Handle("GET /activity/inactive", middleware.CreateStack(
	middleware.IsAuthenticated(cfg, ah.Logger),
	)(http.HandlerFunc(ah.GetAllInactiveActivities)))
	router.Handle("PATCH /activity/{id}", middleware.CreateStack(
	middleware.IsAuthenticated(cfg, ah.Logger),
	)(http.HandlerFunc(ah.UpdateActivity)))
	router.Handle("DELETE /activity/{id}", middleware.CreateStack(
	middleware.IsAuthenticated(cfg, ah.Logger),
	)(http.HandlerFunc(ah.DeleteActivity)))

}

func (ah *ActivityHandler) DeleteActivity(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	rawID := r.PathValue("id")
	id, err := uuid.Parse(rawID)
	if err != nil {
		ah.Logger.Error("Failed to parse request path parameter", slog.Any("error", err),
			slog.Any("value", rawID),
		)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}

	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	tx, err := conn.Begin(r.Context())
	if err != nil {
		ah.Logger.Error("Failed to start transaction", slog.Any("error", err))
		http.Error(w, `{"error":"Cannot process your request at the moment"}`, http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	err = repo.DeleteActivity(r.Context(), id)
	if err != nil {
		ah.Logger.Error("Failed to delete activity", slog.Any("error", err), slog.Any("activity", id))
		http.Error(w, `{"error":"Cannot process your request at the moment"}`, http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		ah.Logger.Error("Error while committing transaction", slog.Any("error", err), slog.Any("activity", id))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{"message": "Activity deleted successfully"})
}

func (ah *ActivityHandler) UpdateActivity(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	rawID := r.PathValue("id")
	id, err := uuid.Parse(rawID)
	if err != nil {
		ah.Logger.Error("Failed to parse request path parameter", slog.Any("error", err),
			slog.Any("value", rawID),
		)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}

	requestBody := repository.UpdateActivityParams{}

	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		ah.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}
	requestBody.ID = id

	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	tx, err := conn.Begin(r.Context())
	if err != nil {
		ah.Logger.Error("Failed to start transaction", slog.Any("error", err))
		http.Error(w, `{"error":"Cannot process your request at the moment"}`, http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	activity, err := repo.UpdateActivity(r.Context(), requestBody)
	if err != nil {
		ah.Logger.Error("Failed to update activity", slog.Any("error", err), slog.Any("activity", requestBody))
		http.Error(w, `{"error":"Cannot process your request at the moment"}`, http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		ah.Logger.Error("Error while committing transaction", slog.Any("error", err), slog.Any("activity", activity))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}
	json.NewEncoder(w).Encode(activity)
}

func (ah *ActivityHandler) GetAllInactiveActivities(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	tx, err := conn.Begin(r.Context())
	if err != nil {
		ah.Logger.Error("Failed to start transaction", slog.Any("error", err))
		http.Error(w, `{"error":"Cannot process your request at the moment"}`, http.StatusInternalServerError)
		return
	}

	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	// Parse pagination params
	pageParams := pagination.ParsePageParams(r)

	totalCount, err := repo.GetAllInactiveActivitiesCount(r.Context())
	if err != nil {
		ah.Logger.Error("Failed to get total activity count", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We couldn't provide all activities at the moment.",
		})
		return
	}

	activities, err := repo.GetAllInactiveActivities(r.Context(), repository.GetAllInactiveActivitiesParams{
		Limit:  int32(pageParams.PageSize),
		Offset: int32(pageParams.Offset),
	})

	if err != nil {
		ah.Logger.Error("Failed to retrieve inactive activities", slog.Any("error", err),
			slog.Any("parameters",
				repository.GetAllInactiveActivitiesParams{
					Limit:  int32(pageParams.PageSize),
					Offset: int32(pageParams.Offset),
				}))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We couldn't provide activities at the moment.",
		})
		return
	}

	response := pagination.BuildPaginatedResponse(r, totalCount, activities, pageParams)
	json.NewEncoder(w).Encode(response)
}

func (ah *ActivityHandler) GetAllActiveActivities(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	tx, err := conn.Begin(r.Context())
	if err != nil {
		ah.Logger.Error("Failed to start transaction", slog.Any("error", err))
		http.Error(w, `{"error":"Cannot process your request at the moment"}`, http.StatusInternalServerError)
		return
	}

	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	// Parse pagination params
	pageParams := pagination.ParsePageParams(r)

	totalCount, err := repo.GetAllActiveActivitiesCount(r.Context())
	if err != nil {
		ah.Logger.Error("Failed to get total activity count", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We couldn't provide all activities at the moment.",
		})
		return
	}

	activities, err := repo.GetAllActiveActivities(r.Context(), repository.GetAllActiveActivitiesParams{
		Limit:  int32(pageParams.PageSize),
		Offset: int32(pageParams.Offset),
	})

	if err != nil {
		ah.Logger.Error("Failed to retrieve active activities", slog.Any("error", err),
			slog.Any("parameters",
				repository.GetAllActiveActivitiesParams{
					Limit:  int32(pageParams.PageSize),
					Offset: int32(pageParams.Offset),
				}))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We couldn't provide activities at the moment.",
		})
		return
	}

	response := pagination.BuildPaginatedResponse(r, totalCount, activities, pageParams)
	json.NewEncoder(w).Encode(response)
}

func (ah *ActivityHandler) GetAllActivities(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	tx, err := conn.Begin(r.Context())
	if err != nil {
		ah.Logger.Error("Failed to start transaction", slog.Any("error", err))
		http.Error(w, `{"error":"Cannot process your request at the moment"}`, http.StatusInternalServerError)
		return
	}

	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	// Parse pagination params
	pageParams := pagination.ParsePageParams(r)

	totalCount, err := repo.GetAllActivitiesCount(r.Context())
	if err != nil {
		ah.Logger.Error("Failed to get total activity count", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We couldn't provide all activities at the moment.",
		})
		return
	}

	activities, err := repo.GetAllActivities(r.Context(), repository.GetAllActivitiesParams{
		Limit:  int32(pageParams.PageSize),
		Offset: int32(pageParams.Offset),
	})

	if err != nil {
		ah.Logger.Error("Failed to retrieve all activities", slog.Any("error", err),
			slog.Any("parameters",
				repository.GetAllActivitiesParams{
					Limit:  int32(pageParams.PageSize),
					Offset: int32(pageParams.Offset),
				}))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We couldn't provide activities at the moment.",
		})
		return
	}

	response := pagination.BuildPaginatedResponse(r, totalCount, activities, pageParams)
	json.NewEncoder(w).Encode(response)
}

func (ah *ActivityHandler) CreateActivity(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	requestBody := repository.CreateActivityParams{}

	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		ah.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}

	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	tx, err := conn.Begin(r.Context())
	if err != nil {
		ah.Logger.Error("Failed to start transaction", slog.Any("error", err))
		http.Error(w, `{"error":"Cannot process your request at the moment"}`, http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	activity, err := repo.CreateActivity(r.Context(), requestBody)
	if err != nil {
		ah.Logger.Error("Failed to create activity", slog.Any("error", err), slog.Any("activity", requestBody))
		http.Error(w, `{"error":"Cannot process your request at the moment"}`, http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		ah.Logger.Error("Error while committing transaction", slog.Any("error", err), slog.Any("activity", activity))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}
	json.NewEncoder(w).Encode(activity)
}
