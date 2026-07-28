package handlers

import "github.com/opencrafts-io/verisafe/internal/core"

// AppHandler is an alias for core.AppHandler, kept so the existing in-package
// call sites keep reading as AppHandler(h.Something).
//
// This is a type alias rather than a defined type, so handlers.AppHandler and
// core.AppHandler are the same type: a value of one satisfies a parameter of
// the other and no conversion is needed anywhere.
type AppHandler = core.AppHandler
