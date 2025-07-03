package app

import (
	"net/http"

	"github.com/opencrafts-io/verisafe/internal/auth"
	"github.com/opencrafts-io/verisafe/internal/handlers"
)

func (a *App) loadRoutes() http.Handler {
	router := http.NewServeMux()

	auth := auth.NewAuthenticator(a.config, a.logger)

	// ping handler
	router.HandleFunc("GET /ping", handlers.PingHandler)

	// Auth handlers
	router.HandleFunc("GET /auth/{provider}", auth.LoginHandler)
	router.HandleFunc("GET /auth/{provider}/callback", auth.CallbackHandler)
	router.HandleFunc("GET /auth/{provider}/logout", auth.LogoutHandler)
	return router

}
