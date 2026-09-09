package billing

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opencrafts-io/verisafe/internal/repository"
)

func newTestOrderService(q repository.Querier) *orderService {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	return NewOrderService(q, logger).(*orderService)
}

func (m *mockQuerier) CreateOrder(
	ctx context.Context,
	params repository.CreateOrderParams,
) (repository.Order, error) {
	args := m.Called(ctx, params)

	var order repository.Order
	if value := args.Get(0); value != nil {
		order = value.(repository.Order)
	}

	return order, args.Error(1)
}

func (m *mockQuerier) GetOrder(
	ctx context.Context,
	id string,
) (repository.Order, error) {
	args := m.Called(ctx, id)

	var order repository.Order
	if value := args.Get(0); value != nil {
		order = value.(repository.Order)
	}

	return order, args.Error(1)
}

func (m *mockQuerier) GetUserOrder(
	ctx context.Context,
	params repository.GetUserOrderParams,
) (repository.Order, error) {
	args := m.Called(ctx, params)

	var order repository.Order
	if value := args.Get(0); value != nil {
		order = value.(repository.Order)
	}

	return order, args.Error(1)
}

func (m *mockQuerier) ListOrdersByUser(
	ctx context.Context,
	params repository.ListOrdersByUserParams,
) ([]repository.Order, error) {
	args := m.Called(ctx, params)

	var orders []repository.Order
	if value := args.Get(0); value != nil {
		orders = value.([]repository.Order)
	}

	return orders, args.Error(1)
}

func (m *mockQuerier) ListOrdersByStatus(
	ctx context.Context,
	params repository.ListOrdersByStatusParams,
) ([]repository.Order, error) {
	args := m.Called(ctx, params)

	var orders []repository.Order
	if value := args.Get(0); value != nil {
		orders = value.([]repository.Order)
	}

	return orders, args.Error(1)
}

func (m *mockQuerier) UpdateOrder(
	ctx context.Context,
	params repository.UpdateOrderParams,
) (repository.Order, error) {
	args := m.Called(ctx, params)

	var order repository.Order
	if value := args.Get(0); value != nil {
		order = value.(repository.Order)
	}

	return order, args.Error(1)
}

func (m *mockQuerier) CancelOrder(
	ctx context.Context,
	id string,
) (repository.Order, error) {
	args := m.Called(ctx, id)

	var order repository.Order
	if value := args.Get(0); value != nil {
		order = value.(repository.Order)
	}

	return order, args.Error(1)
}

func (m *mockQuerier) MarkOrderPaid(
	ctx context.Context,
	id string,
) (repository.Order, error) {
	args := m.Called(ctx, id)

	var order repository.Order
	if value := args.Get(0); value != nil {
		order = value.(repository.Order)
	}

	return order, args.Error(1)
}

func (m *mockQuerier) RecalculateOrderTotals(
	ctx context.Context,
	id string,
) error {
	args := m.Called(ctx, id)

	return args.Error(0)
}

func testOrder() repository.Order {
	now := time.Now()

	return repository.Order{
		ID:        "ORD-001",
		UserID:    uuid.New(),
		Status:    repository.OrderStatusPending,
		Subtotal:  5000,
		Discount:  100,
		Tax:       900,
		Total:     5800,
		Currency:  "KES",
		Metadata:  []byte(`{"source":"test"}`),
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: ptrTime(now.Add(time.Hour)),
	}
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

func TestOrderService_CreateOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderService(q)

		userID := uuid.New()
		expiresAt := time.Now().Add(time.Hour)
		metadata := []byte(`{"source":"test"}`)

		req := CreateOrder{
			UserID:    userID,
			Currency:  "KES",
			Metadata:  metadata,
			ExpiresAt: &expiresAt,
		}

		row := testOrder()

		expectedParams := repository.CreateOrderParams{
			UserID:    req.UserID,
			Currency:  req.Currency,
			Metadata:  req.Metadata,
			ExpiresAt: req.ExpiresAt,
		}

		q.On(
			"CreateOrder",
			ctx,
			expectedParams,
		).Return(row, nil).Once()

		result, err := service.CreateOrder(ctx, req)

		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, row.ID, result.ID)
		assert.Equal(t, row.UserID, result.UserID)
		assert.Equal(t, string(row.Status), result.Status)
		assert.Equal(t, row.Subtotal, result.Subtotal)
		assert.Equal(t, row.Discount, result.Discount)
		assert.Equal(t, row.Tax, result.Tax)
		assert.Equal(t, row.Total, result.Total)
		assert.Equal(t, row.Currency, result.Currency)
		assert.Equal(t, row.Metadata, result.Metadata)
		assert.Equal(t, row.CreatedAt, result.CreatedAt)
		assert.Equal(t, row.UpdatedAt, result.UpdatedAt)
		assert.Equal(t, row.ExpiresAt, result.ExpiresAt)

		q.AssertExpectations(t)
	})

	t.Run("error", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderService(q)

		req := CreateOrder{
			UserID:   uuid.New(),
			Currency: "KES",
			Metadata: []byte(`{}`),
		}

		expectedParams := repository.CreateOrderParams{
			UserID:    req.UserID,
			Currency:  req.Currency,
			Metadata:  req.Metadata,
			ExpiresAt: req.ExpiresAt,
		}

		expectedErr := errors.New("database error")

		q.On(
			"CreateOrder",
			ctx,
			expectedParams,
		).Return(repository.Order{}, expectedErr).Once()

		result, err := service.CreateOrder(ctx, req)

		require.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, expectedErr)

		q.AssertExpectations(t)
	})
}

func TestOrderService_GetOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderService(q)

		row := testOrder()

		req := GetOrder{
			ID: row.ID,
		}

		q.On(
			"GetOrder",
			ctx,
			req.ID,
		).Return(row, nil).Once()

		result, err := service.GetOrder(ctx, req)

		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, row.ID, result.ID)
		assert.Equal(t, row.UserID, result.UserID)
		assert.Equal(t, string(row.Status), result.Status)
		assert.Equal(t, row.Subtotal, result.Subtotal)
		assert.Equal(t, row.Discount, result.Discount)
		assert.Equal(t, row.Tax, result.Tax)
		assert.Equal(t, row.Total, result.Total)
		assert.Equal(t, row.Currency, result.Currency)
		assert.Equal(t, row.Metadata, result.Metadata)
		assert.Equal(t, row.CreatedAt, result.CreatedAt)
		assert.Equal(t, row.UpdatedAt, result.UpdatedAt)
		assert.Equal(t, row.ExpiresAt, result.ExpiresAt)

		q.AssertExpectations(t)
	})

	t.Run("not found", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderService(q)

		req := GetOrder{
			ID: "ORD-001",
		}

		q.On(
			"GetOrder",
			ctx,
			req.ID,
		).Return(repository.Order{}, pgx.ErrNoRows).Once()

		result, err := service.GetOrder(ctx, req)

		require.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrOrderNotFound)

		q.AssertExpectations(t)
	})

	t.Run("error", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderService(q)

		req := GetOrder{
			ID: "ORD-001",
		}

		expectedErr := errors.New("database error")

		q.On(
			"GetOrder",
			ctx,
			req.ID,
		).Return(repository.Order{}, expectedErr).Once()

		result, err := service.GetOrder(ctx, req)

		require.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, expectedErr)

		q.AssertExpectations(t)
	})
}

func TestOrderService_GetUserOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderService(q)

		row := testOrder()

		req := GetUserOrder{
			ID:     row.ID,
			UserID: row.UserID,
		}

		expectedParams := repository.GetUserOrderParams{
			ID:     req.ID,
			UserID: req.UserID,
		}

		q.On(
			"GetUserOrder",
			ctx,
			expectedParams,
		).Return(row, nil).Once()

		result, err := service.GetUserOrder(ctx, req)

		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, row.ID, result.ID)
		assert.Equal(t, row.UserID, result.UserID)
		assert.Equal(t, string(row.Status), result.Status)
		assert.Equal(t, row.Total, result.Total)

		q.AssertExpectations(t)
	})

	t.Run("not found", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderService(q)

		req := GetUserOrder{
			ID:     "ORD-001",
			UserID: uuid.New(),
		}

		expectedParams := repository.GetUserOrderParams{
			ID:     req.ID,
			UserID: req.UserID,
		}

		q.On(
			"GetUserOrder",
			ctx,
			expectedParams,
		).Return(repository.Order{}, pgx.ErrNoRows).Once()

		result, err := service.GetUserOrder(ctx, req)

		require.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrOrderNotFound)

		q.AssertExpectations(t)
	})

	t.Run("error", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderService(q)

		req := GetUserOrder{
			ID:     "ORD-001",
			UserID: uuid.New(),
		}

		expectedParams := repository.GetUserOrderParams{
			ID:     req.ID,
			UserID: req.UserID,
		}

		expectedErr := errors.New("database error")

		q.On(
			"GetUserOrder",
			ctx,
			expectedParams,
		).Return(repository.Order{}, expectedErr).Once()

		result, err := service.GetUserOrder(ctx, req)

		require.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, expectedErr)

		q.AssertExpectations(t)
	})
}

func TestOrderService_ListOrdersByUser(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderService(q)

		row1 := testOrder()
		row2 := testOrder()
		row2.ID = "ORD-002"

		req := ListOrdersByUser{
			UserID:     uuid.New(),
			PageSize:   20,
			PageOffset: 0,
		}

		expectedParams := repository.ListOrdersByUserParams{
			UserID:     req.UserID,
			PageSize:   req.PageSize,
			PageOffset: req.PageOffset,
		}

		rows := []repository.Order{
			row1,
			row2,
		}

		q.On(
			"ListOrdersByUser",
			ctx,
			expectedParams,
		).Return(rows, nil).Once()

		result, err := service.ListOrdersByUser(ctx, req)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result, 2)

		assert.Equal(t, row1.ID, result[0].ID)
		assert.Equal(t, row2.ID, result[1].ID)

		q.AssertExpectations(t)
	})

	t.Run("empty", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderService(q)

		req := ListOrdersByUser{
			UserID:     uuid.New(),
			PageSize:   20,
			PageOffset: 0,
		}

		expectedParams := repository.ListOrdersByUserParams{
			UserID:     req.UserID,
			PageSize:   req.PageSize,
			PageOffset: req.PageOffset,
		}

		q.On(
			"ListOrdersByUser",
			ctx,
			expectedParams,
		).Return([]repository.Order{}, nil).Once()

		result, err := service.ListOrdersByUser(ctx, req)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Empty(t, result)

		q.AssertExpectations(t)
	})

	t.Run("error", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderService(q)

		req := ListOrdersByUser{
			UserID:     uuid.New(),
			PageSize:   20,
			PageOffset: 0,
		}

		expectedParams := repository.ListOrdersByUserParams{
			UserID:     req.UserID,
			PageSize:   req.PageSize,
			PageOffset: req.PageOffset,
		}

		expectedErr := errors.New("database error")

		q.On(
			"ListOrdersByUser",
			ctx,
			expectedParams,
		).Return(nil, expectedErr).Once()

		result, err := service.ListOrdersByUser(ctx, req)

		require.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, expectedErr)

		q.AssertExpectations(t)
	})
}

func TestOrderService_ListOrdersByStatus(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderService(q)

		row1 := testOrder()
		row2 := testOrder()
		row2.ID = "ORD-002"

		req := ListOrdersByStatus{
			Status:     string(repository.OrderStatusPending),
			PageSize:   20,
			PageOffset: 0,
		}

		expectedParams := repository.ListOrdersByStatusParams{
			Status:     repository.OrderStatus(req.Status),
			PageSize:   req.PageSize,
			PageOffset: req.PageOffset,
		}

		rows := []repository.Order{
			row1,
			row2,
		}

		q.On(
			"ListOrdersByStatus",
			ctx,
			expectedParams,
		).Return(rows, nil).Once()

		result, err := service.ListOrdersByStatus(ctx, req)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result, 2)

		assert.Equal(t, row1.ID, result[0].ID)
		assert.Equal(t, row2.ID, result[1].ID)

		q.AssertExpectations(t)
	})

	t.Run("empty", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderService(q)

		req := ListOrdersByStatus{
			Status:     string(repository.OrderStatusPending),
			PageSize:   20,
			PageOffset: 0,
		}

		expectedParams := repository.ListOrdersByStatusParams{
			Status:     repository.OrderStatus(req.Status),
			PageSize:   req.PageSize,
			PageOffset: req.PageOffset,
		}

		q.On(
			"ListOrdersByStatus",
			ctx,
			expectedParams,
		).Return([]repository.Order{}, nil).Once()

		result, err := service.ListOrdersByStatus(ctx, req)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Empty(t, result)

		q.AssertExpectations(t)
	})

	t.Run("error", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderService(q)

		req := ListOrdersByStatus{
			Status:     string(repository.OrderStatusPending),
			PageSize:   20,
			PageOffset: 0,
		}

		expectedParams := repository.ListOrdersByStatusParams{
			Status:     repository.OrderStatus(req.Status),
			PageSize:   req.PageSize,
			PageOffset: req.PageOffset,
		}

		expectedErr := errors.New("database error")

		q.On(
			"ListOrdersByStatus",
			ctx,
			expectedParams,
		).Return(nil, expectedErr).Once()

		result, err := service.ListOrdersByStatus(ctx, req)

		require.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, expectedErr)

		q.AssertExpectations(t)
	})
}

func TestOrderService_UpdateOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderService(q)

		expiresAt := time.Now().Add(2 * time.Hour)

		req := UpdateOrder{
			ID:        "ORD-001",
			Currency:  "KES",
			Metadata:  []byte(`{"updated":true}`),
			ExpiresAt: &expiresAt,
		}

		row := testOrder()
		row.ID = req.ID
		row.Currency = req.Currency
		row.Metadata = req.Metadata
		row.ExpiresAt = req.ExpiresAt

		expectedParams := repository.UpdateOrderParams{
			ID:        req.ID,
			Currency:  req.Currency,
			Metadata:  req.Metadata,
			ExpiresAt: req.ExpiresAt,
		}

		q.On(
			"UpdateOrder",
			ctx,
			expectedParams,
		).Return(row, nil).Once()

		result, err := service.UpdateOrder(ctx, req)

		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, row.ID, result.ID)
		assert.Equal(t, row.Currency, result.Currency)
		assert.Equal(t, row.Metadata, result.Metadata)
		assert.Equal(t, row.ExpiresAt, result.ExpiresAt)

		q.AssertExpectations(t)
	})

	t.Run("not found", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderService(q)

		req := UpdateOrder{
			ID:       "ORD-001",
			Currency: "KES",
			Metadata: []byte(`{}`),
		}

		expectedParams := repository.UpdateOrderParams{
			ID:        req.ID,
			Currency:  req.Currency,
			Metadata:  req.Metadata,
			ExpiresAt: req.ExpiresAt,
		}

		q.On(
			"UpdateOrder",
			ctx,
			expectedParams,
		).Return(repository.Order{}, pgx.ErrNoRows).Once()

		result, err := service.UpdateOrder(ctx, req)

		require.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrOrderNotFound)

		q.AssertExpectations(t)
	})

	t.Run("error", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderService(q)

		req := UpdateOrder{
			ID:       "ORD-001",
			Currency: "KES",
			Metadata: []byte(`{}`),
		}

		expectedParams := repository.UpdateOrderParams{
			ID:        req.ID,
			Currency:  req.Currency,
			Metadata:  req.Metadata,
			ExpiresAt: req.ExpiresAt,
		}

		expectedErr := errors.New("database error")

		q.On(
			"UpdateOrder",
			ctx,
			expectedParams,
		).Return(repository.Order{}, expectedErr).Once()

		result, err := service.UpdateOrder(ctx, req)

		require.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, expectedErr)

		q.AssertExpectations(t)
	})
}

func TestOrderService_CancelOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderService(q)

		req := CancelOrder{
			ID: "ORD-001",
		}

		row := testOrder()
		row.ID = req.ID
		row.Status = repository.OrderStatusCancelled

		q.On(
			"CancelOrder",
			ctx,
			req.ID,
		).Return(row, nil).Once()

		result, err := service.CancelOrder(ctx, req)

		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, row.ID, result.ID)
		assert.Equal(t, string(row.Status), result.Status)

		q.AssertExpectations(t)
	})

	t.Run("not cancellable", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderService(q)

		req := CancelOrder{
			ID: "ORD-001",
		}

		q.On(
			"CancelOrder",
			ctx,
			req.ID,
		).Return(repository.Order{}, pgx.ErrNoRows).Once()

		result, err := service.CancelOrder(ctx, req)

		require.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrOrderNotCancellable)

		q.AssertExpectations(t)
	})

	t.Run("error", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderService(q)

		req := CancelOrder{
			ID: "ORD-001",
		}

		expectedErr := errors.New("database error")

		q.On(
			"CancelOrder",
			ctx,
			req.ID,
		).Return(repository.Order{}, expectedErr).Once()

		result, err := service.CancelOrder(ctx, req)

		require.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, expectedErr)

		q.AssertExpectations(t)
	})
}

func TestOrderService_MarkOrderPaid(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderService(q)

		req := MarkOrderPaid{
			ID: "ORD-001",
		}

		row := testOrder()
		row.ID = req.ID
		row.Status = repository.OrderStatusPaid

		q.On(
			"MarkOrderPaid",
			ctx,
			req.ID,
		).Return(row, nil).Once()

		result, err := service.MarkOrderPaid(ctx, req)

		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, row.ID, result.ID)
		assert.Equal(t, string(row.Status), result.Status)

		q.AssertExpectations(t)
	})

	t.Run("not found", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderService(q)

		req := MarkOrderPaid{
			ID: "ORD-001",
		}

		q.On(
			"MarkOrderPaid",
			ctx,
			req.ID,
		).Return(repository.Order{}, pgx.ErrNoRows).Once()

		result, err := service.MarkOrderPaid(ctx, req)

		require.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrOrderNotFound)

		q.AssertExpectations(t)
	})

	t.Run("error", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderService(q)

		req := MarkOrderPaid{
			ID: "ORD-001",
		}

		expectedErr := errors.New("database error")

		q.On(
			"MarkOrderPaid",
			ctx,
			req.ID,
		).Return(repository.Order{}, expectedErr).Once()

		result, err := service.MarkOrderPaid(ctx, req)

		require.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, expectedErr)

		q.AssertExpectations(t)
	})
}

func TestOrderService_RecalculateOrderTotals(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderService(q)

		req := RecalculateOrderTotals{
			ID: "ORD-001",
		}

		q.On(
			"RecalculateOrderTotals",
			ctx,
			req.ID,
		).Return(nil).Once()

		err := service.RecalculateOrderTotals(ctx, req)

		require.NoError(t, err)

		q.AssertExpectations(t)
	})

	t.Run("error", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderService(q)

		req := RecalculateOrderTotals{
			ID: "ORD-001",
		}

		expectedErr := errors.New("database error")

		q.On(
			"RecalculateOrderTotals",
			ctx,
			req.ID,
		).Return(expectedErr).Once()

		err := service.RecalculateOrderTotals(ctx, req)

		require.Error(t, err)
		assert.ErrorIs(t, err, expectedErr)

		q.AssertExpectations(t)
	})
}
