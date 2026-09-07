package jsonlstore

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
)

func testRecords() []core.Record {
	return []core.Record{
		{ID: "1", Timestamp: march1At(10), Tags: []string{"work"}, Data: map[string]any{"note": "morning standup"}},
		{ID: "2", Timestamp: march1At(14), Tags: []string{"work", "meeting"}, Data: map[string]any{"note": "sprint review"}},
		{ID: "3", Timestamp: march1At(20), Tags: []string{"personal"}, Data: map[string]any{"note": "gym"}},
	}
}

func march1At(hour int) time.Time {
	return time.Date(2026, 3, 1, hour, 0, 0, 0, time.UTC)
}

func setupWriter(t *testing.T) (*JSONLWriter, string) {
	t.Helper()
	dir := t.TempDir()
	w := NewJSONLWriter(dir, "journal")
	var relPath string
	for _, e := range testRecords() {
		rp, err := w.Append(e)
		if err != nil {
			t.Fatalf("append: %v", err)
		}
		relPath = rp
	}
	return w, relPath
}

func closeWriter(t *testing.T, w *JSONLWriter) {
	t.Helper()
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
}

func TestRead_Filters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    []ReadOption
		wantIDs []string
	}{
		{
			name:    "no filters",
			wantIDs: []string{"1", "2", "3"},
		},
		{
			name:    "tag matches multiple records",
			opts:    []ReadOption{WithTags("work")},
			wantIDs: []string{"1", "2"},
		},
		{
			name:    "tag has no match",
			opts:    []ReadOption{WithTags("travel")},
			wantIDs: []string{},
		},
		{
			name:    "bounded time range",
			opts:    []ReadOption{WithTimeRange(march1At(12), march1At(22))},
			wantIDs: []string{"2", "3"},
		},
		{
			name:    "from only",
			opts:    []ReadOption{WithTimeRange(march1At(15), time.Time{})},
			wantIDs: []string{"3"},
		},
		{
			name:    "to only",
			opts:    []ReadOption{WithTimeRange(time.Time{}, march1At(12))},
			wantIDs: []string{"1"},
		},
		{
			name:    "combined filters",
			opts:    []ReadOption{WithTags("work"), WithTimeRange(march1At(12), march1At(23))},
			wantIDs: []string{"2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w, relPath := setupWriter(t)
			t.Cleanup(func() { closeWriter(t, w) })

			records, err := w.Read(relPath, tt.opts...)
			require.NoError(t, err)
			assert.Equal(t, tt.wantIDs, recordIDs(records))
		})
	}
}

func TestRead_FileNotFound(t *testing.T) {
	t.Parallel()

	w := NewJSONLWriter(t.TempDir(), "journal")
	t.Cleanup(func() { closeWriter(t, w) })

	records, err := w.Read("nonexistent/path.jsonl")
	require.NoError(t, err)
	assert.Nil(t, records)
}

func TestRead_EmptyFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	relPath := "2026/2026-03-01_journal.jsonl"
	fpath := filepath.Join(dir, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(fpath), 0o755))
	require.NoError(t, os.WriteFile(fpath, nil, 0o644))

	w := NewJSONLWriter(dir, "journal")
	t.Cleanup(func() { closeWriter(t, w) })

	records, err := w.Read(relPath)
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestMatchFilters_TagEdges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line []byte
		tags map[string]struct{}
	}{
		{
			name: "tag value inside data is ignored",
			line: []byte(`{"tags":["a"],"ts":"2026-03-01T10:00:00Z","d":{"note":"meeting"}}`),
			tags: map[string]struct{}{"meeting": {}},
		},
		{
			name: "empty tags array does not match",
			line: []byte(`{"tags":[],"ts":"2026-03-01T10:00:00Z"}`),
			tags: map[string]struct{}{"work": {}},
		},
		{
			name: "missing tags field does not match",
			line: []byte(`{"ts":"2026-03-01T10:00:00Z","d":{}}`),
			tags: map[string]struct{}{"work": {}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := &ReadFilters{Tags: tt.tags}
			assert.False(t, matchFilters(tt.line, f))
		})
	}
}

func recordIDs(records []core.Record) []string {
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
	}
	return ids
}
