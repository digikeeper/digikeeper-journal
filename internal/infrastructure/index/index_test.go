package index

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/pkg/timefmt"
)

func TestStoreSearch(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	for _, row := range []Row{
		{
			File:      "dk_journal/2026/2026-03-08_journal.jsonl",
			Tags:      []string{"work", "focus"},
			Types:     []string{"note"},
			Timestamp: mustParseTime(t, "2026-03-08T10:00:00Z"),
		},
		{
			File:      "dk_journal/2026/2026-03-09_journal.jsonl",
			Tags:      []string{"fitness", "health"},
			Types:     []string{"exercise"},
			Timestamp: mustParseTime(t, "2026-03-09T10:00:00Z"),
		},
		{
			File:      "dk_journal/2026/2026-03-10_journal.jsonl",
			Tags:      []string{"health", "nutrition"},
			Types:     []string{"meal"},
			Timestamp: mustParseTime(t, "2026-03-10T10:00:00Z"),
		},
	} {
		mustInsertIndexRow(t, store, row)
	}

	tests := []struct {
		name      string
		params    SearchParams
		wantFiles []string
	}{
		{
			name:   "repeated tags use OR semantics",
			params: SearchParams{Tags: []string{"fitness", "nutrition"}},
			wantFiles: []string{
				"dk_journal/2026/2026-03-10_journal.jsonl",
				"dk_journal/2026/2026-03-09_journal.jsonl",
			},
		},
		{
			name:   "repeated types use OR semantics",
			params: SearchParams{Types: []string{"exercise", "meal"}},
			wantFiles: []string{
				"dk_journal/2026/2026-03-10_journal.jsonl",
				"dk_journal/2026/2026-03-09_journal.jsonl",
			},
		},
		{
			name:      "tag and type filters are combined",
			params:    SearchParams{Tags: []string{"health"}, Types: []string{"meal"}},
			wantFiles: []string{"dk_journal/2026/2026-03-10_journal.jsonl"},
		},
		{
			name: "time filters match overlapping files",
			params: SearchParams{
				From: mustParseTime(t, "2026-03-09T00:00:00Z"),
				To:   mustParseTime(t, "2026-03-09T23:59:59Z"),
			},
			wantFiles: []string{"dk_journal/2026/2026-03-09_journal.jsonl"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := store.Search(t.Context(), tt.params)
			require.NoError(t, err)
			assert.Equal(t, tt.wantFiles, resultFiles(results))
		})
	}
}

func TestStoreInsertMergesFileMetadata(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	for _, row := range []Row{
		{
			File:      "dk_journal/2026/2026-03-08_journal.jsonl",
			Tags:      []string{"work"},
			Types:     []string{"note"},
			Timestamp: mustParseTime(t, "2026-03-08T10:00:00Z"),
		},
		{
			File:      "dk_journal/2026/2026-03-08_journal.jsonl",
			Tags:      []string{"fitness"},
			Types:     []string{"exercise"},
			Timestamp: mustParseTime(t, "2026-03-08T14:00:00Z"),
		},
	} {
		mustInsertIndexRow(t, store, row)
	}

	results, err := store.Search(t.Context(), SearchParams{
		Tags:  []string{"fitness"},
		Types: []string{"exercise"},
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "dk_journal/2026/2026-03-08_journal.jsonl", results[0].File)
	assert.Equal(t, mustParseTime(t, "2026-03-08T10:00:00Z"), results[0].MinTS)
	assert.Equal(t, mustParseTime(t, "2026-03-08T14:00:00Z"), results[0].MaxTS)
	assert.ElementsMatch(t, []string{"work", "fitness"}, results[0].Tags)
	assert.ElementsMatch(t, []string{"note", "exercise"}, results[0].Types)
}

func TestStoreRebuildPartition(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	partition := core.PartitionFromTime(mustParseTime(t, "2026-03-08T10:00:00Z"))
	require.NoError(t, store.Insert(t.Context(), Row{
		File:      "2026/2026-03-08_journal.jsonl",
		Tags:      []string{"old"},
		Types:     []string{"note"},
		Timestamp: mustParseTime(t, "2026-03-08T09:00:00Z"),
	}))

	err := store.RebuildPartition(t.Context(), partition, []core.Record{
		{
			Timestamp: mustParseTime(t, "2026-03-08T10:00:00.123456789Z"),
			Type:      "note",
			Tags:      []string{"work"},
		},
		{
			Timestamp: mustParseTime(t, "2026-03-08T14:00:00.987654321Z"),
			Type:      "meal",
			Tags:      []string{"health"},
		},
	})
	require.NoError(t, err)

	results, err := store.Search(t.Context(), SearchParams{Tags: []string{"work"}})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "2026/2026-03-08_journal.jsonl", results[0].File)
	assert.ElementsMatch(t, []string{"work", "health"}, results[0].Tags)
	assert.ElementsMatch(t, []string{"note", "meal"}, results[0].Types)
	assert.Equal(t, mustParseTime(t, "2026-03-08T10:00:00.123Z"), results[0].MinTS)
	assert.Equal(t, mustParseTime(t, "2026-03-08T14:00:00.987Z"), results[0].MaxTS)
}

func TestStoreRebuildPartitionEmptyDeletesRow(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	partition := core.PartitionFromTime(mustParseTime(t, "2026-03-08T10:00:00Z"))
	require.NoError(t, store.Insert(t.Context(), Row{
		File:      "2026/2026-03-08_journal.jsonl",
		Tags:      []string{"work"},
		Types:     []string{"note"},
		Timestamp: mustParseTime(t, "2026-03-08T10:00:00Z"),
	}))

	require.NoError(t, store.RebuildPartition(t.Context(), partition, nil))

	results, err := store.Search(t.Context(), SearchParams{Tags: []string{"work"}})
	require.NoError(t, err)
	assert.Empty(t, results)
}

func newTestStore(t *testing.T) *Store {
	t.Helper()

	store, err := NewIdx(filepath.Join(t.TempDir(), "index.db"), Config{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func mustInsertIndexRow(t *testing.T, store *Store, row Row) {
	t.Helper()

	require.NoError(t, store.Insert(t.Context(), row))
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()

	ts, err := timefmt.Parse(value)
	require.NoError(t, err)
	return ts
}

func resultFiles(results []Result) []string {
	files := make([]string, 0, len(results))
	for _, result := range results {
		files = append(files, result.File)
	}
	return files
}
