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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/opencrafts-io/verisafe/internal/broker"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/eventbus"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	institutionsvc "github.com/opencrafts-io/verisafe/internal/service/institution"
)

type InstitutionHandler struct {
	Cacher              core.Cacher
	DB                  core.IDBProvider
	Logger              *slog.Logger
	Cfg                 *config.Config
	InstitutionEventBus eventbus.InstitutionPublisher
	Publisher           *broker.Publisher

	// Pool is used only by FanoutInstitutions, an admin backfill that runs a
	// worker pool and so needs to acquire a connection per worker rather than
	// share the single one every other method uses. It is the one place in
	// this package that holds a concrete pgx type instead of core.IDBProvider,
	// and consequently the one method that cannot be driven by a mock.
	Pool *pgxpool.Pool

	// Service builds an institution service bound to the caller's connection
	// or transaction. Left nil it falls back to the real implementation; see
	// the role handler for why this field is the testing seam. Unlike other
	// handlers' svc helpers, this one takes repository.DBTX rather than
	// pgx.Tx, because five read methods here query the acquired connection
	// directly with no transaction at all -- preserving that, rather than
	// wrapping them in a transaction they never had, is why the parameter is
	// the wider interface.
	Service func(repository.Querier) institutionsvc.Service
}

func (ih *InstitutionHandler) svc(db repository.DBTX) institutionsvc.Service {
	if ih.Service != nil {
		return ih.Service(repository.New(db))
	}
	return institutionsvc.NewService(repository.New(db))
}

func (ih *InstitutionHandler) RegisterHandlers(router core.Router) {
	// Register endpoints using the new pattern
	router.Handle("POST /institutions/register", middleware.CreateStack(
		middleware.IsAuthenticated(ih.Cfg, ih.DB, ih.Cacher, ih.Logger),
		middleware.HasPermission([]string{"create:institutions:any"}),
	)(core.AppHandler(ih.RegisterInstitution)))

	router.Handle("GET /institutions/fanout",
		middleware.CreateStack(
			middleware.IsAuthenticated(ih.Cfg, ih.DB, ih.Cacher, ih.Logger),
			middleware.HasPermission([]string{"create:institutions:any"}),
		)(http.HandlerFunc(ih.FanoutInstitutions)))

	router.Handle("PATCH /institutions/update/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(ih.Cfg, ih.DB, ih.Cacher, ih.Logger),
			middleware.HasPermission([]string{"update:institutions:any"}),
		)(core.AppHandler(ih.UpdateInstitutionDetails)))

	router.Handle("GET /institutions/find/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(ih.Cfg, ih.DB, ih.Cacher, ih.Logger),
		)(core.AppHandler(ih.GetInstitutionByID)))

	router.Handle("GET /institutions/all",
		middleware.CreateStack(
			middleware.IsAuthenticated(ih.Cfg, ih.DB, ih.Cacher, ih.Logger),
			middleware.HasPermission([]string{"list:institutions:any"}),
		)(core.AppHandler(ih.GetAllInstitutions)))

	router.Handle("GET /institutions/search",
		middleware.CreateStack(
			middleware.IsAuthenticated(ih.Cfg, ih.DB, ih.Cacher, ih.Logger),
		)(core.AppHandler(ih.SearchInstitutions)))

	router.Handle("DELETE /institutions/delete/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(ih.Cfg, ih.DB, ih.Cacher, ih.Logger),
			middleware.HasPermission([]string{"delete:institutions:any"}),
		)(core.AppHandler(ih.DeleteInstitution)))

	// Institution account management
	router.Handle("POST /institutions/account",
		middleware.CreateStack(
			middleware.IsAuthenticated(ih.Cfg, ih.DB, ih.Cacher, ih.Logger),
		)(core.AppHandler(ih.AddAcountInstitution)))

	router.Handle("DELETE /institutions/account",
		middleware.CreateStack(
			middleware.IsAuthenticated(ih.Cfg, ih.DB, ih.Cacher, ih.Logger),
		)(core.AppHandler(ih.RemoveAccountInstitution)))

	router.Handle("GET /institutions/for-account",
		middleware.CreateStack(
			middleware.IsAuthenticated(ih.Cfg, ih.DB, ih.Cacher, ih.Logger),
		)(core.AppHandler(ih.ListInstitutionForAccount)))

	router.Handle("GET /institutions/accounts",
		middleware.CreateStack(
			middleware.IsAuthenticated(ih.Cfg, ih.DB, ih.Cacher, ih.Logger),
		)(core.AppHandler(ih.ListAccountsForInstitution)))

	router.Handle("GET /institutions/accounts/fanout",
		middleware.CreateStack(
			middleware.IsAuthenticated(ih.Cfg, ih.DB, ih.Cacher, ih.Logger),
		)(core.AppHandler(ih.FanoutInstitutionConnections)))
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
) error {
	var req repository.CreateInstitutionParams
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ih.Logger.Error("Failed to parse request body", slog.Any("error", err))
		return core.Public(core.ErrInvalidInput, msgInvalidBody)
	}

	created, err := core.InTx(
		r.Context(), ih.DB,
		func(tx pgx.Tx) (repository.Institution, error) {
			created, err := ih.svc(tx).Create(r.Context(), req)
			if err != nil {
				ih.Logger.Error(
					"Failed to create institution", slog.Any("error", err),
				)
				return repository.Institution{}, core.Public(
					core.ErrInternal, msgCreateFailed,
				)
			}
			return created, nil
		},
	)
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgInternalServer)
	}

	if ih.InstitutionEventBus != nil {
		_ = ih.InstitutionEventBus.PublishInstitutionCreated(
			r.Context(), created, eventbus.GenerateRequestID(),
		)
	}

	core.WriteJSON(w, http.StatusCreated, created)
	return nil
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
) error {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		return core.Public(core.ErrInvalidInput, msgInvalidID)
	}

	var req repository.UpdateInstitutionParams
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ih.Logger.Error("Failed to parse request body", slog.Any("error", err))
		return core.Public(core.ErrInvalidInput, msgInvalidBody)
	}
	req.InstitutionID = int32(id)

	updated, err := core.InTx(
		r.Context(), ih.DB,
		func(tx pgx.Tx) (repository.Institution, error) {
			updated, err := ih.svc(tx).Update(r.Context(), req)
			if err != nil {
				ih.Logger.Error(
					"Failed to update institution", slog.Any("error", err),
				)
				return repository.Institution{}, core.Public(
					core.ErrInternal, msgUpdateFailed,
				)
			}
			return updated, nil
		},
	)
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgInternalServer)
	}

	if ih.InstitutionEventBus != nil {
		_ = ih.InstitutionEventBus.PublishInstitutionUpdated(
			r.Context(), updated, eventbus.GenerateRequestID(),
		)
	}

	core.WriteJSON(w, http.StatusOK, updated)
	return nil
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
) error {
	conn, err := ih.DB.Acquire(r.Context())
	if err != nil {
		ih.Logger.Error("Error while processing request", slog.Any("error", err))
		return core.Public(core.ErrInternal, msgInternalServer)
	}
	defer conn.Release()

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		return core.Public(core.ErrInvalidInput, msgInvalidID)
	}

	inst, err := ih.svc(conn).GetByID(r.Context(), int32(id))
	if err != nil {
		// Every error here becomes 404, not just a genuine not-found -- that
		// is what this endpoint did before the extraction (no errors.Is
		// check existed) and is preserved rather than tightened.
		ih.Logger.Error("Failed to get institution", slog.Any("error", err))
		return core.Public(core.ErrNotFound, msgNotFound)
	}

	core.WriteJSON(w, http.StatusOK, inst)
	return nil
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
) error {
	conn, err := ih.DB.Acquire(r.Context())
	if err != nil {
		ih.Logger.Error("Error while processing request", slog.Any("error", err))
		return core.Public(core.ErrInternal, msgInternalServer)
	}
	defer conn.Release()

	p := middleware.GetPagination(r.Context())

	insts, err := ih.svc(conn).List(r.Context(), int32(p.Limit), int32(p.Offset))
	if err != nil {
		ih.Logger.Error("Failed to list institutions", slog.Any("error", err))
		return core.Public(core.ErrInternal, msgFetchFailed)
	}

	core.WriteJSON(w, http.StatusOK, insts)
	return nil
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
) error {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		return core.Public(core.ErrInvalidInput, msgInvalidID)
	}

	var deleted repository.Institution
	if err := core.InTxDo(r.Context(), ih.DB, func(tx pgx.Tx) error {
		svc := ih.svc(tx)

		inst, err := svc.GetByID(r.Context(), int32(id))
		if err != nil {
			// Same always-404 behaviour as GetInstitutionByID.
			ih.Logger.Error("Failed to get institution", slog.Any("error", err))
			return core.Public(core.ErrNotFound, msgNotFound)
		}
		deleted = inst

		if err := svc.Delete(r.Context(), int32(id)); err != nil {
			ih.Logger.Error(
				"Failed to delete institution", slog.Any("error", err),
			)
			return core.Public(core.ErrInternal, msgDeleteFailed)
		}
		return nil
	}); err != nil {
		return core.Fallback(err, core.ErrInternal, msgInternalServer)
	}

	if ih.InstitutionEventBus != nil {
		_ = ih.InstitutionEventBus.PublishInstitutionDeleted(
			r.Context(), deleted, eventbus.GenerateRequestID(),
		)
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
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
) error {
	conn, err := ih.DB.Acquire(r.Context())
	if err != nil {
		ih.Logger.Error("DB connection missing", slog.Any("error", err))
		return core.Public(core.ErrInternal, msgInternalServer)
	}
	defer conn.Release()

	q := r.URL.Query().Get("q")
	if q == "" {
		return core.Public(core.ErrInvalidInput, msgMissingQuery)
	}

	p := middleware.GetPagination(r.Context())
	insts, err := ih.svc(conn).SearchByName(
		r.Context(), q, int32(p.Limit), int32(p.Offset),
	)
	if err != nil {
		ih.Logger.Error("Search failed", slog.Any("error", err))
		return core.Public(core.ErrInternal, msgSearchFailed)
	}

	core.WriteJSON(w, http.StatusOK, insts)
	return nil
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
) error {
	var req repository.AddAccountInstitutionParams
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ih.Logger.Error("Failed to parse request body", slog.Any("error", err))
		return core.Public(core.ErrInvalidInput, msgInvalidBody)
	}

	claims, ok := middleware.ClaimsFromContext(r.Context())
	perms := middleware.PermissionsFromContext(r.Context())
	isAdmin := slices.Contains(perms, "manage:institutions:accounts:any")
	if !ok || (!isAdmin && req.AccountID.String() != claims.Subject) {
		return core.Public(core.ErrForbidden, msgOwnMembership)
	}

	created, err := core.InTx(
		r.Context(), ih.DB,
		func(tx pgx.Tx) (repository.AddAccountInstitutionRow, error) {
			created, err := ih.svc(tx).AddAccount(r.Context(), req)
			if err != nil {
				ih.Logger.Error(
					"Failed to create institution", slog.Any("error", err),
				)
				return repository.AddAccountInstitutionRow{}, core.Public(
					core.ErrInternal, msgLinkFailed,
				)
			}
			return created, nil
		},
	)
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgInternalServer)
	}

	if ih.Publisher != nil {
		publishConnectionEventPayload(
			r.Context(), ih.Publisher, ih.Logger,
			"user.institution.connected", req.AccountID, req.InstitutionID,
		)
	}

	core.WriteJSON(w, http.StatusCreated, created)
	return nil
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
) error {
	conn, err := ih.DB.Acquire(r.Context())
	if err != nil {
		ih.Logger.Error("Error while processing request", slog.Any("error", err))
		return core.Public(core.ErrInternal, msgInternalServer)
	}
	defer conn.Release()

	q := r.URL.Query().Get("account_id")
	if q == "" {
		return core.Public(core.ErrInvalidInput, msgMissingQuery)
	}

	id, err := uuid.Parse(q)
	if err != nil {
		return core.Public(core.ErrInvalidInput, msgInvalidUUIDParam)
	}

	p := middleware.GetPagination(r.Context())
	insts, err := ih.svc(conn).ListForAccount(
		r.Context(), id, int32(p.Limit), int32(p.Offset),
	)
	if err != nil {
		ih.Logger.Error("Failed to list institutions", slog.Any("error", err))
		return core.Public(core.ErrInternal, msgFetchFailed)
	}

	core.WriteJSON(w, http.StatusOK, insts)
	return nil
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
) error {
	conn, err := ih.DB.Acquire(r.Context())
	if err != nil {
		ih.Logger.Error("Error while processing request", slog.Any("error", err))
		return core.Public(core.ErrInternal, msgInternalServer)
	}
	defer conn.Release()

	q := r.URL.Query().Get("institution_id")
	if q == "" {
		return core.Public(core.ErrInvalidInput, msgMissingQuery)
	}

	id, err := strconv.Atoi(q)
	if err != nil {
		return core.Public(core.ErrInvalidInput, msgInvalidInstIDParam)
	}

	p := middleware.GetPagination(r.Context())
	accounts, err := ih.svc(conn).ListAccountsForInstitution(
		r.Context(), int32(id), int32(p.Limit), int32(p.Offset),
	)
	if err != nil {
		ih.Logger.Error("Failed to list institutions", slog.Any("error", err))
		return core.Public(core.ErrInternal, msgFetchFailed)
	}

	core.WriteJSON(w, http.StatusOK, accounts)
	return nil
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
) error {
	var req repository.RemoveAccountInstitutionParams
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ih.Logger.Error("Failed to parse request body", slog.Any("error", err))
		return core.Public(core.ErrInvalidInput, msgInvalidBody)
	}

	claims, ok := middleware.ClaimsFromContext(r.Context())
	perms := middleware.PermissionsFromContext(r.Context())
	isAdmin := slices.Contains(perms, "manage:institutions:accounts:any")
	if !ok || (!isAdmin && req.AccountID.String() != claims.Subject) {
		return core.Public(core.ErrForbidden, msgOwnMembership)
	}

	if err := core.InTxDo(r.Context(), ih.DB, func(tx pgx.Tx) error {
		if err := ih.svc(tx).RemoveAccount(r.Context(), req); err != nil {
			// msgCreateFailed here reproduces a wording bug in the original
			// handler: the repo-call failure for this endpoint reused
			// RegisterInstitution's "failed to create institution" message
			// rather than one about removal. Kept exactly as it shipped.
			ih.Logger.Error(
				"Failed to create institution", slog.Any("error", err),
			)
			return core.Public(core.ErrInternal, msgCreateFailed)
		}
		return nil
	}); err != nil {
		return core.Fallback(err, core.ErrInternal, msgInternalServer)
	}

	if ih.Publisher != nil {
		publishConnectionEventPayload(
			r.Context(), ih.Publisher, ih.Logger,
			"user.institution.disconnected", req.AccountID, req.InstitutionID,
		)
	}

	core.WriteJSON(w, http.StatusOK, map[string]any{
		"message": "Successfully removed from institution",
	})
	return nil
}

// publishConnectionEventPayload builds and sends the raw broker.Publisher
// event both AddAcountInstitution and RemoveAccountInstitution emit. It is
// unchanged from before this extraction other than being a shared helper
// instead of two duplicated inline blocks: same exchange, same topic
// exchange type, same payload shape.
func publishConnectionEventPayload(
	ctx context.Context,
	pub *broker.Publisher,
	logger *slog.Logger,
	eventType string,
	accountID uuid.UUID,
	institutionID int32,
) {
	payload := map[string]any{
		"meta": map[string]any{
			"event_type":        eventType,
			"timestamp":         time.Now().UTC().Format(time.RFC3339),
			"source_service_id": "io.opencrafts.verisafe",
			"request_id":        uuid.New().String(),
		},
		"institution_connection": map[string]any{
			"account_id":     accountID,
			"institution_id": institutionID,
		},
	}
	if err := pub.Publish(
		ctx, "verisafe.events.topic", broker.TopicExchangeType, eventType, payload,
	); err != nil {
		logger.Error(
			"failed to publish institution connection event", "error", err,
		)
	}
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
) error {
	ctx := r.Context()
	conn, err := ih.DB.Acquire(ctx)
	if err != nil {
		ih.Logger.Error("DB connection missing", slog.Any("error", err))
		return core.Public(core.ErrInternal, msgInternalServer)
	}
	defer conn.Release()

	svc := ih.svc(conn)

	const batchSize = 500
	const workerCount = 10

	jobChan := make(chan repository.AccountInstitution, batchSize)
	var wg sync.WaitGroup

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for connection := range jobChan {
				if ih.Publisher == nil {
					continue
				}
				publishConnectionEventPayload(
					ctx, ih.Publisher, ih.Logger, "user.institution.connected",
					connection.AccountID, connection.InstitutionID,
				)
			}
		}()
	}

	go func() {
		defer close(jobChan)
		offset := 0
		for {
			connections, err := svc.ListConnectionsBatch(
				ctx, int32(batchSize), int32(offset),
			)
			if err != nil {
				ih.Logger.Error(
					"Batch fetch failed", "offset", offset, "error", err,
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

	core.WriteJSON(w, http.StatusOK, map[string]string{"status": "fanout complete"})
	return nil
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
