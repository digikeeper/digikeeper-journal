package commandstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/internal/domain/errs"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/index"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
)

// TestStoreExclusiveLockRespectsContext proves a writer gives up when its
// context expires while a reader holds the directory. Two Dir handles over one
// path contend exactly as two processes would.
func TestStoreExclusiveLockRespectsContext(t *testing.T) {
	t.Parallel()

	path := t.TempDir()
	held, err := storefs.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = held.Close() })
	other, err := storefs.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = other.Close() })

	err = held.WithShared(t.Context(), func(storefs.Tx) error {
		blocked, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()
		return other.WithExclusive(blocked, func(storefs.WriteTx) error { return nil })
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestStoreReadRecord(t *testing.T) {
	t.Parallel()

	dd, err := storefs.Open(t.TempDir())
	require.NoError(t, err)
	idx, err := index.New(dd.IndexPath(), index.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })

	store, err := NewStore(dd, idx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// A domain service opens the transaction before calling in, so the test
	// has to stand in for the service.
	ctx := t.Context()
	require.NoError(t, store.WithShared(ctx, func(tx storefs.Tx) error {
		partition := core.PartitionFromTime(time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC))
		want := core.Record{
			ID:        "record-a",
			Timestamp: time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC),
			Type:      "note",
			Tags:      []string{"work"},
			Data:      map[string]any{"note": "test"},
		}
		require.NoError(t, store.Append(ctx, tx, want))

		got, err := store.ReadRecord(ctx, tx, want.ID, partition)
		require.NoError(t, err)
		assert.Equal(t, want.ID, got.ID)
		assert.Equal(t, want.Type, got.Type)

		_, err = store.ReadRecord(ctx, tx, "missing", partition)
		require.True(t, errors.Is(err, errs.ErrRecordNotFound))
		return nil
	}))
}
