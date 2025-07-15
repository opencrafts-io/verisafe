package middleware

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/repository"
	"github.com/opencrafts-io/verisafe/internal/utils"
)

const AuthUserClaims = "middleware.auth.claims"

func IsAuthenticated(cfg *config.Config, logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Content-Type", "application/json")

			authHeader := r.Header.Get("Authorization")
			apiKey := r.Header.Get("X-API-Key")

			var claims *utils.VerisafeClaims

			conn, err := GetDBConnFromContext(r.Context())
			if err != nil {
				logger.Error("failed to get db conn", slog.String("err", err.Error()))
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]any{"error": "Internal server error"})
				return
			}

			tx, err := conn.Begin(r.Context())
			if err != nil {
				logger.Error("failed to begin tx", slog.String("err", err.Error()))
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]any{"error": "Internal server error"})
				return
			}
			defer tx.Rollback(r.Context())

			repo := repository.New(tx)

			switch {
			// --- Bearer Token
			case strings.HasPrefix(authHeader, "Bearer "):
				token := strings.TrimPrefix(authHeader, "Bearer ")
				parsedClaims, err := utils.ValidateJWT(token, cfg.JWTConfig.ApiSecret)
				if err != nil {
					w.WriteHeader(http.StatusUnauthorized)
					json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
					return
				}
				claims = parsedClaims

			// --- X-API-Key
			case apiKey != "":

				hashed := utils.HashToken(apiKey)
				serviceToken, err := repo.GetServiceTokenByHash(r.Context(), hashed)

				isInvalid := serviceToken.RevokedAt != nil || serviceToken.ExpiresAt.Before(time.Now())	
				if err != nil || isInvalid  {
					w.WriteHeader(http.StatusUnauthorized)
					json.NewEncoder(w).Encode(map[string]any{"error": "Invalid or expired API key"})
					return
				}

				// Get account and perms
				account, err := repo.GetAccountByID(r.Context(), serviceToken.AccountID)
				if err != nil {
					logger.Error("Failed to load account from API key", slog.Any("error", err))
					w.WriteHeader(http.StatusUnauthorized)
					json.NewEncoder(w).Encode(map[string]any{"error": "Unauthorized"})
					return
				}

				roles, _ := repo.GetAllUserRoles(r.Context(), account.ID)
				perms, _ := repo.GetUserPermissions(r.Context(), account.ID)

				var permissionStrings []string
				for _, p := range perms {
					permissionStrings = append(permissionStrings, p.Permission)
				}

				claims = &utils.VerisafeClaims{
					Account:     account,
					Roles:       roles,
					Permissions: permissionStrings,
					RegisteredClaims: jwt.RegisteredClaims{
						Subject: account.ID.String(),
					},
				}

			default:
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]any{"error": "Missing Authorization or X-API-Key header"})
				return
			}

			// Inject the unified claims into context
			ctx := context.WithValue(r.Context(), AuthUserClaims, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Checks whether the request bearer token has the necessary permission to continue
// IsAuthenticated must be called before invoking this middleware so that the context
// is populated with the claims from the decoded jwt
func HasPermission(permissions []string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract claims from the context (Assuming it was set by IsAuthenticated middleware)
			var claims = r.Context().Value(AuthUserClaims).(*utils.VerisafeClaims)

			// Check if the user has the required permissions
			for _, perm := range permissions {
				if !slices.Contains(claims.Permissions, perm) {
					w.WriteHeader(http.StatusUnauthorized)
					json.NewEncoder(w).Encode(map[string]any{
						"error": "You do not have the necessary permissions to perform this action",
					})
					return
				}
			}
			// Proceed to the next handler
			next.ServeHTTP(w, r)
		})
	}
}
