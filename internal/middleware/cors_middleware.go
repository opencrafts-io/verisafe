package middleware

import "net/http"

// CORS reflects back Access-Control-Allow-Origin only for origins present in
// allowedOrigins, instead of a single hardcoded value — a browser rejects
// any response whose Allow-Origin doesn't exactly match its own Origin, so
// a fixed value only ever works for one deployment (e.g. local dev) and
// silently blocks every other one (e.g. production).
func CORS(allowedOrigins []string) Middleware {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[origin] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if _, ok := allowed[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			w.Header().
				Set("Access-Control-Allow-Methods", "POST, GET, PATCH, OPTIONS, PUT, DELETE")
			w.Header().
				Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
