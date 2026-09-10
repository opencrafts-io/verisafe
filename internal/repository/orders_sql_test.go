package repository

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCreateOrderInitializesFinancialTotals(t *testing.T) {
	assert.True(
		t,
		strings.Contains(
			createOrder,
			"expires_at,\n    subtotal,\n    total",
		),
		"new orders must start with explicit zero financial totals",
	)
}
