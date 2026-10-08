package billing

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	billingSvc "github.com/opencrafts-io/verisafe/internal/service/billing"
)

type OrderItemHandler struct {
	DB      core.IDBProvider
	Logger  *slog.Logger
	Cfg     *config.Config
	Cacher  core.Cacher
	Service func(repository.Querier) billingSvc.OrderItemService
}

const (
	msgCreateOrderItemFailed  = "failed to create order item"
	msgFetchOrderItemFailed   = "failed to fetch order item"
	msgFetchOrderItemsFailed  = "failed to fetch order items"
	msgUpdateOrderItemFailed  = "failed to update order item"
	msgDeleteOrderItemFailed  = "failed to delete order item"
	msgDeleteOrderItemsFailed = "failed to delete order items"
	msgOrderItemNotFound      = "order item not found"
)

func (oih *OrderItemHandler) svc(
	db repository.DBTX,
) billingSvc.OrderItemService {
	if oih.Service != nil {
		return oih.Service(repository.New(db))
	}

	return billingSvc.NewOrderItemService(
		repository.New(db),
		oih.Logger,
	)
}

func (oih *OrderItemHandler) RegisterHandlers(router core.Router) {
	router.Handle(
		"POST /orders/{order_id}/items",
		middleware.CreateStack(
			middleware.IsAuthenticated(
				oih.Cfg,
				oih.DB,
				oih.Cacher,
				oih.Logger,
			),
			middleware.HasAnyPermission([]string{"create:order-item:own", "create:order-item:any"}),
		)(core.AppHandler(oih.CreateOrderItem)),
	)

	router.Handle(
		"GET /orders/{order_id}/items",
		middleware.CreateStack(
			middleware.IsAuthenticated(
				oih.Cfg,
				oih.DB,
				oih.Cacher,
				oih.Logger,
			),
			middleware.HasAnyPermission([]string{"read:order-item:own", "read:order-item:any"}),
		)(core.AppHandler(oih.ListOrderItemsByOrder)),
	)

	router.Handle(
		"GET /orders/{order_id}/items/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(
				oih.Cfg,
				oih.DB,
				oih.Cacher,
				oih.Logger,
			),
			middleware.HasAnyPermission([]string{"read:order-item:own", "read:order-item:any"}),
		)(core.AppHandler(oih.GetOrderItem)),
	)

	router.Handle(
		"PATCH /orders/{order_id}/items/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(
				oih.Cfg,
				oih.DB,
				oih.Cacher,
				oih.Logger,
			),
			middleware.HasAnyPermission([]string{"update:order-item:own", "update:order-item:any"}),
		)(core.AppHandler(oih.UpdateOrderItem)),
	)

	router.Handle(
		"DELETE /orders/{order_id}/items/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(
				oih.Cfg,
				oih.DB,
				oih.Cacher,
				oih.Logger,
			),
			middleware.HasAnyPermission([]string{"delete:order-item:own", "delete:order-item:any"}),
		)(core.AppHandler(oih.DeleteOrderItem)),
	)

	router.Handle(
		"DELETE /orders/{order_id}/items",
		middleware.CreateStack(
			middleware.IsAuthenticated(
				oih.Cfg,
				oih.DB,
				oih.Cacher,
				oih.Logger,
			),
			middleware.HasAnyPermission([]string{"delete:order-item:own", "delete:order-item:any"}),
		)(core.AppHandler(oih.DeleteOrderItemsByOrder)),
	)
}

// CreateOrderItem godoc
//
// @Summary      Create an order item
// @Description  Adds an item to an order for the authenticated user.
// @Tags         orders
// @Accept       json
// @Produce      json
// @Param        order_id path      string                  true  "Order ID"
// @Param        item     body      billing.CreateOrderItem true  "Order item to create"
// @Success      201      {object}  billing.OrderItem
// @Failure      400      {object}  core.APIError "Invalid request body"
// @Failure      401      {object}  core.APIError "Missing or invalid claims"
// @Failure      500      {object}  core.APIError "Failed to create order item"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /orders/{order_id}/items [post]
func (oih *OrderItemHandler) CreateOrderItem(
	w http.ResponseWriter,
	r *http.Request,
) error {
	var req billingSvc.CreateOrderItem

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return core.ErrInvalidInput
	}

	orderID := r.PathValue("order_id")
	req.OrderID = orderID

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return core.Public(core.ErrUnauthorized, msgAuthRequired)
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return core.ErrInvalidInput
	}

	req.AddedBy = userID

	item, err := core.InTx(
		r.Context(),
		oih.DB,
		func(tx pgx.Tx) (*billingSvc.OrderItem, error) {
			if err := oih.authorizeOwnOrderItem(
				r,
				tx,
				orderID,
				"create:order-item:any",
			); err != nil {
				return nil, err
			}
			return oih.svc(tx).CreateOrderItem(
				r.Context(),
				req,
			)
		},
	)
	if err != nil {
		oih.Logger.ErrorContext(
			r.Context(),
			"failed to create order item",
			"error", err,
			"order_id", orderID,
		)

		return core.Public(core.ErrInternal, msgCreateOrderItemFailed)
	}

	core.WriteJSON(w, http.StatusCreated, item)
	return nil
}

// GetOrderItem godoc
//
// @Summary      Get an order item
// @Description  Retrieves an item from an order by its identifier.
// @Tags         orders
// @Produce      json
// @Param        order_id path      string true "Order ID"
// @Param        id       path      string true "Order item ID"
// @Success      200      {object}  billing.OrderItem
// @Failure      400      {object}  core.APIError "Invalid order item ID"
// @Failure      401      {object}  core.APIError "Missing or invalid claims"
// @Failure      404      {object}  core.APIError "Order item not found"
// @Failure      500      {object}  core.APIError "Failed to fetch order item"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /orders/{order_id}/items/{id} [get]
func (oih *OrderItemHandler) GetOrderItem(
	w http.ResponseWriter,
	r *http.Request,
) error {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return core.ErrInvalidInput
	}

	item, err := core.InTx(
		r.Context(),
		oih.DB,
		func(tx pgx.Tx) (*billingSvc.OrderItem, error) {
			if err := oih.authorizeOwnOrderItem(
				r,
				tx,
				r.PathValue("order_id"),
				"read:order-item:any",
			); err != nil {
				return nil, err
			}
			return oih.svc(tx).GetOrderItem(
				r.Context(),
				billingSvc.GetOrderItem{
					ID:      id,
					OrderID: r.PathValue("order_id"),
				},
			)
		},
	)
	if err != nil {
		if errors.Is(err, billingSvc.ErrOrderItemNotFound) {
			return core.Public(core.ErrNotFound, msgOrderItemNotFound)
		}

		oih.Logger.ErrorContext(
			r.Context(),
			"failed to fetch order item",
			"error", err,
			"order_item_id", id,
		)

		return core.Public(core.ErrInternal, msgFetchOrderItemFailed)
	}

	core.WriteJSON(w, http.StatusOK, item)
	return nil
}

// ListOrderItemsByOrder godoc
//
// @Summary      List order items
// @Description  Lists all items for an order.
// @Tags         orders
// @Produce      json
// @Param        order_id path     string true "Order ID"
// @Success      200      {array}  billing.OrderItem
// @Failure      401      {object} core.APIError "Missing or invalid claims"
// @Failure      500      {object} core.APIError "Failed to fetch order items"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /orders/{order_id}/items [get]
func (oih *OrderItemHandler) ListOrderItemsByOrder(
	w http.ResponseWriter,
	r *http.Request,
) error {
	orderID := r.PathValue("order_id")
	items, err := core.InTx(
		r.Context(),
		oih.DB,
		func(tx pgx.Tx) ([]billingSvc.OrderItem, error) {
			if err := oih.authorizeOwnOrderItem(
				r,
				tx,
				orderID,
				"read:order-item:any",
			); err != nil {
				return nil, err
			}
			return oih.svc(tx).ListOrderItemsByOrder(
				r.Context(),
				billingSvc.ListOrderItemsByOrder{
					OrderID: orderID,
				},
			)
		},
	)
	if err != nil {
		oih.Logger.ErrorContext(
			r.Context(),
			"failed to fetch order items",
			"error", err,
			"order_id", orderID,
		)

		return core.Public(core.ErrInternal, msgFetchOrderItemsFailed)
	}
	core.WriteJSON(w, http.StatusOK, items)
	return nil
}

// UpdateOrderItem godoc
//
// @Summary      Update an order item
// @Description  Updates mutable fields of an order item.
// @Tags         orders
// @Accept       json
// @Produce      json
// @Param        order_id path      string                  true "Order ID"
// @Param        id       path      string                  true "Order item ID"
// @Param        item     body      billing.UpdateOrderItem true "Order item updates"
// @Success      200      {object}  billing.OrderItem
// @Failure      400      {object}  core.APIError "Invalid request body or order item ID"
// @Failure      401      {object}  core.APIError "Missing or invalid claims"
// @Failure      404      {object}  core.APIError "Order item not found"
// @Failure      500      {object}  core.APIError "Failed to update order item"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /orders/{order_id}/items/{id} [patch]
func (oih *OrderItemHandler) UpdateOrderItem(
	w http.ResponseWriter,
	r *http.Request,
) error {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return core.ErrInvalidInput
	}

	var req billingSvc.UpdateOrderItem

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return core.ErrInvalidInput
	}

	req.ID = id
	req.OrderID = r.PathValue("order_id")

	item, err := core.InTx(
		r.Context(),
		oih.DB,
		func(tx pgx.Tx) (*billingSvc.OrderItem, error) {
			if err := oih.authorizeOwnOrderItem(
				r,
				tx,
				req.OrderID,
				"update:order-item:any",
			); err != nil {
				return nil, err
			}
			return oih.svc(tx).UpdateOrderItem(
				r.Context(),
				req,
			)
		},
	)
	if err != nil {
		if errors.Is(err, billingSvc.ErrOrderItemNotFound) {
			return core.Public(core.ErrNotFound, msgOrderItemNotFound)
		}

		oih.Logger.ErrorContext(
			r.Context(),
			"failed to update order item",
			"error", err,
			"order_item_id", id,
		)

		return core.Public(core.ErrInternal, msgUpdateOrderItemFailed)
	}

	core.WriteJSON(w, http.StatusOK, item)

	return nil
}

// DeleteOrderItem godoc
//
// @Summary      Delete an order item
// @Description  Deletes an item from an order.
// @Tags         orders
// @Produce      json
// @Param        order_id path string true "Order ID"
// @Param        id       path string true "Order item ID"
// @Success      204
// @Failure      400 {object} core.APIError "Invalid order item ID"
// @Failure      401 {object} core.APIError "Missing or invalid claims"
// @Failure      404 {object} core.APIError "Order item not found"
// @Failure      500 {object} core.APIError "Failed to delete order item"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /orders/{order_id}/items/{id} [delete]
func (oih *OrderItemHandler) DeleteOrderItem(
	w http.ResponseWriter,
	r *http.Request,
) error {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return core.ErrInvalidInput
	}

	err = core.InTxDo(
		r.Context(),
		oih.DB,
		func(tx pgx.Tx) error {
			if err := oih.authorizeOwnOrderItem(
				r,
				tx,
				r.PathValue("order_id"),
				"delete:order-item:any",
			); err != nil {
				return err
			}
			return oih.svc(tx).DeleteOrderItem(
				r.Context(),
				billingSvc.DeleteOrderItem{
					ID:      id,
					OrderID: r.PathValue("order_id"),
				},
			)
		},
	)
	if err != nil {
		if errors.Is(err, billingSvc.ErrOrderItemNotFound) {
			return core.Public(core.ErrNotFound, msgOrderItemNotFound)
		}

		oih.Logger.ErrorContext(
			r.Context(),
			"failed to delete order item",
			"error", err,
			"order_item_id", id,
		)

		return core.Public(core.ErrInternal, msgDeleteOrderItemFailed)
	}

	core.NoContent(w)

	return nil
}

// DeleteOrderItemsByOrder godoc
//
// @Summary      Delete order items
// @Description  Deletes all items from an order.
// @Tags         orders
// @Produce      json
// @Param        order_id path string true "Order ID"
// @Success      204
// @Failure      401 {object} core.APIError "Missing or invalid claims"
// @Failure      500 {object} core.APIError "Failed to delete order items"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /orders/{order_id}/items [delete]
func (oih *OrderItemHandler) DeleteOrderItemsByOrder(
	w http.ResponseWriter,
	r *http.Request,
) error {
	orderID := r.PathValue("order_id")

	err := core.InTxDo(
		r.Context(),
		oih.DB,
		func(tx pgx.Tx) error {
			if err := oih.authorizeOwnOrderItem(
				r,
				tx,
				orderID,
				"delete:order-item:any",
			); err != nil {
				return err
			}
			return oih.svc(tx).DeleteOrderItemsByOrder(
				r.Context(),
				billingSvc.DeleteOrderItemsByOrder{
					OrderID: orderID,
				},
			)
		},
	)
	if err != nil {
		oih.Logger.ErrorContext(
			r.Context(),
			"failed to delete order items",
			"error", err,
			"order_id", orderID,
		)

		return core.Public(core.ErrInternal, msgDeleteOrderItemsFailed)
	}

	core.NoContent(w)
	return nil
}

func (oih *OrderItemHandler) authorizeOwnOrderItem(
	r *http.Request,
	tx pgx.Tx,
	orderID string,
	anyPermission string,
) error {
	if middleware.HasContextPermission(r.Context(), anyPermission) {
		return nil
	}

	userID, err := orderCallerID(r)
	if err != nil {
		return err
	}

	_, err = billingSvc.NewOrderService(
		repository.New(tx),
		oih.Logger,
	).GetUserOrder(
		r.Context(),
		billingSvc.GetUserOrder{
			ID:     orderID,
			UserID: userID,
		},
	)
	return err
}
