package backup

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
	"github.com/digikeeper/digikeeper-journal/internal/jsonx"
)

// PushStats summarizes one dump.
type PushStats struct {
	Since     time.Time // CreatedAt of the dump we compared against; zero on a first dump
	Total     int       // files considered
	Uploaded  int       // files whose bytes were sent
	Skipped   int       // files already present remotely with the same content
	Removed   int       // files in the last dump that no longer exist locally
	BytesSent int64
}

// Push captures the master copy under one exclusive lock, uploads changed
// staged bytes after releasing the lock, then writes the manifest as the remote
// commit marker.
func Push(ctx context.Context, root *storefs.Root, remote Remote, logger *slog.Logger) (PushStats, error) {
	previous, err := loadManifest(ctx, remote)
	if err != nil {
		return PushStats{}, fmt.Errorf("backup: read last manifest: %w", err)
	}

	snapshot, err := buildLocalSnapshot(ctx, root, previous)
	if err != nil {
		return PushStats{}, err
	}

	stats := PushStats{
		Total:   snapshot.total,
		Skipped: snapshot.skipped,
		Removed: snapshot.removed,
	}
	if previous != nil {
		stats.Since = previous.CreatedAt
		for rel := range previous.Entries {
			if _, exists := snapshot.manifest.Entries[rel]; !exists {
				logger.Warn("file gone since last dump, dropped from manifest", slog.String("file", rel))
			}
		}
	}

	for _, file := range snapshot.staged {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		entry := snapshot.manifest.Entries[file.rel]
		if err := remote.Put(ctx, dataKey(file.rel), bytes.NewReader(file.content), entry.Size); err != nil {
			return stats, fmt.Errorf("backup: upload %s: %w", file.rel, err)
		}
		stats.Uploaded++
		stats.BytesSent += entry.Size
		logger.Info("uploaded", slog.String("file", file.rel), slog.Int64("bytes", entry.Size))
	}

	if err := putManifest(ctx, remote, snapshot.manifest); err != nil {
		return stats, err
	}
	return stats, nil
}

func putManifest(ctx context.Context, remote Remote, manifest *Manifest) error {
	raw, err := jsonx.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("backup: encode manifest: %w", err)
	}
	if err := remote.Put(ctx, manifestKey, bytes.NewReader(raw), int64(len(raw))); err != nil {
		return fmt.Errorf("backup: upload manifest: %w", err)
	}
	return nil
}
