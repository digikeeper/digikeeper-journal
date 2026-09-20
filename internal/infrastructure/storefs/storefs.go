// Package storefs owns a journal data directory, its names, lock, and file paths.
// The lock is directory-wide because each instance stores one user's records.
package storefs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/pkg/flock"
)

// Dir is a safely opened data directory.
type Dir struct {
	path string
	lock *flock.RWLock
	root *os.Root
}

// Open prepares the data directory at path, creating the trees it needs.
// Close releases the directory handle it holds open.
func Open(path string) (*Dir, error) {
	d := &Dir{path: path, lock: flock.NewRWLock(filepath.Join(path, dataLockName))}

	dirs := []string{d.JournalDir()}
	for _, dir := range candidateDirs() {
		dirs = append(dirs, filepath.Join(d.CandidatesDir(), dir))
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("storefs: mkdir %s: %w", dir, err)
		}
	}

	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, fmt.Errorf("storefs: open root %s: %w", path, err)
	}
	d.root = root

	return d, nil
}

// Close releases the directory handle. The lock is not held by it, so closing
// does not disturb another process's transaction.
func (d *Dir) Close() error {
	if d.root == nil {
		return nil
	}
	return d.root.Close()
}

// FS returns a read-only io/fs view of the directory.
// It uses os.Root, which refuses symlinks that leave the data directory.
func (d *Dir) FS() fs.FS {
	return d.root.FS()
}

// trees lists the record directories. The derived index is addressed by IndexPath.
func trees() []string {
	return []string{journalDirName, candidatesDirName}
}

// JournalDir returns the journal tree.
func (d *Dir) JournalDir() string { return filepath.Join(d.path, journalDirName) }

// CandidatesDir returns the candidate tree.
func (d *Dir) CandidatesDir() string { return filepath.Join(d.path, candidatesDirName) }

// IndexPath returns the SQLite index. It is derived from the JSONL trees and
// can be rebuilt from them, so it is never dumped.
func (d *Dir) IndexPath() string { return filepath.Join(d.path, indexFileName) }

// ServerLockPath returns the lock held by a running server for directory ownership.
func (d *Dir) ServerLockPath() string { return filepath.Join(d.path, serverLockName) }

// JournalPath returns one journal partition's file.
func (d *Dir) JournalPath(p core.Partition) string {
	return filepath.Join(d.JournalDir(), filepath.FromSlash(JournalKey(p)))
}

// CandidatePath returns a partition's candidate file for the given state.
// It returns an error when the state has no directory.
func (d *Dir) CandidatePath(state core.CandidateState, p core.Partition) (string, error) {
	dir, ok := CandidateDir(state)
	if !ok {
		return "", fmt.Errorf("storefs: no directory for candidate state %q", state)
	}
	return filepath.Join(d.CandidatesDir(), filepath.FromSlash(candidateKey(dir, p))), nil
}

// AuditPath returns one partition's candidate audit log.
func (d *Dir) AuditPath(p core.Partition) string {
	return filepath.Join(d.CandidatesDir(), filepath.FromSlash(auditKey(p)))
}

// Resolve turns a directory-relative key into an absolute path.
func (d *Dir) Resolve(rel string) (string, error) {
	// rel may come from a remote manifest, so reject names that could escape the tree.
	if !fs.ValidPath(rel) {
		return "", fmt.Errorf("storefs: %q: %w", rel, fs.ErrInvalid)
	}
	return filepath.Join(d.path, filepath.FromSlash(rel)), nil
}

// File is one records file, addressed relative to the data directory.
type File struct {
	Rel     string // slash-separated, e.g. dk_journal/2026/2026-09-12_journal.jsonl
	Size    int64
	ModTime time.Time
}

// Partition returns the partition the file holds.
func (f File) Partition() (core.Partition, bool) { return PartitionOfFile(f.Rel) }

// InJournal reports whether the file belongs to the journal tree — the only
// tree the index is built from.
func (f File) InJournal() bool {
	return strings.HasPrefix(f.Rel, journalDirName+"/")
}

// Files lists record files in sorted order, skipping locks and compaction
// temporaries. It walks the rooted FS and returns directory-relative names.
func (d *Dir) Files() ([]File, error) {
	fsys := d.FS()

	var files []File
	for _, tree := range trees() {
		err := fs.WalkDir(fsys, tree, func(rel string, entry fs.DirEntry, err error) error {
			switch {
			case errors.Is(err, fs.ErrNotExist):
				return fs.SkipDir // tree not created yet
			case err != nil:
				return err
			case entry.IsDir(), !IsRecordsFile(entry.Name()):
				return nil
			}

			info, err := entry.Info()
			if err != nil {
				return err
			}
			files = append(files, File{
				Rel:     rel,
				Size:    info.Size(),
				ModTime: info.ModTime(),
			})
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("storefs: walk %s: %w", tree, err)
		}
	}

	slices.SortFunc(files, func(a, b File) int { return strings.Compare(a.Rel, b.Rel) })
	return files, nil
}

// CompactTemps lists compaction temporaries left behind by a crash.
func (d *Dir) CompactTemps() []string {
	matches, _ := filepath.Glob(filepath.Join(d.JournalDir(), "*", "*"+CompactTmpSuffix))
	return matches
}
