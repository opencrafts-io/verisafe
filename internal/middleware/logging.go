package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// wrappedWriter is a custom http.ResponseWriter implementation used to capture
// the HTTP status code set by the subsequent (wrapped) handler.
// It embeds the original http.ResponseWriter to inherit all its methods,
// and adds a field to store the status code.
type wrappedWriter struct {
	http.ResponseWriter // Anonymous field: embeds the original ResponseWriter,
	// promoting its methods (e.g., Write, Header) directly
	// to wrappedWriter, so we don't have to reimplement them.
	statusCode int // statusCode stores the HTTP status code set by WriteHeader.
}

// WriteHeader overrides the default WriteHeader method of the embedded
// http.ResponseWriter.
// This method is called by the wrapped handler to set the HTTP status code.
//
// It first calls the original WriteHeader method to ensure the status code
// is sent to the client, and then it stores the `statusCode` internally
// in the `wrappedWriter` struct for later retrieval (e.g., for logging).
func (w *wrappedWriter) WriteHeader(statusCode int) {
	w.ResponseWriter.WriteHeader(statusCode) // Call the original WriteHeader method.
	w.statusCode = statusCode                // Store the status code for logging.
}

// Logging is a middleware function that logs details about incoming HTTP requests
// and their corresponding responses.
//
// It calculates the request duration, captures the HTTP status code,
// and logs the status, HTTP method, URL path, and elapsed time.
//
// Parameters:
//
//	next http.Handler: The next http.Handler in the middleware chain or
//	                    the final application handler to be executed.
//
// Returns:
//
//	http.Handler: A new http.Handler that wraps the 'next' handler with logging
//	              functionality.
func logging(logger *slog.Logger, next http.Handler) http.Handler {
	// http.HandlerFunc is an adapter that allows a function with the signature
	// `func(w http.ResponseWriter, r *http.Request)` to be used as an http.Handler.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now() // Record the start time of the request.

		// Create an instance of our custom wrappedWriter.
		// We pass the original http.ResponseWriter (`w`) to it, and
		// initialize the statusCode to http.StatusOK (200) as a default.
		// If WriteHeader is explicitly called later by the handler, this
		// default will be overridden. If WriteHeader is *never* called,
		// http.StatusOK is the implicit default in Go's http server.
		wrapped := &wrappedWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK, // Default status code if not explicitly set
		}

		// Call the next handler in the chain. Crucially, we pass our `wrapped`
		// ResponseWriter instead of the original `w`. This ensures that any call
		// to `WriteHeader` by `next` will go through our `wrappedWriter`'s method,
		// allowing us to capture the status code.
		next.ServeHTTP(wrapped, r)

		end := time.Since(start)

		// After the next handler has completed its execution (and potentially
		// written its response), log the request details.
		//
		// Logged information includes:
		// - `wrapped.statusCode`: The HTTP status code returned (captured by wrappedWriter).
		// - `r.Method`: The HTTP method (GET, POST, etc.).
		// - `r.URL.Path`: The path component of the request URL.
		// - `time.Since(start)`: The duration the request took to process.
		logger.Info(
			"Request handled",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("host", r.Host),
			slog.Int64("duration_ns", end.Nanoseconds()),
			slog.Int("status", wrapped.statusCode),
			slog.String("remote_addr", r.RemoteAddr),
		)
	})
}

// Logging middleware is used to write log information out to the console
// on each request/response.
func Logging(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return logging(logger, next)
	}
}
