package billing

import (
	"time"

	"github.com/google/uuid"
)

type Plan struct {
	Code                string    `json:"code"`
	Name                string    `json:"name"`
	Price               int64     `json:"price"`
	Currency            string    `json:"currency"`
	BillingIntervalDays int16     `json:"billing_interval_days"`
	Active              *bool     `json:"active"`
	Visible             *bool     `json:"visible"`
	Description         *string   `json:"description"`
	CreatedBy           uuid.UUID `json:"created_by"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type CreatePlan struct {
	Code                string    `json:"code"`
	Name                string    `json:"name"`
	Price               int64     `json:"price"`
	Currency            string    `json:"currency"`
	BillingIntervalDays int16     `json:"billing_interval_days"`
	Active              bool      `json:"active"`
	Visible             bool      `json:"visible"`
	CreatedBy           uuid.UUID `json:"created_by"`
	Description         *string   `json:"description"`
}

type UpdatePlan struct {
	Code                string  `json:"code"`
	Name                *string `json:"name"`
	Price               *int64  `json:"price"`
	Currency            *string `json:"currency"`
	BillingIntervalDays *int16  `json:"billing_interval_days"`
	Active              *bool   `json:"active"`
	Visible             *bool   `json:"visible"`
	Description         *string `json:"description"`
}

type ListPlans struct {
	Visible *bool `json:"visible"`
}

type Entitlement struct {
	PlanCode    string    `json:"plan_code"`
	Key         string    `json:"key"`
	Value       int       `json:"value"`
	Unit        string    `json:"unit"`
	Description *string   `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ListEntitlementsByPlanCode struct {
	PlanCode string `json:"plan_code"`
}

type GetEntitlement struct {
	PlanCode string `json:"plan_code"`
	Key      string `json:"key"`
}

type CreateEntitlement struct {
	PlanCode    string  `json:"plan_code"`
	Key         string  `json:"key"`
	Value       int     `json:"value"`
	Unit        string  `json:"unit"`
	Description *string `json:"description"`
}

type UpdateEntitlement struct {
	PlanCode    string  `json:"plan_code"`
	Key         string  `json:"key"`
	Value       *int    `json:"value"`
	Unit        *string `json:"unit"`
	Description *string `json:"description"`
}

type DeleteEntitlement struct {
	PlanCode string `json:"plan_code"`
	Key      string `json:"key"`
}

type CreateOrder struct {
	UserID    uuid.UUID  `json:"user_id"`
	Currency  string     `json:"currency"`
	Metadata  []byte     `json:"metadata"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type GetOrder struct {
	ID string `json:"id"`
}

type GetUserOrder struct {
	ID     string    `json:"id"`
	UserID uuid.UUID `json:"user_id"`
}

type ListOrdersByUser struct {
	UserID     uuid.UUID `json:"user_id"`
	PageSize   int32     `json:"page_size"`
	PageOffset int32     `json:"page_offset"`
}

type ListOrdersByStatus struct {
	Status     string `json:"status"`
	PageSize   int32  `json:"page_size"`
	PageOffset int32  `json:"page_offset"`
}

type UpdateOrder struct {
	ID        string     `json:"id"`
	Currency  string     `json:"currency"`
	Metadata  []byte     `json:"metadata"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type CancelOrder struct {
	ID string `json:"id"`
}

type MarkOrderPaid struct {
	ID string `json:"id"`
}

type RecalculateOrderTotals struct {
	ID string `json:"id"`
}

type Order struct {
	ID          string     `json:"id"`
	UserID      uuid.UUID  `json:"user_id"`
	Status      string     `json:"status"`
	Subtotal    int64      `json:"subtotal"`
	Discount    int64      `json:"discount"`
	Tax         int64      `json:"tax"`
	Total       int64      `json:"total"`
	Currency    string     `json:"currency"`
	Metadata    []byte     `json:"metadata"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	PaidAt      *time.Time `json:"paid_at"`
	CancelledAt *time.Time `json:"cancelled_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
}

type OrderItem struct {
	ID        uuid.UUID `json:"id"`
	OrderID   string    `json:"order_id"`
	AddedBy   uuid.UUID `json:"added_by"`
	UnitPrice int64     `json:"unit_price"`
	Discount  int64     `json:"discount"`
	Quantity  int16     `json:"quantity"`
	Tax       int64     `json:"tax"`
	PlanID    *int32    `json:"plan_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateOrderItem struct {
	OrderID   string    `json:"order_id"`
	AddedBy   uuid.UUID `json:"added_by"`
	UnitPrice int64     `json:"unit_price"`
	Discount  int64     `json:"discount"`
	Quantity  int16     `json:"quantity"`
	Tax       int64     `json:"tax"`
	PlanID    *int32    `json:"plan_id,omitempty"`
}

type GetOrderItem struct {
	ID uuid.UUID `json:"id"`
}

type ListOrderItemsByOrder struct {
	OrderID string `json:"order_id"`
}

type UpdateOrderItem struct {
	ID        uuid.UUID `json:"id"`
	UnitPrice int64     `json:"unit_price"`
	Discount  int64     `json:"discount"`
	Quantity  int16     `json:"quantity"`
	Tax       int64     `json:"tax"`
	PlanID    *int32    `json:"plan_id,omitempty"`
}

type DeleteOrderItem struct {
	ID uuid.UUID `json:"id"`
}

type DeleteOrderItemsByOrder struct {
	OrderID string `json:"order_id"`
}

type ChargeAttempt struct {
	ID               uuid.UUID  `json:"id"`
	OrderID          string     `json:"order_id"`
	Status           string     `json:"status"`
	PayerPhoneNumber string     `json:"payer_phone_number"`
	Amount           int64      `json:"amount"`
	Notes            *string    `json:"notes,omitempty"`
	RequestedAt      time.Time  `json:"requested_at"`
	ResolvedAt       *time.Time `json:"resolved_at,omitempty"`
}

type CreateChargeAttempt struct {
	ID               uuid.UUID `json:"id"`
	OrderID          string    `json:"order_id"`
	PayerPhoneNumber string    `json:"payer_phone_number"`
	Amount           int64     `json:"amount"`
}

type ListPendingChargeAttempts struct {
	OrderID string `json:"order_id"`
	Limit   int32  `json:"limit"`
	Offset  int32  `json:"offset"`
}

type ResolveChargeAttempt struct {
	ID     uuid.UUID `json:"id"`
	Status string    `json:"status"`
	Notes  *string   `json:"notes,omitempty"`
}

type ChargeOrder struct {
	OrderID          string `json:"order_id"`
	PayerPhoneNumber string `json:"payer_phone_number"`
}
