package app

import "net/http"

func (a *App) loadRoutes() http.Handler {
	router := http.NewServeMux()
	return router
}
