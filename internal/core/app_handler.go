package core

import "net/http"

// AppHandler is a http.HandlerFunc that can return an error.
// Use it instead of http.HandlerFunc when you want centralised error handling.
//
// It lives in core rather than in a transport package because core already
// owns the sentinels and HandleError, the two halves of the contract this
// type completes. Keeping it here also means any package can adopt the
// pattern without importing a sibling handler package for one type.
type AppHandler func(w http.ResponseWriter, r *http.Request) error

// ServeHTTP implements http.Handler, adapting AppHandler into the standard library.
// All error-to-HTTP mapping lives in HandleError — handlers never touch
// w.WriteHeader for errors.
func (h AppHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h(w, r); err != nil {
		HandleError(w, err)
	}
}
