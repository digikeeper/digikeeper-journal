// Package flock provides two OS-level advisory file locking patterns that
// appear throughout database and storage engine implementations:
//
//  1. Startup exclusion (Lock / Acquire): a single LOCK_EX|LOCK_NB on a
//     well-known file to prevent duplicate server instances from opening the
//     same data directory. Used by PostgreSQL (postmaster.pid), Redis
//     (server.pid), MongoDB (mongod.lock), and RocksDB (LOCK file).
//
//  2. Sidecar reader-writer lock (RWLock): a per-resource .lock sidecar file
//     where each reader acquires LOCK_SH and each writer acquires LOCK_EX.
//     Used by LMDB (mdb.lock) and SQLite WAL mode (WAL lock files).
//
// Both patterns rely on a fundamental flock(2) property: lock state is
// associated with the kernel open-file description, not the inode or PID.
// Each open(2) call produces an independent description with its own lock
// state, and the OS releases all locks automatically when the fd is closed
// or the process exits — including crashes and SIGKILL.
//
// See also: https://man7.org/linux/man-pages/man2/flock.2.html
package flock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ErrLocked is returned when a non-blocking flock acquisition would block.
var ErrLocked = errors.New("flock: lock is held")

// Lock holds a startup-exclusion flock on a file (pattern 1).
// The lock is automatically released by the OS when the process exits,
// including crashes and SIGKILL — no stale lock cleanup required.
type Lock struct {
	fd *os.File
}

// Acquire opens (or creates) the file at path and takes an exclusive
// non-blocking flock. Returns an error if another process already holds
// the lock.
func Acquire(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("flock: open %s: %w", path, err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if isLocked(err) {
			return nil, fmt.Errorf("flock: acquire %s: %w: %w", path, ErrLocked, err)
		}
		return nil, fmt.Errorf("flock: acquire %s: %w", path, err)
	}

	return &Lock{fd: f}, nil
}

// Release unlocks and closes the lock file.
func (l *Lock) Release() error {
	_ = syscall.Flock(int(l.fd.Fd()), syscall.LOCK_UN)
	return l.fd.Close()
}

// RWLock implements the sidecar reader-writer lock pattern (pattern 2).
// A dedicated .lock file where each acquisition opens a fresh file descriptor
// and calls flock(2) with LOCK_SH or LOCK_EX.
//
// This is the same design used by LMDB (mdb.lock: readers call LOCK_SH via
// mdb_reader_lock, the single writer calls LOCK_EX) and SQLite WAL mode (the
// WAL lock file coordinates readers against the checkpointer).
//
// The fresh-fd discipline is essential: flock(2) lock state belongs to the
// kernel file description, not the inode. Two goroutines that each call
// open(2) independently get independent lock state — both can hold LOCK_SH
// simultaneously without conflict.
type RWLock struct {
	path string
}

// Guard represents a held flock. Call Release to unlock and close the fd.
type Guard struct {
	fd *os.File
}

// NewRWLock returns a lock bound to the given sidecar file path.
// The file is created on first lock acquisition, not here.
func NewRWLock(path string) *RWLock {
	return &RWLock{path: path}
}

// Path of lock
func (l *RWLock) Path() string { return l.path }

// SharedLock acquires a shared (LOCK_SH) flock, blocking until available.
// Multiple shared locks can coexist; an exclusive lock blocks until all shared holders release.
func (l *RWLock) SharedLock() (*Guard, error) {
	return l.acquire(syscall.LOCK_SH)
}

// TrySharedLock attempts a non-blocking shared lock.
// Returns (nil, ErrLocked) if an exclusive lock is already held.
func (l *RWLock) TrySharedLock() (*Guard, error) {
	return l.acquire(syscall.LOCK_SH | syscall.LOCK_NB)
}

// ExclusiveLock acquires an exclusive (LOCK_EX) flock, blocking until
// all existing shared and exclusive holders release.
func (l *RWLock) ExclusiveLock() (*Guard, error) {
	return l.acquire(syscall.LOCK_EX)
}

// TryExclusiveLock attempts a non-blocking exclusive lock.
// Returns (nil, ErrLocked) if the lock is already held.
func (l *RWLock) TryExclusiveLock() (*Guard, error) {
	return l.acquire(syscall.LOCK_EX | syscall.LOCK_NB)
}

func (l *RWLock) acquire(how int) (*Guard, error) {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return nil, fmt.Errorf("flock: mkdir %s: %w", filepath.Dir(l.path), err)
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("flock: open %s: %w", l.path, err)
	}

	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		_ = f.Close()
		if how&syscall.LOCK_NB != 0 && isLocked(err) {
			return nil, fmt.Errorf("flock: lock %s: %w: %w", l.path, ErrLocked, err)
		}
		return nil, fmt.Errorf("flock: lock %s: %w", l.path, err)
	}

	return &Guard{fd: f}, nil
}

// Release unlocks and closes the underlying file descriptor.
// Safe to call multiple times.
func (g *Guard) Release() error {
	if g == nil || g.fd == nil {
		return nil
	}
	_ = syscall.Flock(int(g.fd.Fd()), syscall.LOCK_UN)
	err := g.fd.Close()
	g.fd = nil
	return err
}

func isLocked(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}
