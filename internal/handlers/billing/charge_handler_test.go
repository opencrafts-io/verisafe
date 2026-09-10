package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opencrafts-io/verisafe/internal/core"
	billingSvc "github.com/opencrafts-io/verisafe/internal/service/billing"
)

type chargeServiceStub struct {
	attempt *billingSvc.ChargeAttempt
	err     error
	request billingSvc.ChargeOrder
}

func (s *chargeServiceStub) ChargeOrder(
	_ context.Context,
	request billingSvc.ChargeOrder,
) (*billingSvc.ChargeAttempt, error) {
	s.request = request
	return s.attempt, s.err
}

func (*chargeServiceStub) HandleChargeResult(context.Context, []byte) error {
	return nil
}

func TestChargeHandler_ChargeOrderUsesPathOrderID(t *testing.T) {
	service := &chargeServiceStub{attempt: &billingSvc.ChargeAttempt{
		ID:               uuid.New(),
		OrderID:          "ORD-path",
		Status:           "pending",
		PayerPhoneNumber: "254712345678",
		Amount:           5800,
	}}
	handler := &ChargeHandler{Service: service}
	req := httptest.NewRequest(
		http.MethodPost,
		"/orders/ORD-path/charge",
		bytes.NewBufferString(`{"order_id":"ORD-body","payer_phone_number":"254712345678"}`),
	)
	req.SetPathValue("id", "ORD-path")
	res := httptest.NewRecorder()

	core.AppHandler(handler.ChargeOrder).ServeHTTP(res, req)

	require.Equal(t, http.StatusAccepted, res.Code)
	assert.Equal(t, billingSvc.ChargeOrder{
		OrderID:          "ORD-path",
		PayerPhoneNumber: "254712345678",
	}, service.request)

	var body billingSvc.ChargeAttempt
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
	assert.Equal(t, service.attempt.ID, body.ID)
}

func TestChargeHandler_ChargeOrderMapsBusinessErrors(t *testing.T) {
	tests := []struct {
		name       string
		serviceErr error
		status     int
	}{
		{
			name:       "already paid",
			serviceErr: billingSvc.ErrOrderAlreadyPaid,
			status:     http.StatusConflict,
		},
		{
			name:       "pending attempt",
			serviceErr: billingSvc.ErrPendingChargeAttempt,
			status:     http.StatusConflict,
		},
		{
			name:       "publisher unavailable",
			serviceErr: billingSvc.ErrChargePublishFailed,
			status:     http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &ChargeHandler{Service: &chargeServiceStub{err: tt.serviceErr}}
			req := httptest.NewRequest(
				http.MethodPost,
				"/orders/ORD-001/charge",
				bytes.NewBufferString(`{"payer_phone_number":"254712345678"}`),
			)
			req.SetPathValue("id", "ORD-001")
			res := httptest.NewRecorder()

			core.AppHandler(handler.ChargeOrder).ServeHTTP(res, req)

			assert.Equal(t, tt.status, res.Code)
		})
	}
}
