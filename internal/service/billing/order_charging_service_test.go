package billing

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/eventbus"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

type chargingQuerier struct {
	repository.Querier

	order                repository.Order
	getOrderErr          error
	pendingAttempts      []repository.ChargeAttempt
	pendingAfterCreate   []repository.ChargeAttempt
	getPendingErr        error
	createdAttempt       repository.ChargeAttempt
	createAttemptErr     error
	createdAttemptParams []repository.CreateChargeAttemptParams
	pendingParams        []repository.GetPendingChargeAttemptsByOrderParams
	getChargeAttempt     repository.ChargeAttempt
	getChargeAttemptErr  error
	getChargeAttemptIDs  []uuid.UUID
	resolvedAttempt      repository.ChargeAttempt
	resolveAttemptErr    error
	resolveAttemptParams []repository.ResolveChargeAttemptParams
}

func (q *chargingQuerier) GetOrder(
	_ context.Context,
	_ string,
) (repository.Order, error) {
	return q.order, q.getOrderErr
}

func (q *chargingQuerier) GetPendingChargeAttemptsByOrder(
	_ context.Context,
	params repository.GetPendingChargeAttemptsByOrderParams,
) ([]repository.ChargeAttempt, error) {
	q.pendingParams = append(q.pendingParams, params)
	if len(q.pendingParams) > 1 && q.pendingAfterCreate != nil {
		return q.pendingAfterCreate, q.getPendingErr
	}

	return q.pendingAttempts, q.getPendingErr
}

func (q *chargingQuerier) CreateChargeAttempt(
	_ context.Context,
	params repository.CreateChargeAttemptParams,
) (repository.ChargeAttempt, error) {
	q.createdAttemptParams = append(q.createdAttemptParams, params)
	if q.createAttemptErr != nil {
		return repository.ChargeAttempt{}, q.createAttemptErr
	}

	q.createdAttempt.ID = params.ID
	q.createdAttempt.OrderID = params.OrderID
	q.createdAttempt.PayerPhoneNumber = params.PayerPhoneNumber
	q.createdAttempt.Amount = params.Amount
	return q.createdAttempt, nil
}

func (q *chargingQuerier) GetChargeAttempt(
	_ context.Context,
	id uuid.UUID,
) (repository.ChargeAttempt, error) {
	q.getChargeAttemptIDs = append(q.getChargeAttemptIDs, id)
	return q.getChargeAttempt, q.getChargeAttemptErr
}

func (q *chargingQuerier) ResolveChargeAttempt(
	_ context.Context,
	params repository.ResolveChargeAttemptParams,
) (repository.ChargeAttempt, error) {
	q.resolveAttemptParams = append(q.resolveAttemptParams, params)
	return q.resolvedAttempt, q.resolveAttemptErr
}

type publishedChargeEvent struct {
	routingKey string
	event      any
}

type chargingEventBus struct {
	published  []publishedChargeEvent
	publishErr error
}

func (b *chargingEventBus) Publish(
	_ context.Context,
	routingKey string,
	event any,
) error {
	b.published = append(b.published, publishedChargeEvent{
		routingKey: routingKey,
		event:      event,
	})
	return b.publishErr
}

func (*chargingEventBus) Subscribe(string, func([]byte)) error { return nil }
func (*chargingEventBus) Close()                               {}

var _ eventbus.EventBus = (*chargingEventBus)(nil)

func newTestChargeService(
	querier repository.Querier,
	db core.IDBProvider,
	bus eventbus.EventBus,
) ChargeService {
	return NewChargeService(
		&config.Config{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		querier,
		db,
		bus,
	)
}

func TestChargeService_ChargeOrderPublishesAttemptIDAndOrderTotal(t *testing.T) {
	ctx := context.Background()
	order := testOrder()
	order.Total = 5800

	querier := &chargingQuerier{
		order: order,
		createdAttempt: repository.ChargeAttempt{
			Status: repository.ChargeAttemptStatusPending,
		},
	}
	bus := &chargingEventBus{}
	service := newTestChargeService(querier, nil, bus)

	attempt, err := service.ChargeOrder(ctx, ChargeOrder{
		OrderID:          order.ID,
		PayerPhoneNumber: "254712345678",
	})

	require.NoError(t, err)
	require.NotNil(t, attempt)
	require.Len(t, querier.createdAttemptParams, 1)
	require.Len(t, bus.published, 1)

	created := querier.createdAttemptParams[0]
	published, ok := bus.published[0].event.(VeribrokeSTKRequest)
	require.True(t, ok)

	assert.NotEqual(t, uuid.Nil, attempt.ID)
	assert.Equal(t, created.ID, attempt.ID)
	assert.Equal(t, created.ID.String(), published.RequestID)
	assert.Equal(t, order.Total, created.Amount)
	assert.Equal(t, order.Total, published.TransAmount)
	assert.Equal(t, VeribrokeSTKRoutingKey, bus.published[0].routingKey)
	assert.Equal(t, repository.GetPendingChargeAttemptsByOrderParams{
		OrderID:    order.ID,
		PageSize:   1,
		PageOffset: 0,
	}, querier.pendingParams[0])
}

func TestChargeService_ChargeOrderRejectsIneligibleOrders(t *testing.T) {
	expiredAt := time.Now().Add(-time.Minute)

	tests := []struct {
		name      string
		status    repository.OrderStatus
		total     int64
		expiresAt *time.Time
		phone     string
		want      error
	}{
		{
			name:   "paid",
			status: repository.OrderStatusPaid,
			total:  5800,
			phone:  "254712345678",
			want:   ErrOrderAlreadyPaid,
		},
		{
			name:   "cancelled",
			status: repository.OrderStatusCancelled,
			total:  5800,
			phone:  "254712345678",
			want:   ErrOrderCancelled,
		},
		{
			name:   "expired",
			status: repository.OrderStatusExpired,
			total:  5800,
			phone:  "254712345678",
			want:   ErrOrderNotChargeable,
		},
		{
			name:      "past expiry time",
			status:    repository.OrderStatusPending,
			total:     5800,
			expiresAt: &expiredAt,
			phone:     "254712345678",
			want:      ErrOrderNotChargeable,
		},
		{
			name:   "zero total",
			status: repository.OrderStatusPending,
			total:  0,
			phone:  "254712345678",
			want:   ErrInvalidChargeAmount,
		},
		{
			name:   "missing payer phone",
			status: repository.OrderStatusPending,
			total:  5800,
			want:   ErrInvalidPayerPhone,
		},
		{
			name:   "invalid payer phone",
			status: repository.OrderStatusPending,
			total:  5800,
			phone:  "1234",
			want:   ErrInvalidPayerPhone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order := testOrder()
			order.Status = tt.status
			order.Total = tt.total
			order.ExpiresAt = tt.expiresAt
			querier := &chargingQuerier{order: order}
			bus := &chargingEventBus{}

			attempt, err := newTestChargeService(
				querier,
				nil,
				bus,
			).ChargeOrder(
				context.Background(),
				ChargeOrder{
					OrderID:          order.ID,
					PayerPhoneNumber: tt.phone,
				},
			)

			assert.Nil(t, attempt)
			assert.ErrorIs(t, err, tt.want)
			assert.Empty(t, querier.createdAttemptParams)
			assert.Empty(t, bus.published)
		})
	}
}

func TestChargeService_ChargeOrderRejectsPendingAttempt(t *testing.T) {
	order := testOrder()
	querier := &chargingQuerier{
		order: order,
		pendingAttempts: []repository.ChargeAttempt{{
			ID:      uuid.New(),
			OrderID: order.ID,
			Status:  repository.ChargeAttemptStatusPending,
		}},
	}
	bus := &chargingEventBus{}

	attempt, err := newTestChargeService(querier, nil, bus).ChargeOrder(
		context.Background(),
		ChargeOrder{OrderID: order.ID, PayerPhoneNumber: "254712345678"},
	)

	assert.Nil(t, attempt)
	assert.ErrorIs(t, err, ErrPendingChargeAttempt)
	assert.Empty(t, querier.createdAttemptParams)
	assert.Empty(t, bus.published)
}

func TestChargeService_ChargeOrderHandlesConcurrentPendingAttempt(t *testing.T) {
	order := testOrder()
	querier := &chargingQuerier{
		order:            order,
		createAttemptErr: &pgconn.PgError{Code: "23505"},
		pendingAfterCreate: []repository.ChargeAttempt{{
			ID:      uuid.New(),
			OrderID: order.ID,
			Status:  repository.ChargeAttemptStatusPending,
		}},
	}
	bus := &chargingEventBus{}

	attempt, err := newTestChargeService(querier, nil, bus).ChargeOrder(
		context.Background(),
		ChargeOrder{OrderID: order.ID, PayerPhoneNumber: "254712345678"},
	)

	assert.Nil(t, attempt)
	assert.ErrorIs(t, err, ErrPendingChargeAttempt)
	assert.Len(t, querier.createdAttemptParams, 1)
	assert.Len(t, querier.pendingParams, 2)
	assert.Empty(t, bus.published)
}

func TestChargeService_ChargeOrderDoesNotPublishWhenCreationFails(t *testing.T) {
	order := testOrder()
	createErr := errors.New("database unavailable")
	querier := &chargingQuerier{
		order:            order,
		createAttemptErr: createErr,
	}
	bus := &chargingEventBus{}

	attempt, err := newTestChargeService(querier, nil, bus).ChargeOrder(
		context.Background(),
		ChargeOrder{OrderID: order.ID, PayerPhoneNumber: "254712345678"},
	)

	assert.Nil(t, attempt)
	assert.ErrorIs(t, err, createErr)
	assert.Empty(t, bus.published)
}

func TestChargeService_ChargeOrderRejectsAStaleOrderTotal(t *testing.T) {
	order := testOrder()
	querier := &chargingQuerier{
		order:            order,
		createAttemptErr: pgx.ErrNoRows,
	}
	bus := &chargingEventBus{}

	attempt, err := newTestChargeService(querier, nil, bus).ChargeOrder(
		context.Background(),
		ChargeOrder{OrderID: order.ID, PayerPhoneNumber: "254712345678"},
	)

	assert.Nil(t, attempt)
	assert.ErrorIs(t, err, ErrOrderNotChargeable)
	assert.Len(t, querier.createdAttemptParams, 1)
	assert.Empty(t, bus.published)
}

func TestChargeService_ChargeOrderRecordsPublishFailure(t *testing.T) {
	order := testOrder()
	publishErr := errors.New("rabbitmq unavailable")
	querier := &chargingQuerier{
		order: order,
		createdAttempt: repository.ChargeAttempt{
			Status: repository.ChargeAttemptStatusPending,
		},
		resolvedAttempt: repository.ChargeAttempt{
			Status: repository.ChargeAttemptStatusFailure,
		},
	}
	bus := &chargingEventBus{publishErr: publishErr}

	attempt, err := newTestChargeService(querier, nil, bus).ChargeOrder(
		context.Background(),
		ChargeOrder{OrderID: order.ID, PayerPhoneNumber: "254712345678"},
	)

	assert.Nil(t, attempt)
	assert.ErrorIs(t, err, ErrChargePublishFailed)
	require.Len(t, querier.createdAttemptParams, 1)
	require.Len(t, querier.resolveAttemptParams, 1)
	assert.Equal(
		t,
		repository.ChargeAttemptStatusFailure,
		querier.resolveAttemptParams[0].Status,
	)
	require.NotNil(t, querier.resolveAttemptParams[0].Notes)
	assert.Equal(t, publishErr.Error(), *querier.resolveAttemptParams[0].Notes)
	assert.Equal(
		t,
		querier.createdAttemptParams[0].ID,
		querier.resolveAttemptParams[0].ID,
	)
}

type chargeResultRow struct {
	scan func(dest ...any) error
}

func (r chargeResultRow) Scan(dest ...any) error {
	return r.scan(dest...)
}

type chargeResultTx struct {
	pgx.Tx

	rows       []pgx.Row
	queryCalls int
	commits    int
	rollbacks  int
}

func (tx *chargeResultTx) QueryRow(
	_ context.Context,
	_ string,
	_ ...any,
) pgx.Row {
	row := tx.rows[tx.queryCalls]
	tx.queryCalls++
	return row
}

func (tx *chargeResultTx) Commit(context.Context) error {
	tx.commits++
	return nil
}

func (tx *chargeResultTx) Rollback(context.Context) error {
	tx.rollbacks++
	return nil
}

type chargeResultConnection struct {
	core.IDBConnection
	tx       pgx.Tx
	released bool
}

func (conn *chargeResultConnection) Begin(context.Context) (pgx.Tx, error) {
	return conn.tx, nil
}

func (conn *chargeResultConnection) Release() {
	conn.released = true
}

type chargeResultDBProvider struct {
	conn core.IDBConnection
	err  error
}

func (db *chargeResultDBProvider) Acquire(
	context.Context,
) (core.IDBConnection, error) {
	return db.conn, db.err
}

func repositoryChargeAttemptRow(
	attempt repository.ChargeAttempt,
) pgx.Row {
	return chargeResultRow{scan: func(dest ...any) error {
		*dest[0].(*uuid.UUID) = attempt.ID
		*dest[1].(*string) = attempt.OrderID
		*dest[2].(*repository.ChargeAttemptStatus) = attempt.Status
		*dest[3].(*string) = attempt.PayerPhoneNumber
		*dest[4].(*int64) = attempt.Amount
		*dest[5].(**string) = attempt.Notes
		*dest[6].(*time.Time) = attempt.RequestedAt
		*dest[7].(**time.Time) = attempt.ResolvedAt
		return nil
	}}
}

func successfulOrderRow() pgx.Row {
	return chargeResultRow{scan: func(...any) error { return nil }}
}

func chargeResultPayload(
	t *testing.T,
	result VeribrokeChargeResult,
) []byte {
	t.Helper()
	payload, err := json.Marshal(result)
	require.NoError(t, err)
	return payload
}

func TestChargeService_HandleChargeResultMarksOrderPaidOnSuccess(t *testing.T) {
	attempt := repository.ChargeAttempt{
		ID:          uuid.New(),
		OrderID:     "ORD-001",
		Status:      repository.ChargeAttemptStatusPending,
		RequestedAt: time.Now(),
	}
	resolved := attempt
	resolved.Status = repository.ChargeAttemptStatusSuccess
	tx := &chargeResultTx{rows: []pgx.Row{
		repositoryChargeAttemptRow(resolved),
		successfulOrderRow(),
	}}
	conn := &chargeResultConnection{tx: tx}
	querier := &chargingQuerier{getChargeAttempt: attempt}

	err := newTestChargeService(
		querier,
		&chargeResultDBProvider{conn: conn},
		&chargingEventBus{},
	).HandleChargeResult(
		context.Background(),
		chargeResultPayload(t, VeribrokeChargeResult{
			RequestID: attempt.ID.String(),
			Status:    "success",
			Message:   "payment accepted",
		}),
	)

	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{attempt.ID}, querier.getChargeAttemptIDs)
	assert.Equal(t, 2, tx.queryCalls)
	assert.Equal(t, 1, tx.commits)
	assert.Equal(t, 0, tx.rollbacks)
	assert.True(t, conn.released)
}

func TestChargeService_HandleChargeResultLeavesOrderUnpaidOnFailure(t *testing.T) {
	attempt := repository.ChargeAttempt{
		ID:      uuid.New(),
		OrderID: "ORD-001",
		Status:  repository.ChargeAttemptStatusPending,
	}
	querier := &chargingQuerier{
		getChargeAttempt: attempt,
		resolvedAttempt: repository.ChargeAttempt{
			ID:     attempt.ID,
			Status: repository.ChargeAttemptStatusFailure,
		},
	}

	err := newTestChargeService(querier, nil, &chargingEventBus{}).
		HandleChargeResult(
			context.Background(),
			chargeResultPayload(t, VeribrokeChargeResult{
				RequestID: attempt.ID.String(),
				Status:    "failure",
				Message:   "insufficient funds",
			}),
		)

	require.NoError(t, err)
	require.Len(t, querier.resolveAttemptParams, 1)
	assert.Equal(
		t,
		repository.ChargeAttemptStatusFailure,
		querier.resolveAttemptParams[0].Status,
	)
	assert.Equal(t, attempt.ID, querier.resolveAttemptParams[0].ID)
	require.NotNil(t, querier.resolveAttemptParams[0].Notes)
	assert.Equal(t, "insufficient funds", *querier.resolveAttemptParams[0].Notes)
}

func TestChargeService_HandleChargeResultRejectsMalformedAndUnknownAttempts(t *testing.T) {
	t.Run("malformed request id", func(t *testing.T) {
		err := newTestChargeService(
			&chargingQuerier{},
			nil,
			&chargingEventBus{},
		).HandleChargeResult(
			context.Background(),
			chargeResultPayload(t, VeribrokeChargeResult{
				RequestID: "not-a-uuid",
				Status:    "success",
			}),
		)

		assert.ErrorIs(t, err, ErrInvalidChargeResult)
	})

	t.Run("unknown attempt", func(t *testing.T) {
		attemptID := uuid.New()
		querier := &chargingQuerier{getChargeAttemptErr: pgx.ErrNoRows}

		err := newTestChargeService(querier, nil, &chargingEventBus{}).
			HandleChargeResult(
				context.Background(),
				chargeResultPayload(t, VeribrokeChargeResult{
					RequestID: attemptID.String(),
					Status:    "success",
				}),
			)

		assert.ErrorIs(t, err, ErrNoChargeAttemptFound)
		assert.Equal(t, []uuid.UUID{attemptID}, querier.getChargeAttemptIDs)
	})
}

func TestChargeService_HandleChargeResultDoesNotOverwriteResolvedAttempt(t *testing.T) {
	tests := []struct {
		name          string
		currentStatus repository.ChargeAttemptStatus
		incoming      string
	}{
		{
			name:          "success followed by failure",
			currentStatus: repository.ChargeAttemptStatusSuccess,
			incoming:      "failure",
		},
		{
			name:          "failure followed by success",
			currentStatus: repository.ChargeAttemptStatusFailure,
			incoming:      "success",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attempt := repository.ChargeAttempt{
				ID:     uuid.New(),
				Status: tt.currentStatus,
			}
			querier := &chargingQuerier{getChargeAttempt: attempt}

			err := newTestChargeService(querier, nil, &chargingEventBus{}).
				HandleChargeResult(
					context.Background(),
					chargeResultPayload(t, VeribrokeChargeResult{
						RequestID: attempt.ID.String(),
						Status:    tt.incoming,
					}),
				)

			require.NoError(t, err)
			assert.Empty(t, querier.resolveAttemptParams)
		})
	}
}

func TestChargeService_HandleChargeResultRollsBackWhenMarkingOrderPaidFails(t *testing.T) {
	attempt := repository.ChargeAttempt{
		ID:          uuid.New(),
		OrderID:     "ORD-001",
		Status:      repository.ChargeAttemptStatusPending,
		RequestedAt: time.Now(),
	}
	resolved := attempt
	resolved.Status = repository.ChargeAttemptStatusSuccess
	markPaidErr := errors.New("order update failed")
	tx := &chargeResultTx{rows: []pgx.Row{
		repositoryChargeAttemptRow(resolved),
		chargeResultRow{scan: func(...any) error { return markPaidErr }},
	}}
	conn := &chargeResultConnection{tx: tx}
	querier := &chargingQuerier{getChargeAttempt: attempt}

	err := newTestChargeService(
		querier,
		&chargeResultDBProvider{conn: conn},
		&chargingEventBus{},
	).HandleChargeResult(
		context.Background(),
		chargeResultPayload(t, VeribrokeChargeResult{
			RequestID: attempt.ID.String(),
			Status:    "success",
		}),
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, markPaidErr)
	assert.Equal(t, 0, tx.commits)
	assert.Equal(t, 1, tx.rollbacks)
	assert.True(t, conn.released)
}
