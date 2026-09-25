package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
)

// localSnapshot is an immutable view of the master copy. Changed bytes are
// staged while the data lock is held, so uploading does not read live files.
type localSnapshot struct {
	manifest *Manifest
	staged   []snapshotFile
	total    int
	skipped  int
	removed  int
}

// stagedFile is one changed file's bytes captured under the data lock.
type snapshotFile struct {
	rel     string
	content []byte
}

func buildLocalSnapshot(
	ctx context.Context,
	store *storefs.Root,
	previous *Manifest,
) (localSnapshot, error) {
	snapshot := localSnapshot{manifest: newManifest(time.Now().UTC())}
	err := store.WithExclusive(ctx, func(_ storefs.WriteTx) error {
		files, err := store.Files()
		if err != nil {
			return err
		}
		snapshot.total = len(files)

		for _, file := range files {
			if err := ctx.Err(); err != nil {
				return err
			}

			previousEntry, existed := entry(previous, file.Rel)
			if existed && previousEntry.IsUnchanged(file) {
				snapshot.manifest.Entries[file.Rel] = previousEntry
				snapshot.skipped++
				continue
			}

			abs, err := store.Resolve(file.Rel)
			if err != nil {
				return err
			}
			content, err := os.ReadFile(abs)
			if err != nil {
				return fmt.Errorf("backup: read %s: %w", file.Rel, err)
			}

			sum := sha256.Sum256(content)
			current := Entry{
				Size:    int64(len(content)),
				ModTime: file.ModTime,
				SHA256:  hex.EncodeToString(sum[:]),
			}
			snapshot.manifest.Entries[file.Rel] = current

			if existed && current.SHA256 == previousEntry.SHA256 {
				snapshot.skipped++
				continue
			}
			snapshot.staged = append(snapshot.staged, snapshotFile{
				rel: file.Rel, content: content,
			})
		}
		return nil
	})
	if err != nil {
		return localSnapshot{}, fmt.Errorf("backup: snapshot data: %w", err)
	}

	if previous != nil {
		for rel := range previous.Entries {
			if _, exists := snapshot.manifest.Entries[rel]; !exists {
				snapshot.removed++
			}
		}
	}
	return snapshot, nil
}

func entry(manifest *Manifest, rel string) (Entry, bool) {
	if manifest == nil {
		return Entry{}, false
	}
	value, ok := manifest.Entries[rel]
	return value, ok
}
