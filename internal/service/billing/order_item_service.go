package billing

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jinzhu/copier"

	"github.com/opencrafts-io/verisafe/internal/repository"
)

var ErrOrderItemNotFound = errors.New("order item not found")

type OrderItemService interface {
	CreateOrderItem(
		ctx context.Context,
		req CreateOrderItem,
	) (*OrderItem, error)

	GetOrderItem(
		ctx context.Context,
		req GetOrderItem,
	) (*OrderItem, error)

	ListOrderItemsByOrder(
		ctx context.Context,
		req ListOrderItemsByOrder,
	) ([]OrderItem, error)

	UpdateOrderItem(
		ctx context.Context,
		req UpdateOrderItem,
	) (*OrderItem, error)

	DeleteOrderItem(
		ctx context.Context,
		req DeleteOrderItem,
	) error

	DeleteOrderItemsByOrder(
		ctx context.Context,
		req DeleteOrderItemsByOrder,
	) error
}

type orderItemService struct {
	querier repository.Querier
	logger  *slog.Logger
}

func NewOrderItemService(
	querier repository.Querier,
	logger *slog.Logger,
) OrderItemService {
	return &orderItemService{
		querier: querier,
		logger:  logger,
	}
}

func (s *orderItemService) CreateOrderItem(
	ctx context.Context,
	req CreateOrderItem,
) (*OrderItem, error) {
	row, err := s.querier.CreateOrderItem(
		ctx,
		repository.CreateOrderItemParams{
			OrderID:   req.OrderID,
			AddedBy:   req.AddedBy,
			UnitPrice: req.UnitPrice,
			Discount:  req.Discount,
			Quantity:  req.Quantity,
			Tax:       req.Tax,
			PlanID:    req.PlanID,
		},
	)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"failed to create order item",
			"error", err,
			"order_id", req.OrderID,
		)

		return nil, err
	}

	item := s.mapOrderItem(row)
	return &item, nil
}

func (s *orderItemService) GetOrderItem(
	ctx context.Context,
	req GetOrderItem,
) (*OrderItem, error) {
	row, err := s.querier.GetOrderItem(ctx, req.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrOrderItemNotFound
		}

		s.logger.ErrorContext(
			ctx,
			"failed to retrieve order item",
			"error", err,
			"order_item_id", req.ID,
		)

		return nil, err
	}

	item := s.mapOrderItem(row)
	return &item, nil
}

func (s *orderItemService) ListOrderItemsByOrder(
	ctx context.Context,
	req ListOrderItemsByOrder,
) ([]OrderItem, error) {
	rows, err := s.querier.ListOrderItemsByOrder(
		ctx,
		req.OrderID,
	)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"failed to list order items",
			"error", err,
			"order_id", req.OrderID,
		)

		return nil, err
	}

	items := make([]OrderItem, 0, len(rows))

	for _, row := range rows {
		items = append(items, s.mapOrderItem(row))
	}

	return items, nil
}

func (s *orderItemService) UpdateOrderItem(
	ctx context.Context,
	req UpdateOrderItem,
) (*OrderItem, error) {
	row, err := s.querier.UpdateOrderItem(
		ctx,
		repository.UpdateOrderItemParams{
			ID:        req.ID,
			UnitPrice: req.UnitPrice,
			Discount:  req.Discount,
			Quantity:  req.Quantity,
			Tax:       req.Tax,
			PlanID:    req.PlanID,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrOrderItemNotFound
		}

		s.logger.ErrorContext(
			ctx,
			"failed to update order item",
			"error", err,
			"order_item_id", req.ID,
		)

		return nil, err
	}

	item := s.mapOrderItem(row)
	return &item, nil
}

func (s *orderItemService) DeleteOrderItem(
	ctx context.Context,
	req DeleteOrderItem,
) error {
	_, err := s.querier.DeleteOrderItem(ctx, req.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrOrderItemNotFound
		}

		s.logger.ErrorContext(
			ctx,
			"failed to delete order item",
			"error", err,
			"order_item_id", req.ID,
		)

		return err
	}

	return nil
}

func (s *orderItemService) DeleteOrderItemsByOrder(
	ctx context.Context,
	req DeleteOrderItemsByOrder,
) error {
	err := s.querier.DeleteOrderItemsByOrder(
		ctx,
		req.OrderID,
	)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"failed to delete order items",
			"error", err,
			"order_id", req.OrderID,
		)

		return err
	}

	return nil
}

func (s *orderItemService) mapOrderItem(
	row repository.OrderItem,
) OrderItem {
	var item OrderItem

	if err := copier.Copy(&item, &row); err != nil {
		s.logger.Error(
			"failed to map order item to DTO",
			slog.String("order_item_id", row.ID.String()),
			slog.String("error", err.Error()),
		)
	}

	return item
}
