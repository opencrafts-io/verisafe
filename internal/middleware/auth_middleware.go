package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/utils"
)

const AuthUserClaims = "middleware.auth.claims"

func IsAuthenticated(cfg *config.Config) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authorization := r.Header.Get("Authorization")
			w.Header().Add("Content-Type", "application/json")

			// Check that the header begins with a prefix of Bearer
			if !strings.HasPrefix(authorization, "Bearer ") {
				json.NewEncoder(w).Encode(map[string]any{
					"error": "Please provide your authorization  token",
				})
				return

			}

			// Pull out the token
			token := strings.TrimPrefix(authorization, "Bearer ")
			claims, err := utils.ValidateJWT(token, cfg.JWTConfig.ApiSecret)
			if err != nil {
				json.NewEncoder(w).Encode(map[string]any{
					"error": err.Error(),
				})
				return
			}

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
