package billing

import (
	"time"

	"github.com/google/uuid"
)

type Plan struct {
	Code            string    `json:"code"`
	Price           int64     `json:"price"`
	Currency        string    `json:"currency"`
	BillingInterval int16     `json:"billing_interval"`
	Active          *bool     `json:"active"`
	Visible         *bool     `json:"visible"`
	Description     *string   `json:"description"`
	CreatedBy       uuid.UUID `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type CreatePlan struct {
	Code            string    `json:"code"`
	Price           int64     `json:"price"`
	Currency        string    `json:"currency"`
	BillingInterval int16     `json:"billing_interval"`
	Active          bool      `json:"active"`
	Visible         bool      `json:"visible"`
	CreatedBy       uuid.UUID `json:"created_by"`
	Description     *string   `json:"description"`
}

type UpdatePlan struct {
	Code            string  `json:"code"`
	Price           *int64  `json:"price"`
	Currency        *string `json:"currency"`
	BillingInterval *int16  `json:"billing_interval"`
	Active          *bool   `json:"active"`
	Visible         *bool   `json:"visible"`
	Description     *string `json:"description"`
}

type ListPlans struct {
	Visible *bool `json:"visible"`
}

type Entitlement struct {
	PlanCode    string
	Key         string
	Value       int
	Unit        string
	Description *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ListEntitlementsByPlanCode struct {
	PlanCode string
}

type GetEntitlement struct {
	PlanCode string
	Key      string
}

type CreateEntitlement struct {
	PlanCode    string
	Key         string
	Value       int
	Unit        string
	Description *string
}

type UpdateEntitlement struct {
	PlanCode    string
	Key         string
	Value       *int
	Unit        *string
	Description *string
}

type DeleteEntitlement struct {
	PlanCode string
	Key      string
}
