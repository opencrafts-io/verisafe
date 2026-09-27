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
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

type mockQuerier struct {
	mock.Mock
	repository.Querier
}

func (m *mockQuerier) ListPlans(
	ctx context.Context,
	visible *bool,
) ([]repository.ListPlansRow, error) {
	args := m.Called(ctx, visible)

	var plans []repository.ListPlansRow
	if value := args.Get(0); value != nil {
		plans = value.([]repository.ListPlansRow)
	}

	return plans, args.Error(1)
}

func (m *mockQuerier) GetPlanByCode(
	ctx context.Context,
	code string,
) (repository.GetPlanByCodeRow, error) {
	args := m.Called(ctx, code)

	var plan repository.GetPlanByCodeRow
	if value := args.Get(0); value != nil {
		plan = value.(repository.GetPlanByCodeRow)
	}

	return plan, args.Error(1)
}

func (m *mockQuerier) CreatePlan(
	ctx context.Context,
	params repository.CreatePlanParams,
) (repository.CreatePlanRow, error) {
	args := m.Called(ctx, params)

	var plan repository.CreatePlanRow
	if value := args.Get(0); value != nil {
		plan = value.(repository.CreatePlanRow)
	}

	return plan, args.Error(1)
}

func (m *mockQuerier) UpdatePlan(
	ctx context.Context,
	params repository.UpdatePlanParams,
) (repository.UpdatePlanRow, error) {
	args := m.Called(ctx, params)

	var plan repository.UpdatePlanRow
	if value := args.Get(0); value != nil {
		plan = value.(repository.UpdatePlanRow)
	}

	return plan, args.Error(1)
}

func newTestPlanService(q repository.Querier) *planService {
	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)
	return New(
		&config.Config{},
		logger,
		q,
	)
}

func testListPlansRow() repository.ListPlansRow {
	createdBy := uuid.New()
	active := true
	visible := true
	description := "Professional billing plan"
	createdAt := time.Now()
	updatedAt := time.Now()

	return repository.ListPlansRow{
		Code:                "PLN-001",
		Name:                "PRO",
		Price:               2500,
		Currency:            "KES",
		BillingIntervalDays: 30,
		Active:              &active,
		Visible:             &visible,
		Description:         &description,
		CreatedBy:           &createdBy,
		CreatedAt:           &createdAt,
		UpdatedAt:           &updatedAt,
	}
}

func testGetPlanByCodeRow() repository.GetPlanByCodeRow {
	createdBy := uuid.New()
	active := true
	visible := true
	description := "Professional billing plan"
	createdAt := time.Now()
	updatedAt := time.Now()

	return repository.GetPlanByCodeRow{
		Code:                "PRO",
		Name:                "PRO",
		Price:               2500,
		Currency:            "KES",
		BillingIntervalDays: 30,
		Active:              &active,
		Visible:             &visible,
		Description:         &description,
		CreatedBy:           &createdBy,
		CreatedAt:           &createdAt,
		UpdatedAt:           &updatedAt,
	}
}

func testCreatePlanRow() repository.CreatePlanRow {
	active := true
	visible := true
	description := "Professional billing plan"
	createdAt := time.Now()
	updatedAt := time.Now()

	return repository.CreatePlanRow{
		Name:                "PRO",
		Code:                "PRO",
		Price:               2500,
		Currency:            "KES",
		BillingIntervalDays: 30,
		Active:              &active,
		Visible:             &visible,
		Description:         &description,
		CreatedAt:           &createdAt,
		UpdatedAt:           &updatedAt,
	}
}

func testUpdatePlanRow() repository.UpdatePlanRow {
	active := true
	visible := true
	description := "Updated professional plan"
	createdAt := time.Now()
	updatedAt := time.Now()

	return repository.UpdatePlanRow{
		Code:                "PRO",
		Name:                "PRO",
		Price:               3000,
		Currency:            "KES",
		BillingIntervalDays: 30,
		Active:              &active,
		Visible:             &visible,
		Description:         &description,
		CreatedAt:           &createdAt,
		UpdatedAt:           &updatedAt,
	}
}

func TestPlanService_ListPlans(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ctx := context.Background()
		visible := true

		expected := []repository.ListPlansRow{
			testListPlansRow(),
			testListPlansRow(),
		}

		mockRepo := new(mockQuerier)

		mockRepo.
			On("ListPlans", ctx, &visible).
			Return(expected, nil).
			Once()

		service := newTestPlanService(mockRepo)

		result, err := service.ListPlans(ctx, ListPlans{
			Visible: &visible,
		})

		require.NoError(t, err)
		require.Len(t, result, 2)

		assert.Equal(t, expected[0].Code, result[0].Code)
		assert.Equal(t, expected[0].Name, result[0].Name)
		assert.Equal(t, expected[0].Price, result[0].Price)
		assert.Equal(t, expected[0].Currency, result[0].Currency)
		assert.Equal(
			t,
			expected[0].BillingIntervalDays,
			result[0].BillingIntervalDays,
		)
		assert.Equal(t, expected[0].Active, result[0].Active)
		assert.Equal(t, expected[0].Visible, result[0].Visible)
		assert.Equal(t, expected[0].Description, result[0].Description)
		assert.Equal(t, expected[0].CreatedBy, &result[0].CreatedBy)
		assert.Equal(t, *expected[0].CreatedAt, result[0].CreatedAt)
		assert.Equal(t, *expected[0].UpdatedAt, result[0].UpdatedAt)

		mockRepo.AssertExpectations(t)
	})

	t.Run("no rows", func(t *testing.T) {
		ctx := context.Background()
		visible := true

		mockRepo := new(mockQuerier)

		mockRepo.
			On("ListPlans", ctx, &visible).
			Return(nil, pgx.ErrNoRows).
			Once()

		service := newTestPlanService(mockRepo)

		result, err := service.ListPlans(ctx, ListPlans{
			Visible: &visible,
		})

		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrNoPlanFound)

		mockRepo.AssertExpectations(t)
	})

	t.Run("repository error", func(t *testing.T) {
		ctx := context.Background()
		visible := true

		repositoryErr := errors.New("database connection failed")

		mockRepo := new(mockQuerier)

		mockRepo.
			On("ListPlans", ctx, &visible).
			Return(nil, repositoryErr).
			Once()

		service := newTestPlanService(mockRepo)

		result, err := service.ListPlans(ctx, ListPlans{
			Visible: &visible,
		})

		assert.Nil(t, result)
		assert.ErrorIs(t, err, repositoryErr)

		mockRepo.AssertExpectations(t)
	})

	t.Run("nil visibility", func(t *testing.T) {
		ctx := context.Background()

		expected := []repository.ListPlansRow{
			testListPlansRow(),
		}

		mockRepo := new(mockQuerier)

		mockRepo.
			On("ListPlans", ctx, (*bool)(nil)).
			Return(expected, nil).
			Once()

		service := newTestPlanService(mockRepo)

		result, err := service.ListPlans(ctx, ListPlans{})

		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Equal(t, expected[0].Code, result[0].Code)

		mockRepo.AssertExpectations(t)
	})
}

func TestPlanService_GetPlanByCode(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ctx := context.Background()
		rawPlan := testGetPlanByCodeRow()

		mockRepo := new(mockQuerier)

		mockRepo.
			On("GetPlanByCode", ctx, "PRO").
			Return(rawPlan, nil).
			Once()

		service := newTestPlanService(mockRepo)

		result, err := service.GetPlanByCode(ctx, "PRO")

		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, rawPlan.Code, result.Code)
		assert.Equal(t, rawPlan.Name, result.Name)
		assert.Equal(t, rawPlan.Price, result.Price)
		assert.Equal(t, rawPlan.Currency, result.Currency)
		assert.Equal(t, rawPlan.BillingIntervalDays, result.BillingIntervalDays)
		assert.Equal(t, rawPlan.Active, result.Active)
		assert.Equal(t, rawPlan.Visible, result.Visible)
		assert.Equal(t, rawPlan.Description, result.Description)
		assert.Equal(t, *rawPlan.CreatedBy, result.CreatedBy)
		assert.Equal(t, *rawPlan.CreatedAt, result.CreatedAt)
		assert.Equal(t, *rawPlan.UpdatedAt, result.UpdatedAt)

		mockRepo.AssertExpectations(t)
	})

	t.Run("not found", func(t *testing.T) {
		ctx := context.Background()

		mockRepo := new(mockQuerier)

		mockRepo.
			On("GetPlanByCode", ctx, "DOES_NOT_EXIST").
			Return(repository.GetPlanByCodeRow{}, pgx.ErrNoRows).
			Once()

		service := newTestPlanService(mockRepo)

		result, err := service.GetPlanByCode(ctx, "DOES_NOT_EXIST")

		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrNoPlanFound)

		mockRepo.AssertExpectations(t)
	})

	t.Run("repository error", func(t *testing.T) {
		ctx := context.Background()

		repositoryErr := errors.New("database unavailable")

		mockRepo := new(mockQuerier)

		mockRepo.
			On("GetPlanByCode", ctx, "PRO").
			Return(repository.GetPlanByCodeRow{}, repositoryErr).
			Once()

		service := newTestPlanService(mockRepo)

		result, err := service.GetPlanByCode(ctx, "PRO")

		assert.Nil(t, result)
		assert.ErrorIs(t, err, repositoryErr)

		mockRepo.AssertExpectations(t)
	})
}

func TestPlanService_CreatePlan(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ctx := context.Background()

		createdBy := uuid.New()
		description := "Professional billing plan"

		params := CreatePlan{
			Code:                "PRO",
			Name:                "PRO",
			Price:               2500,
			Currency:            "KES",
			BillingIntervalDays: 30,
			Active:              true,
			Visible:             true,
			CreatedBy:           createdBy,
			Description:         &description,
		}

		rawPlan := testCreatePlanRow()

		expectedParams := repository.CreatePlanParams{
			Name:                params.Name,
			Code:                params.Code,
			Currency:            params.Currency,
			Price:               params.Price,
			BillingIntervalDays: params.BillingIntervalDays,
			Active:              &params.Active,
			Visible:             &params.Visible,
			CreatedBy:           &createdBy,
			Description:         params.Description,
		}

		mockRepo := new(mockQuerier)

		mockRepo.
			On("CreatePlan", ctx, expectedParams).
			Return(rawPlan, nil).
			Once()

		service := newTestPlanService(mockRepo)

		result, err := service.CreatePlan(ctx, params)

		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, rawPlan.Code, result.Code)
		assert.Equal(t, rawPlan.Price, result.Price)
		assert.Equal(t, rawPlan.Currency, result.Currency)
		assert.Equal(t, rawPlan.BillingIntervalDays, result.BillingIntervalDays)
		assert.Equal(t, rawPlan.Active, result.Active)
		assert.Equal(t, rawPlan.Visible, result.Visible)
		assert.Equal(t, rawPlan.Description, result.Description)
		assert.Equal(t, *rawPlan.CreatedAt, result.CreatedAt)
		assert.Equal(t, *rawPlan.UpdatedAt, result.UpdatedAt)

		mockRepo.AssertExpectations(t)
	})

	t.Run("repository error", func(t *testing.T) {
		ctx := context.Background()

		createdBy := uuid.New()

		params := CreatePlan{
			Code:                "PRO",
			Name:                "pro",
			Price:               2500,
			Currency:            "KES",
			BillingIntervalDays: 30,
			Active:              true,
			Visible:             true,
			CreatedBy:           createdBy,
		}

		repositoryErr := errors.New("duplicate plan code")

		expectedParams := repository.CreatePlanParams{
			Name:                params.Name,
			Code:                params.Code,
			Currency:            params.Currency,
			Price:               params.Price,
			BillingIntervalDays: params.BillingIntervalDays,
			Active:              &params.Active,
			Visible:             &params.Visible,
			CreatedBy:           &createdBy,
			Description:         params.Description,
		}

		mockRepo := new(mockQuerier)

		mockRepo.
			On("CreatePlan", ctx, expectedParams).
			Return(repository.CreatePlanRow{}, repositoryErr).
			Once()

		service := newTestPlanService(mockRepo)

		result, err := service.CreatePlan(ctx, params)

		assert.Nil(t, result)
		assert.ErrorIs(t, err, repositoryErr)

		mockRepo.AssertExpectations(t)
	})
}

func TestPlanService_UpdatePlan(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ctx := context.Background()

		price := int64(3000)
		currency := "KES"
		interval := int16(30)
		active := true
		visible := true
		description := "Updated professional plan"

		params := UpdatePlan{
			Code:                "PRO",
			Price:               &price,
			Currency:            &currency,
			BillingIntervalDays: &interval,
			Active:              &active,
			Visible:             &visible,
			Description:         &description,
		}

		rawPlan := testUpdatePlanRow()

		expectedParams := repository.UpdatePlanParams{
			Code:                params.Code,
			Currency:            params.Currency,
			Price:               params.Price,
			BillingIntervalDays: params.BillingIntervalDays,
			Active:              params.Active,
			Visible:             params.Visible,
			Description:         params.Description,
		}

		mockRepo := new(mockQuerier)

		mockRepo.
			On("UpdatePlan", ctx, expectedParams).
			Return(rawPlan, nil).
			Once()

		service := newTestPlanService(mockRepo)

		result, err := service.UpdatePlan(ctx, params)

		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, rawPlan.Code, result.Code)
		assert.Equal(t, rawPlan.Price, result.Price)
		assert.Equal(t, rawPlan.Currency, result.Currency)
		assert.Equal(t, rawPlan.BillingIntervalDays, result.BillingIntervalDays)
		assert.Equal(t, rawPlan.Active, result.Active)
		assert.Equal(t, rawPlan.Visible, result.Visible)
		assert.Equal(t, rawPlan.Description, result.Description)
		assert.Equal(t, *rawPlan.CreatedAt, result.CreatedAt)
		assert.Equal(t, *rawPlan.UpdatedAt, result.UpdatedAt)

		mockRepo.AssertExpectations(t)
	})

	t.Run("not found", func(t *testing.T) {
		ctx := context.Background()

		price := int64(3000)

		params := UpdatePlan{
			Code:  "DOES_NOT_EXIST",
			Price: &price,
		}

		expectedParams := repository.UpdatePlanParams{
			Code:                params.Code,
			Currency:            params.Currency,
			Price:               params.Price,
			BillingIntervalDays: params.BillingIntervalDays,
			Active:              params.Active,
			Visible:             params.Visible,
			Description:         params.Description,
		}

		mockRepo := new(mockQuerier)

		mockRepo.
			On("UpdatePlan", ctx, expectedParams).
			Return(repository.UpdatePlanRow{}, pgx.ErrNoRows).
			Once()

		service := newTestPlanService(mockRepo)

		result, err := service.UpdatePlan(ctx, params)

		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrNoPlanFound)

		mockRepo.AssertExpectations(t)
	})

	t.Run("repository error", func(t *testing.T) {
		ctx := context.Background()

		price := int64(3000)

		params := UpdatePlan{
			Code:  "PRO",
			Price: &price,
		}

		repositoryErr := errors.New("database unavailable")

		expectedParams := repository.UpdatePlanParams{
			Code:                params.Code,
			Currency:            params.Currency,
			Price:               params.Price,
			BillingIntervalDays: params.BillingIntervalDays,
			Active:              params.Active,
			Visible:             params.Visible,
			Description:         params.Description,
		}

		mockRepo := new(mockQuerier)

		mockRepo.
			On("UpdatePlan", ctx, expectedParams).
			Return(repository.UpdatePlanRow{}, repositoryErr).
			Once()

		service := newTestPlanService(mockRepo)

		result, err := service.UpdatePlan(ctx, params)

		assert.Nil(t, result)
		assert.ErrorIs(t, err, repositoryErr)

		mockRepo.AssertExpectations(t)
	})
}
