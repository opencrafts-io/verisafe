package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	"github.com/opencrafts-io/verisafe/internal/utils"
)

type ServiceTokenHandler struct {
	Logger *slog.Logger
	Cfg    *config.Config
}

// ServiceTokenRequest represents the request to create a service token
type ServiceTokenRequest struct {
	Name              string                 `json:"name" validate:"required,min=1,max=100"`
	Description       *string                `json:"description"`
	ExpiresInDays     *int                   `json:"expires_in_days" validate:"omitempty,min=1,max=3650"` // Max 10 years
	Scopes            []string               `json:"scopes"`
	MaxUses           *int                   `json:"max_uses" validate:"omitempty,min=1"`
	RotationPolicy    *RotationPolicy        `json:"rotation_policy"`
	IPWhitelist       []string               `json:"ip_whitelist"`
	UserAgentPattern  *string                `json:"user_agent_pattern"`
	Metadata          map[string]interface{} `json:"metadata"`
}



// ServiceTokenResponse represents the response for service token operations
type ServiceTokenResponse struct {
	ID          uuid.UUID              `json:"id"`
	Name        string                 `json:"name"`
	Description *string                `json:"description"`
	Token       string                 `json:"token,omitempty"` // Only included on creation
	ExpiresAt   *time.Time             `json:"expires_at"`
	Scopes      []string               `json:"scopes"`
	MaxUses     *int                   `json:"max_uses"`
	UseCount    int                    `json:"use_count"`
	CreatedAt   time.Time              `json:"created_at"`
	LastUsedAt  *time.Time             `json:"last_used_at"`
	RotatedAt   *time.Time             `json:"rotated_at"`
	RevokedAt   *time.Time             `json:"revoked_at"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// ServiceTokenUpdateRequest represents the request to update a service token
type ServiceTokenUpdateRequest struct {
	Name             *string                `json:"name" validate:"omitempty,min=1,max=100"`
	Description      *string                `json:"description"`
	Scopes           []string               `json:"scopes"`
	MaxUses          *int                   `json:"max_uses" validate:"omitempty,min=1"`
	RotationPolicy   *RotationPolicy        `json:"rotation_policy"`
	IPWhitelist      []string               `json:"ip_whitelist"`
	UserAgentPattern *string                `json:"user_agent_pattern"`
	Metadata         map[string]interface{} `json:"metadata"`
}

// ServiceTokenStats represents usage statistics for service tokens
type ServiceTokenStats struct {
	TotalTokens       int `json:"total_tokens"`
	ActiveTokens      int `json:"active_tokens"`
	RevokedTokens     int `json:"revoked_tokens"`
	ExpiredTokens     int `json:"expired_tokens"`
	RecentlyUsedTokens int `json:"recently_used_tokens"`
}

func (sth *ServiceTokenHandler) RegisterHandlers(router *http.ServeMux) {
	// Service token management routes
	router.Handle("POST /api/v1/service-tokens",
		middleware.CreateStack(
			middleware.IsAuthenticated(sth.Cfg, sth.Logger),
			middleware.HasPermission([]string{"create:service_token:own"}),
		)(http.HandlerFunc(sth.CreateServiceToken)))

	router.Handle("GET /api/v1/service-tokens",
		middleware.CreateStack(
			middleware.IsAuthenticated(sth.Cfg, sth.Logger),
			middleware.HasPermission([]string{"list:service_token:own"}),
		)(http.HandlerFunc(sth.ListServiceTokens)))

	router.Handle("GET /api/v1/service-tokens/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(sth.Cfg, sth.Logger),
			middleware.HasPermission([]string{"read:service_token:own"}),
		)(http.HandlerFunc(sth.GetServiceToken)))

	router.Handle("PUT /api/v1/service-tokens/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(sth.Cfg, sth.Logger),
			middleware.HasPermission([]string{"update:service_token:own"}),
		)(http.HandlerFunc(sth.UpdateServiceToken)))

	router.Handle("POST /api/v1/service-tokens/{id}/rotate",
		middleware.CreateStack(
			middleware.IsAuthenticated(sth.Cfg, sth.Logger),
			middleware.HasPermission([]string{"rotate:service_token:own"}),
		)(http.HandlerFunc(sth.RotateServiceToken)))

	router.Handle("DELETE /api/v1/service-tokens/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(sth.Cfg, sth.Logger),
			middleware.HasPermission([]string{"revoke:service_token:own"}),
		)(http.HandlerFunc(sth.RevokeServiceToken)))

	router.Handle("GET /api/v1/service-tokens/stats",
		middleware.CreateStack(
			middleware.IsAuthenticated(sth.Cfg, sth.Logger),
			middleware.HasPermission([]string{"read:service_token:own"}),
		)(http.HandlerFunc(sth.GetServiceTokenStats)))

	// Admin routes for managing any service tokens
	router.Handle("GET /api/v1/admin/service-tokens",
		middleware.CreateStack(
			middleware.IsAuthenticated(sth.Cfg, sth.Logger),
			middleware.HasPermission([]string{"list:service_token:any"}),
		)(http.HandlerFunc(sth.ListAllServiceTokens)))

	router.Handle("POST /api/v1/admin/service-tokens/cleanup",
		middleware.CreateStack(
			middleware.IsAuthenticated(sth.Cfg, sth.Logger),
			middleware.HasPermission([]string{"update:service_token:any"}),
		)(http.HandlerFunc(sth.CleanupExpiredTokens)))
}

// CreateServiceToken creates a new service token for a bot account
func (sth *ServiceTokenHandler) CreateServiceToken(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(middleware.AuthUserClaims).(*utils.VerisafeClaims)
	accountID, err := uuid.Parse(claims.Subject)
	if err != nil {
		sth.Logger.Error("Failed to parse account ID from claims", slog.String("error", err.Error()))
		http.Error(w, "Invalid token", http.StatusBadRequest)
		return
	}

	// Verify the account is a bot account
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		sth.Logger.Error("Failed to get database connection", slog.String("error", err.Error()))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	tx, err := conn.Begin(r.Context())
	if err != nil {
		sth.Logger.Error("Failed to begin transaction", slog.String("error", err.Error()))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())

	repo := repository.New(tx)

	// Get account and verify it's a bot
	account, err := repo.GetAccountByID(r.Context(), accountID)
	if err != nil {
		sth.Logger.Error("Failed to get account", slog.String("error", err.Error()))
		http.Error(w, "Account not found", http.StatusNotFound)
		return
	}

	if account.Type != repository.AccountTypeBot {
		http.Error(w, "Only bot accounts can create service tokens", http.StatusForbidden)
		return
	}

	// Parse request
	var req ServiceTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate request
	if err := sth.validateServiceTokenRequest(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Generate secure token
	token, err := sth.generateSecureToken()
	if err != nil {
		sth.Logger.Error("Failed to generate secure token", slog.String("error", err.Error()))
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	// Calculate expiry
	var expiresAt *time.Time
	if req.ExpiresInDays != nil {
		expiry := time.Now().AddDate(0, 0, *req.ExpiresInDays)
		expiresAt = &expiry
	} else {
		// Default to 1 year
		expiry := time.Now().AddDate(1, 0, 0)
		expiresAt = &expiry
	}

	// Prepare rotation policy
	var rotationPolicyJSON []byte
	if req.RotationPolicy != nil {
		rotationPolicyJSON, err = json.Marshal(req.RotationPolicy)
		if err != nil {
			sth.Logger.Error("Failed to marshal rotation policy", slog.String("error", err.Error()))
			http.Error(w, "Invalid rotation policy", http.StatusBadRequest)
			return
		}
	}

	// Prepare metadata
	var metadataJSON []byte
	if req.Metadata != nil {
		metadataJSON, err = json.Marshal(req.Metadata)
		if err != nil {
			sth.Logger.Error("Failed to marshal metadata", slog.String("error", err.Error()))
			http.Error(w, "Invalid metadata", http.StatusBadRequest)
			return
		}
	}

	// Create service token
	serviceToken, err := repo.CreateServiceToken(r.Context(), repository.CreateServiceTokenParams{
		AccountID:         accountID,
		Name:              req.Name,
		Description:       req.Description,
		TokenHash:         utils.HashToken(token),
		ExpiresAt:         expiresAt,
		Scopes:            req.Scopes,
		MaxUses:           func() *int32 {
			if req.MaxUses == nil {
				return nil
			}
			val := int32(*req.MaxUses)
			return &val
		}(),
		RotationPolicy:    rotationPolicyJSON,
		IpWhitelist:       req.IPWhitelist,
		UserAgentPattern:  req.UserAgentPattern,
		CreatedBy:         pgtype.UUID{Bytes: accountID, Valid: true},
		Metadata:          metadataJSON,
	})
	if err != nil {
		sth.Logger.Error("Failed to create service token", slog.String("error", err.Error()))
		http.Error(w, "Failed to create service token", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		sth.Logger.Error("Failed to commit transaction", slog.String("error", err.Error()))
		http.Error(w, "Failed to create service token", http.StatusInternalServerError)
		return
	}

	// Return response
	response := ServiceTokenResponse{
		ID:          serviceToken.ID,
		Name:        serviceToken.Name,
		Description: serviceToken.Description,
		Token:       token, // Only include token on creation
		ExpiresAt:   serviceToken.ExpiresAt,
		Scopes:      serviceToken.Scopes,
		MaxUses:     func() *int {
			if serviceToken.MaxUses == nil {
				return nil
			}
			val := int(*serviceToken.MaxUses)
			return &val
		}(),
		UseCount:    int(*serviceToken.UseCount),
		CreatedAt:   serviceToken.CreatedAt.Time,
		LastUsedAt:  serviceToken.LastUsedAt,
		RotatedAt:   serviceToken.RotatedAt,
		RevokedAt:   serviceToken.RevokedAt,
	}

	if serviceToken.Metadata != nil {
		json.Unmarshal(serviceToken.Metadata, &response.Metadata)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// ListServiceTokens lists all service tokens for the authenticated account
func (sth *ServiceTokenHandler) ListServiceTokens(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(middleware.AuthUserClaims).(*utils.VerisafeClaims)
	accountID, err := uuid.Parse(claims.Subject)
	if err != nil {
		sth.Logger.Error("Failed to parse account ID from claims", slog.String("error", err.Error()))
		http.Error(w, "Invalid token", http.StatusBadRequest)
		return
	}

	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		sth.Logger.Error("Failed to get database connection", slog.String("error", err.Error()))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	repo := repository.New(conn)
	tokens, err := repo.ListServiceTokensByAccount(r.Context(), accountID)
	if err != nil {
		sth.Logger.Error("Failed to list service tokens", slog.String("error", err.Error()))
		http.Error(w, "Failed to list service tokens", http.StatusInternalServerError)
		return
	}

	// Convert to response format
	responses := make([]ServiceTokenResponse, len(tokens))
	for i, token := range tokens {
		responses[i] = sth.convertToServiceTokenResponse(token)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(responses)
}

// GetServiceToken retrieves a specific service token
func (sth *ServiceTokenHandler) GetServiceToken(w http.ResponseWriter, r *http.Request) {
	// Extract token ID from URL
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 5 {
		http.Error(w, "Invalid token ID", http.StatusBadRequest)
		return
	}
	tokenID, err := uuid.Parse(pathParts[4])
	if err != nil {
		http.Error(w, "Invalid token ID", http.StatusBadRequest)
		return
	}

	claims := r.Context().Value(middleware.AuthUserClaims).(*utils.VerisafeClaims)
	accountID, err := uuid.Parse(claims.Subject)
	if err != nil {
		sth.Logger.Error("Failed to parse account ID from claims", slog.String("error", err.Error()))
		http.Error(w, "Invalid token", http.StatusBadRequest)
		return
	}

	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		sth.Logger.Error("Failed to get database connection", slog.String("error", err.Error()))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	repo := repository.New(conn)
	token, err := repo.GetServiceTokenByID(r.Context(), tokenID)
	if err != nil {
		http.Error(w, "Service token not found", http.StatusNotFound)
		return
	}

	// Verify ownership (unless admin)
	perms := r.Context().Value(middleware.AuthUserPerms).([]string)
	isAdmin := false
	for _, perm := range perms {
		if perm == "read:service_token:any" {
			isAdmin = true
			break
		}
	}

	if !isAdmin && token.AccountID != accountID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	response := sth.convertToServiceTokenResponse(token)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// UpdateServiceToken updates a service token
func (sth *ServiceTokenHandler) UpdateServiceToken(w http.ResponseWriter, r *http.Request) {
	// Extract token ID from URL
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 5 {
		http.Error(w, "Invalid token ID", http.StatusBadRequest)
		return
	}
	tokenID, err := uuid.Parse(pathParts[4])
	if err != nil {
		http.Error(w, "Invalid token ID", http.StatusBadRequest)
		return
	}

	claims := r.Context().Value(middleware.AuthUserClaims).(*utils.VerisafeClaims)
	accountID, err := uuid.Parse(claims.Subject)
	if err != nil {
		sth.Logger.Error("Failed to parse account ID from claims", slog.String("error", err.Error()))
		http.Error(w, "Invalid token", http.StatusBadRequest)
		return
	}

	// Parse request
	var req ServiceTokenUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		sth.Logger.Error("Failed to get database connection", slog.String("error", err.Error()))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	tx, err := conn.Begin(r.Context())
	if err != nil {
		sth.Logger.Error("Failed to begin transaction", slog.String("error", err.Error()))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())

	repo := repository.New(tx)

	// Get existing token
	token, err := repo.GetServiceTokenByID(r.Context(), tokenID)
	if err != nil {
		http.Error(w, "Service token not found", http.StatusNotFound)
		return
	}

	// Verify ownership (unless admin)
	perms := r.Context().Value(middleware.AuthUserPerms).([]string)
	isAdmin := false
	for _, perm := range perms {
		if perm == "update:service_token:any" {
			isAdmin = true
			break
		}
	}

	if !isAdmin && token.AccountID != accountID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Prepare update parameters
	var rotationPolicyJSON []byte
	if req.RotationPolicy != nil {
		rotationPolicyJSON, err = json.Marshal(req.RotationPolicy)
		if err != nil {
			sth.Logger.Error("Failed to marshal rotation policy", slog.String("error", err.Error()))
			http.Error(w, "Invalid rotation policy", http.StatusBadRequest)
			return
		}
	}

	var metadataJSON []byte
	if req.Metadata != nil {
		metadataJSON, err = json.Marshal(req.Metadata)
		if err != nil {
			sth.Logger.Error("Failed to marshal metadata", slog.String("error", err.Error()))
			http.Error(w, "Invalid metadata", http.StatusBadRequest)
			return
		}
	}

	// Update token
	err = repo.UpdateServiceToken(r.Context(), repository.UpdateServiceTokenParams{
		ID:               tokenID,
		Name:             *req.Name,
		Description:      req.Description,
		Scopes:           req.Scopes,
		MaxUses:          func() *int32 {
			if req.MaxUses == nil {
				return nil
			}
			val := int32(*req.MaxUses)
			return &val
		}(),
		RotationPolicy:   rotationPolicyJSON,
		IpWhitelist:      req.IPWhitelist,
		UserAgentPattern: req.UserAgentPattern,
		Metadata:         metadataJSON,
	})
	if err != nil {
		sth.Logger.Error("Failed to update service token", slog.String("error", err.Error()))
		http.Error(w, "Failed to update service token", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		sth.Logger.Error("Failed to commit transaction", slog.String("error", err.Error()))
		http.Error(w, "Failed to update service token", http.StatusInternalServerError)
		return
	}

	// Get updated token
	updatedToken, err := repo.GetServiceTokenByID(r.Context(), tokenID)
	if err != nil {
		sth.Logger.Error("Failed to get updated token", slog.String("error", err.Error()))
		http.Error(w, "Failed to retrieve updated token", http.StatusInternalServerError)
		return
	}

	response := sth.convertToServiceTokenResponse(updatedToken)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// RotateServiceToken rotates a service token
func (sth *ServiceTokenHandler) RotateServiceToken(w http.ResponseWriter, r *http.Request) {
	// Extract token ID from URL
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 6 {
		http.Error(w, "Invalid token ID", http.StatusBadRequest)
		return
	}
	tokenID, err := uuid.Parse(pathParts[4])
	if err != nil {
		http.Error(w, "Invalid token ID", http.StatusBadRequest)
		return
	}

	claims := r.Context().Value(middleware.AuthUserClaims).(*utils.VerisafeClaims)
	accountID, err := uuid.Parse(claims.Subject)
	if err != nil {
		sth.Logger.Error("Failed to parse account ID from claims", slog.String("error", err.Error()))
		http.Error(w, "Invalid token", http.StatusBadRequest)
		return
	}

	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		sth.Logger.Error("Failed to get database connection", slog.String("error", err.Error()))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	tx, err := conn.Begin(r.Context())
	if err != nil {
		sth.Logger.Error("Failed to begin transaction", slog.String("error", err.Error()))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())

	repo := repository.New(tx)

	// Get existing token
	token, err := repo.GetServiceTokenByID(r.Context(), tokenID)
	if err != nil {
		http.Error(w, "Service token not found", http.StatusNotFound)
		return
	}

	// Verify ownership (unless admin)
	perms := r.Context().Value(middleware.AuthUserPerms).([]string)
	isAdmin := false
	for _, perm := range perms {
		if perm == "rotate:service_token:any" {
			isAdmin = true
			break
		}
	}

	if !isAdmin && token.AccountID != accountID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Generate new token
	newToken, err := sth.generateSecureToken()
	if err != nil {
		sth.Logger.Error("Failed to generate secure token", slog.String("error", err.Error()))
		http.Error(w, "Failed to generate new token", http.StatusInternalServerError)
		return
	}

	// Rotate token
	err = repo.RotateServiceToken(r.Context(), repository.RotateServiceTokenParams{
		ID:        tokenID,
		TokenHash: utils.HashToken(newToken),
		ExpiresAt: token.ExpiresAt,
	})
	if err != nil {
		sth.Logger.Error("Failed to rotate service token", slog.String("error", err.Error()))
		http.Error(w, "Failed to rotate service token", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		sth.Logger.Error("Failed to commit transaction", slog.String("error", err.Error()))
		http.Error(w, "Failed to rotate service token", http.StatusInternalServerError)
		return
	}

	// Get updated token
	updatedToken, err := repo.GetServiceTokenByID(r.Context(), tokenID)
	if err != nil {
		sth.Logger.Error("Failed to get updated token", slog.String("error", err.Error()))
		http.Error(w, "Failed to retrieve updated token", http.StatusInternalServerError)
		return
	}

	response := sth.convertToServiceTokenResponse(updatedToken)
	response.Token = newToken // Include new token in response

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// RevokeServiceToken revokes a service token
func (sth *ServiceTokenHandler) RevokeServiceToken(w http.ResponseWriter, r *http.Request) {
	// Extract token ID from URL
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 5 {
		http.Error(w, "Invalid token ID", http.StatusBadRequest)
		return
	}
	tokenID, err := uuid.Parse(pathParts[4])
	if err != nil {
		http.Error(w, "Invalid token ID", http.StatusBadRequest)
		return
	}

	claims := r.Context().Value(middleware.AuthUserClaims).(*utils.VerisafeClaims)
	accountID, err := uuid.Parse(claims.Subject)
	if err != nil {
		sth.Logger.Error("Failed to parse account ID from claims", slog.String("error", err.Error()))
		http.Error(w, "Invalid token", http.StatusBadRequest)
		return
	}

	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		sth.Logger.Error("Failed to get database connection", slog.String("error", err.Error()))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	tx, err := conn.Begin(r.Context())
	if err != nil {
		sth.Logger.Error("Failed to begin transaction", slog.String("error", err.Error()))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())

	repo := repository.New(tx)

	// Get existing token
	token, err := repo.GetServiceTokenByID(r.Context(), tokenID)
	if err != nil {
		http.Error(w, "Service token not found", http.StatusNotFound)
		return
	}

	// Verify ownership (unless admin)
	perms := r.Context().Value(middleware.AuthUserPerms).([]string)
	isAdmin := false
	for _, perm := range perms {
		if perm == "revoke:service_token:any" {
			isAdmin = true
			break
		}
	}

	if !isAdmin && token.AccountID != accountID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Revoke token
	err = repo.RevokeServiceToken(r.Context(), tokenID)
	if err != nil {
		sth.Logger.Error("Failed to revoke service token", slog.String("error", err.Error()))
		http.Error(w, "Failed to revoke service token", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		sth.Logger.Error("Failed to commit transaction", slog.String("error", err.Error()))
		http.Error(w, "Failed to revoke service token", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetServiceTokenStats returns usage statistics for service tokens
func (sth *ServiceTokenHandler) GetServiceTokenStats(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(middleware.AuthUserClaims).(*utils.VerisafeClaims)
	accountID, err := uuid.Parse(claims.Subject)
	if err != nil {
		sth.Logger.Error("Failed to parse account ID from claims", slog.String("error", err.Error()))
		http.Error(w, "Invalid token", http.StatusBadRequest)
		return
	}

	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		sth.Logger.Error("Failed to get database connection", slog.String("error", err.Error()))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	repo := repository.New(conn)
	stats, err := repo.GetServiceTokenUsageStats(r.Context(), accountID)
	if err != nil {
		sth.Logger.Error("Failed to get service token stats", slog.String("error", err.Error()))
		http.Error(w, "Failed to get service token stats", http.StatusInternalServerError)
		return
	}

	response := ServiceTokenStats{
		TotalTokens:       int(stats.TotalTokens),
		ActiveTokens:      int(stats.ActiveTokens),
		RevokedTokens:     int(stats.RevokedTokens),
		ExpiredTokens:     int(stats.ExpiredTokens),
		RecentlyUsedTokens: int(stats.RecentlyUsedTokens),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// ListAllServiceTokens lists all service tokens (admin only)
func (sth *ServiceTokenHandler) ListAllServiceTokens(w http.ResponseWriter, r *http.Request) {
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		sth.Logger.Error("Failed to get database connection", slog.String("error", err.Error()))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	repo := repository.New(conn)
	tokens, err := repo.ListActiveServiceTokens(r.Context())
	if err != nil {
		sth.Logger.Error("Failed to list all service tokens", slog.String("error", err.Error()))
		http.Error(w, "Failed to list service tokens", http.StatusInternalServerError)
		return
	}

	// Convert to response format
	responses := make([]ServiceTokenResponse, len(tokens))
	for i, token := range tokens {
		responses[i] = sth.convertActiveServiceTokenToResponse(token)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(responses)
}

// CleanupExpiredTokens cleans up expired tokens (admin only)
func (sth *ServiceTokenHandler) CleanupExpiredTokens(w http.ResponseWriter, r *http.Request) {
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		sth.Logger.Error("Failed to get database connection", slog.String("error", err.Error()))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	repo := repository.New(conn)
	err = repo.CleanupExpiredServiceTokens(r.Context())
	if err != nil {
		sth.Logger.Error("Failed to cleanup expired tokens", slog.String("error", err.Error()))
		http.Error(w, "Failed to cleanup expired tokens", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Helper methods

// generateSecureToken generates a cryptographically secure token
func (sth *ServiceTokenHandler) generateSecureToken() (string, error) {
	// Generate 32 bytes of random data
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	
	// Encode as base64 and add prefix for identification
	token := "vst_" + base64.URLEncoding.EncodeToString(bytes)
	return token, nil
}

// validateServiceTokenRequest validates the service token request
func (sth *ServiceTokenHandler) validateServiceTokenRequest(req *ServiceTokenRequest) error {
	// Validate name
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("name is required")
	}

	// Validate scopes if provided
	for _, scope := range req.Scopes {
		if !sth.isValidScope(scope) {
			return fmt.Errorf("invalid scope: %s", scope)
		}
	}

	// Validate IP whitelist if provided
	for _, ip := range req.IPWhitelist {
		if !sth.isValidIP(ip) {
			return fmt.Errorf("invalid IP address: %s", ip)
		}
	}

	// Validate user agent pattern if provided
	if req.UserAgentPattern != nil {
		if _, err := regexp.Compile(*req.UserAgentPattern); err != nil {
			return fmt.Errorf("invalid user agent pattern: %s", err.Error())
		}
	}

	return nil
}

// isValidScope validates if a scope is valid
func (sth *ServiceTokenHandler) isValidScope(scope string) bool {
	// Add your scope validation logic here
	// For now, just check if it's not empty and contains only valid characters
	if strings.TrimSpace(scope) == "" {
		return false
	}
	
	// Check for valid characters (alphanumeric, colon, dot, underscore, hyphen)
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9:._-]+$`, scope)
	return matched
}

// isValidIP validates if an IP address is valid
func (sth *ServiceTokenHandler) isValidIP(ip string) bool {
	// Simple IP validation - you might want to use a more robust library
	matched, _ := regexp.MatchString(`^(\d{1,3}\.){3}\d{1,3}$`, ip)
	if !matched {
		return false
	}
	
	// Check each octet
	parts := strings.Split(ip, ".")
	for _, part := range parts {
		if len(part) > 3 || len(part) == 0 {
			return false
		}
		if part[0] == '0' && len(part) > 1 {
			return false
		}
	}
	
	return true
}

// convertToServiceTokenResponse converts a repository ServiceToken to ServiceTokenResponse
func (sth *ServiceTokenHandler) convertToServiceTokenResponse(token repository.ServiceToken) ServiceTokenResponse {
	response := ServiceTokenResponse{
		ID:         token.ID,
		Name:       token.Name,
		Description: token.Description,
		ExpiresAt:  token.ExpiresAt,
		Scopes:     token.Scopes,
		MaxUses:    func() *int {
			if token.MaxUses == nil {
				return nil
			}
			val := int(*token.MaxUses)
			return &val
		}(),
		UseCount:   int(*token.UseCount),
		CreatedAt:  token.CreatedAt.Time,
		LastUsedAt: token.LastUsedAt,
		RotatedAt:  token.RotatedAt,
		RevokedAt:  token.RevokedAt,
	}

	if token.Metadata != nil {
		json.Unmarshal(token.Metadata, &response.Metadata)
	}

	return response
}

// convertActiveServiceTokenToResponse converts a repository ActiveServiceToken to ServiceTokenResponse
func (sth *ServiceTokenHandler) convertActiveServiceTokenToResponse(token repository.ActiveServiceToken) ServiceTokenResponse {
	response := ServiceTokenResponse{
		ID:         token.ID,
		Name:       token.Name,
		Description: token.Description,
		ExpiresAt:  token.ExpiresAt,
		Scopes:     token.Scopes,
		MaxUses:    func() *int {
			if token.MaxUses == nil {
				return nil
			}
			val := int(*token.MaxUses)
			return &val
		}(),
		UseCount:   int(*token.UseCount),
		CreatedAt:  token.CreatedAt.Time,
		LastUsedAt: token.LastUsedAt,
		RotatedAt:  token.RotatedAt,
		RevokedAt:  token.RevokedAt,
	}

	if token.Metadata != nil {
		json.Unmarshal(token.Metadata, &response.Metadata)
	}

	return response
}

