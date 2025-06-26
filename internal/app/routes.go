package app

import (
	"net/http"

	"github.com/opencrafts-io/verisafe/internal/auth"
)

func (a *App) loadRoutes() http.Handler {
	router := http.NewServeMux()

	auth := auth.NewAuthenticator(a.config, a.logger)

	router.HandleFunc("GET /auth/{provider}", auth.LoginHandler)
	router.HandleFunc("GET /auth/{provider}/callback", auth.CallbackHandler)
	router.HandleFunc("GET /auth/{provider}/logout", auth.LogoutHandler)
	return router

}
