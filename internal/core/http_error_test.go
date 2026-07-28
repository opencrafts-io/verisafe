package core

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// These lock the exact response bytes HandleError produces for each sentinel.
// The handler migration routes every error through this function, so a change
// here silently rewrites error responses across the whole API. Asserting on the
// raw body string rather than with JSONEq is deliberate: the trailing newline
// json.Encoder emits is part of what clients receive today.

func TestHandleError_MapsSentinelsToStatusAndBody(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode int
		wantBody string
	}{
		{
			name:     "invalid input is 400 and echoes the message",
			err:      ErrInvalidInput,
			wantCode: http.StatusBadRequest,
			wantBody: `{"error":"the provided input is invalid or malformed"}` + "\n",
		},
		{
			name:     "not found is 404",
			err:      ErrNotFound,
			wantCode: http.StatusNotFound,
			wantBody: `{"error":"the requested resource was not found"}` + "\n",
		},
		{
			name:     "unauthorized is 401",
			err:      ErrUnauthorized,
			wantCode: http.StatusUnauthorized,
			wantBody: `{"error":"authentication is required to access this resource"}` + "\n",
		},
		{
			name:     "forbidden is 403",
			err:      ErrForbidden,
			wantCode: http.StatusForbidden,
			wantBody: `{"error":"you do not have permission to perform this action"}` + "\n",
		},
		{
			name:     "conflict is 409",
			err:      ErrConflict,
			wantCode: http.StatusConflict,
			wantBody: `{"error":"a conflict occurred with the current state of the resource"}` + "\n",
		},
		{
			name:     "unavailable is 503",
			err:      ErrUnavailable,
			wantCode: http.StatusServiceUnavailable,
			wantBody: `{"error":"an upstream dependency is temporarily unavailable"}` + "\n",
		},
		{
			// Internal is the one sentinel whose message is NOT echoed: the
			// wrapped detail could carry a driver error or a query fragment.
			name:     "internal is 500 and hides the detail",
			err:      fmt.Errorf("%w: connection refused to 10.0.0.4:5432", ErrInternal),
			wantCode: http.StatusInternalServerError,
			wantBody: `{"error":"something went wrong"}` + "\n",
		},
		{
			name:     "an unrecognised error is 500 and hides the detail",
			err:      errors.New("some error nobody classified"),
			wantCode: http.StatusInternalServerError,
			wantBody: `{"error":"something went wrong"}` + "\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()

			HandleError(rr, tc.err)

			assert.Equal(t, tc.wantCode, rr.Code)
			assert.Equal(t, tc.wantBody, rr.Body.String())
			assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
		})
	}
}

// Wrapping must not change the mapping: services wrap sentinels with context
// (fmt.Errorf("%w: ...")) and handlers rely on errors.Is seeing through it.
func TestHandleError_SeesThroughWrapping(t *testing.T) {
	rr := httptest.NewRecorder()

	err := fmt.Errorf("loading role: %w", fmt.Errorf("%w: no such row", ErrNotFound))
	HandleError(rr, err)

	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Equal(t, `{"error":"loading role: the requested resource was not found: no such row"}`+"\n", rr.Body.String())
}

func TestAppHandler_WritesNothingWhenHandlerSucceeds(t *testing.T) {
	rr := httptest.NewRecorder()

	AppHandler(func(w http.ResponseWriter, r *http.Request) error {
		WriteJSON(w, http.StatusCreated, map[string]string{"id": "abc"})
		return nil
	}).ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/", nil))

	assert.Equal(t, http.StatusCreated, rr.Code)
	assert.Equal(t, `{"id":"abc"}`+"\n", rr.Body.String())
}

func TestAppHandler_RoutesReturnedErrorThroughHandleError(t *testing.T) {
	rr := httptest.NewRecorder()

	AppHandler(func(w http.ResponseWriter, r *http.Request) error {
		return fmt.Errorf("%w: id is not a uuid", ErrInvalidInput)
	}).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Equal(
		t,
		`{"error":"the provided input is invalid or malformed: id is not a uuid"}`+"\n",
		rr.Body.String(),
	)
}
