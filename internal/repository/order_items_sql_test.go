package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOrderItemMutationsLockAnEditableParentOrder(t *testing.T) {
	queries := map[string]string{
		"create":     createOrderItem,
		"update":     updateOrderItem,
		"delete":     deleteOrderItem,
		"delete all": deleteOrderItemsByOrder,
	}

	for name, query := range queries {
		t.Run(name, func(t *testing.T) {
			assert.Contains(t, query, "WITH editable_order AS")
			assert.Contains(t, query, "FOR UPDATE")
			assert.Contains(t, query, "NOT EXISTS")
			assert.Contains(t, query, "charge_attempts")
		})
	}
}

func TestOrderItemQueriesAreScopedToTheirOrder(t *testing.T) {
	assert.Contains(t, getOrderItem, "AND order_id =")
	assert.Contains(t, updateOrderItem, "oi.order_id =")
	assert.Contains(t, deleteOrderItem, "oi.order_id =")
}
