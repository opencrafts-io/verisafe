package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCreateChargeAttemptLocksAndVerifiesTheOrderTotal(t *testing.T) {
	assert.Contains(t, createChargeAttempt, "WITH chargeable_order AS")
	assert.Contains(t, createChargeAttempt, "FOR UPDATE")
	assert.Contains(t, createChargeAttempt, "chargeable_order.total =")
}
