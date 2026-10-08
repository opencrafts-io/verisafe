package billing

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	billingSvc "github.com/opencrafts-io/verisafe/internal/service/billing"
)

type ChargeHandler struct {
	DB      core.IDBProvider
	Logger  *slog.Logger
	Cfg     *config.Config
	Cacher  core.Cacher
	Service billingSvc.ChargeService
}

const (
	msgChargeOrderFailed    = "Failed to charge order."
	msgOrderAlreadyPaid     = "Order has already been paid."
	msgOrderCancelled       = "Order has been cancelled."
	msgOrderNotChargeable   = "Order cannot be charged."
	msgPendingChargeAttempt = "A charge attempt is already pending for this order."
	msgChargeUnavailable    = "Charging is temporarily unavailable."
)

func (h *ChargeHandler) RegisterHandlers(router core.Router) {
	router.Handle(
		"POST /orders/{id}/charge",
		middleware.CreateStack(
			middleware.IsAuthenticated(h.Cfg, h.DB, h.Cacher, h.Logger),
			middleware.HasAnyPermission([]string{"update:order:own", "update:order:any"}),
		)(core.AppHandler(h.ChargeOrder)),
	)
}

// ChargeOrder godoc
//
// @Summary      Charge an order
// @Description  Requests an M-Pesa STK charge for an unpaid order.
// @Tags         orders
// @Accept       json
// @Produce      json
// @Param        id      path  string                  true  "Order ID"
// @Param        request body  billingSvc.ChargeOrder  true  "Payer phone number"
// @Success      202 {object} billingSvc.ChargeAttempt
// @Failure      400 {object} core.APIError "Invalid request body or order"
// @Failure      401 {object} core.APIError "Missing or invalid claims"
// @Failure      404 {object} core.APIError "Order not found"
// @Failure      409 {object} core.APIError "Order cannot be charged"
// @Failure      503 {object} core.APIError "RabbitMQ unavailable"
// @Failure      500 {object} core.APIError "Failed to charge order"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /orders/{id}/charge [post]
func (h *ChargeHandler) ChargeOrder(
	w http.ResponseWriter,
	r *http.Request,
) error {
	var req billingSvc.ChargeOrder
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.Logger.Error("error while decoding charge request", slog.Any("error", err))
		return core.Public(core.ErrInvalidInput, msgInvalidBody)
	}

	req.OrderID = r.PathValue("id")
	if err := h.authorizeChargeOrder(r, req.OrderID); err != nil {
		if errors.Is(err, billingSvc.ErrOrderNotFound) {
			return core.Public(core.ErrNotFound, msgOrderNotFound)
		}
		return err
	}

	attempt, err := h.Service.ChargeOrder(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, billingSvc.ErrOrderNotFound):
			return core.Public(core.ErrNotFound, msgOrderNotFound)
		case errors.Is(err, billingSvc.ErrOrderAlreadyPaid):
			return core.Public(core.ErrConflict, msgOrderAlreadyPaid)
		case errors.Is(err, billingSvc.ErrOrderCancelled):
			return core.Public(core.ErrConflict, msgOrderCancelled)
		case errors.Is(err, billingSvc.ErrPendingChargeAttempt):
			return core.Public(core.ErrConflict, msgPendingChargeAttempt)
		case errors.Is(err, billingSvc.ErrInvalidChargeAmount),
			errors.Is(err, billingSvc.ErrInvalidPayerPhone),
			errors.Is(err, billingSvc.ErrOrderNotChargeable):
			return core.Public(core.ErrInvalidInput, msgOrderNotChargeable)
		case errors.Is(err, billingSvc.ErrChargePublishFailed):
			return core.Public(core.ErrUnavailable, msgChargeUnavailable)
		default:
			h.Logger.Error("error while charging order",
				slog.String("order_id", req.OrderID),
				slog.Any("error", err),
			)
			return core.Public(core.ErrInternal, msgChargeOrderFailed)
		}
	}

	core.WriteJSON(w, http.StatusAccepted, attempt)
	return nil
}

func (h *ChargeHandler) authorizeChargeOrder(
	r *http.Request,
	orderID string,
) error {
	if middleware.HasContextPermission(r.Context(), "update:order:any") {
		return nil
	}

	userID, err := orderCallerID(r)
	if err != nil {
		return err
	}

	return core.InTxDo(
		r.Context(),
		h.DB,
		func(tx pgx.Tx) error {
			_, err := billingSvc.NewOrderService(
				repository.New(tx),
				h.Logger,
			).GetUserOrder(
				r.Context(),
				billingSvc.GetUserOrder{
					ID:     orderID,
					UserID: userID,
				},
			)
			return err
		},
	)
}
