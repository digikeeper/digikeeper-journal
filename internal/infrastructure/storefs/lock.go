package storefs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/digikeeper/digikeeper-journal/pkg/flock"
)

// retryDelay paces re-acquisition while another holder has the lock.
const retryDelay = 10 * time.Millisecond

// Mode is how strongly the data directory is held.
type Mode int

const (
	Shared Mode = iota + 1
	Exclusive
)

func (m Mode) String() string {
	switch m {
	case Shared:
		return "shared"
	case Exclusive:
		return "exclusive"
	default:
		return "none"
	}
}

var (
	ErrTxDone    = errors.New("storefs: transaction already finished")
	ErrForeignTx = errors.New("storefs: transaction belongs to another data directory")
)

// Tx proves that the data directory is locked for reading.
// Only this package can create one, and functions requiring it cannot run unlocked.
type Tx interface {
	// Mode reports how strongly the directory is held.
	Mode() Mode
	state() *txState
}

// WriteTx additionally permits rewriting files. WithExclusive yields one;
// WithShared does not.
type WriteTx interface {
	Tx
	writeTx()
}

// txState is the shared body of both transaction kinds.
type txState struct {
	dir  *Dir
	mode Mode
	done bool
}

func (t *txState) Mode() Mode      { return t.mode }
func (t *txState) state() *txState { return t }

// writeTx adds the write capability to the same state.
type writeTx struct{ *txState }

func (writeTx) writeTx() {}

// WithShared runs fn under a shared lock. Reads and appends can run concurrently.
// The transaction is valid only for the duration of fn.
func (d *Dir) WithShared(ctx context.Context, fn func(tx Tx) error) error {
	return d.transact(ctx, Shared, func(st *txState) error { return fn(st) })
}

// WithExclusive runs fn under an exclusive lock for rewrites, backups, and restores.
// Keep the whole use case in fn; nested lock acquisition waits for the outer call.
func (d *Dir) WithExclusive(ctx context.Context, fn func(tx WriteTx) error) error {
	return d.transact(ctx, Exclusive, func(st *txState) error { return fn(writeTx{st}) })
}

// Check reports whether tx is a live transaction on this directory. Stores call
// it before touching a file: the type says a lock is held, and this says it is
// this directory's lock and that it is still held.
func (d *Dir) Check(tx Tx) error {
	st := tx.state()
	switch {
	case st.dir != d:
		return fmt.Errorf("%w: %s", ErrForeignTx, st.dir.path)
	case st.done:
		return fmt.Errorf("%w: %s", ErrTxDone, st.mode)
	}
	return nil
}

func (d *Dir) transact(ctx context.Context, mode Mode, fn func(*txState) error) error {
	guard, err := d.acquire(ctx, mode)
	if err != nil {
		return err
	}

	st := &txState{dir: d, mode: mode}
	defer func() {
		// Finished before the release, so a transaction captured by fn and used
		// afterwards is refused rather than racing the next holder.
		st.done = true
		if err := guard.Release(); err != nil {
			slog.Warn("storefs: releasing data lock failed", slog.Any("error", err))
		}
	}()

	return fn(st)
}

func (d *Dir) acquire(ctx context.Context, mode Mode) (*flock.Guard, error) {
	try := d.lock.TrySharedLock
	if mode == Exclusive {
		try = d.lock.TryExclusiveLock
	}

	for {
		guard, err := try()
		switch {
		case err == nil:
			return guard, nil
		case !errors.Is(err, flock.ErrLocked):
			return nil, fmt.Errorf("storefs: %s: %w", mode, err)
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("storefs: %s: %w", mode, ctx.Err())
		case <-time.After(retryDelay):
		}
	}
}
