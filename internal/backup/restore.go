package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"time"

	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
)

// ErrNoDump is returned when the remote holds no dump to restore from.
var ErrNoDump = errors.New("backup: remote holds no dump")

// RestoreStats summarizes one restore.
type RestoreStats struct {
	DumpedAt      time.Time
	Total         int
	Downloaded    int
	Skipped       int // already present locally with the manifest's hash
	BytesReceived int64
}

// Restore materializes the remote's last dump into dataPath. Files already
// present with the manifest's hash are left alone, so a restore resumes cheaply
// after an interruption. The SQLite index is not restored — run Reindex after.
func Restore(ctx context.Context, dir *storefs.Root, r Remote, logger *slog.Logger) (RestoreStats, error) {
	manifest, err := loadManifest(ctx, r)
	if err != nil {
		return RestoreStats{}, fmt.Errorf("backup: read last dump: %w", err)
	}
	if manifest == nil {
		return RestoreStats{}, ErrNoDump
	}

	stats := RestoreStats{DumpedAt: manifest.CreatedAt, Total: len(manifest.Entries)}
	for rel, entry := range manifest.Entries {
		if err := ctx.Err(); err != nil {
			return stats, err
		}

		abs, err := dir.Resolve(rel)
		if err != nil {
			return stats, fmt.Errorf("backup: manifest entry %q: %w", rel, err)
		}
		if localHash(abs) == entry.SHA256 {
			stats.Skipped++
			continue
		}
		if err := download(ctx, r, dataKey(rel), rel, abs, entry); err != nil {
			return stats, err
		}
		stats.Downloaded++
		stats.BytesReceived += entry.Size
		logger.Info("restored", slog.String("file", rel), slog.Int64("bytes", entry.Size))
	}
	return stats, nil
}

// download fetches the object at key and installs it atomically at abs,
// refusing content whose hash does not match the manifest.
func download(ctx context.Context, r Remote, key, rel, abs string, entry Entry) error {
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("backup: mkdir for %s: %w", rel, err)
	}

	rc, err := r.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("backup: download %s: %w", rel, err)
	}
	defer func() { _ = rc.Close() }()

	tmp, err := os.CreateTemp(filepath.Dir(abs), filepath.Base(abs)+".restore-*")
	if err != nil {
		return fmt.Errorf("backup: temp file for %s: %w", rel, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hash), rc); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("backup: write %s: %w", rel, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("backup: sync %s: %w", rel, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("backup: close %s: %w", rel, err)
	}

	if got := hex.EncodeToString(hash.Sum(nil)); got != entry.SHA256 {
		return fmt.Errorf("backup: %s: content hash %s, manifest says %s", rel, got, entry.SHA256)
	}
	if err := os.Rename(tmpName, abs); err != nil {
		return fmt.Errorf("backup: install %s: %w", rel, err)
	}
	return nil
}

// localHash returns the sha256 of an existing file, or "" if it cannot be read.
func localHash(abs string) string {
	f, err := os.Open(abs)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return ""
	}
	return hex.EncodeToString(hash.Sum(nil))
}
