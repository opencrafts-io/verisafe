package handlers

import (
	"encoding/json"
	"net/http"
)

// Returns an abitrary message to the caller..
// Used for checking service health
func PingHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("content-type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{"message": "he is risen"})
}
