package billing

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateOrderMetadataDecodesRawJSON(t *testing.T) {
	t.Run("array metadata", func(t *testing.T) {
		var req CreateOrder
		err := json.Unmarshal([]byte(`{"metadata":[]}`), &req)

		require.NoError(t, err)
		assert.JSONEq(t, `[]`, string(req.Metadata))
	})

	t.Run("object metadata", func(t *testing.T) {
		var req CreateOrder
		err := json.Unmarshal([]byte(`{"metadata":{"source":"checkout"}}`), &req)

		require.NoError(t, err)
		assert.JSONEq(t, `{"source":"checkout"}`, string(req.Metadata))
	})
}
