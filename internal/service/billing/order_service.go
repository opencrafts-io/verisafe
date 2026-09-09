package billing

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jinzhu/copier"

	"github.com/opencrafts-io/verisafe/internal/repository"
)

var (
	ErrOrderNotFound       = errors.New("order not found")
	ErrOrderNotCancellable = errors.New("order cannot be cancelled")
)

type OrderService interface {
	CreateOrder(
		ctx context.Context,
		req CreateOrder,
	) (*Order, error)

	GetOrder(
		ctx context.Context,
		req GetOrder,
	) (*Order, error)

	GetUserOrder(
		ctx context.Context,
		req GetUserOrder,
	) (*Order, error)

	ListOrdersByUser(
		ctx context.Context,
		req ListOrdersByUser,
	) ([]Order, error)

	ListOrdersByStatus(
		ctx context.Context,
		req ListOrdersByStatus,
	) ([]Order, error)

	UpdateOrder(
		ctx context.Context,
		req UpdateOrder,
	) (*Order, error)

	CancelOrder(
		ctx context.Context,
		req CancelOrder,
	) (*Order, error)

	MarkOrderPaid(
		ctx context.Context,
		req MarkOrderPaid,
	) (*Order, error)

	RecalculateOrderTotals(
		ctx context.Context,
		req RecalculateOrderTotals,
	) error
}

type orderService struct {
	querier repository.Querier
	logger  *slog.Logger
}

func NewOrderService(
	querier repository.Querier,
	logger *slog.Logger,
) OrderService {
	return &orderService{
		querier: querier,
		logger:  logger,
	}
}

func (s *orderService) CreateOrder(
	ctx context.Context,
	req CreateOrder,
) (*Order, error) {
	row, err := s.querier.CreateOrder(ctx, repository.CreateOrderParams{
		UserID:    req.UserID,
		Currency:  req.Currency,
		Metadata:  req.Metadata,
		ExpiresAt: req.ExpiresAt,
	})
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"failed to create order",
			"error", err,
			"user_id", req.UserID,
		)

		return nil, err
	}

	order := s.mapOrder(row)
	return &order, nil
}

func (s *orderService) GetOrder(
	ctx context.Context,
	req GetOrder,
) (*Order, error) {
	row, err := s.querier.GetOrder(ctx, req.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrOrderNotFound
		}

		s.logger.ErrorContext(
			ctx,
			"failed to retrieve order",
			"error", err,
			"order_id", req.ID,
		)

		return nil, err
	}

	order := s.mapOrder(row)
	return &order, nil
}

func (s *orderService) GetUserOrder(
	ctx context.Context,
	req GetUserOrder,
) (*Order, error) {
	row, err := s.querier.GetUserOrder(
		ctx,
		repository.GetUserOrderParams{
			ID:     req.ID,
			UserID: req.UserID,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrOrderNotFound
		}

		s.logger.ErrorContext(
			ctx,
			"failed to retrieve user order",
			"error", err,
			"order_id", req.ID,
			"user_id", req.UserID,
		)

		return nil, err
	}

	order := s.mapOrder(row)
	return &order, nil
}

func (s *orderService) ListOrdersByUser(
	ctx context.Context,
	req ListOrdersByUser,
) ([]Order, error) {
	rows, err := s.querier.ListOrdersByUser(
		ctx,
		repository.ListOrdersByUserParams{
			UserID:     req.UserID,
			PageSize:   req.PageSize,
			PageOffset: req.PageOffset,
		},
	)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"failed to list user orders",
			"error", err,
			"user_id", req.UserID,
		)

		return nil, err
	}

	orders := make([]Order, 0, len(rows))

	for _, row := range rows {
		orders = append(orders, s.mapOrder(row))
	}

	return orders, nil
}

func (s *orderService) ListOrdersByStatus(
	ctx context.Context,
	req ListOrdersByStatus,
) ([]Order, error) {
	rows, err := s.querier.ListOrdersByStatus(
		ctx,
		repository.ListOrdersByStatusParams{
			Status:     repository.OrderStatus(req.Status),
			PageSize:   req.PageSize,
			PageOffset: req.PageOffset,
		},
	)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"failed to list orders by status",
			"error", err,
			"status", req.Status,
		)

		return nil, err
	}

	orders := make([]Order, 0, len(rows))

	for _, row := range rows {
		orders = append(orders, s.mapOrder(row))
	}

	return orders, nil
}

func (s *orderService) UpdateOrder(
	ctx context.Context,
	req UpdateOrder,
) (*Order, error) {
	row, err := s.querier.UpdateOrder(
		ctx,
		repository.UpdateOrderParams{
			ID:        req.ID,
			Currency:  req.Currency,
			Metadata:  req.Metadata,
			ExpiresAt: req.ExpiresAt,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrOrderNotFound
		}

		s.logger.ErrorContext(
			ctx,
			"failed to update order",
			"error", err,
			"order_id", req.ID,
		)

		return nil, err
	}

	order := s.mapOrder(row)
	return &order, nil
}

func (s *orderService) CancelOrder(
	ctx context.Context,
	req CancelOrder,
) (*Order, error) {
	row, err := s.querier.CancelOrder(ctx, req.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrOrderNotCancellable
		}

		s.logger.ErrorContext(
			ctx,
			"failed to cancel order",
			"error", err,
			"order_id", req.ID,
		)

		return nil, err
	}

	order := s.mapOrder(row)
	return &order, nil
}

func (s *orderService) MarkOrderPaid(
	ctx context.Context,
	req MarkOrderPaid,
) (*Order, error) {
	row, err := s.querier.MarkOrderPaid(ctx, req.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrOrderNotFound
		}

		s.logger.ErrorContext(
			ctx,
			"failed to mark order as paid",
			"error", err,
			"order_id", req.ID,
		)

		return nil, err
	}
	order := s.mapOrder(row)
	return &order, nil
}

func (s *orderService) RecalculateOrderTotals(
	ctx context.Context,
	req RecalculateOrderTotals,
) error {
	err := s.querier.RecalculateOrderTotals(ctx, req.ID)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"failed to recalculate order totals",
			"error", err,
			"order_id", req.ID,
		)

		return err
	}

	return nil
}

func (s *orderService) mapOrder(row repository.Order) Order {
	var order Order
	if err := copier.Copy(&order, &row); err != nil {
		s.logger.Error(
			"failed to map order to DTO",
			slog.String("order_id", row.ID),
			slog.String("error", err.Error()),
		)
	}

	return order
}
