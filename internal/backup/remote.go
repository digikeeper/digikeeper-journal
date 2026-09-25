package backup

import (
	"context"
	"errors"
	"io"
	"io/fs"
)

// ErrNotExist is returned by Remote.Get for a key the remote does not hold.
var ErrNotExist = errors.New("backup: key does not exist on remote")

// IsNotExist reports whether err means "the remote has no such key".
func IsNotExist(err error) bool {
	return errors.Is(err, ErrNotExist) || errors.Is(err, fs.ErrNotExist)
}

// Remote is a dump destination — a cloud drive, an object store, or a
// directory. Backup owns the manifest protocol; remotes only move objects.
type Remote interface {
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Put(ctx context.Context, key string, r io.Reader, size int64) error
}
