package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateTopupGroupRatioRejectsNegativeAndPreservesCurrentValues(t *testing.T) {
	original := TopupGroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateTopupGroupRatioByJSONString(original))
	})

	require.NoError(t, UpdateTopupGroupRatioByJSONString(`{"free":0}`))
	require.ErrorContains(t, UpdateTopupGroupRatioByJSONString(`{"vip":-1}`), "cannot be negative")
	assert.Equal(t, float64(0), GetTopupGroupRatio("free"))
	assert.JSONEq(t, `{"free":0}`, TopupGroupRatio2JSONString())
}
