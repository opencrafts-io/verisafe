package billing

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	billingSvc "github.com/opencrafts-io/verisafe/internal/service/billing"
)

type OrderHandler struct {
	DB      core.IDBProvider
	Logger  *slog.Logger
	Cfg     *config.Config
	Cacher  core.Cacher
	Service func(repository.Querier) billingSvc.OrderService
}

const (
	msgFetchOrdersFailed      = "Failed to fetch orders."
	msgCreateOrderFailed      = "Failed to create order."
	msgFetchOrderFailed       = "Failed to fetch order."
	msgUpdateOrderFailed      = "Failed to update order."
	msgCancelOrderFailed      = "Failed to cancel order."
	msgRecalculateOrderFailed = "Failed to recalculate order totals."
	msgOrderNotFound          = "Order not found."
	msgOrderNotCancellable    = "Order cannot be cancelled."
)

func (oh *OrderHandler) svc(db repository.DBTX) billingSvc.OrderService {
	if oh.Service != nil {
		return oh.Service(repository.New(db))
	}

	return billingSvc.NewOrderService(
		repository.New(db),
		oh.Logger,
	)
}

func (oh *OrderHandler) RegisterHandlers(router core.Router) {
	router.Handle(
		"POST /orders",
		middleware.CreateStack(
			middleware.IsAuthenticated(
				oh.Cfg,
				oh.DB,
				oh.Cacher,
				oh.Logger,
			),
			middleware.HasPermission([]string{"create:order:any"}),
		)(core.AppHandler(oh.CreateOrder)),
	)

	router.Handle(
		"GET /orders",
		middleware.CreateStack(
			middleware.IsAuthenticated(
				oh.Cfg,
				oh.DB,
				oh.Cacher,
				oh.Logger,
			),
			middleware.HasPermission([]string{"read:order:any"}),
		)(core.AppHandler(oh.ListOrders)),
	)

	router.Handle(
		"GET /orders/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(
				oh.Cfg,
				oh.DB,
				oh.Cacher,
				oh.Logger,
			),
			middleware.HasPermission([]string{"read:order:any"}),
		)(core.AppHandler(oh.GetOrder)),
	)

	router.Handle(
		"PATCH /orders/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(
				oh.Cfg,
				oh.DB,
				oh.Cacher,
				oh.Logger,
			),
			middleware.HasPermission([]string{"update:order:any"}),
		)(core.AppHandler(oh.UpdateOrder)),
	)

	router.Handle(
		"POST /orders/{id}/cancellation",
		middleware.CreateStack(
			middleware.IsAuthenticated(
				oh.Cfg,
				oh.DB,
				oh.Cacher,
				oh.Logger,
			),
			middleware.HasPermission([]string{"update:order:any"}),
		)(core.AppHandler(oh.CancelOrder)),
	)

	router.Handle(
		"POST /orders/{id}/totals/recalculation",
		middleware.CreateStack(
			middleware.IsAuthenticated(
				oh.Cfg,
				oh.DB,
				oh.Cacher,
				oh.Logger,
			),
			middleware.HasPermission([]string{"update:order:any"}),
		)(core.AppHandler(oh.RecalculateOrderTotals)),
	)
}

// CreateOrder godoc
//
// @Summary      Create an order
// @Description  Creates a new billing order for the authenticated user.
// @Tags         orders
// @Accept       json
// @Produce      json
// @Param        order  body      billing.CreateOrder  true  "Order to create"
// @Success      201   {object}  billing.Order
// @Failure      400   {object}  core.APIError  "Invalid request body"
// @Failure      401   {object}  core.APIError  "Missing or invalid claims"
// @Failure      500   {object}  core.APIError  "Failed to create order"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /orders [post]
func (oh *OrderHandler) CreateOrder(
	w http.ResponseWriter,
	r *http.Request,
) error {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return core.Public(core.ErrUnauthorized, msgAuthRequired)
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		oh.Logger.Error(
			"Error while parsing user id",
			slog.Any("error", err),
		)
		return core.Public(core.ErrInternal, msgFetchAccountFailed)
	}

	var req billingSvc.CreateOrder
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		oh.Logger.Error(
			"Error while decoding request body",
			slog.Any("error", err),
		)
		return core.Public(core.ErrInvalidInput, msgInvalidBody)
	}

	req.UserID = userID

	order, err := core.InTx(
		r.Context(),
		oh.DB,
		func(tx pgx.Tx) (*billingSvc.Order, error) {
			order, err := oh.svc(tx).CreateOrder(
				r.Context(),
				req,
			)
			if err != nil {
				oh.Logger.Error(
					"Error while creating order",
					slog.Any("error", err),
				)
				return nil, core.Public(
					core.ErrInternal,
					msgCreateOrderFailed,
				)
			}

			return order, nil
		},
	)
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusCreated, order)
	return nil
}

// GetOrder godoc
//
// @Summary      Get an order
// @Description  Retrieves an order by its identifier.
// @Tags         orders
// @Produce      json
// @Param        id  path      string  true  "Order ID"
// @Success      200 {object}  billing.Order
// @Failure      401 {object}  core.APIError  "Missing or invalid claims"
// @Failure      404 {object}  core.APIError  "Order not found"
// @Failure      500 {object}  core.APIError  "Failed to fetch order"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /orders/{id} [get]
func (oh *OrderHandler) GetOrder(
	w http.ResponseWriter,
	r *http.Request,
) error {
	id := r.PathValue("id")

	order, err := core.InTx(
		r.Context(),
		oh.DB,
		func(tx pgx.Tx) (*billingSvc.Order, error) {
			return oh.svc(tx).GetOrder(
				r.Context(),
				billingSvc.GetOrder{
					ID: id,
				},
			)
		},
	)
	if err != nil {
		if errors.Is(err, billingSvc.ErrOrderNotFound) {
			return core.Public(
				core.ErrNotFound,
				msgOrderNotFound,
			)
		}

		oh.Logger.Error(
			"Error while fetching order",
			slog.Any("error", err),
			slog.String("order_id", id),
		)

		return core.Public(
			core.ErrInternal,
			msgFetchOrderFailed,
		)
	}

	core.WriteJSON(w, http.StatusOK, order)
	return nil
}

// ListOrders godoc
//
// @Summary      List orders
// @Description  Lists orders with optional status and pagination filters.
// @Tags         orders
// @Produce      json
// @Param        status  query  string  false  "Order status"
// @Param        page    query  int     false  "Page number"
// @Param        page_size query int    false "Number of orders per page"
// @Success      200 {array} billing.Order
// @Failure      400 {object} core.APIError "Invalid query parameter"
// @Failure      401 {object} core.APIError "Missing or invalid claims"
// @Failure      500 {object} core.APIError "Failed to fetch orders"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /orders [get]
func (oh *OrderHandler) ListOrders(
	w http.ResponseWriter,
	r *http.Request,
) error {
	query := r.URL.Query()

	pageSize := int32(20)
	pageOffset := int32(0)

	if value := query.Get("page_size"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 32)
		if err != nil || parsed <= 0 {
			return core.Public(
				core.ErrInvalidInput,
				msgInvalidQueryParam,
			)
		}

		pageSize = int32(parsed)
	}

	if value := query.Get("page"); value != "" {
		page, err := strconv.ParseInt(value, 10, 32)
		if err != nil || page <= 0 {
			return core.Public(
				core.ErrInvalidInput,
				msgInvalidQueryParam,
			)
		}

		pageOffset = (int32(page) - 1) * pageSize
	}

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return core.Public(
			core.ErrUnauthorized,
			msgAuthRequired,
		)
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		oh.Logger.Error(
			"Error while parsing user id",
			slog.Any("error", err),
		)
		return core.Public(
			core.ErrInternal,
			msgFetchAccountFailed,
		)
	}

	orders, err := core.InTx(
		r.Context(),
		oh.DB,
		func(tx pgx.Tx) ([]billingSvc.Order, error) {
			if status := query.Get("status"); status != "" {
				return oh.svc(tx).ListOrdersByStatus(
					r.Context(),
					billingSvc.ListOrdersByStatus{
						Status:     status,
						PageSize:   pageSize,
						PageOffset: pageOffset,
					},
				)
			}

			return oh.svc(tx).ListOrdersByUser(
				r.Context(),
				billingSvc.ListOrdersByUser{
					UserID:     userID,
					PageSize:   pageSize,
					PageOffset: pageOffset,
				},
			)
		},
	)
	if err != nil {
		oh.Logger.Error(
			"Error while fetching orders",
			slog.Any("error", err),
			slog.String("user_id", userID.String()),
		)

		return core.Public(
			core.ErrInternal,
			msgFetchOrdersFailed,
		)
	}

	core.WriteJSON(w, http.StatusOK, orders)
	return nil
}

// UpdateOrder godoc
//
// @Summary      Update an order
// @Description  Updates mutable fields of an order.
// @Tags         orders
// @Accept       json
// @Produce      json
// @Param        id     path      string              true  "Order ID"
// @Param        order  body      billing.UpdateOrder true  "Order updates"
// @Success      200   {object}  billing.Order
// @Failure      400   {object}  core.APIError  "Invalid request body"
// @Failure      401   {object}  core.APIError  "Missing or invalid claims"
// @Failure      404   {object}  core.APIError  "Order not found"
// @Failure      500   {object}  core.APIError  "Failed to update order"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /orders/{id} [patch]
func (oh *OrderHandler) UpdateOrder(
	w http.ResponseWriter,
	r *http.Request,
) error {
	id := r.PathValue("id")

	var req billingSvc.UpdateOrder
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		oh.Logger.Error(
			"Error while decoding request body",
			slog.Any("error", err),
		)
		return core.Public(
			core.ErrInvalidInput,
			msgInvalidBody,
		)
	}

	req.ID = id

	order, err := core.InTx(
		r.Context(),
		oh.DB,
		func(tx pgx.Tx) (*billingSvc.Order, error) {
			return oh.svc(tx).UpdateOrder(
				r.Context(),
				req,
			)
		},
	)
	if err != nil {
		if errors.Is(err, billingSvc.ErrOrderNotFound) {
			return core.Public(
				core.ErrNotFound,
				msgOrderNotFound,
			)
		}

		oh.Logger.Error(
			"Error while updating order",
			slog.Any("error", err),
			slog.String("order_id", id),
		)

		return core.Public(
			core.ErrInternal,
			msgUpdateOrderFailed,
		)
	}

	core.WriteJSON(w, http.StatusOK, order)
	return nil
}

// CancelOrder godoc
//
// @Summary      Cancel an order
// @Description  Cancels an order if it is currently cancellable.
// @Tags         orders
// @Produce      json
// @Param        id  path      string  true  "Order ID"
// @Success      200 {object} billing.Order
// @Failure      401 {object} core.APIError "Missing or invalid claims"
// @Failure      404 {object} core.APIError "Order not found or cannot be cancelled"
// @Failure      500 {object} core.APIError "Failed to cancel order"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /orders/{id}/cancellation [post]
func (oh *OrderHandler) CancelOrder(
	w http.ResponseWriter,
	r *http.Request,
) error {
	id := r.PathValue("id")

	order, err := core.InTx(
		r.Context(),
		oh.DB,
		func(tx pgx.Tx) (*billingSvc.Order, error) {
			return oh.svc(tx).CancelOrder(
				r.Context(),
				billingSvc.CancelOrder{
					ID: id,
				},
			)
		},
	)
	if err != nil {
		if errors.Is(err, billingSvc.ErrOrderNotCancellable) {
			return core.Public(
				core.ErrInvalidInput,
				msgOrderNotCancellable,
			)
		}

		oh.Logger.Error(
			"Error while cancelling order",
			slog.Any("error", err),
			slog.String("order_id", id),
		)

		return core.Public(
			core.ErrInternal,
			msgCancelOrderFailed,
		)
	}

	core.WriteJSON(w, http.StatusOK, order)
	return nil
}

// RecalculateOrderTotals godoc
//
// @Summary      Recalculate order totals
// @Description  Recalculates the subtotal, discount, tax, and total for an order.
// @Tags         orders
// @Produce      json
// @Param        id  path      string  true  "Order ID"
// @Success      204
// @Failure      401 {object} core.APIError "Missing or invalid claims"
// @Failure      500 {object} core.APIError "Failed to recalculate order totals"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /orders/{id}/totals/recalculation [post]
func (oh *OrderHandler) RecalculateOrderTotals(
	w http.ResponseWriter,
	r *http.Request,
) error {
	id := r.PathValue("id")

	err := core.InTxDo(
		r.Context(),
		oh.DB,
		func(tx pgx.Tx) error {
			return oh.svc(tx).RecalculateOrderTotals(
				r.Context(),
				billingSvc.RecalculateOrderTotals{
					ID: id,
				},
			)
		},
	)
	if err != nil {
		oh.Logger.Error(
			"Error while recalculating order totals",
			slog.Any("error", err),
			slog.String("order_id", id),
		)

		return core.Public(
			core.ErrInternal,
			msgRecalculateOrderFailed,
		)
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}
