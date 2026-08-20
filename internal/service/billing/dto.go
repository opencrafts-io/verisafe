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
