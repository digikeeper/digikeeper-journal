package candidatestore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commandmodel "github.com/digikeeper/digikeeper-journal/internal/domain/command/model"
	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/internal/domain/errs"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
)

func TestStoreCandidateLifecycle(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()
	inTx(t, store, func(tx storefs.WriteTx) {

		partition := testPartition(t)
		candidate := testCandidate("candidate-a", "record-a")
		require.NoError(t, store.AppendCandidate(ctx, tx, candidate))

		pending, err := store.ListPending(ctx, tx, partition)
		require.NoError(t, err)
		require.Len(t, pending, 1)
		assert.Equal(t, candidate.ID, pending[0].ID)

		candidate.Action = core.Apply
		candidate.ResolvedBy = "tester"
		candidate.ResolvedAt = time.Now().UTC()
		require.NoError(t, store.MoveCandidates(ctx, tx, partition, []commandmodel.Candidate{candidate}, nil))

		pending, err = store.ListPending(ctx, tx, partition)
		require.NoError(t, err)
		assert.Empty(t, pending)

		applied, err := store.ListApplied(ctx, tx, partition)
		require.NoError(t, err)
		require.Len(t, applied, 1)
		assert.Equal(t, candidate.ID, applied[0].ID)

		require.NoError(t, store.DeleteApplied(ctx, tx, partition, []string{candidate.ID}))
		applied, err = store.ListApplied(ctx, tx, partition)
		require.NoError(t, err)
		assert.Empty(t, applied)
	})
}

func TestStoreMoveCandidatesAppliedConflict(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()
	inTx(t, store, func(tx storefs.WriteTx) {

		partition := testPartition(t)
		first := testCandidate("candidate-a", "record-a")
		first.Action = core.Apply
		require.NoError(t, store.MoveCandidates(ctx, tx, partition, []commandmodel.Candidate{first}, nil))

		second := testCandidate("candidate-b", "record-b")
		second.Action = core.Apply
		err := store.MoveCandidates(ctx, tx, partition, []commandmodel.Candidate{second}, nil)
		require.True(t, errors.Is(err, errs.ErrConflict))
	})
}

func TestStoreListMissingFilesReturnsEmpty(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()
	inTx(t, store, func(tx storefs.WriteTx) {

		pending, err := store.ListPending(ctx, tx, testPartition(t))
		require.NoError(t, err)
		assert.Empty(t, pending)

		applied, err := store.ListApplied(ctx, tx, testPartition(t))
		require.NoError(t, err)
		assert.Empty(t, applied)
	})
}

// TestStoreSharedLockRespectsContext proves a reader gives up when its context
// expires while a writer holds the directory. Two Dir handles over one path
// contend exactly as two processes would, which is what the file lock buys.
func TestStoreSharedLockRespectsContext(t *testing.T) {
	t.Parallel()

	path := t.TempDir()
	held, err := storefs.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = held.Close() })
	other, err := storefs.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = other.Close() })

	err = New(held).WithExclusive(t.Context(), func(storefs.WriteTx) error {
		blocked, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()
		return New(other).WithShared(blocked, func(storefs.Tx) error { return nil })
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

// inTx runs fn with a write transaction, standing in for the domain service
// that would normally have opened one. A write transaction also satisfies the
// methods that only need a read.
func inTx(t *testing.T, store *Store, fn func(tx storefs.WriteTx)) {
	t.Helper()
	require.NoError(t, store.WithExclusive(t.Context(), func(tx storefs.WriteTx) error {
		fn(tx)
		return nil
	}))
}

// newTestStore opens a data directory and a candidate store over it.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir, err := storefs.Open(t.TempDir())
	require.NoError(t, err)
	return New(dir)
}

func testPartition(t *testing.T) core.Partition {
	t.Helper()
	partition, err := core.ParsePartition("2026-03-08")
	require.NoError(t, err)
	return partition
}

func testCandidate(id, recordID string) commandmodel.Candidate {
	ts := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)
	return commandmodel.Candidate{
		ID:                id,
		RecordID:          recordID,
		OriginalTimestamp: ts,
		Record: core.Record{
			ID:        recordID,
			Timestamp: ts,
			Type:      "note",
			Tags:      []string{"work"},
			Data:      map[string]any{"note": id},
		},
		CreatedAt: ts,
	}
}
