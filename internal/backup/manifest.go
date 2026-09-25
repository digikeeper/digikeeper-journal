// Package backup dumps the journal's source-of-truth files to a remote and
// restores them. The SQLite index is never dumped: it is derived from the JSONL
// files and is rebuilt by Reindex.
package backup

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
	"github.com/digikeeper/digikeeper-journal/internal/jsonx"
)

const (
	manifestKey = "manifest.json"
	dataPrefix  = "data/"
)

type manifestVersion int

const manifestV1 manifestVersion = 1

// Entry describes one dumped file as of the dump that produced it.
type Entry struct {
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
	SHA256  string    `json:"sha256"`
}

// IsUnchanged reports whether f has the same cheap identity as this entry.
// A possible change is confirmed by hashing while building the local snapshot.
func (e Entry) IsUnchanged(f storefs.File) bool {
	return e.Size == f.Size && e.ModTime.UnixNano() == f.ModTime.UnixNano()
}

// Manifest records one completed remote snapshot.
type Manifest struct {
	Version   manifestVersion  `json:"version"`
	CreatedAt time.Time        `json:"created_at"`
	Entries   map[string]Entry `json:"entries"`
}

func newManifest(createdAt time.Time) *Manifest {
	return &Manifest{
		Version:   manifestV1,
		CreatedAt: createdAt,
		Entries:   make(map[string]Entry),
	}
}

func dataKey(rel string) string { return dataPrefix + rel }

// ReadManifest decodes and validates the remote commit marker.
func ReadManifest(r io.Reader) (*Manifest, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("backup: read manifest: %w", err)
	}
	var m Manifest
	if err := jsonx.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("backup: decode manifest: %w", err)
	}
	if m.Version != manifestV1 {
		return nil, fmt.Errorf("backup: manifest version %d, want %d", m.Version, manifestV1)
	}
	if m.Entries == nil {
		m.Entries = make(map[string]Entry)
	}
	return &m, nil
}

// loadManifest loads the last committed remote snapshot. A missing manifest
// means the destination has never received a complete backup.
func loadManifest(ctx context.Context, remote Remote) (*Manifest, error) {
	rc, err := remote.Get(ctx, manifestKey)
	if err != nil {
		if IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return ReadManifest(rc)
}
