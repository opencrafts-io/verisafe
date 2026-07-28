package activity

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/middleware/pagination"
	"github.com/opencrafts-io/verisafe/internal/repository"
	activitysvc "github.com/opencrafts-io/verisafe/internal/service/activity"
)

type ActivityHandler struct {
	Logger *slog.Logger
	Cacher core.Cacher
	DB     core.IDBProvider
	Cfg    *config.Config

	// Service builds an activity service bound to the caller's transaction.
	// Left nil it falls back to the real implementation; see the role handler
	// for why this field is the testing seam.
	Service func(repository.Querier) activitysvc.Service
}

func (ah *ActivityHandler) svc(tx pgx.Tx) activitysvc.Service {
	if ah.Service != nil {
		return ah.Service(repository.New(tx))
	}
	return activitysvc.NewService(repository.New(tx))
}

// acquireAndRun calls Acquire and WithTransaction separately rather than
// through core.InTx, because every method in this handler gives Acquire
// failure a different message from Begin failure (msgInternalServer vs
// msgCannotProcess), and InTx would collapse both into the same bare
// sentinel. WithTransaction still owns conn.Release, so there is no leak
// window between the two calls.
//
// Used by the four read-only list methods, none of which distinguished a
// commit failure before this extraction -- they never committed at all, only
// deferred a rollback. Any commit failure they now hit is new behaviour with
// no precedent to match, so it falls through to the same msgCannotProcess a
// Begin failure would produce.
func (ah *ActivityHandler) acquireAndRun(
	r *http.Request,
	fn func(tx pgx.Tx) error,
) error {
	conn, err := ah.DB.Acquire(r.Context())
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		return core.Public(core.ErrInternal, msgInternalServer)
	}

	if err := core.WithTransaction(r.Context(), conn, fn); err != nil {
		return core.Fallback(err, core.ErrInternal, msgCannotProcess)
	}
	return nil
}

// acquireRunAndCommit is acquireAndRun for the three write methods, which DO
// distinguish a commit failure (msgGeneric) from a Begin failure
// (msgCannotProcess) -- two different messages for two different failure
// points that WithTransaction's error alone cannot tell apart, since both
// wrap the same bare core.ErrInternal.
//
// The distinction is made by tracking whether fn reached its success return:
// if it did and WithTransaction still errored, the failure must have been the
// commit; if fn's error is already Public (a repo-call failure), it is
// returned unchanged; otherwise fn was never reached at all, meaning Begin
// failed.
func (ah *ActivityHandler) acquireRunAndCommit(
	r *http.Request,
	fn func(tx pgx.Tx) error,
) error {
	conn, err := ah.DB.Acquire(r.Context())
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		return core.Public(core.ErrInternal, msgInternalServer)
	}

	var reachedSuccess bool
	err = core.WithTransaction(r.Context(), conn, func(tx pgx.Tx) error {
		if err := fn(tx); err != nil {
			return err
		}
		reachedSuccess = true
		return nil
	})
	if err == nil {
		return nil
	}
	if reachedSuccess {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}
	return core.Fallback(err, core.ErrInternal, msgCannotProcess)
}

func (ah *ActivityHandler) RegisterHandlers(router core.Router) {
	router.Handle("POST /activity/add", middleware.CreateStack(
		middleware.IsAuthenticated(ah.Cfg, ah.DB, ah.Cacher, ah.Logger),
	)(core.AppHandler(ah.CreateActivity)))
	router.Handle("GET /activity/all", middleware.CreateStack(
		middleware.IsAuthenticated(ah.Cfg, ah.DB, ah.Cacher, ah.Logger),
	)(core.AppHandler(ah.GetAllActivities)))
	router.Handle("GET /activity/active", middleware.CreateStack(
		middleware.IsAuthenticated(ah.Cfg, ah.DB, ah.Cacher, ah.Logger),
	)(core.AppHandler(ah.GetAllActiveActivities)))
	router.Handle("GET /activity/inactive", middleware.CreateStack(
		middleware.IsAuthenticated(ah.Cfg, ah.DB, ah.Cacher, ah.Logger),
	)(core.AppHandler(ah.GetAllInactiveActivities)))
	router.Handle("PATCH /activity/{id}", middleware.CreateStack(
		middleware.IsAuthenticated(ah.Cfg, ah.DB, ah.Cacher, ah.Logger),
	)(core.AppHandler(ah.UpdateActivity)))
	router.Handle("DELETE /activity/{id}", middleware.CreateStack(
		middleware.IsAuthenticated(ah.Cfg, ah.DB, ah.Cacher, ah.Logger),
	)(core.AppHandler(ah.DeleteActivity)))

	// Activity completions
	router.Handle(
		"GET /users/activity/completions/for-user/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(ah.Cfg, ah.DB, ah.Cacher, ah.Logger),
		)(core.AppHandler(ah.GetAllUserActivityCompletions)),
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
) error {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		ah.Logger.Error(
			"Failed to parse user's uuid from id path parameter",
			slog.Any("error", err),
		)
		return core.Public(core.ErrInvalidInput, msgCheckBodyRetry)
	}

	pageParams := pagination.ParsePageParams(r)

	var total int64
	var rows []repository.ActivityCompletion
	if err := ah.acquireAndRun(r, func(tx pgx.Tx) error {
		var err error
		total, rows, err = ah.svc(tx).ListCompletionsForUser(
			r.Context(), id, int32(pageParams.PageSize), int32(pageParams.Offset),
		)
		if err != nil {
			ah.Logger.Error("Failed to get completed activities for user",
				slog.Any("error", err), slog.Any("id", id),
			)
			return core.Public(core.ErrInternal, msgCompletionsFail)
		}
		return nil
	}); err != nil {
		return err
	}

	core.WriteJSON(
		w, http.StatusOK,
		pagination.BuildPaginatedResponse(r, total, rows, pageParams),
	)
	return nil
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
) error {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		ah.Logger.Error(
			"Failed to parse request path parameter",
			slog.Any("error", err), slog.Any("value", r.PathValue("id")),
		)
		return core.Public(core.ErrInvalidInput, msgCheckBody)
	}

	if err := ah.acquireRunAndCommit(r, func(tx pgx.Tx) error {
		if err := ah.svc(tx).Delete(r.Context(), id); err != nil {
			ah.Logger.Error(
				"Failed to delete activity",
				slog.Any("error", err), slog.Any("activity", id),
			)
			return core.Public(core.ErrInternal, msgCannotProcess)
		}
		return nil
	}); err != nil {
		return err
	}

	core.WriteJSON(w, http.StatusOK, map[string]any{
		"message": "Activity deleted successfully",
	})
	return nil
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
) error {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		ah.Logger.Error(
			"Failed to parse request path parameter",
			slog.Any("error", err), slog.Any("value", r.PathValue("id")),
		)
		return core.Public(core.ErrInvalidInput, msgCheckBody)
	}

	requestBody := repository.UpdateActivityParams{}
	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		ah.Logger.Error("Failed to parse request body", slog.Any("error", err))
		return core.Public(core.ErrInvalidInput, msgCheckBody)
	}
	requestBody.ID = id

	var updated repository.Activity
	if err := ah.acquireRunAndCommit(r, func(tx pgx.Tx) error {
		var err error
		updated, err = ah.svc(tx).Update(r.Context(), requestBody)
		if err != nil {
			ah.Logger.Error(
				"Failed to update activity",
				slog.Any("error", err), slog.Any("activity", requestBody),
			)
			return core.Public(core.ErrInternal, msgCannotProcess)
		}
		return nil
	}); err != nil {
		return err
	}

	core.WriteJSON(w, http.StatusOK, updated)
	return nil
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
) error {
	pageParams := pagination.ParsePageParams(r)

	var total int64
	var rows []repository.Activity
	if err := ah.acquireAndRun(r, func(tx pgx.Tx) error {
		var err error
		total, rows, err = ah.svc(tx).ListInactive(
			r.Context(), int32(pageParams.PageSize), int32(pageParams.Offset),
		)
		if err != nil {
			ah.Logger.Error(
				"Failed to retrieve inactive activities", slog.Any("error", err),
			)
			return core.Public(core.ErrInternal, msgActivitiesFail)
		}
		return nil
	}); err != nil {
		return err
	}

	core.WriteJSON(
		w, http.StatusOK,
		pagination.BuildPaginatedResponse(r, total, rows, pageParams),
	)
	return nil
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
) error {
	pageParams := pagination.ParsePageParams(r)

	var total int64
	var rows []repository.Activity
	if err := ah.acquireAndRun(r, func(tx pgx.Tx) error {
		var err error
		total, rows, err = ah.svc(tx).ListActive(
			r.Context(), int32(pageParams.PageSize), int32(pageParams.Offset),
		)
		if err != nil {
			ah.Logger.Error(
				"Failed to retrieve active activities", slog.Any("error", err),
			)
			return core.Public(core.ErrInternal, msgActivitiesFail)
		}
		return nil
	}); err != nil {
		return err
	}

	core.WriteJSON(
		w, http.StatusOK,
		pagination.BuildPaginatedResponse(r, total, rows, pageParams),
	)
	return nil
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
) error {
	pageParams := pagination.ParsePageParams(r)

	var total int64
	var rows []repository.Activity
	if err := ah.acquireAndRun(r, func(tx pgx.Tx) error {
		var err error
		total, rows, err = ah.svc(tx).List(
			r.Context(), int32(pageParams.PageSize), int32(pageParams.Offset),
		)
		if err != nil {
			ah.Logger.Error(
				"Failed to retrieve all activities", slog.Any("error", err),
			)
			return core.Public(core.ErrInternal, msgActivitiesFail)
		}
		return nil
	}); err != nil {
		return err
	}

	core.WriteJSON(
		w, http.StatusOK,
		pagination.BuildPaginatedResponse(r, total, rows, pageParams),
	)
	return nil
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
) error {
	requestBody := repository.CreateActivityParams{}
	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		ah.Logger.Error("Failed to parse request body", slog.Any("error", err))
		return core.Public(core.ErrInvalidInput, msgCheckBody)
	}

	var created repository.Activity
	if err := ah.acquireRunAndCommit(r, func(tx pgx.Tx) error {
		var err error
		created, err = ah.svc(tx).Create(r.Context(), requestBody)
		if err != nil {
			ah.Logger.Error(
				"Failed to create activity",
				slog.Any("error", err), slog.Any("activity", requestBody),
			)
			return core.Public(core.ErrInternal, msgCannotProcess)
		}
		return nil
	}); err != nil {
		return err
	}

	core.WriteJSON(w, http.StatusOK, created)
	return nil
}
