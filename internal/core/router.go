package core

import "net/http"

// Router is the registration surface a handler needs. *http.ServeMux
// satisfies it.
//
// Handlers take this rather than a concrete *http.ServeMux so the route table
// is observable: a test can pass a recording implementation and read back
// exactly what every handler registered, which is otherwise impossible because
// http.ServeMux does not expose its patterns.
type Router interface {
	Handle(pattern string, handler http.Handler)
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
}
