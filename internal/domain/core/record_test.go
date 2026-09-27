package core

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordMeta_UsesCanonicalFieldNames(t *testing.T) {
	t.Parallel()

	meta := RecordMeta{SchemaVersion: 1, Revision: 2, Source: 3}
	data, err := json.Marshal(meta)
	require.NoError(t, err)

	var stored map[string]any
	require.NoError(t, json.Unmarshal(data, &stored))
	assert.Equal(t, float64(1), stored["schm_ver"])
	assert.Equal(t, float64(2), stored["rev"])
	assert.Equal(t, float64(3), stored["src"])
	assert.NotContains(t, stored, "v")
	assert.NotContains(t, stored, "schema_version")
	assert.NotContains(t, stored, "s")
}
