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

func newTestOrderItemService(q repository.Querier) *orderItemService {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	return NewOrderItemService(q, logger).(*orderItemService)
}

func (m *mockQuerier) CreateOrderItem(
	ctx context.Context,
	params repository.CreateOrderItemParams,
) (repository.OrderItem, error) {
	args := m.Called(ctx, params)

	var item repository.OrderItem
	if value := args.Get(0); value != nil {
		item = value.(repository.OrderItem)
	}

	return item, args.Error(1)
}

func (m *mockQuerier) GetOrderItem(
	ctx context.Context,
	params repository.GetOrderItemParams,
) (repository.OrderItem, error) {
	args := m.Called(ctx, params)

	var item repository.OrderItem
	if value := args.Get(0); value != nil {
		item = value.(repository.OrderItem)
	}

	return item, args.Error(1)
}

func (m *mockQuerier) ListOrderItemsByOrder(
	ctx context.Context,
	orderID string,
) ([]repository.OrderItem, error) {
	args := m.Called(ctx, orderID)

	var items []repository.OrderItem
	if value := args.Get(0); value != nil {
		items = value.([]repository.OrderItem)
	}

	return items, args.Error(1)
}

func (m *mockQuerier) UpdateOrderItem(
	ctx context.Context,
	params repository.UpdateOrderItemParams,
) (repository.OrderItem, error) {
	args := m.Called(ctx, params)

	var item repository.OrderItem
	if value := args.Get(0); value != nil {
		item = value.(repository.OrderItem)
	}

	return item, args.Error(1)
}

func (m *mockQuerier) DeleteOrderItem(
	ctx context.Context,
	params repository.DeleteOrderItemParams,
) (repository.OrderItem, error) {
	args := m.Called(ctx, params)

	var item repository.OrderItem
	if value := args.Get(0); value != nil {
		item = value.(repository.OrderItem)
	}

	return item, args.Error(1)
}

func (m *mockQuerier) DeleteOrderItemsByOrder(
	ctx context.Context,
	orderID string,
) error {
	args := m.Called(ctx, orderID)

	return args.Error(0)
}

func testOrderItem() repository.OrderItem {
	now := time.Now()
	planID := int32(1)

	return repository.OrderItem{
		ID:        uuid.New(),
		OrderID:   "ORD-001",
		AddedBy:   uuid.New(),
		UnitPrice: 2500,
		Discount:  100,
		Quantity:  2,
		Tax:       450,
		PlanID:    &planID,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func TestOrderItemService_CreateOrderItem(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderItemService(q)

		addedBy := uuid.New()
		planID := int32(1)

		req := CreateOrderItem{
			OrderID:   "ORD-001",
			AddedBy:   addedBy,
			UnitPrice: 2500,
			Discount:  100,
			Quantity:  2,
			Tax:       450,
			PlanID:    &planID,
		}

		row := testOrderItem()

		expectedParams := repository.CreateOrderItemParams{
			OrderID:   req.OrderID,
			AddedBy:   req.AddedBy,
			UnitPrice: req.UnitPrice,
			Discount:  req.Discount,
			Quantity:  req.Quantity,
			Tax:       req.Tax,
			PlanID:    req.PlanID,
		}

		q.On(
			"CreateOrderItem",
			ctx,
			expectedParams,
		).Return(row, nil).Once()
		q.On("RecalculateOrderTotals", ctx, row.OrderID).Return(nil).Once()

		result, err := service.CreateOrderItem(ctx, req)

		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, row.ID, result.ID)
		assert.Equal(t, row.OrderID, result.OrderID)
		assert.Equal(t, row.AddedBy, result.AddedBy)
		assert.Equal(t, row.UnitPrice, result.UnitPrice)
		assert.Equal(t, row.Discount, result.Discount)
		assert.Equal(t, row.Quantity, result.Quantity)
		assert.Equal(t, row.Tax, result.Tax)
		assert.Equal(t, row.PlanID, result.PlanID)

		q.AssertExpectations(t)
	})

	t.Run("error", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderItemService(q)

		req := CreateOrderItem{
			OrderID:   "ORD-001",
			AddedBy:   uuid.New(),
			UnitPrice: 2500,
			Discount:  100,
			Quantity:  2,
			Tax:       450,
		}

		expectedParams := repository.CreateOrderItemParams{
			OrderID:   req.OrderID,
			AddedBy:   req.AddedBy,
			UnitPrice: req.UnitPrice,
			Discount:  req.Discount,
			Quantity:  req.Quantity,
			Tax:       req.Tax,
			PlanID:    req.PlanID,
		}

		expectedErr := errors.New("database error")

		q.On(
			"CreateOrderItem",
			ctx,
			expectedParams,
		).Return(repository.OrderItem{}, expectedErr).Once()

		result, err := service.CreateOrderItem(ctx, req)

		require.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, expectedErr)

		q.AssertExpectations(t)
	})
}

func TestOrderItemService_GetOrderItem(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderItemService(q)

		row := testOrderItem()

		req := GetOrderItem{
			ID:      row.ID,
			OrderID: row.OrderID,
		}
		expectedParams := repository.GetOrderItemParams{
			ID:      req.ID,
			OrderID: req.OrderID,
		}

		q.On(
			"GetOrderItem",
			ctx,
			expectedParams,
		).Return(row, nil).Once()

		result, err := service.GetOrderItem(ctx, req)

		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, row.ID, result.ID)
		assert.Equal(t, row.OrderID, result.OrderID)
		assert.Equal(t, row.AddedBy, result.AddedBy)
		assert.Equal(t, row.UnitPrice, result.UnitPrice)
		assert.Equal(t, row.Discount, result.Discount)
		assert.Equal(t, row.Quantity, result.Quantity)
		assert.Equal(t, row.Tax, result.Tax)
		assert.Equal(t, row.PlanID, result.PlanID)

		q.AssertExpectations(t)
	})

	t.Run("not found", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderItemService(q)

		id := uuid.New()

		req := GetOrderItem{
			ID:      id,
			OrderID: "ORD-001",
		}
		expectedParams := repository.GetOrderItemParams{
			ID:      req.ID,
			OrderID: req.OrderID,
		}

		q.On(
			"GetOrderItem",
			ctx,
			expectedParams,
		).Return(repository.OrderItem{}, pgx.ErrNoRows).Once()

		result, err := service.GetOrderItem(ctx, req)

		require.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrOrderItemNotFound)

		q.AssertExpectations(t)
	})

	t.Run("error", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderItemService(q)

		id := uuid.New()

		req := GetOrderItem{
			ID:      id,
			OrderID: "ORD-001",
		}
		expectedParams := repository.GetOrderItemParams{
			ID:      req.ID,
			OrderID: req.OrderID,
		}

		expectedErr := errors.New("database error")

		q.On(
			"GetOrderItem",
			ctx,
			expectedParams,
		).Return(repository.OrderItem{}, expectedErr).Once()

		result, err := service.GetOrderItem(ctx, req)

		require.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, expectedErr)

		q.AssertExpectations(t)
	})
}

func TestOrderItemService_ListOrderItemsByOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderItemService(q)

		row1 := testOrderItem()
		row2 := testOrderItem()

		req := ListOrderItemsByOrder{
			OrderID: "ORD-001",
		}

		rows := []repository.OrderItem{
			row1,
			row2,
		}

		q.On(
			"ListOrderItemsByOrder",
			ctx,
			req.OrderID,
		).Return(rows, nil).Once()

		result, err := service.ListOrderItemsByOrder(ctx, req)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result, 2)

		assert.Equal(t, row1.ID, result[0].ID)
		assert.Equal(t, row2.ID, result[1].ID)

		q.AssertExpectations(t)
	})

	t.Run("empty", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderItemService(q)

		req := ListOrderItemsByOrder{
			OrderID: "ORD-001",
		}

		q.On(
			"ListOrderItemsByOrder",
			ctx,
			req.OrderID,
		).Return([]repository.OrderItem{}, nil).Once()

		result, err := service.ListOrderItemsByOrder(ctx, req)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Empty(t, result)

		q.AssertExpectations(t)
	})

	t.Run("error", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderItemService(q)

		req := ListOrderItemsByOrder{
			OrderID: "ORD-001",
		}

		expectedErr := errors.New("database error")

		q.On(
			"ListOrderItemsByOrder",
			ctx,
			req.OrderID,
		).Return(nil, expectedErr).Once()

		result, err := service.ListOrderItemsByOrder(ctx, req)

		require.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, expectedErr)

		q.AssertExpectations(t)
	})
}

func TestOrderItemService_UpdateOrderItem(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderItemService(q)

		id := uuid.New()
		planID := int32(2)

		req := UpdateOrderItem{
			ID:        id,
			OrderID:   "ORD-001",
			UnitPrice: 3000,
			Discount:  150,
			Quantity:  3,
			Tax:       500,
			PlanID:    &planID,
		}

		row := testOrderItem()
		row.ID = id
		row.UnitPrice = req.UnitPrice
		row.Discount = req.Discount
		row.Quantity = req.Quantity
		row.Tax = req.Tax
		row.PlanID = req.PlanID

		expectedParams := repository.UpdateOrderItemParams{
			ID:        req.ID,
			OrderID:   req.OrderID,
			UnitPrice: req.UnitPrice,
			Discount:  req.Discount,
			Quantity:  req.Quantity,
			Tax:       req.Tax,
			PlanID:    req.PlanID,
		}

		q.On(
			"UpdateOrderItem",
			ctx,
			expectedParams,
		).Return(row, nil).Once()
		q.On("RecalculateOrderTotals", ctx, row.OrderID).Return(nil).Once()

		result, err := service.UpdateOrderItem(ctx, req)

		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, row.ID, result.ID)
		assert.Equal(t, row.UnitPrice, result.UnitPrice)
		assert.Equal(t, row.Discount, result.Discount)
		assert.Equal(t, row.Quantity, result.Quantity)
		assert.Equal(t, row.Tax, result.Tax)
		assert.Equal(t, row.PlanID, result.PlanID)

		q.AssertExpectations(t)
	})

	t.Run("not found", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderItemService(q)

		id := uuid.New()

		req := UpdateOrderItem{
			ID:        id,
			OrderID:   "ORD-001",
			UnitPrice: 3000,
			Discount:  150,
			Quantity:  3,
			Tax:       500,
		}

		expectedParams := repository.UpdateOrderItemParams{
			ID:        req.ID,
			OrderID:   req.OrderID,
			UnitPrice: req.UnitPrice,
			Discount:  req.Discount,
			Quantity:  req.Quantity,
			Tax:       req.Tax,
			PlanID:    req.PlanID,
		}

		q.On(
			"UpdateOrderItem",
			ctx,
			expectedParams,
		).Return(repository.OrderItem{}, pgx.ErrNoRows).Once()

		result, err := service.UpdateOrderItem(ctx, req)

		require.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrOrderItemNotFound)

		q.AssertExpectations(t)
	})

	t.Run("error", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderItemService(q)

		id := uuid.New()

		req := UpdateOrderItem{
			ID:        id,
			OrderID:   "ORD-001",
			UnitPrice: 3000,
			Discount:  150,
			Quantity:  3,
			Tax:       500,
		}

		expectedParams := repository.UpdateOrderItemParams{
			ID:        req.ID,
			OrderID:   req.OrderID,
			UnitPrice: req.UnitPrice,
			Discount:  req.Discount,
			Quantity:  req.Quantity,
			Tax:       req.Tax,
			PlanID:    req.PlanID,
		}

		expectedErr := errors.New("database error")

		q.On(
			"UpdateOrderItem",
			ctx,
			expectedParams,
		).Return(repository.OrderItem{}, expectedErr).Once()

		result, err := service.UpdateOrderItem(ctx, req)

		require.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, expectedErr)

		q.AssertExpectations(t)
	})
}

func TestOrderItemService_DeleteOrderItem(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderItemService(q)

		id := uuid.New()

		req := DeleteOrderItem{
			ID:      id,
			OrderID: "ORD-001",
		}
		expectedParams := repository.DeleteOrderItemParams{
			ID:      req.ID,
			OrderID: req.OrderID,
		}

		row := testOrderItem()
		row.ID = id

		q.On(
			"DeleteOrderItem",
			ctx,
			expectedParams,
		).Return(row, nil).Once()
		q.On("RecalculateOrderTotals", ctx, row.OrderID).Return(nil).Once()

		err := service.DeleteOrderItem(ctx, req)

		require.NoError(t, err)

		q.AssertExpectations(t)
	})

	t.Run("not found", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderItemService(q)

		id := uuid.New()

		req := DeleteOrderItem{
			ID:      id,
			OrderID: "ORD-001",
		}
		expectedParams := repository.DeleteOrderItemParams{
			ID:      req.ID,
			OrderID: req.OrderID,
		}

		q.On(
			"DeleteOrderItem",
			ctx,
			expectedParams,
		).Return(repository.OrderItem{}, pgx.ErrNoRows).Once()

		err := service.DeleteOrderItem(ctx, req)

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrOrderItemNotFound)

		q.AssertExpectations(t)
	})

	t.Run("error", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderItemService(q)

		id := uuid.New()

		req := DeleteOrderItem{
			ID:      id,
			OrderID: "ORD-001",
		}
		expectedParams := repository.DeleteOrderItemParams{
			ID:      req.ID,
			OrderID: req.OrderID,
		}

		expectedErr := errors.New("database error")

		q.On(
			"DeleteOrderItem",
			ctx,
			expectedParams,
		).Return(repository.OrderItem{}, expectedErr).Once()

		err := service.DeleteOrderItem(ctx, req)

		require.Error(t, err)
		assert.ErrorIs(t, err, expectedErr)

		q.AssertExpectations(t)
	})
}

func TestOrderItemService_DeleteOrderItemsByOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderItemService(q)

		req := DeleteOrderItemsByOrder{
			OrderID: "ORD-001",
		}

		q.On(
			"DeleteOrderItemsByOrder",
			ctx,
			req.OrderID,
		).Return(nil).Once()
		q.On("RecalculateOrderTotals", ctx, req.OrderID).Return(nil).Once()

		err := service.DeleteOrderItemsByOrder(ctx, req)

		require.NoError(t, err)

		q.AssertExpectations(t)
	})

	t.Run("error", func(t *testing.T) {
		q := new(mockQuerier)
		service := newTestOrderItemService(q)

		req := DeleteOrderItemsByOrder{
			OrderID: "ORD-001",
		}

		expectedErr := errors.New("database error")

		q.On(
			"DeleteOrderItemsByOrder",
			ctx,
			req.OrderID,
		).Return(expectedErr).Once()

		err := service.DeleteOrderItemsByOrder(ctx, req)

		require.Error(t, err)
		assert.ErrorIs(t, err, expectedErr)

		q.AssertExpectations(t)
	})
}
