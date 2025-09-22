package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	"github.com/opencrafts-io/verisafe/internal/utils"
)

type AccountHandler struct {
	Logger *slog.Logger
	Cfg    *config.Config
}

func (ah *AccountHandler) RegisterHandlers(router *http.ServeMux) {
	router.Handle("POST /accounts/bot/create",
		middleware.CreateStack(
			middleware.IsAuthenticated(ah.Cfg, ah.Logger),
			middleware.HasPermission([]string{"create:account:any"}),
		)(http.HandlerFunc(ah.CreateBotAccount)),
	)

	router.Handle("GET /accounts/me",
		middleware.CreateStack(
			middleware.IsAuthenticated(ah.Cfg, ah.Logger),
			middleware.HasPermission([]string{"read:account:own"}),
		)(http.HandlerFunc(ah.GetPersonalAccount)),
	)

	router.Handle("GET /accounts/all",
		middleware.CreateStack(
			middleware.IsAuthenticated(ah.Cfg, ah.Logger),
			middleware.HasPermission([]string{"read:account:any"}),
		)(http.HandlerFunc(ah.GetAllUserAccounts)),
	)

	router.Handle("PATCH /accounts/me",
		middleware.CreateStack(
			middleware.IsAuthenticated(ah.Cfg, ah.Logger),
			middleware.HasPermission([]string{"update:account:own"}),
		)(http.HandlerFunc(ah.UpdatePersonalAccount)),
	)

	router.Handle("PATCH /accounts/me/phone",
		middleware.CreateStack(
			middleware.IsAuthenticated(ah.Cfg, ah.Logger),
			middleware.HasPermission([]string{"update:account:own"}),
		)(http.HandlerFunc(ah.VerifyPhone)),
	)

	router.Handle("GET /accounts/search/email",
		middleware.CreateStack(
			middleware.IsAuthenticated(ah.Cfg, ah.Logger),
			middleware.HasPermission([]string{"read:account:any"}),
			middleware.PaginationMiddleware(10, 100),
		)(http.HandlerFunc(ah.SearchAccountsByEmail)),
	)

	router.Handle("GET /accounts/search/name",
		middleware.CreateStack(
			middleware.IsAuthenticated(ah.Cfg, ah.Logger),
			middleware.HasPermission([]string{"read:account:any"}),
			middleware.PaginationMiddleware(10, 100),
		)(http.HandlerFunc(ah.SearchAccountsByName)),
	)

	router.Handle("GET /accounts/search/username",
		middleware.CreateStack(
			middleware.IsAuthenticated(ah.Cfg, ah.Logger),
			middleware.HasPermission([]string{"read:account:any"}),
			middleware.PaginationMiddleware(10, 100),
		)(http.HandlerFunc(ah.SearchAccountsByUsername)),
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
		Name             string                 `json:"name" validate:"required,min=1,max=100"`
		Description      *string                `json:"description"`
		ExpiresInDays    *int                   `json:"expires_in_days" validate:"omitempty,min=1,max=3650"`
		Scopes           []string               `json:"scopes"`
		MaxUses          *int                   `json:"max_uses" validate:"omitempty,min=1"`
		RotationPolicy   *RotationPolicy        `json:"rotation_policy"`
		IPWhitelist      []string               `json:"ip_whitelist"`
		UserAgentPattern *string                `json:"user_agent_pattern"`
		Metadata         map[string]interface{} `json:"metadata"`
	} `json:"service_token"`
}

// RotationPolicy defines token rotation behavior
type RotationPolicy struct {
	AutoRotate           bool `json:"auto_rotate"`
	RotationIntervalDays int  `json:"rotation_interval_days" validate:"omitempty,min=1,max=365"`
	NotifyBeforeDays     int  `json:"notify_before_days" validate:"omitempty,min=1,max=30"`
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
		ID          uuid.UUID              `json:"id"`
		Name        string                 `json:"name"`
		Description *string                `json:"description"`
		Token       string                 `json:"token"`
		ExpiresAt   *time.Time             `json:"expires_at"`
		Scopes      []string               `json:"scopes"`
		MaxUses     *int                   `json:"max_uses"`
		CreatedAt   time.Time              `json:"created_at"`
		Metadata    map[string]interface{} `json:"metadata"`
	} `json:"service_token"`
}

// Creates a bot account with an enhanced service token
func (ah *AccountHandler) CreateBotAccount(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Parse request
	var req BotAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ah.Logger.Error("Failed to parse request body", slog.String("error", err.Error()))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid request body",
		})
		return
	}

	// Validate request
	if req.Account.Email == "" || req.Account.Name == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Email and name are required",
		})
		return
	}

	if req.ServiceToken.Name == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Service token name is required",
		})
		return
	}

	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	tx, err := conn.Begin(r.Context())
	if err != nil {
		ah.Logger.Error("Error while beginning transaction", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}
	defer tx.Rollback(r.Context())

	repo := repository.New(tx)

	// Create bot account
	accData := repository.CreateAccountParams{
		Email:     req.Account.Email,
		Name:      req.Account.Name,
		Type:      repository.AccountTypeBot,
		AvatarUrl: req.Account.AvatarUrl,
	}

	created, err := repo.CreateAccount(r.Context(), accData)
	if err != nil {
		ah.Logger.Error("Failed to create account",
			slog.Any("error", err),
			slog.Any("account", accData),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't create this account at the moment please try again later",
		})
		return
	}

	// Assign bot role
	role, err := repo.GetRoleByName(r.Context(), "bot")
	if err != nil {
		ah.Logger.Error("Failed to retrieve bot role",
			slog.Any("error", err),
			slog.Any("account", accData),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't found a suitable role to assign to your bot",
		})
		return
	}

	if _, err := repo.AssignRole(r.Context(), repository.AssignRoleParams{
		UserID: created.ID, RoleID: role.ID,
	}); err != nil {
		ah.Logger.Error("Failed to assign role",
			slog.Any("error", err),
			slog.Any("account", accData),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't assign default bot role",
		})
		return
	}

	// Generate secure service token
	token, err := ah.generateSecureToken()
	if err != nil {
		ah.Logger.Error("Failed to generate secure token", slog.String("error", err.Error()))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Failed to generate service token",
		})
		return
	}

	// Calculate expiry
	var expiresAt *time.Time
	if req.ServiceToken.ExpiresInDays != nil {
		expiry := time.Now().AddDate(0, 0, *req.ServiceToken.ExpiresInDays)
		expiresAt = &expiry
	} else {
		// Default to 1 year
		expiry := time.Now().AddDate(1, 0, 0)
		expiresAt = &expiry
	}

	// Prepare rotation policy
	var rotationPolicyJSON []byte
	if req.ServiceToken.RotationPolicy != nil {
		rotationPolicyJSON, err = json.Marshal(req.ServiceToken.RotationPolicy)
		if err != nil {
			ah.Logger.Error("Failed to marshal rotation policy", slog.String("error", err.Error()))
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Invalid rotation policy",
			})
			return
		}
	}

	// Prepare metadata
	var metadataJSON []byte
	if req.ServiceToken.Metadata != nil {
		metadataJSON, err = json.Marshal(req.ServiceToken.Metadata)
		if err != nil {
			ah.Logger.Error("Failed to marshal metadata", slog.String("error", err.Error()))
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Invalid metadata",
			})
			return
		}
	}

	// Create service token
	serviceToken, err := repo.CreateServiceToken(r.Context(), repository.CreateServiceTokenParams{
		AccountID:   created.ID,
		Name:        req.ServiceToken.Name,
		Description: req.ServiceToken.Description,
		TokenHash:   utils.HashToken(token),
		ExpiresAt:   expiresAt,
		Scopes:      req.ServiceToken.Scopes,
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
		ah.Logger.Error("Failed to create service token", slog.String("error", err.Error()))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Failed to create service token",
		})
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		ah.Logger.Error("Error while committing transaction", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	// Prepare response
	response := BotAccountResponse{}

	// Account info
	response.Account.ID = created.ID
	response.Account.Email = created.Email
	response.Account.Name = created.Name
	response.Account.Type = string(created.Type)
	response.Account.CreatedAt = created.CreatedAt.Time

	// Service token info
	response.ServiceToken.ID = serviceToken.ID
	response.ServiceToken.Name = serviceToken.Name
	response.ServiceToken.Description = serviceToken.Description
	response.ServiceToken.Token = token
	response.ServiceToken.ExpiresAt = serviceToken.ExpiresAt
	response.ServiceToken.Scopes = serviceToken.Scopes
	response.ServiceToken.MaxUses = func() *int {
		if serviceToken.MaxUses == nil {
			return nil
		}
		val := int(*serviceToken.MaxUses)
		return &val
	}()
	response.ServiceToken.CreatedAt = serviceToken.CreatedAt.Time

	if serviceToken.Metadata != nil {
		json.Unmarshal(serviceToken.Metadata, &response.ServiceToken.Metadata)
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// generateSecureToken generates a cryptographically secure token
func (ah *AccountHandler) generateSecureToken() (string, error) {
	// Generate 32 bytes of random data
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	// Encode as base64 and add prefix for identification
	token := "vst_" + base64.URLEncoding.EncodeToString(bytes)
	return token, nil
}

func (ah *AccountHandler) GetPersonalAccount(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(middleware.AuthUserClaims).(*utils.VerisafeClaims)
	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		ah.Logger.Error("Error while parsing user id", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into an error while trying to fetch your account",
		})
		return
	}

	user, err := repo.GetAccountByID(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Account does not exist your token might be from a different flavor",
		})
		return

	}
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into an error while trying to fetch your account",
		})
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(user)
}

func (ah *AccountHandler) UpdatePersonalAccount(w http.ResponseWriter, r *http.Request) {
	var accData repository.UpdateAccountDetailsParams
	if err := json.NewDecoder(r.Body).Decode(&accData); err != nil || accData.Name == "" {
		ah.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}
	claims := r.Context().Value(middleware.AuthUserClaims).(*utils.VerisafeClaims)

	// Check if the user is indeed the owner of the account
	if accData.ID.String() != claims.Subject {
		ah.Logger.Error("Attempting to update wrong account")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "You dont have permissions to update this account",
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	err = repo.UpdateAccountDetails(r.Context(), accData)
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into an error while trying to update your account",
		})
		return
	}
	updated, err := repo.GetAccountByID(r.Context(), accData.ID)
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into an error while trying to fetch your account",
		})
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		ah.Logger.Error("Error while committing transaction", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(updated)
}

// TODO: implement verifying mechanisms
// Use a provider such as AT or One Signal etc
func (ah *AccountHandler) VerifyPhone(w http.ResponseWriter, r *http.Request) {
	var accData repository.UpdateAccountPhoneNumberParams
	if err := json.NewDecoder(r.Body).Decode(&accData); err != nil || len(accData.Phone) < 5 {
		ah.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}
	claims := r.Context().Value(middleware.AuthUserClaims).(*utils.VerisafeClaims)

	// Check if the user is indeed the owner of the account
	if accData.ID.String() != claims.Subject {
		ah.Logger.Error("Attempting to update wrong account")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "You dont have permissions to update this account",
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	err = repo.UpdateAccountPhoneNumber(r.Context(), accData)
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into an error while trying to update your account",
		})
		return
	}
	updated, err := repo.GetAccountByID(r.Context(), accData.ID)
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into an error while trying to fetch your account",
		})
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		ah.Logger.Error("Error while committing transaction", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(updated)
}

// SearchAccountsByEmail handles searching for accounts by email address
func (ah *AccountHandler) SearchAccountsByEmail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Get search query from URL parameters
	query := r.URL.Query().Get("q")
	if query == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Search query parameter 'q' is required",
		})
		return
	}

	// Get pagination from context
	pagination := middleware.GetPagination(r.Context())

	// Get database connection
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	// Begin transaction
	tx, err := conn.Begin(r.Context())
	if err != nil {
		ah.Logger.Error("Error while beginning transaction", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}
	defer tx.Rollback(r.Context())

	repo := repository.New(tx)

	// Search accounts by email
	accounts, err := repo.SearchAccountByEmail(r.Context(), repository.SearchAccountByEmailParams{
		Lower:  query,
		Limit:  int32(pagination.Limit),
		Offset: int32(pagination.Offset),
	})
	if err != nil {
		ah.Logger.Error("Failed to search accounts by email", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't complete this request at the moment please try again later",
		})
		return
	}

	// Commit transaction
	if err = tx.Commit(r.Context()); err != nil {
		ah.Logger.Error("Error while committing transaction", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	// Prepare response
	response := map[string]interface{}{
		"accounts": accounts,
		"pagination": map[string]interface{}{
			"limit":  pagination.Limit,
			"offset": pagination.Offset,
			"total":  len(accounts),
		},
		"query":       query,
		"search_type": "email",
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// SearchAccountsByName handles searching for accounts by name
func (ah *AccountHandler) SearchAccountsByName(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Get search query from URL parameters
	query := r.URL.Query().Get("q")
	if query == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Search query parameter 'q' is required",
		})
		return
	}

	// Get pagination from context
	pagination := middleware.GetPagination(r.Context())

	// Get database connection
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	// Begin transaction
	tx, err := conn.Begin(r.Context())
	if err != nil {
		ah.Logger.Error("Error while beginning transaction", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}
	defer tx.Rollback(r.Context())

	repo := repository.New(tx)

	// Search accounts by name
	accounts, err := repo.SearchAccountByName(r.Context(), repository.SearchAccountByNameParams{
		Lower:  query,
		Limit:  int32(pagination.Limit),
		Offset: int32(pagination.Offset),
	})
	if err != nil {
		ah.Logger.Error("Failed to search accounts by name", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't complete this request at the moment please try again later",
		})
		return
	}

	// Commit transaction
	if err = tx.Commit(r.Context()); err != nil {
		ah.Logger.Error("Error while committing transaction", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	// Prepare response
	response := map[string]interface{}{
		"accounts": accounts,
		"pagination": map[string]interface{}{
			"limit":  pagination.Limit,
			"offset": pagination.Offset,
			"total":  len(accounts),
		},
		"query":       query,
		"search_type": "name",
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (ah *AccountHandler) GetAllUserAccounts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// Get pagination from context
	pagination := middleware.GetPagination(r.Context())
	// Get database connection
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	// Begin transaction
	tx, err := conn.Begin(r.Context())
	if err != nil {
		ah.Logger.Error("Error while beginning transaction", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}
	defer tx.Rollback(r.Context())

	repo := repository.New(tx)

	// Search accounts by username
	accounts, err := repo.GetAllAccounts(r.Context(), repository.GetAllAccountsParams{
		Limit:  int32(pagination.Limit),
		Offset: int32(pagination.Offset),
	})
	if err != nil {
		ah.Logger.Error("Failed to get all accounts", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't complete this request at the moment please try again later",
		})
		return
	}

	// Commit transaction
	if err = tx.Commit(r.Context()); err != nil {
		ah.Logger.Error("Error while committing transaction", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(accounts)
}

// SearchAccountsByUsername handles searching for accounts by username
func (ah *AccountHandler) SearchAccountsByUsername(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Get search query from URL parameters
	query := r.URL.Query().Get("q")
	if query == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Search query parameter 'q' is required",
		})
		return
	}

	// Get pagination from context
	pagination := middleware.GetPagination(r.Context())

	// Get database connection
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	// Begin transaction
	tx, err := conn.Begin(r.Context())
	if err != nil {
		ah.Logger.Error("Error while beginning transaction", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}
	defer tx.Rollback(r.Context())

	repo := repository.New(tx)

	// Search accounts by username
	accounts, err := repo.SearchAccountByUsername(r.Context(), repository.SearchAccountByUsernameParams{
		Lower:  query,
		Limit:  int32(pagination.Limit),
		Offset: int32(pagination.Offset),
	})
	if err != nil {
		ah.Logger.Error("Failed to search accounts by username", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't complete this request at the moment please try again later",
		})
		return
	}

	// Commit transaction
	if err = tx.Commit(r.Context()); err != nil {
		ah.Logger.Error("Error while committing transaction", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	// Prepare response
	response := map[string]interface{}{
		"accounts": accounts,
		"pagination": map[string]interface{}{
			"limit":  pagination.Limit,
			"offset": pagination.Offset,
			"total":  len(accounts),
		},
		"query":       query,
		"search_type": "username",
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
