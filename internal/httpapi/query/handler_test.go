package query

import (
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryInputResolve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		input         QueryInput
		wantLocations []string
	}{
		{
			name: "valid filters",
			input: QueryInput{
				Types: []string{"note", "exercise"},
				From:  mustParseQueryTime(t, "2026-03-08T00:00:00Z"),
				To:    mustParseQueryTime(t, "2026-03-09T00:00:00Z"),
			},
		},
		{
			name:          "empty type is invalid",
			input:         QueryInput{Types: []string{""}},
			wantLocations: []string{"query.type"},
		},
		{
			name: "from after to is invalid",
			input: QueryInput{
				From: mustParseQueryTime(t, "2026-03-09T00:00:00Z"),
				To:   mustParseQueryTime(t, "2026-03-08T00:00:00Z"),
			},
			wantLocations: []string{"query.from"},
		},
		{
			name: "returns all validation errors",
			input: QueryInput{
				Types: []string{" "},
				From:  mustParseQueryTime(t, "2026-03-09T00:00:00Z"),
				To:    mustParseQueryTime(t, "2026-03-08T00:00:00Z"),
			},
			wantLocations: []string{"query.from", "query.type"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.input.Resolve(nil)
			require.Len(t, errs, len(tt.wantLocations))

			gotLocations := make([]string, 0, len(errs))
			for _, err := range errs {
				detail := &huma.ErrorDetail{}
				require.ErrorAs(t, err, &detail)
				gotLocations = append(gotLocations, detail.Location)
			}
			assert.ElementsMatch(t, tt.wantLocations, gotLocations)
		})
	}
}

func mustParseQueryTime(t *testing.T, value string) time.Time {
	t.Helper()

	ts, err := time.Parse(time.RFC3339, value)
	require.NoError(t, err)
	return ts
}
