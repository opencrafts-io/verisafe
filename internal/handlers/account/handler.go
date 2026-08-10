package account

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/eventbus"
	"github.com/opencrafts-io/verisafe/internal/handlers/servicetoken"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	accountsvc "github.com/opencrafts-io/verisafe/internal/service/account"
)

type AccountHandler struct {
	Cacher       core.Cacher
	DB           core.IDBProvider
	Logger       *slog.Logger
	Cfg          *config.Config
	UserEventBus eventbus.UserPublisher

	// Service builds an account service bound to the caller's connection or
	// transaction. Left nil it falls back to the real implementation; see the
	// role handler for why this field is the testing seam, and the
	// institution handler for why the parameter is repository.DBTX rather
	// than pgx.Tx -- FanoutAccouts queries the acquired connection directly
	// with no transaction at all.
	Service func(repository.Querier) accountsvc.Service
}

func (ah *AccountHandler) svc(db repository.DBTX) accountsvc.Service {
	if ah.Service != nil {
		return ah.Service(repository.New(db))
	}
	return accountsvc.NewService(repository.New(db))
}

func (ah *AccountHandler) RegisterHandlers(router core.Router) {
	router.Handle("POST /accounts/bot/create",
		middleware.CreateStack(
			middleware.IsAuthenticated(ah.Cfg, ah.DB, ah.Cacher, ah.Logger),
			middleware.HasPermission([]string{"create:account:any"}),
		)(core.AppHandler(ah.CreateBotAccount)),
	)

	router.Handle("GET /accounts/fanout",
		middleware.CreateStack(
			middleware.IsAuthenticated(ah.Cfg, ah.DB, ah.Cacher, ah.Logger),
			middleware.HasPermission([]string{"create:account:any"}),
		)(core.AppHandler(ah.FanoutAccouts)),
	)

	router.Handle("GET /accounts/me",
		middleware.CreateStack(
			middleware.IsAuthenticated(ah.Cfg, ah.DB, ah.Cacher, ah.Logger),
			middleware.HasPermission([]string{"read:account:own"}),
		)(core.AppHandler(ah.GetPersonalAccount)),
	)

	router.Handle("GET /accounts/all",
		middleware.CreateStack(
			middleware.IsAuthenticated(ah.Cfg, ah.DB, ah.Cacher, ah.Logger),
			middleware.HasPermission([]string{"read:account:any"}),
		)(core.AppHandler(ah.GetAllUserAccounts)),
	)

	router.Handle("PATCH /accounts/me",
		middleware.CreateStack(
			middleware.IsAuthenticated(ah.Cfg, ah.DB, ah.Cacher, ah.Logger),
			middleware.HasPermission([]string{"update:account:own"}),
		)(core.AppHandler(ah.UpdatePersonalAccount)),
	)

	router.Handle("POST /accounts/deletion-request",
		middleware.CreateStack(
			middleware.IsAuthenticated(ah.Cfg, ah.DB, ah.Cacher, ah.Logger),
			middleware.HasPermission([]string{"update:account:own"}),
		)(core.AppHandler(ah.MarkAccountForDeletion)),
	)
	router.Handle("POST /accounts/recovery",
		middleware.CreateStack(
			middleware.IsAuthenticated(ah.Cfg, ah.DB, ah.Cacher, ah.Logger),
			middleware.HasPermission([]string{"update:account:own"}),
		)(core.AppHandler(ah.RecoverAccountFromDeletion)),
	)

	router.Handle("PATCH /accounts/me/phone",
		middleware.CreateStack(
			middleware.IsAuthenticated(ah.Cfg, ah.DB, ah.Cacher, ah.Logger),
			middleware.HasPermission([]string{"update:account:own"}),
		)(core.AppHandler(ah.VerifyPhone)),
	)

	router.Handle("GET /accounts/search/email",
		middleware.CreateStack(
			middleware.IsAuthenticated(ah.Cfg, ah.DB, ah.Cacher, ah.Logger),
			middleware.HasPermission([]string{"read:account:any"}),
			middleware.PaginationMiddleware(10, 100),
		)(core.AppHandler(ah.SearchAccountsByEmail)),
	)

	router.Handle("GET /accounts/search/name",
		middleware.CreateStack(
			middleware.IsAuthenticated(ah.Cfg, ah.DB, ah.Cacher, ah.Logger),
			middleware.HasPermission([]string{"read:account:any"}),
			middleware.PaginationMiddleware(10, 100),
		)(core.AppHandler(ah.SearchAccountsByName)),
	)

	router.Handle("GET /accounts/search/username",
		middleware.CreateStack(
			middleware.IsAuthenticated(ah.Cfg, ah.DB, ah.Cacher, ah.Logger),
			middleware.HasPermission([]string{"read:account:any"}),
			middleware.PaginationMiddleware(10, 100),
		)(core.AppHandler(ah.SearchAccountsByUsername)),
	)
}

// BotAccountRequest represents the request to create a bot account with enhanced service token
type BotAccountRequest struct {
	Account struct {
		Email     string  `json:"email" validate:"required,email"`
		Name      string  `json:"name" validate:"required,min=1,max=100"`
		AvatarUrl *string `json:"avatar_url"`
	} `json:"account"`
	ServiceToken struct {
		Name             string                       `json:"name" validate:"required,min=1,max=100"`
		Description      *string                      `json:"description"`
		ExpiresInDays    *int                         `json:"expires_in_days" validate:"omitempty,min=1,max=3650"`
		Scopes           []string                     `json:"scopes"`
		MaxUses          *int                         `json:"max_uses" validate:"omitempty,min=1"`
		RotationPolicy   *servicetoken.RotationPolicy `json:"rotation_policy"`
		IPWhitelist      []string                     `json:"ip_whitelist"`
		UserAgentPattern *string                      `json:"user_agent_pattern"`
		Metadata         map[string]any               `json:"metadata"`
	} `json:"service_token"`
}

// BotAccountResponse represents the response for bot account creation with token
type BotAccountResponse struct {
	Account struct {
		ID        uuid.UUID `json:"id"`
		Email     string    `json:"email"`
		Name      string    `json:"name"`
		Type      string    `json:"type"`
		CreatedAt time.Time `json:"created_at"`
	} `json:"account"`
	ServiceToken struct {
		ID          uuid.UUID      `json:"id"`
		Name        string         `json:"name"`
		Description *string        `json:"description"`
		Token       string         `json:"token"`
		ExpiresAt   *time.Time     `json:"expires_at"`
		Scopes      []string       `json:"scopes"`
		MaxUses     *int           `json:"max_uses"`
		CreatedAt   time.Time      `json:"created_at"`
		Metadata    map[string]any `json:"metadata"`
	} `json:"service_token"`
}

// generateSecureToken generates a cryptographically secure token
func (ah *AccountHandler) generateSecureToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := "vst_" + base64.URLEncoding.EncodeToString(bytes)
	return token, nil
}

// CreateBotAccount godoc
//
// @Summary      Create a bot account with a service token
// @Description  Creates a service (bot) account, assigns it the "bot" role, and issues a service token for it in one call.
// @Tags         accounts
// @Accept       json
// @Produce      json
// @Param        request  body      account.BotAccountRequest  true  "Bot account and service token details"
// @Success      201  {object}  account.BotAccountResponse
// @Failure      400  {object}  core.APIError  "Invalid request body, or missing required fields"
// @Failure      500  {object}  core.APIError  "Failed to create account, assign role, or issue service token"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /accounts/bot/create [post]
func (ah *AccountHandler) CreateBotAccount(
	w http.ResponseWriter,
	r *http.Request,
) error {
	var req BotAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ah.Logger.Error("Failed to parse request body", slog.String("error", err.Error()))
		return core.Public(core.ErrInvalidInput, msgInvalidBody)
	}
	if req.Account.Email == "" || req.Account.Name == "" {
		return core.Public(core.ErrInvalidInput, msgEmailNameRequired)
	}
	if req.ServiceToken.Name == "" {
		return core.Public(core.ErrInvalidInput, msgTokenNameRequired)
	}

	type result struct {
		account      repository.Account
		serviceToken repository.ServiceToken
		rawToken     string
	}

	res, err := core.InTx(r.Context(), ah.DB, func(tx pgx.Tx) (result, error) {
		svc := ah.svc(tx)

		created, err := svc.Create(r.Context(), repository.CreateAccountParams{
			Email:     req.Account.Email,
			Name:      req.Account.Name,
			Type:      repository.AccountTypeBot,
			AvatarUrl: req.Account.AvatarUrl,
		})
		if err != nil {
			ah.Logger.Error("Failed to create account", slog.Any("error", err))
			return result{}, core.Public(core.ErrInternal, msgAccountCreateFailed)
		}

		role, err := svc.GetBotRole(r.Context())
		if err != nil {
			ah.Logger.Error("Failed to retrieve bot role", slog.Any("error", err))
			return result{}, core.Public(core.ErrInternal, msgRoleLookupFailed)
		}

		if err := svc.AssignRole(r.Context(), created.ID, role.ID); err != nil {
			ah.Logger.Error("Failed to assign role", slog.Any("error", err))
			return result{}, core.Public(core.ErrInternal, msgRoleAssignFailed)
		}

		token, err := ah.generateSecureToken()
		if err != nil {
			ah.Logger.Error(
				"Failed to generate secure token", slog.String("error", err.Error()),
			)
			return result{}, core.Public(core.ErrInternal, msgTokenGenFailed)
		}

		var expiresAt *time.Time
		if req.ServiceToken.ExpiresInDays != nil {
			expiry := time.Now().AddDate(0, 0, *req.ServiceToken.ExpiresInDays)
			expiresAt = &expiry
		} else {
			expiry := time.Now().AddDate(1, 0, 0)
			expiresAt = &expiry
		}

		var rotationPolicyJSON []byte
		if req.ServiceToken.RotationPolicy != nil {
			rotationPolicyJSON, err = json.Marshal(req.ServiceToken.RotationPolicy)
			if err != nil {
				ah.Logger.Error(
					"Failed to marshal rotation policy", slog.String("error", err.Error()),
				)
				return result{}, core.Public(core.ErrInvalidInput, msgInvalidRotation)
			}
		}

		var metadataJSON []byte
		if req.ServiceToken.Metadata != nil {
			metadataJSON, err = json.Marshal(req.ServiceToken.Metadata)
			if err != nil {
				ah.Logger.Error(
					"Failed to marshal metadata", slog.String("error", err.Error()),
				)
				return result{}, core.Public(core.ErrInvalidInput, msgInvalidMetadata)
			}
		}

		serviceToken, err := svc.CreateServiceToken(r.Context(), token, repository.CreateServiceTokenParams{
			AccountID:   created.ID,
			Name:        req.ServiceToken.Name,
			Description: req.ServiceToken.Description,
			// TokenHash is set by the service, which hashes rawToken -- this
			// literal value is ignored. Before this migration, the handler
			// passed the raw token straight through as TokenHash with no
			// hashing at all, so every bot account created through this
			// endpoint received a service token that could never
			// authenticate (the X-API-Key check hashes the presented key and
			// looks up by that hash). This was fixed as part of the
			// extraction rather than reproduced; see ADR 0009.
			ExpiresAt: expiresAt,
			Scopes:    req.ServiceToken.Scopes,
			MaxUses: func() *int32 {
				if req.ServiceToken.MaxUses == nil {
					return nil
				}
				val := int32(*req.ServiceToken.MaxUses)
				return &val
			}(),
			RotationPolicy:   rotationPolicyJSON,
			IpWhitelist:      req.ServiceToken.IPWhitelist,
			UserAgentPattern: req.ServiceToken.UserAgentPattern,
			CreatedBy:        pgtype.UUID{Bytes: created.ID, Valid: true},
			Metadata:         metadataJSON,
		})
		if err != nil {
			ah.Logger.Error(
				"Failed to create service token", slog.String("error", err.Error()),
			)
			return result{}, core.Public(core.ErrInternal, msgServiceTokenFailed)
		}

		return result{account: created, serviceToken: serviceToken, rawToken: token}, nil
	})
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	response := BotAccountResponse{}
	response.Account.ID = res.account.ID
	response.Account.Email = res.account.Email
	response.Account.Name = res.account.Name
	response.Account.Type = string(res.account.Type)
	response.Account.CreatedAt = res.account.CreatedAt.Time

	response.ServiceToken.ID = res.serviceToken.ID
	response.ServiceToken.Name = res.serviceToken.Name
	response.ServiceToken.Description = res.serviceToken.Description
	response.ServiceToken.Token = res.rawToken
	response.ServiceToken.ExpiresAt = res.serviceToken.ExpiresAt
	response.ServiceToken.Scopes = res.serviceToken.Scopes
	response.ServiceToken.MaxUses = func() *int {
		if res.serviceToken.MaxUses == nil {
			return nil
		}
		val := int(*res.serviceToken.MaxUses)
		return &val
	}()
	response.ServiceToken.CreatedAt = res.serviceToken.CreatedAt.Time
	if res.serviceToken.Metadata != nil {
		json.Unmarshal(res.serviceToken.Metadata, &response.ServiceToken.Metadata)
	}

	core.WriteJSON(w, http.StatusCreated, response)
	return nil
}

// FanoutAccouts godoc
//
// @Summary      Republish every account to the event bus
// @Description  Batches through all accounts and republishes a UserCreated event for each, via a worker pool. Intended for backfilling downstream consumers, not routine use.
// @Tags         accounts
// @Produce      json
// @Success      200  {object}  map[string]any  "Publish count message"
// @Failure      500  {object}  core.APIError  "Failed to read accounts or publish one or more batches"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /accounts/fanout [get]
func (ah *AccountHandler) FanoutAccouts(
	w http.ResponseWriter,
	r *http.Request,
) error {
	conn, err := ah.DB.Acquire(r.Context())
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		return core.Public(core.ErrInternal, msgGeneric)
	}
	defer conn.Release()

	svc := ah.svc(conn)

	userCount, err := svc.Count(r.Context())
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		return core.Public(core.ErrInternal, msgFanoutServiceUnavailable)
	}

	batchSize := 1000
	totalBatches := (int(userCount) + batchSize - 1) / batchSize
	publishedCount := 0

	workerCount := 5
	semaphore := make(chan struct{}, workerCount)
	var wg sync.WaitGroup
	errChan := make(chan error, totalBatches)

	for batch := range totalBatches {
		wg.Add(1)
		semaphore <- struct{}{}

		go func(batchNum int) {
			defer wg.Done()
			defer func() { <-semaphore }()

			offset := batchNum * batchSize
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()

			users, err := svc.ListBatch(ctx, int32(batchSize), int32(offset))
			if err != nil {
				ah.Logger.Error("Error fetching batch",
					slog.Any("error", err), slog.Int("batch", batchNum),
				)
				errChan <- fmt.Errorf("batch %d: %w", batchNum, err)
				return
			}

			for _, user := range users {
				if err := ah.UserEventBus.PublishUserCreated(ctx, user, user.ID.String()); err != nil {
					ah.Logger.Error("Error publishing user",
						slog.Any("error", err), slog.String("user_id", user.ID.String()),
					)
					errChan <- err
					return
				}
				publishedCount++
			}
		}(batch)
	}

	wg.Wait()
	close(errChan)

	if len(errChan) > 0 {
		return core.Public(core.ErrInternal, msgSomeBatchesFailed)
	}

	core.WriteJSON(w, http.StatusOK, map[string]any{
		"message": fmt.Sprintf("Published %d users to the event bus", publishedCount),
	})
	return nil
}

// GetPersonalAccount godoc
//
// @Summary      Get the authenticated user's own account
// @Description  Returns the account belonging to the caller's own JWT/API-key subject.
// @Tags         accounts
// @Produce      json
// @Success      200  {object}  repository.Account
// @Failure      401  {object}  core.APIError  "Missing or invalid claims"
// @Failure      500  {object}  core.APIError  "Failed to fetch account"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /accounts/me [get]
func (ah *AccountHandler) GetPersonalAccount(
	w http.ResponseWriter,
	r *http.Request,
) error {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return core.Public(core.ErrUnauthorized, msgAuthRequired)
	}

	user, err := core.InTx(r.Context(), ah.DB, func(tx pgx.Tx) (repository.Account, error) {
		id, err := uuid.Parse(claims.Subject)
		if err != nil {
			ah.Logger.Error("Error while parsing user id", slog.Any("error", err))
			return repository.Account{}, core.Public(core.ErrInternal, msgFetchAccountFailed)
		}

		user, err := ah.svc(tx).GetByID(r.Context(), id)
		if errors.Is(err, core.ErrNotFound) {
			ah.Logger.Error("Error while processing request", slog.Any("error", err))
			return repository.Account{}, core.Public(core.ErrInternal, msgWrongFlavor)
		}
		if err != nil {
			ah.Logger.Error("Error while processing request", slog.Any("error", err))
			return repository.Account{}, core.Public(core.ErrInternal, msgFetchAccountFailed)
		}
		return user, nil
	})
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusOK, user)
	return nil
}

// UpdatePersonalAccount godoc
//
// @Summary      Update the authenticated user's own account details
// @Description  The request body's id must match the caller's own subject — updating a different account's id is rejected.
// @Tags         accounts
// @Accept       json
// @Produce      json
// @Param        request  body      repository.UpdateAccountDetailsParams  true  "Account fields to update; id must be the caller's own"
// @Success      200  {object}  repository.Account
// @Failure      400  {object}  core.APIError  "Invalid request body"
// @Failure      401  {object}  core.APIError  "Missing or invalid claims"
// @Failure      500  {object}  core.APIError  "Not the account owner, or failed to update/fetch account"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /accounts/me [patch]
func (ah *AccountHandler) UpdatePersonalAccount(
	w http.ResponseWriter,
	r *http.Request,
) error {
	var accData repository.UpdateAccountDetailsParams
	if err := json.NewDecoder(r.Body).Decode(&accData); err != nil || accData.Name == "" {
		ah.Logger.Error("Failed to parse request body", slog.Any("error", err))
		return core.Public(core.ErrInvalidInput, msgCheckBody)
	}

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return core.Public(core.ErrUnauthorized, msgAuthRequired)
	}

	// Preserved exactly as it shipped: this is a 500, not a 403, despite
	// being an ownership check.
	if accData.ID.String() != claims.Subject {
		ah.Logger.Error("Attempting to update wrong account")
		return core.Public(core.ErrInternal, msgOwnershipViolation)
	}

	updated, err := core.InTx(r.Context(), ah.DB, func(tx pgx.Tx) (repository.Account, error) {
		svc := ah.svc(tx)

		if err := svc.Update(r.Context(), accData); err != nil {
			ah.Logger.Error("Error while processing request", slog.Any("error", err))
			return repository.Account{}, core.Public(core.ErrInternal, msgUpdateAccountFailed)
		}

		updated, err := svc.GetByID(r.Context(), accData.ID)
		if err != nil {
			ah.Logger.Error("Error while processing request", slog.Any("error", err))
			return repository.Account{}, core.Public(core.ErrInternal, msgFetchAccountFailed)
		}
		return updated, nil
	})
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	go ah.publishUserUpdated(updated)

	core.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// VerifyPhone godoc
//
// @Summary      Update the authenticated user's own phone number
// @Description  The request body's id must match the caller's own subject. Note: this only updates the stored phone number today — it does not yet verify it (see TODO below).
// @Tags         accounts
// @Accept       json
// @Produce      json
// @Param        request  body      repository.UpdateAccountPhoneNumberParams  true  "New phone number; id must be the caller's own"
// @Success      200  {object}  repository.Account
// @Failure      400  {object}  core.APIError  "Invalid request body or phone number too short"
// @Failure      401  {object}  core.APIError  "Missing or invalid claims"
// @Failure      500  {object}  core.APIError  "Not the account owner, or failed to update/fetch account"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /accounts/me/phone [patch]
//
// TODO: implement verifying mechanisms
// Use a provider such as AT or One Signal etc
func (ah *AccountHandler) VerifyPhone(w http.ResponseWriter, r *http.Request) error {
	var accData repository.UpdateAccountPhoneNumberParams
	if err := json.NewDecoder(r.Body).Decode(&accData); err != nil || len(accData.Phone) < 5 {
		ah.Logger.Error("Failed to parse request body", slog.Any("error", err))
		return core.Public(core.ErrInvalidInput, msgCheckBody)
	}

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return core.Public(core.ErrUnauthorized, msgAuthRequired)
	}

	// Preserved exactly as it shipped: this is a 500, not a 403.
	if accData.ID.String() != claims.Subject {
		ah.Logger.Error("Attempting to update wrong account")
		return core.Public(core.ErrInternal, msgOwnershipViolation)
	}

	updated, err := core.InTx(r.Context(), ah.DB, func(tx pgx.Tx) (repository.Account, error) {
		svc := ah.svc(tx)

		if err := svc.UpdatePhone(r.Context(), accData); err != nil {
			ah.Logger.Error("Error while processing request", slog.Any("error", err))
			return repository.Account{}, core.Public(core.ErrInternal, msgUpdateAccountFailed)
		}

		updated, err := svc.GetByID(r.Context(), accData.ID)
		if err != nil {
			ah.Logger.Error("Error while processing request", slog.Any("error", err))
			return repository.Account{}, core.Public(core.ErrInternal, msgFetchAccountFailed)
		}
		return updated, nil
	})
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	go ah.publishUserUpdated(updated)

	core.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// publishUserUpdated is the fire-and-forget notification both
// UpdatePersonalAccount and VerifyPhone spawn after a successful commit.
// Extracted to one place; the goroutine, the 10-second timeout and the
// lack of any completion signal are all unchanged from before this
// extraction.
func (ah *AccountHandler) publishUserUpdated(updated repository.Account) {
	eventRequestID := eventbus.GenerateRequestID()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := ah.UserEventBus.PublishUserUpdated(ctx, updated, eventRequestID); err != nil {
		ah.Logger.Error("Failed to publish user updated event",
			slog.Any("event_id", eventRequestID),
			slog.Any("event_data", updated),
			slog.Any("error", err),
		)
	}
}

// SearchAccountsByEmail godoc
//
// @Summary      Search accounts by email
// @Description  Case-sensitivity and match semantics follow the underlying SQL query.
// @Tags         accounts
// @Produce      json
// @Param        q       query  string  true   "Email search query"
// @Param        limit   query  int     false  "Page size (default 10, max 100)"
// @Param        offset  query  int     false  "Page offset (default 0)"
// @Success      200  {object}  map[string]any  "accounts, pagination, query, search_type"
// @Failure      400  {object}  core.APIError  "Missing query parameter 'q'"
// @Failure      500  {object}  core.APIError  "Search failed"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /accounts/search/email [get]
func (ah *AccountHandler) SearchAccountsByEmail(
	w http.ResponseWriter,
	r *http.Request,
) error {
	query := r.URL.Query().Get("q")
	if query == "" {
		return core.Public(core.ErrInvalidInput, msgSearchQueryRequired)
	}
	pagination := middleware.GetPagination(r.Context())

	accounts, err := core.InTx(r.Context(), ah.DB, func(tx pgx.Tx) ([]repository.Account, error) {
		accounts, err := ah.svc(tx).SearchByEmail(
			r.Context(), query, int32(pagination.Limit), int32(pagination.Offset),
		)
		if err != nil {
			ah.Logger.Error("Failed to search accounts by email", slog.Any("error", err))
			return nil, core.Public(core.ErrInternal, msgSearchFailed)
		}
		return accounts, nil
	})
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusOK, map[string]any{
		"accounts": accounts,
		"pagination": map[string]any{
			"limit":  pagination.Limit,
			"offset": pagination.Offset,
			"total":  len(accounts),
		},
		"query":       query,
		"search_type": "email",
	})
	return nil
}

// SearchAccountsByName godoc
//
// @Summary      Search accounts by name
// @Description  Returns accounts whose name matches the query, paginated with
// @Description  limit/offset. Matching is on the account's display name; use
// @Description  the username or email search endpoints for those fields.
// @Tags         accounts
// @Produce      json
// @Param        q       query  string  true   "Name search query"
// @Param        limit   query  int     false  "Page size (default 10, max 100)"
// @Param        offset  query  int     false  "Page offset (default 0)"
// @Success      200  {object}  map[string]any  "accounts, pagination, query, search_type"
// @Failure      400  {object}  core.APIError  "Missing query parameter 'q'"
// @Failure      500  {object}  core.APIError  "Search failed"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /accounts/search/name [get]
func (ah *AccountHandler) SearchAccountsByName(
	w http.ResponseWriter,
	r *http.Request,
) error {
	query := r.URL.Query().Get("q")
	if query == "" {
		return core.Public(core.ErrInvalidInput, msgSearchQueryRequired)
	}
	pagination := middleware.GetPagination(r.Context())

	accounts, err := core.InTx(r.Context(), ah.DB, func(tx pgx.Tx) ([]repository.Account, error) {
		accounts, err := ah.svc(tx).SearchByName(
			r.Context(), query, int32(pagination.Limit), int32(pagination.Offset),
		)
		if err != nil {
			ah.Logger.Error("Failed to search accounts by name", slog.Any("error", err))
			return nil, core.Public(core.ErrInternal, msgSearchFailed)
		}
		return accounts, nil
	})
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusOK, map[string]any{
		"accounts": accounts,
		"pagination": map[string]any{
			"limit":  pagination.Limit,
			"offset": pagination.Offset,
			"total":  len(accounts),
		},
		"query":       query,
		"search_type": "name",
	})
	return nil
}

// GetAllUserAccounts godoc
//
// @Summary      List all accounts
// @Description  Note: this route is not wired with pagination middleware, so limit/offset query params are currently ignored — it always returns the first page (10 accounts).
// @Tags         accounts
// @Produce      json
// @Success      200  {array}   repository.Account
// @Failure      500  {object}  core.APIError  "Failed to fetch accounts"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /accounts/all [get]
func (ah *AccountHandler) GetAllUserAccounts(
	w http.ResponseWriter,
	r *http.Request,
) error {
	pagination := middleware.GetPagination(r.Context())

	accounts, err := core.InTx(r.Context(), ah.DB, func(tx pgx.Tx) ([]repository.Account, error) {
		accounts, err := ah.svc(tx).List(
			r.Context(), int32(pagination.Limit), int32(pagination.Offset),
		)
		if err != nil {
			ah.Logger.Error("Failed to get all accounts", slog.Any("error", err))
			return nil, core.Public(core.ErrInternal, msgSearchFailed)
		}
		return accounts, nil
	})
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusOK, accounts)
	return nil
}

// SearchAccountsByUsername godoc
//
// @Summary      Search accounts by username
// @Description  Returns accounts whose username matches the query, paginated
// @Description  with limit/offset. Usernames are unique, so a full-value query
// @Description  yields at most one result.
// @Tags         accounts
// @Produce      json
// @Param        q       query  string  true   "Username search query"
// @Param        limit   query  int     false  "Page size (default 10, max 100)"
// @Param        offset  query  int     false  "Page offset (default 0)"
// @Success      200  {object}  map[string]any  "accounts, pagination, query, search_type"
// @Failure      400  {object}  core.APIError  "Missing query parameter 'q'"
// @Failure      500  {object}  core.APIError  "Search failed"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /accounts/search/username [get]
func (ah *AccountHandler) SearchAccountsByUsername(
	w http.ResponseWriter,
	r *http.Request,
) error {
	query := r.URL.Query().Get("q")
	if query == "" {
		return core.Public(core.ErrInvalidInput, msgSearchQueryRequired)
	}
	pagination := middleware.GetPagination(r.Context())

	accounts, err := core.InTx(r.Context(), ah.DB, func(tx pgx.Tx) ([]repository.Account, error) {
		accounts, err := ah.svc(tx).SearchByUsername(
			r.Context(), query, int32(pagination.Limit), int32(pagination.Offset),
		)
		if err != nil {
			ah.Logger.Error("Failed to search accounts by username", slog.Any("error", err))
			return nil, core.Public(core.ErrInternal, msgSearchFailed)
		}
		return accounts, nil
	})
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusOK, map[string]any{
		"accounts": accounts,
		"pagination": map[string]any{
			"limit":  pagination.Limit,
			"offset": pagination.Offset,
			"total":  len(accounts),
		},
		"query":       query,
		"search_type": "username",
	})
	return nil
}

// MarkAccountForDeletion godoc
//
// @Summary      Request deletion of the authenticated user's own account
// @Description  The account is soft-deleted and permanently removed after a 14-day grace period; signing in again before then cancels the deletion.
// @Tags         accounts
// @Produce      json
// @Success      200  {object}  map[string]any  "Confirmation message"
// @Failure      401  {object}  core.APIError  "Missing or invalid claims"
// @Failure      500  {object}  core.APIError  "Failed to mark account for deletion"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /accounts/deletion-request [post]
func (ah *AccountHandler) MarkAccountForDeletion(
	w http.ResponseWriter,
	r *http.Request,
) error {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return core.Public(core.ErrUnauthorized, msgAuthRequired)
	}

	// Begin gets its own message here, distinct from Acquire and Commit
	// (which share msgGeneric) -- the one grouping in this file that core.InTx
	// cannot express in a single Fallback call, so Acquire and the
	// transaction are handled separately rather than through core.InTx.
	conn, err := ah.DB.Acquire(r.Context())
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		return core.Public(core.ErrInternal, msgGeneric)
	}
	defer conn.Release()

	tx, err := conn.Begin(r.Context())
	if err != nil {
		ah.Logger.Error("Error attempting to prepare transaction", slog.Any("error", err))
		return core.Public(core.ErrInternal, msgDeletionBeginFailed)
	}

	txErr := func() error {
		id, err := uuid.Parse(claims.Subject)
		if err != nil {
			ah.Logger.Error("Error while parsing user id", slog.Any("error", err))
			return core.Public(core.ErrInternal, msgDeletionBeginFailed)
		}

		if err := ah.svc(tx).MarkForDeletion(r.Context(), id); err != nil {
			ah.Logger.Error(
				"Error while attempting to mark account for deletion", slog.Any("error", err),
			)
			return core.Public(core.ErrInternal, msgDeletionFailed)
		}
		return nil
	}()
	if txErr != nil {
		tx.Rollback(r.Context())
		return txErr
	}

	if err := tx.Commit(r.Context()); err != nil {
		ah.Logger.Error("Error while committing transaction", slog.Any("error", err))
		return core.Public(core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusOK, map[string]any{
		"message": "Your account will be permanently deleted after 14 days. You may cancel this request by signing in before that time.",
	})
	return nil
}

// RecoverAccountFromDeletion godoc
//
// @Summary      Cancel a pending deletion of the authenticated user's own account
// @Description  Restores full access to an account that was previously marked for deletion, provided it's still within the 14-day grace period.
// @Tags         accounts
// @Produce      json
// @Success      200  {object}  map[string]any  "Confirmation message"
// @Failure      401  {object}  core.APIError  "Missing or invalid claims"
// @Failure      500  {object}  core.APIError  "Failed to recover account"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /accounts/recovery [post]
func (ah *AccountHandler) RecoverAccountFromDeletion(
	w http.ResponseWriter,
	r *http.Request,
) error {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return core.Public(core.ErrUnauthorized, msgAuthRequired)
	}

	// See MarkAccountForDeletion for why Acquire and the transaction are
	// handled separately here rather than through core.InTx.
	conn, err := ah.DB.Acquire(r.Context())
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		return core.Public(core.ErrInternal, msgGeneric)
	}
	defer conn.Release()

	tx, err := conn.Begin(r.Context())
	if err != nil {
		ah.Logger.Error("Error attempting to prepare transaction", slog.Any("error", err))
		return core.Public(core.ErrInternal, msgRecoveryBeginFailed)
	}

	txErr := func() error {
		id, err := uuid.Parse(claims.Subject)
		if err != nil {
			ah.Logger.Error("Error while parsing user id", slog.Any("error", err))
			return core.Public(core.ErrInternal, msgRecoveryBeginFailed)
		}

		if err := ah.svc(tx).MarkForRecovery(r.Context(), id); err != nil {
			ah.Logger.Error(
				"Error while attempting to recover account from deletion", slog.Any("error", err),
			)
			return core.Public(core.ErrInternal, msgRecoveryFailed)
		}
		return nil
	}()
	if txErr != nil {
		tx.Rollback(r.Context())
		return txErr
	}

	if err := tx.Commit(r.Context()); err != nil {
		ah.Logger.Error("Error while committing transaction", slog.Any("error", err))
		return core.Public(core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusOK, map[string]any{
		"message": "Account recovery was successful. All access has been restored",
	})
	return nil
}
