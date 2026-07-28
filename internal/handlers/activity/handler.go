package activity

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

type ActivityHandler struct {
	Logger *slog.Logger
	Cacher core.Cacher
	DB     core.IDBProvider
	Cfg    *config.Config
}

func (ah *ActivityHandler) RegisterHandlers(router core.Router) {
	router.Handle("POST /activity/add", middleware.CreateStack(
		middleware.IsAuthenticated(ah.Cfg, ah.DB, ah.Cacher, ah.Logger),
	)(http.HandlerFunc(ah.CreateActivity)))
	router.Handle("GET /activity/all", middleware.CreateStack(
		middleware.IsAuthenticated(ah.Cfg, ah.DB, ah.Cacher, ah.Logger),
	)(http.HandlerFunc(ah.GetAllActivities)))
	router.Handle("GET /activity/active", middleware.CreateStack(
		middleware.IsAuthenticated(ah.Cfg, ah.DB, ah.Cacher, ah.Logger),
	)(http.HandlerFunc(ah.GetAllActiveActivities)))
	router.Handle("GET /activity/inactive", middleware.CreateStack(
		middleware.IsAuthenticated(ah.Cfg, ah.DB, ah.Cacher, ah.Logger),
	)(http.HandlerFunc(ah.GetAllInactiveActivities)))
	router.Handle("PATCH /activity/{id}", middleware.CreateStack(
		middleware.IsAuthenticated(ah.Cfg, ah.DB, ah.Cacher, ah.Logger),
	)(http.HandlerFunc(ah.UpdateActivity)))
	router.Handle("DELETE /activity/{id}", middleware.CreateStack(
		middleware.IsAuthenticated(ah.Cfg, ah.DB, ah.Cacher, ah.Logger),
	)(http.HandlerFunc(ah.DeleteActivity)))

	// Activity completions
	router.Handle(
		"GET /users/activity/completions/for-user/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(ah.Cfg, ah.DB, ah.Cacher, ah.Logger),
		)(http.HandlerFunc(ah.GetAllUserActivityCompletions)),
	)
}

// GetAllUserActivityCompletions godoc
//
// @Summary      List a given user's completed activities
// @Description  Note: any authenticated caller can view any other user's activity completions by id — there is no ownership check on this endpoint today.
// @Tags         activities
// @Produce      json
// @Param        id         path   string  true   "Account ID"
// @Param        page       query  int     false  "Page number (default 1)"
// @Param        page_size  query  int     false  "Page size (default 10, max 100)"
// @Success      200  {object}  pagination.PaginatedResponse
// @Failure      400  {object}  core.APIError  "Invalid id"
// @Failure      500  {object}  core.APIError  "Failed to fetch completions"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /users/activity/completions/for-user/{id} [get]
func (ah *ActivityHandler) GetAllUserActivityCompletions(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")
	rawID := r.PathValue("id")
	id, err := uuid.Parse(rawID)
	if err != nil {
		ah.Logger.Error(
			"Failed to parse user's uuid from id path parameter",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).
			Encode(map[string]any{"error": "Please check your request body and try that again"})
		return
	}
	conn, err := ah.DB.Acquire(r.Context())
	if err != nil {
		ah.Logger.Error(
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
	defer conn.Release()

	tx, err := conn.Begin(r.Context())
	if err != nil {
		ah.Logger.Error("Failed to start transaction", slog.Any("error", err))
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

	totalCount, err := repo.GetAllUserActivityCompletionsCount(r.Context(), id)
	if err != nil {
		ah.Logger.Error("Failed to get total activities completed for user",
			slog.Any("error", err),
			slog.Any("id", id),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We couldn't provide all activity completions for that user at the moment.",
		})
		return
	}

	activities, err := repo.GetAllUserActivityCompletions(
		r.Context(),
		repository.GetAllUserActivityCompletionsParams{
			Limit:     int32(pageParams.PageSize),
			Offset:    int32(pageParams.Offset),
			AccountID: id,
		},
	)
	if err != nil {
		ah.Logger.Error("Failed to retrieve completed activities for user",
			slog.Any("error", err),
			slog.Any("parameters",
				repository.GetAllUserActivityCompletionsParams{
					AccountID: id,
					Limit:     int32(pageParams.PageSize),
					Offset:    int32(pageParams.Offset),
				}))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We couldn't provide all activity completions for that user at the moment.",
		})
		return
	}

	response := pagination.BuildPaginatedResponse(
		r,
		totalCount,
		activities,
		pageParams,
	)
	json.NewEncoder(w).Encode(response)
}

// DeleteActivity godoc
//
// @Summary      Delete an activity definition
// @Description  Note: Activity is a shared catalog resource with no per-user owner — this route only checks IsAuthenticated today, with no admin-style permission gate (see ADR 0006 for the planned fix).
// @Tags         activities
// @Produce      json
// @Param        id  path  string  true  "Activity ID"
// @Success      200  {object}  map[string]any  "Confirmation message"
// @Failure      400  {object}  core.APIError  "Invalid id"
// @Failure      500  {object}  core.APIError  "Failed to delete activity"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /activity/{id} [delete]
func (ah *ActivityHandler) DeleteActivity(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")

	rawID := r.PathValue("id")
	id, err := uuid.Parse(rawID)
	if err != nil {
		ah.Logger.Error(
			"Failed to parse request path parameter",
			slog.Any("error", err),
			slog.Any("value", rawID),
		)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}

	conn, err := ah.DB.Acquire(r.Context())
	if err != nil {
		ah.Logger.Error(
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
	defer conn.Release()

	tx, err := conn.Begin(r.Context())
	if err != nil {
		ah.Logger.Error("Failed to start transaction", slog.Any("error", err))
		http.Error(
			w,
			`{"error":"Cannot process your request at the moment"}`,
			http.StatusInternalServerError,
		)
		return
	}
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	err = repo.DeleteActivity(r.Context(), id)
	if err != nil {
		ah.Logger.Error(
			"Failed to delete activity",
			slog.Any("error", err),
			slog.Any("activity", id),
		)
		http.Error(
			w,
			`{"error":"Cannot process your request at the moment"}`,
			http.StatusInternalServerError,
		)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		ah.Logger.Error(
			"Error while committing transaction",
			slog.Any("error", err),
			slog.Any("activity", id),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).
		Encode(map[string]any{"message": "Activity deleted successfully"})
}

// UpdateActivity godoc
//
// @Summary      Update an activity definition
// @Description  Note: Activity is a shared catalog resource with no per-user owner — this route only checks IsAuthenticated today, with no admin-style permission gate (see ADR 0006 for the planned fix).
// @Tags         activities
// @Accept       json
// @Produce      json
// @Param        id       path  string                            true  "Activity ID"
// @Param        request  body  repository.UpdateActivityParams  true  "Fields to update"
// @Success      200  {object}  repository.Activity
// @Failure      400  {object}  core.APIError  "Invalid id or request body"
// @Failure      500  {object}  core.APIError  "Failed to update activity"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /activity/{id} [patch]
func (ah *ActivityHandler) UpdateActivity(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")

	rawID := r.PathValue("id")
	id, err := uuid.Parse(rawID)
	if err != nil {
		ah.Logger.Error(
			"Failed to parse request path parameter",
			slog.Any("error", err),
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

	conn, err := ah.DB.Acquire(r.Context())
	if err != nil {
		ah.Logger.Error(
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
	defer conn.Release()

	tx, err := conn.Begin(r.Context())
	if err != nil {
		ah.Logger.Error("Failed to start transaction", slog.Any("error", err))
		http.Error(
			w,
			`{"error":"Cannot process your request at the moment"}`,
			http.StatusInternalServerError,
		)
		return
	}
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	activity, err := repo.UpdateActivity(r.Context(), requestBody)
	if err != nil {
		ah.Logger.Error(
			"Failed to update activity",
			slog.Any("error", err),
			slog.Any("activity", requestBody),
		)
		http.Error(
			w,
			`{"error":"Cannot process your request at the moment"}`,
			http.StatusInternalServerError,
		)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		ah.Logger.Error(
			"Error while committing transaction",
			slog.Any("error", err),
			slog.Any("activity", activity),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}
	json.NewEncoder(w).Encode(activity)
}

// GetAllInactiveActivities godoc
//
// @Summary      List inactive activity definitions
// @Description  Returns activity definitions that are currently disabled, so
// @Description  completions against them are no longer accepted. Paginated with
// @Description  page/page_size; the response is a count/next/previous/results
// @Description  envelope.
// @Tags         activities
// @Produce      json
// @Param        page       query  int  false  "Page number (default 1)"
// @Param        page_size  query  int  false  "Page size (default 10, max 100)"
// @Success      200  {object}  pagination.PaginatedResponse
// @Failure      500  {object}  core.APIError  "Failed to fetch activities"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /activity/inactive [get]
func (ah *ActivityHandler) GetAllInactiveActivities(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := ah.DB.Acquire(r.Context())
	if err != nil {
		ah.Logger.Error(
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
	defer conn.Release()

	tx, err := conn.Begin(r.Context())
	if err != nil {
		ah.Logger.Error("Failed to start transaction", slog.Any("error", err))
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

	totalCount, err := repo.GetAllInactiveActivitiesCount(r.Context())
	if err != nil {
		ah.Logger.Error(
			"Failed to get total activity count",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We couldn't provide all activities at the moment.",
		})
		return
	}

	activities, err := repo.GetAllInactiveActivities(
		r.Context(),
		repository.GetAllInactiveActivitiesParams{
			Limit:  int32(pageParams.PageSize),
			Offset: int32(pageParams.Offset),
		},
	)
	if err != nil {
		ah.Logger.Error(
			"Failed to retrieve inactive activities",
			slog.Any("error", err),
			slog.Any("parameters",
				repository.GetAllInactiveActivitiesParams{
					Limit:  int32(pageParams.PageSize),
					Offset: int32(pageParams.Offset),
				}),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We couldn't provide activities at the moment.",
		})
		return
	}

	response := pagination.BuildPaginatedResponse(
		r,
		totalCount,
		activities,
		pageParams,
	)
	json.NewEncoder(w).Encode(response)
}

// GetAllActiveActivities godoc
//
// @Summary      List active activity definitions
// @Description  Returns activity definitions users can currently earn
// @Description  completions against. Paginated with page/page_size; the
// @Description  response is a count/next/previous/results envelope.
// @Tags         activities
// @Produce      json
// @Param        page       query  int  false  "Page number (default 1)"
// @Param        page_size  query  int  false  "Page size (default 10, max 100)"
// @Success      200  {object}  pagination.PaginatedResponse
// @Failure      500  {object}  core.APIError  "Failed to fetch activities"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /activity/active [get]
func (ah *ActivityHandler) GetAllActiveActivities(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := ah.DB.Acquire(r.Context())
	if err != nil {
		ah.Logger.Error(
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
	defer conn.Release()

	tx, err := conn.Begin(r.Context())
	if err != nil {
		ah.Logger.Error("Failed to start transaction", slog.Any("error", err))
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

	totalCount, err := repo.GetAllActiveActivitiesCount(r.Context())
	if err != nil {
		ah.Logger.Error(
			"Failed to get total activity count",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We couldn't provide all activities at the moment.",
		})
		return
	}

	activities, err := repo.GetAllActiveActivities(
		r.Context(),
		repository.GetAllActiveActivitiesParams{
			Limit:  int32(pageParams.PageSize),
			Offset: int32(pageParams.Offset),
		},
	)
	if err != nil {
		ah.Logger.Error(
			"Failed to retrieve active activities",
			slog.Any("error", err),
			slog.Any("parameters",
				repository.GetAllActiveActivitiesParams{
					Limit:  int32(pageParams.PageSize),
					Offset: int32(pageParams.Offset),
				}),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We couldn't provide activities at the moment.",
		})
		return
	}

	response := pagination.BuildPaginatedResponse(
		r,
		totalCount,
		activities,
		pageParams,
	)
	json.NewEncoder(w).Encode(response)
}

// GetAllActivities godoc
//
// @Summary      List all activity definitions
// @Description  Returns every activity definition regardless of active state.
// @Description  Paginated with page/page_size; the response is a
// @Description  count/next/previous/results envelope.
// @Tags         activities
// @Produce      json
// @Param        page       query  int  false  "Page number (default 1)"
// @Param        page_size  query  int  false  "Page size (default 10, max 100)"
// @Success      200  {object}  pagination.PaginatedResponse
// @Failure      500  {object}  core.APIError  "Failed to fetch activities"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /activity/all [get]
func (ah *ActivityHandler) GetAllActivities(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := ah.DB.Acquire(r.Context())
	if err != nil {
		ah.Logger.Error(
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
	defer conn.Release()

	tx, err := conn.Begin(r.Context())
	if err != nil {
		ah.Logger.Error("Failed to start transaction", slog.Any("error", err))
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

	totalCount, err := repo.GetAllActivitiesCount(r.Context())
	if err != nil {
		ah.Logger.Error(
			"Failed to get total activity count",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We couldn't provide all activities at the moment.",
		})
		return
	}

	activities, err := repo.GetAllActivities(
		r.Context(),
		repository.GetAllActivitiesParams{
			Limit:  int32(pageParams.PageSize),
			Offset: int32(pageParams.Offset),
		},
	)
	if err != nil {
		ah.Logger.Error(
			"Failed to retrieve all activities",
			slog.Any("error", err),
			slog.Any("parameters",
				repository.GetAllActivitiesParams{
					Limit:  int32(pageParams.PageSize),
					Offset: int32(pageParams.Offset),
				}),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We couldn't provide activities at the moment.",
		})
		return
	}

	response := pagination.BuildPaginatedResponse(
		r,
		totalCount,
		activities,
		pageParams,
	)
	json.NewEncoder(w).Encode(response)
}

// CreateActivity godoc
//
// @Summary      Create an activity definition
// @Description  Note: this route only checks IsAuthenticated today, with no admin-style permission gate (see ADR 0006 for the planned fix).
// @Tags         activities
// @Accept       json
// @Produce      json
// @Param        request  body      repository.CreateActivityParams  true  "Activity to create"
// @Success      200  {object}  repository.Activity
// @Failure      400  {object}  core.APIError  "Invalid request body"
// @Failure      500  {object}  core.APIError  "Failed to create activity"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /activity/add [post]
func (ah *ActivityHandler) CreateActivity(
	w http.ResponseWriter,
	r *http.Request,
) {
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

	conn, err := ah.DB.Acquire(r.Context())
	if err != nil {
		ah.Logger.Error(
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
	defer conn.Release()

	tx, err := conn.Begin(r.Context())
	if err != nil {
		ah.Logger.Error("Failed to start transaction", slog.Any("error", err))
		http.Error(
			w,
			`{"error":"Cannot process your request at the moment"}`,
			http.StatusInternalServerError,
		)
		return
	}
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	activity, err := repo.CreateActivity(r.Context(), requestBody)
	if err != nil {
		ah.Logger.Error(
			"Failed to create activity",
			slog.Any("error", err),
			slog.Any("activity", requestBody),
		)
		http.Error(
			w,
			`{"error":"Cannot process your request at the moment"}`,
			http.StatusInternalServerError,
		)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		ah.Logger.Error(
			"Error while committing transaction",
			slog.Any("error", err),
			slog.Any("activity", activity),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}
	json.NewEncoder(w).Encode(activity)
}
