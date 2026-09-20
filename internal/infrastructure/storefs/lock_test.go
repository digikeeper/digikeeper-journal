package storefs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
)

// TestWithUpdate_YieldsAWriteTxAndWithSharedDoesNot is the property the whole
// scheme rests on, and it is checked by the compiler rather than here: a
// method needing storefs.WriteTx cannot be handed what WithShared yields. This
// test only records that the two kinds differ at runtime too.
func TestWithUpdate_YieldsAWriteTxAndWithSharedDoesNot(t *testing.T) {
	dir := open(t)

	require.NoError(t, dir.WithShared(t.Context(), func(tx storefs.Tx) error {
		require.Equal(t, storefs.Shared, tx.Mode())
		_, isWrite := tx.(storefs.WriteTx)
		require.False(t, isWrite, "a shared transaction must not satisfy WriteTx")
		return nil
	}))

	require.NoError(t, dir.WithExclusive(t.Context(), func(tx storefs.WriteTx) error {
		require.Equal(t, storefs.Exclusive, tx.Mode())
		return nil
	}))
}

// TestTx_IsRefusedAfterItsFunctionReturns covers a transaction captured out of
// the closure and used later. The lock is gone by then, so the call would run
// unprotected; Check is what stops it.
func TestTx_IsRefusedAfterItsFunctionReturns(t *testing.T) {
	dir := open(t)

	var escaped storefs.Tx
	require.NoError(t, dir.WithShared(t.Context(), func(tx storefs.Tx) error {
		escaped = tx
		require.NoError(t, dir.Check(tx))
		return nil
	}))

	require.ErrorIs(t, dir.Check(escaped), storefs.ErrTxDone)
}

// TestTx_IsRefusedByAnotherDirectory stops a transaction on one data directory
// from vouching for a call against another. It is the one property the
// transaction types cannot carry, so it is the one thing Check still tests.
func TestTx_IsRefusedByAnotherDirectory(t *testing.T) {
	a, b := open(t), open(t)

	require.NoError(t, a.WithShared(t.Context(), func(tx storefs.Tx) error {
		require.NoError(t, a.Check(tx))
		require.ErrorIs(t, b.Check(tx), storefs.ErrForeignTx)
		return nil
	}))
}

// TestTransact_ReleasesOnEveryExit covers the reason the span is a closure
// rather than a deferred unlock: an error, or a panic, still releases.
func TestTransact_ReleasesOnEveryExit(t *testing.T) {
	dir := open(t)
	sentinel := errors.New("boom")

	require.ErrorIs(t, dir.WithExclusive(t.Context(), func(storefs.WriteTx) error {
		return sentinel
	}), sentinel)

	// The lock must be free, so a second transaction succeeds immediately.
	quick, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, dir.WithExclusive(quick, func(storefs.WriteTx) error { return nil }))
}

// TestLock_ContendsAcrossHandles pins the property the scheme rests on: two Dir
// values over the same path — in this process or another — really do wait for
// each other, because the lock is a file lock rather than a mutex.
func TestLock_ContendsAcrossHandles(t *testing.T) {
	path := t.TempDir()

	first, err := storefs.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })

	second, err := storefs.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })

	err = first.WithExclusive(t.Context(), func(storefs.WriteTx) error {
		blocked, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()
		return second.WithShared(blocked, func(storefs.Tx) error { return nil })
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)

	// And once released, the same handle proceeds.
	require.NoError(t, second.WithShared(t.Context(), func(storefs.Tx) error { return nil }))
}

// TestSharedTransactionsRunConcurrently is the other half: reads and appends
// must not serialise against each other, only against a rewrite.
func TestSharedTransactionsRunConcurrently(t *testing.T) {
	path := t.TempDir()

	first, err := storefs.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })

	second, err := storefs.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })

	require.NoError(t, first.WithShared(t.Context(), func(storefs.Tx) error {
		quick, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		return second.WithShared(quick, func(storefs.Tx) error { return nil })
	}))
}
