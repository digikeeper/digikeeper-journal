package storefs_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
)

func open(t *testing.T) *storefs.Dir {
	t.Helper()
	dir, err := storefs.Open(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = dir.Close() })
	return dir
}

// TestResolve_RefusesEscapingNames guards the restore path: relative names
// there come from the remote's manifest, so an escaping name must be refused
// rather than joined and written to.
func TestResolve_RefusesEscapingNames(t *testing.T) {
	dir := open(t)

	escapes := []string{
		"../outside.jsonl",
		"dk_journal/../../outside.jsonl",
		"/etc/passwd",
		"./dk_journal/2026/x.jsonl",
		"",
	}
	for _, rel := range escapes {
		t.Run(rel, func(t *testing.T) {
			_, err := dir.Resolve(rel)
			require.ErrorIs(t, err, fs.ErrInvalid, "%q must not resolve", rel)
		})
	}

	abs, err := dir.Resolve("dk_journal/2026/2026-09-12_journal.jsonl")
	require.NoError(t, err)
	require.Equal(t, filepath.FromSlash("dk_journal/2026/2026-09-12_journal.jsonl"),
		mustRel(t, dirPath(t, dir), abs))
}

// TestFS_IsAValidFSAndStaysInsideTheTree checks the io/fs view the package name
// promises, and that it does not follow a symlink out of the directory the way
// os.DirFS would.
func TestFS_IsAValidFSAndStaysInsideTheTree(t *testing.T) {
	dir := open(t)
	root := dirPath(t, dir)

	rel := "dk_journal/2026/2026-09-12_journal.jsonl"
	abs := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte("{}\n"), 0o644))

	require.NoError(t, fstest.TestFS(dir.FS(), rel))

	// A symlink pointing out of the tree must not be readable through the FS.
	outside := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(outside, []byte("secret"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "escape")))

	_, err := fs.ReadFile(dir.FS(), "escape")
	require.Error(t, err, "os.Root must refuse to follow a symlink out of the tree")
}

func dirPath(t *testing.T, d *storefs.Dir) string {
	t.Helper()
	// JournalDir is <root>/dk_journal; its parent is the root.
	return filepath.Dir(d.JournalDir())
}

func mustRel(t *testing.T, base, target string) string {
	t.Helper()
	rel, err := filepath.Rel(base, target)
	require.NoError(t, err)
	return rel
}

func someTime(t *testing.T) time.Time {
	t.Helper()
	return time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
}
