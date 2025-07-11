
package middleware

import (
	"context"
	"net/http"
	"strconv"
)

// paginationKey is the context key used to store the parsed pagination values
// in the request context. This key is private to avoid collisions.
type paginationKey struct{}

// Pagination represents parsed pagination query parameters: `limit` and `offset`.
type Pagination struct {
	Limit  int // Maximum number of results to return
	Offset int // Number of results to skip
}

// GetPagination extracts the Pagination values from the request context.
// If no pagination was set, it returns sensible default values.
func GetPagination(ctx context.Context) Pagination {
	p, ok := ctx.Value(paginationKey{}).(Pagination)
	if !ok {
		return Pagination{Limit: 10, Offset: 0}
	}
	return p
}

// parsePagination extracts `limit` and `offset` values from the request query string.
// It applies bounds and defaults to ensure safe usage.
func parsePagination(r *http.Request, defaultLimit, maxLimit int) Pagination {
	query := r.URL.Query()

	// Parse "limit"
	limit := defaultLimit
	if lStr := query.Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 && l <= maxLimit {
			limit = l
		}
	}

	// Parse "offset"
	offset := 0
	if oStr := query.Get("offset"); oStr != "" {
		if o, err := strconv.Atoi(oStr); err == nil && o >= 0 {
			offset = o
		}
	}

	return Pagination{Limit: limit, Offset: offset}
}

// pagination is a middleware function that parses `limit` and `offset` query parameters
// from the incoming HTTP request and attaches them to the request context.
//
// Parameters:
//
//	defaultLimit int: The fallback limit to apply if none is provided.
//	maxLimit int: The upper bound for the `limit` to prevent abuse.
//
// Returns:
//
//	http.Handler: A new http.Handler that injects pagination info into context.
func pagination(defaultLimit, maxLimit int, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := parsePagination(r, defaultLimit, maxLimit)
		ctx := context.WithValue(r.Context(), paginationKey{}, p)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// PaginationMiddleware wraps the pagination middleware logic into your app's
// Middleware type (i.e., func(http.Handler) http.Handler).
func PaginationMiddleware(defaultLimit, maxLimit int) Middleware {
	return func(next http.Handler) http.Handler {
		return pagination(defaultLimit, maxLimit, next)
	}
}
