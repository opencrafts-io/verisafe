package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/opencrafts-io/verisafe/internal/handlers"
)

func TestPingHandler(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "/ping", nil)
	if err != nil {
		t.Fatalf("Could not create request: %v\n", err)
	}

	rr := httptest.NewRecorder()

	handlers.PingHandler(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Handler returned the wrong response code: got %v expected %v",
			status, http.StatusOK)
	}

	expectedContentType := "application/json"
	if contentType := rr.Header().Get("Content-Type"); contentType != expectedContentType {
		t.Errorf("handler returned wrong Content-Type header: got %v want %v",
			contentType, expectedContentType)
	}

	var responseBody map[string]string
	err = json.Unmarshal(rr.Body.Bytes(), &responseBody)
	if err != nil {
		t.Fatalf("could not unmarshal response body: %v", err)
	}

	expectedMessage := "he is risen"
	if msg, ok := responseBody["message"]; !ok || msg != expectedMessage {
		t.Errorf("handler returned unexpected message: got %v want %v",
			msg, expectedMessage)
	}

}
