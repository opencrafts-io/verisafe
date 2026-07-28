package handlers

import (
	"encoding/json"
	"net/http"
)

// PingHandler godoc
//
// @Summary      Liveness probe
// @Description  Returns 200 with a fixed message whenever the process is
// @Description  serving. This is a liveness check only: it does not touch the
// @Description  database, Redis, or RabbitMQ, so a 200 here does not imply any
// @Description  dependency is reachable. Unauthenticated by design.
// @Tags         health
// @Produce      json
// @Success      200  {object}  map[string]any  "Fixed liveness message"
// @Router       /ping [get]
func PingHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("content-type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{"message": "he is risen"})
}
