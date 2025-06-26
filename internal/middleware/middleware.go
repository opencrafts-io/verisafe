package middleware

import "net/http" // Package "net/http" provides HTTP client and server implementations.

// Middleware is a type alias representing a function that takes an http.Handler
// and returns an http.Handler. This is the common signature for HTTP middleware
// in Go, allowing chaining of handlers.
// Each middleware can perform actions (e.g., logging, authentication) before or after
// delegating to the next handler in the chain.
type Middleware func(http.Handler) http.Handler

// CreateStack takes a variadic number of Middleware functions and composes them
// into a single Middleware function.
//
// The order of execution for the middleware in the returned stack is from
// right-to-left (last middleware in the `xs` slice wraps the first). This means
// the middleware listed first in the `xs` argument will be the *outermost* middleware
// in the chain (executing first), and the middleware listed last will be the
// *innermost* (executing just before the final `next` handler).
//
// Parameters:
//
//	xs ...Middleware: A variadic slice of Middleware functions to be chained.
//
// Returns:
//
//	Middleware: A single Middleware function that, when executed, applies all
//	the provided middleware in the correct order.
func CreateStack(xs ...Middleware) Middleware {
	// The returned function is itself a Middleware.
	// It takes the 'next' http.Handler, which represents the subsequent handler
	// in the chain (or the final application handler).
	return func(next http.Handler) http.Handler {
		// Iterate through the provided middleware slice 'xs' in reverse order.
		// This is crucial for correctly building the middleware chain,
		// ensuring that the 'next' handler is progressively wrapped by each middleware.
		// The innermost middleware wraps the final handler, then the next middleware
		// wraps that, and so on.
		for i := len(xs) - 1; i >= 0; i-- {
			x := xs[i] // Get the current middleware from the slice.
			// Apply the current middleware 'x' to the current 'next' handler.
			// The result becomes the new 'next' handler for the next iteration
			// (or the final returned handler if this is the first iteration).
			next = x(next)
		}

		// After all middleware have been applied, return the fully wrapped
		// http.Handler. This handler now represents the entire middleware stack
		// leading to the original 'next' handler.
		return next
	}
}
