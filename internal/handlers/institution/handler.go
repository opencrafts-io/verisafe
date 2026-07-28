package institution

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/opencrafts-io/verisafe/internal/broker"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/eventbus"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

type InstitutionHandler struct {
	Cacher              core.Cacher
	DB                  core.IDBProvider
	Logger              *slog.Logger
	Cfg                 *config.Config
	InstitutionEventBus *eventbus.InstitutionEventBus
	Publisher           *broker.Publisher

	// Pool is used only by FanoutInstitutions, an admin backfill that runs a
	// worker pool and so needs to acquire a connection per worker rather than
	// share the single one every other method uses. It is the one place in
	// this package that holds a concrete pgx type instead of core.IDBProvider,
	// and consequently the one method that cannot be driven by a mock.
	Pool *pgxpool.Pool
}

func (ih *InstitutionHandler) RegisterHandlers(router core.Router) {
	// Register endpoints using the new pattern
	router.Handle("POST /institutions/register", middleware.CreateStack(
		middleware.IsAuthenticated(ih.Cfg, ih.DB, ih.Cacher, ih.Logger),
		middleware.HasPermission([]string{"create:institutions:any"}),
	)(http.HandlerFunc(ih.RegisterInstitution)))

	router.Handle("GET /institutions/fanout",
		middleware.CreateStack(
			middleware.IsAuthenticated(ih.Cfg, ih.DB, ih.Cacher, ih.Logger),
			middleware.HasPermission([]string{"create:institutions:any"}),
		)(http.HandlerFunc(ih.FanoutInstitutions)))

	router.Handle("PATCH /institutions/update/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(ih.Cfg, ih.DB, ih.Cacher, ih.Logger),
			middleware.HasPermission([]string{"update:institutions:any"}),
		)(http.HandlerFunc(ih.UpdateInstitutionDetails)))

	router.Handle("GET /institutions/find/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(ih.Cfg, ih.DB, ih.Cacher, ih.Logger),
		)(http.HandlerFunc(ih.GetInstitutionByID)))

	router.Handle("GET /institutions/all",
		middleware.CreateStack(
			middleware.IsAuthenticated(ih.Cfg, ih.DB, ih.Cacher, ih.Logger),
			middleware.HasPermission([]string{"list:institutions:any"}),
		)(http.HandlerFunc(ih.GetAllInstitutions)))

	router.Handle("GET /institutions/search",
		middleware.CreateStack(
			middleware.IsAuthenticated(ih.Cfg, ih.DB, ih.Cacher, ih.Logger),
		)(http.HandlerFunc(ih.SearchInstitutions)))

	router.Handle("DELETE /institutions/delete/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(ih.Cfg, ih.DB, ih.Cacher, ih.Logger),
			middleware.HasPermission([]string{"delete:institutions:any"}),
		)(http.HandlerFunc(ih.DeleteInstitution)))

	// Institution account management
	router.Handle("POST /institutions/account",
		middleware.CreateStack(
			middleware.IsAuthenticated(ih.Cfg, ih.DB, ih.Cacher, ih.Logger),
		)(http.HandlerFunc(ih.AddAcountInstitution)))

	router.Handle("DELETE /institutions/account",
		middleware.CreateStack(
			middleware.IsAuthenticated(ih.Cfg, ih.DB, ih.Cacher, ih.Logger),
		)(http.HandlerFunc(ih.RemoveAccountInstitution)))

	router.Handle("GET /institutions/for-account",
		middleware.CreateStack(
			middleware.IsAuthenticated(ih.Cfg, ih.DB, ih.Cacher, ih.Logger),
		)(http.HandlerFunc((ih.ListInstitutionForAccount))))

	router.Handle("GET /institutions/accounts",
		middleware.CreateStack(
			middleware.IsAuthenticated(ih.Cfg, ih.DB, ih.Cacher, ih.Logger),
		)(http.HandlerFunc(ih.ListAccountsForInstitution)))

	router.Handle("GET /institutions/accounts/fanout",
		middleware.CreateStack(
			middleware.IsAuthenticated(ih.Cfg, ih.DB, ih.Cacher, ih.Logger),
		)(http.HandlerFunc(ih.FanoutInstitutionConnections)))
}

// RegisterInstitution godoc
//
// @Summary      Register a new institution
// @Description  Creates an institution record. Institutions are the entities
// @Description  accounts are linked to; creating one does not link anybody to
// @Description  it. Publishes an institution-created event.
// @Tags         institutions
// @Accept       json
// @Produce      json
// @Param        request  body      repository.CreateInstitutionParams  true  "Institution to create"
// @Success      201  {object}  repository.Institution
// @Failure      400  {object}  core.APIError  "Invalid request body"
// @Failure      500  {object}  core.APIError  "Failed to create institution"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /institutions/register [post]
func (ih *InstitutionHandler) RegisterInstitution(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := ih.DB.Acquire(r.Context())
	if err != nil {
		ih.Logger.Error(
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

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	var req repository.CreateInstitutionParams
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ih.Logger.Error("Failed to parse request body", slog.Any("error", err))
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	created, err := repo.CreateInstitution(r.Context(), req)
	if err != nil {
		ih.Logger.Error("Failed to create institution", slog.Any("error", err))
		http.Error(
			w,
			`{"error":"failed to create institution"}`,
			http.StatusInternalServerError,
		)
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		ih.Logger.Error("Error committing transaction", slog.Any("error", err))
		http.Error(
			w,
			`{"error":"internal server error"}`,
			http.StatusInternalServerError,
		)
		return
	}
	if ih.InstitutionEventBus != nil {
		requestID := eventbus.GenerateRequestID()
		_ = ih.InstitutionEventBus.PublishInstitutionCreated(
			r.Context(),
			created,
			requestID,
		)
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(created)
}

// UpdateInstitutionDetails godoc
//
// @Summary      Update an institution's details
// @Description  Updates an institution in place. Existing account links are
// @Description  preserved. Publishes an institution-updated event.
// @Tags         institutions
// @Accept       json
// @Produce      json
// @Param        id       path  int                                  true  "Institution ID"
// @Param        request  body  repository.UpdateInstitutionParams  true  "Fields to update"
// @Success      200  {object}  repository.Institution
// @Failure      400  {object}  core.APIError  "Invalid id or request body"
// @Failure      500  {object}  core.APIError  "Failed to update institution"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /institutions/update/{id} [patch]
func (ih *InstitutionHandler) UpdateInstitutionDetails(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := ih.DB.Acquire(r.Context())
	if err != nil {
		ih.Logger.Error(
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

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	// Extract ID from URL
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(
			w,
			`{"error":"invalid institution id"}`,
			http.StatusBadRequest,
		)
		return
	}

	var req repository.UpdateInstitutionParams
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ih.Logger.Error("Failed to parse request body", slog.Any("error", err))
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	req.InstitutionID = int32(id)

	updated, err := repo.UpdateInstitution(r.Context(), req)
	if err != nil {
		ih.Logger.Error("Failed to update institution", slog.Any("error", err))
		http.Error(
			w,
			`{"error":"failed to update institution"}`,
			http.StatusInternalServerError,
		)
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		ih.Logger.Error("Error committing transaction", slog.Any("error", err))
		http.Error(
			w,
			`{"error":"internal server error"}`,
			http.StatusInternalServerError,
		)
		return
	}
	if ih.InstitutionEventBus != nil {
		requestID := eventbus.GenerateRequestID()
		_ = ih.InstitutionEventBus.PublishInstitutionUpdated(
			r.Context(),
			updated,
			requestID,
		)
	}

	json.NewEncoder(w).Encode(updated)
}

// GetInstitutionByID godoc
//
// @Summary      Get an institution by id
// @Description  Returns a single institution by its UUID. Responds 404 when no
// @Description  institution carries that id.
// @Tags         institutions
// @Produce      json
// @Param        id  path  int  true  "Institution ID"
// @Success      200  {object}  repository.Institution
// @Failure      400  {object}  core.APIError  "Invalid id"
// @Failure      404  {object}  core.APIError  "Institution not found"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /institutions/find/{id} [get]
func (ih *InstitutionHandler) GetInstitutionByID(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := ih.DB.Acquire(r.Context())
	if err != nil {
		ih.Logger.Error(
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
	repo := repository.New(conn)

	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(
			w,
			`{"error":"invalid institution id"}`,
			http.StatusBadRequest,
		)
		return
	}

	institution, err := repo.GetInstitution(r.Context(), int32(id))
	if err != nil {
		ih.Logger.Error("Failed to get institution", slog.Any("error", err))
		http.Error(w, `{"error":"institution not found"}`, http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(institution)
}

// GetAllInstitutions godoc
//
// @Summary      List institutions
// @Description  Returns a paginated list of every registered institution.
// @Description  Paginated with limit/offset; the response is a bare array, not
// @Description  an envelope.
// @Tags         institutions
// @Produce      json
// @Param        limit   query  int  false  "Page size (default 10, max 100)"
// @Param        offset  query  int  false  "Page offset (default 0)"
// @Success      200  {array}   repository.Institution
// @Failure      500  {object}  core.APIError  "Failed to fetch institutions"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /institutions/all [get]
func (ih *InstitutionHandler) GetAllInstitutions(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := ih.DB.Acquire(r.Context())
	if err != nil {
		ih.Logger.Error(
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
	repo := repository.New(conn)

	p := middleware.GetPagination(r.Context())

	institutions, err := repo.ListInstitutions(
		r.Context(),
		repository.ListInstitutionsParams{
			Limit:  int32(p.Limit),
			Offset: int32(p.Offset),
		},
	)
	if err != nil {
		ih.Logger.Error("Failed to list institutions", slog.Any("error", err))
		http.Error(
			w,
			`{"error":"failed to fetch institutions"}`,
			http.StatusInternalServerError,
		)
		return
	}

	json.NewEncoder(w).Encode(institutions)
}

// DeleteInstitution godoc
//
// @Summary      Delete an institution
// @Description  Permanently deletes an institution. Account links to it are
// @Description  removed by the cascade on account_institutions. Publishes an
// @Description  institution-deleted event. Returns 204 with no body on success.
// @Tags         institutions
// @Produce      json
// @Param        id  path  int  true  "Institution ID"
// @Success      204  "No Content"
// @Failure      400  {object}  core.APIError  "Invalid id"
// @Failure      404  {object}  core.APIError  "Institution not found"
// @Failure      500  {object}  core.APIError  "Failed to delete institution"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /institutions/delete/{id} [delete]
func (ih *InstitutionHandler) DeleteInstitution(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := ih.DB.Acquire(r.Context())
	if err != nil {
		ih.Logger.Error(
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

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(
			w,
			`{"error":"invalid institution id"}`,
			http.StatusBadRequest,
		)
		return
	}

	institution, err := repo.GetInstitution(r.Context(), int32(id))
	if err != nil {
		ih.Logger.Error("Failed to get institution", slog.Any("error", err))
		http.Error(w, `{"error":"institution not found"}`, http.StatusNotFound)
		return
	}

	if err := repo.DeleteInstitution(r.Context(), int32(id)); err != nil {
		ih.Logger.Error("Failed to delete institution", slog.Any("error", err))
		http.Error(
			w,
			`{"error":"failed to delete institution"}`,
			http.StatusInternalServerError,
		)
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		ih.Logger.Error("Error committing transaction", slog.Any("error", err))
		http.Error(
			w,
			`{"error":"internal server error"}`,
			http.StatusInternalServerError,
		)
		return
	}

	if ih.InstitutionEventBus != nil {
		requestID := eventbus.GenerateRequestID()
		_ = ih.InstitutionEventBus.PublishInstitutionDeleted(
			r.Context(),
			institution,
			requestID,
		)
	}

	w.WriteHeader(http.StatusNoContent)
}

// SearchInstitutions godoc
//
// @Summary      Search institutions by name
// @Description  Returns institutions whose name matches the query, paginated
// @Description  with limit/offset.
// @Tags         institutions
// @Produce      json
// @Param        q       query  string  true   "Name search query"
// @Param        limit   query  int     false  "Page size (default 10, max 100)"
// @Param        offset  query  int     false  "Page offset (default 0)"
// @Success      200  {array}   repository.Institution
// @Failure      400  {object}  core.APIError  "Missing query parameter 'q'"
// @Failure      500  {object}  core.APIError  "Search failed"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /institutions/search [get]
func (ih *InstitutionHandler) SearchInstitutions(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")

	conn, err := ih.DB.Acquire(r.Context())
	if err != nil {
		ih.Logger.Error("DB connection missing", slog.Any("error", err))
		http.Error(
			w,
			`{"error":"internal server error"}`,
			http.StatusInternalServerError,
		)
		return
	}
	defer conn.Release()
	repo := repository.New(conn)

	// Extract query param `q`
	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(
			w,
			`{"error":"missing search query param 'q'"}`,
			http.StatusBadRequest,
		)
		return
	}

	// Get pagination values from middleware
	p := middleware.GetPagination(r.Context())

	institutions, err := repo.SearchInstitutionsByName(
		r.Context(),
		repository.SearchInstitutionsByNameParams{
			Name:   q,
			Limit:  int32(p.Limit),
			Offset: int32(p.Offset),
		},
	)
	if err != nil {
		ih.Logger.Error("Search failed", slog.Any("error", err))
		http.Error(
			w,
			`{"error":"failed to search institutions"}`,
			http.StatusInternalServerError,
		)
		return
	}

	if err := json.NewEncoder(w).Encode(institutions); err != nil {
		ih.Logger.Error("Failed to encode response", slog.Any("error", err))
	}
}

// AddAcountInstitution godoc
//
// @Summary      Link an account to an institution
// @Description  The request body's account_id must match the caller's own subject, unless the caller holds manage:institutions:accounts:any.
// @Tags         institutions
// @Accept       json
// @Produce      json
// @Param        request  body      repository.AddAccountInstitutionParams  true  "account_id must be the caller's own, unless an admin"
// @Success      201  {object}  repository.AddAccountInstitutionRow
// @Failure      400  {object}  core.APIError  "Invalid request body"
// @Failure      403  {object}  core.APIError  "account_id does not match the caller and caller is not an admin"
// @Failure      500  {object}  core.APIError  "Failed to link account to institution"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /institutions/account [post]
func (ih *InstitutionHandler) AddAcountInstitution(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := ih.DB.Acquire(r.Context())
	if err != nil {
		ih.Logger.Error(
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

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	var req repository.AddAccountInstitutionParams
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ih.Logger.Error("Failed to parse request body", slog.Any("error", err))
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	claims, ok := middleware.ClaimsFromContext(r.Context())
	perms := middleware.PermissionsFromContext(r.Context())
	isAdmin := slices.Contains(perms, "manage:institutions:accounts:any")
	if !ok || (!isAdmin && req.AccountID.String() != claims.Subject) {
		core.WriteError(w, http.StatusForbidden, "you can only manage your own institution memberships")
		return
	}

	created, err := repo.AddAccountInstitution(r.Context(), req)
	if err != nil {
		ih.Logger.Error("Failed to create institution", slog.Any("error", err))
		http.Error(
			w,
			`{"error":"failed to link you to that organization"}`,
			http.StatusInternalServerError,
		)
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		ih.Logger.Error("Error committing transaction", slog.Any("error", err))
		http.Error(
			w,
			`{"error":"internal server error"}`,
			http.StatusInternalServerError,
		)
		return
	}

	if ih.Publisher != nil {
		eventPayload := map[string]any{
			"meta": map[string]any{
				"event_type":        "user.institution.connected",
				"timestamp":         time.Now().UTC().Format(time.RFC3339),
				"source_service_id": "io.opencrafts.verisafe",
				"request_id":        uuid.New().String(),
			},
			"institution_connection": map[string]any{
				"account_id":     req.AccountID,
				"institution_id": req.InstitutionID,
			},
		}
		err := ih.Publisher.Publish(
			r.Context(),
			"verisafe.events.topic",
			broker.TopicExchangeType,
			"user.institution.connected",
			eventPayload,
		)
		if err != nil {
			ih.Logger.Error(
				"failed to publish institution connection event",
				"error",
				err,
			)
		}
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(created)
}

// ListInstitutionForAccount godoc
//
// @Summary      List institutions linked to a given account
// @Description  Note: any authenticated caller can look up any account's institution memberships by account_id — there is no ownership check on this endpoint today.
// @Tags         institutions
// @Produce      json
// @Param        account_id  query  string  true   "Account ID"
// @Param        limit       query  int     false  "Page size (default 10, max 100)"
// @Param        offset      query  int     false  "Page offset (default 0)"
// @Success      200  {array}   repository.Institution
// @Failure      400  {object}  core.APIError  "Missing or invalid account_id"
// @Failure      500  {object}  core.APIError  "Failed to fetch institutions"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /institutions/for-account [get]
func (ih *InstitutionHandler) ListInstitutionForAccount(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := ih.DB.Acquire(r.Context())
	if err != nil {
		ih.Logger.Error(
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
	repo := repository.New(conn)

	// Extract query param `q`
	q := r.URL.Query().Get("account_id")
	if q == "" {
		http.Error(
			w,
			`{"error":"missing search query param 'q'"}`,
			http.StatusBadRequest,
		)
		return
	}

	// parse the uuid
	id, err := uuid.Parse(q)
	if err != nil {
		http.Error(
			w,
			`{"error":"Could not parse the uuid parameter"}`,
			http.StatusBadRequest,
		)
		return
	}

	p := middleware.GetPagination(r.Context())
	institutions, err := repo.ListInstitutionsForAccount(
		r.Context(),
		repository.ListInstitutionsForAccountParams{
			AccountID: id,
			Limit:     int32(p.Limit),
			Offset:    int32(p.Offset),
		},
	)
	if err != nil {
		ih.Logger.Error("Failed to list institutions", slog.Any("error", err))
		http.Error(
			w,
			`{"error":"failed to fetch institutions"}`,
			http.StatusInternalServerError,
		)
		return
	}

	json.NewEncoder(w).Encode(institutions)
}

// ListAccountsForInstitution godoc
//
// @Summary      List accounts linked to a given institution
// @Description  Returns the accounts linked to the institution, paginated with
// @Description  limit/offset. This is the inverse of listing an account's
// @Description  institutions.
// @Tags         institutions
// @Produce      json
// @Param        institution_id  query  int  true   "Institution ID"
// @Param        limit           query  int  false  "Page size (default 10, max 100)"
// @Param        offset          query  int  false  "Page offset (default 0)"
// @Success      200  {array}   repository.Account
// @Failure      400  {object}  core.APIError  "Missing or invalid institution_id"
// @Failure      500  {object}  core.APIError  "Failed to fetch accounts"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /institutions/accounts [get]
func (ih *InstitutionHandler) ListAccountsForInstitution(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := ih.DB.Acquire(r.Context())
	if err != nil {
		ih.Logger.Error(
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
	repo := repository.New(conn)

	// Extract query param `q`
	q := r.URL.Query().Get("institution_id")
	if q == "" {
		http.Error(
			w,
			`{"error":"missing search query param 'q'"}`,
			http.StatusBadRequest,
		)
		return
	}

	// parse the uuid
	id, err := strconv.Atoi(q)
	if err != nil {
		http.Error(
			w,
			`{"error":"Could not parse the institution id parameter"}`,
			http.StatusBadRequest,
		)
		return
	}

	p := middleware.GetPagination(r.Context())
	institutions, err := repo.ListAccountsForInstitution(
		r.Context(),
		repository.ListAccountsForInstitutionParams{
			InstitutionID: int32(id),
			Limit:         int32(p.Limit),
			Offset:        int32(p.Offset),
		},
	)
	if err != nil {
		ih.Logger.Error("Failed to list institutions", slog.Any("error", err))
		http.Error(
			w,
			`{"error":"failed to fetch institutions"}`,
			http.StatusInternalServerError,
		)
		return
	}

	json.NewEncoder(w).Encode(institutions)
}

// RemoveAccountInstitution godoc
//
// @Summary      Unlink an account from an institution
// @Description  The request body's account_id must match the caller's own subject, unless the caller holds manage:institutions:accounts:any.
// @Tags         institutions
// @Accept       json
// @Produce      json
// @Param        request  body      repository.RemoveAccountInstitutionParams  true  "account_id must be the caller's own, unless an admin"
// @Success      200  {object}  map[string]any  "Confirmation message"
// @Failure      400  {object}  core.APIError  "Invalid request body"
// @Failure      403  {object}  core.APIError  "account_id does not match the caller and caller is not an admin"
// @Failure      500  {object}  core.APIError  "Failed to unlink account from institution"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /institutions/account [delete]
func (ih *InstitutionHandler) RemoveAccountInstitution(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := ih.DB.Acquire(r.Context())
	if err != nil {
		ih.Logger.Error(
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

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	var req repository.RemoveAccountInstitutionParams
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ih.Logger.Error("Failed to parse request body", slog.Any("error", err))
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	claims, ok := middleware.ClaimsFromContext(r.Context())
	perms := middleware.PermissionsFromContext(r.Context())
	isAdmin := slices.Contains(perms, "manage:institutions:accounts:any")
	if !ok || (!isAdmin && req.AccountID.String() != claims.Subject) {
		core.WriteError(w, http.StatusForbidden, "you can only manage your own institution memberships")
		return
	}

	err = repo.RemoveAccountInstitution(r.Context(), req)
	if err != nil {
		ih.Logger.Error("Failed to create institution", slog.Any("error", err))
		http.Error(
			w,
			`{"error":"failed to create institution"}`,
			http.StatusInternalServerError,
		)
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		ih.Logger.Error("Error committing transaction", slog.Any("error", err))
		http.Error(
			w,
			`{"error":"internal server error"}`,
			http.StatusInternalServerError,
		)
		return
	}

	if ih.Publisher != nil {
		eventPayload := map[string]any{
			"meta": map[string]any{
				"event_type":        "user.institution.disconnected",
				"timestamp":         time.Now().UTC().Format(time.RFC3339),
				"source_service_id": "io.opencrafts.verisafe",
				"request_id":        uuid.New().String(),
			},
			"institution_connection": map[string]any{
				"account_id":     req.AccountID,
				"institution_id": req.InstitutionID,
			},
		}
		err := ih.Publisher.Publish(
			r.Context(),
			"verisafe.events.topic",
			broker.TopicExchangeType,
			"user.institution.disconnected",
			eventPayload,
		)
		if err != nil {
			ih.Logger.Error(
				"failed to publish institution connection event",
				"error",
				err,
			)
		}
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).
		Encode(map[string]any{"message": "Successfully removed from institution"})
}

// FanoutInstitutionConnections godoc
//
// @Summary      Republish every institution-account connection to the event bus
// @Description  Batches through all account/institution links and republishes a connected event for each, via a worker pool. Intended for backfilling downstream consumers, not routine use. Note: this route only checks IsAuthenticated today, with no admin-style permission gate (see ADR 0006 for the planned fix).
// @Tags         institutions
// @Produce      json
// @Success      200  {object}  map[string]string  "status: fanout complete"
// @Failure      500  {object}  core.APIError  "Failed to acquire a database connection"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /institutions/accounts/fanout [get]
func (ih *InstitutionHandler) FanoutInstitutionConnections(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx := r.Context()
	conn, err := ih.DB.Acquire(ctx)
	if err != nil {
		ih.Logger.Error("DB connection missing", slog.Any("error", err))
		http.Error(
			w,
			`{"error":"internal server error"}`,
			http.StatusInternalServerError,
		)
		return
	}
	defer conn.Release()

	repo := repository.New(conn)

	const batchSize = 500
	const workerCount = 10

	jobChan := make(chan repository.AccountInstitution, batchSize)
	var wg sync.WaitGroup

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for connection := range jobChan {
				ih.publishConnectionEvent(ctx, connection)
			}
		}()
	}

	go func() {
		defer close(jobChan)
		offset := 0
		for {
			connections, err := repo.ListInstitutionConnections(
				ctx,
				repository.ListInstitutionConnectionsParams{
					Limit:  int32(batchSize),
					Offset: int32(offset),
				},
			)
			if err != nil {
				ih.Logger.Error(
					"Batch fetch failed",
					"offset",
					offset,
					"error",
					err,
				)
				break
			}

			if len(connections) == 0 {
				break
			}

			for _, c := range connections {
				jobChan <- c
			}

			offset += batchSize
		}
	}()

	wg.Wait()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "fanout complete"})
}

// Helper to keep the handler clean
func (ih *InstitutionHandler) publishConnectionEvent(
	ctx context.Context,
	conn repository.AccountInstitution,
) {
	if ih.Publisher == nil {
		return
	}

	payload := map[string]any{
		"meta": map[string]any{
			"event_type":        "user.institution.connected",
			"timestamp":         time.Now().UTC().Format(time.RFC3339),
			"source_service_id": "io.opencrafts.verisafe",
			"request_id":        uuid.New().String(),
		},
		"institution_connection": map[string]any{
			"account_id":     conn.AccountID,
			"institution_id": conn.InstitutionID,
		},
	}

	err := ih.Publisher.Publish(
		ctx,
		"verisafe.events.topic",
		broker.TopicExchangeType,
		"user.institution.connected",
		payload,
	)
	if err != nil {
		ih.Logger.Error(
			"Institution fanout publish failed",
			"account_id",
			conn.AccountID,
			"error",
			err,
		)
	}
}

// FanoutInstitutions godoc
//
// @Summary      Republish every institution to the event bus
// @Description  Batches through all institutions and republishes an InstitutionCreated event for each, via a worker pool. Intended for backfilling downstream consumers, not routine use.
// @Tags         institutions
// @Produce      json
// @Success      200  {object}  map[string]any  "Publish count message"
// @Failure      500  {object}  core.APIError  "Failed to read institutions or publish one or more batches"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /institutions/fanout [get]
func (ih *InstitutionHandler) FanoutInstitutions(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")

	// This backfill fans out across a worker pool, so unlike every other
	// handler it needs the pool itself rather than one acquired connection.
	pool := ih.Pool
	if pool == nil {
		ih.Logger.Error(
			"Error while processing request",
			slog.String("error", "institution handler has no pool configured"),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	// For the initial count, we can use the pool directly
	institutionCount, err := repository.New(pool).
		GetInstitutionsCount(r.Context())
	if err != nil {
		ih.Logger.Error(
			"Error while processing request",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into an error while trying to service your request",
		})
		return
	}

	batchSize := 1000
	totalBatches := (int(institutionCount) + batchSize - 1) / batchSize

	var publishedCount int64
	workerCount := 5
	semaphore := make(chan struct{}, workerCount)
	var wg sync.WaitGroup
	errChan := make(chan error, totalBatches)

	for batch := 0; batch < totalBatches; batch++ {
		wg.Add(1)
		semaphore <- struct{}{}

		go func(batchNum int) {
			defer wg.Done()
			defer func() { <-semaphore }()

			offset := batchNum * batchSize
			ctx, cancel := context.WithTimeout(
				context.Background(),
				5*time.Minute,
			)
			defer cancel()

			// Each goroutine uses the pool - it will acquire and release connections automatically
			repo := repository.New(pool)

			institutions, err := repo.ListInstitutions(
				ctx,
				repository.ListInstitutionsParams{
					Limit:  int32(batchSize),
					Offset: int32(offset),
				},
			)
			if err != nil {
				ih.Logger.Error("Error fetching batch",
					slog.Any("error", err),
					slog.Int("batch", batchNum),
				)
				errChan <- fmt.Errorf("batch %d: %w", batchNum, err)
				return
			}

			// Publish each institution
			for _, institution := range institutions {
				requestID := eventbus.GenerateRequestID()
				if err := ih.InstitutionEventBus.PublishInstitutionCreated(ctx, institution, requestID); err != nil {
					ih.Logger.Error("Error publishing institution",
						slog.Any("error", err),
						slog.Int("batch", batchNum),
					)
					errChan <- fmt.Errorf("batch %d, institution publish: %w", batchNum, err)
					return
				}
				atomic.AddInt64(&publishedCount, 1)
			}
		}(batch)
	}

	wg.Wait()
	close(errChan)

	// Collect all errors
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		ih.Logger.Error("Failed to publish some batches",
			slog.Int("error_count", len(errors)),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error":   "Some batches failed to publish",
			"details": fmt.Sprintf("%d batches failed", len(errors)),
		})
		return
	}

	finalCount := atomic.LoadInt64(&publishedCount)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"message": fmt.Sprintf(
			"Published %d institutions to the event bus",
			finalCount,
		),
	})
}
