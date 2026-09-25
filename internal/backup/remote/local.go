// Package remote holds the dump destinations behind backup.Remote.
package remote

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/digikeeper/digikeeper-journal/internal/backup"
)

// Local stores a dump in a directory — a mounted drive, an rclone mount, or a
// scratch dir in tests.
type Local struct {
	dir string
}

// NewLocal returns a Local rooted at dir, creating it if needed.
func NewLocal(dir string) (*Local, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("remote: mkdir %s: %w", dir, err)
	}
	return &Local{dir: dir}, nil
}

func (l *Local) String() string { return "dir:" + l.dir }

func (l *Local) Get(_ context.Context, key string) (io.ReadCloser, error) {
	f, err := os.Open(l.path(key))
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("remote: %s: %w", key, backup.ErrNotExist)
	}
	if err != nil {
		return nil, fmt.Errorf("remote: open %s: %w", key, err)
	}
	return f, nil
}

func (l *Local) Put(_ context.Context, key string, r io.Reader, _ int64) error {
	dst := l.path(key)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("remote: mkdir for %s: %w", key, err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".tmp-*")
	if err != nil {
		return fmt.Errorf("remote: temp for %s: %w", key, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := io.Copy(tmp, r); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("remote: write %s: %w", key, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("remote: close %s: %w", key, err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return fmt.Errorf("remote: install %s: %w", key, err)
	}
	return nil
}

func (l *Local) path(key string) string {
	return filepath.Join(l.dir, filepath.FromSlash(key))
}
