package backup_test

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/digikeeper/digikeeper-journal/internal/backup"
	"github.com/digikeeper/digikeeper-journal/internal/backup/remote"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
)

func discard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// dataDir builds a data directory shaped like the server's.
func dataDir(t *testing.T) *storefs.Root {
	t.Helper()
	dir := t.TempDir()

	writeAt(t, filepath.Join(dir, filepath.FromSlash("dk_journal/2026/2026-09-11_journal.jsonl")), `{"id":"a","ts":"2026-09-11T10:00:00Z"}`+"\n")
	writeAt(t, filepath.Join(dir, filepath.FromSlash("dk_journal/2026/2026-09-12_journal.jsonl")), `{"id":"b","ts":"2026-09-12T10:00:00Z"}`+"\n")
	writeAt(t, filepath.Join(dir, filepath.FromSlash("dk_candidates/pending/2026/2026-09-12_candidates.jsonl")), `{"id":"c"}`+"\n")

	// Never dumped: derived index, locks, compaction leftovers.
	writeAt(t, filepath.Join(dir, filepath.FromSlash("index.db")), "sqlite")
	writeAt(t, filepath.Join(dir, filepath.FromSlash("server.lock")), "")
	writeAt(t, filepath.Join(dir, filepath.FromSlash("dk_journal/2026/2026-09-12_journal.jsonl.lock")), "")
	writeAt(t, filepath.Join(dir, filepath.FromSlash("dk_candidates/locks/2026-09-12.lock")), "")

	dd, err := storefs.Open(dir)
	require.NoError(t, err)
	return dd
}

// writeAt seeds a file by absolute path, for building a fixture directory
// before it is opened.
func writeAt(t *testing.T, abs, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
}

func write(t *testing.T, dir *storefs.Root, rel, content string) {
	t.Helper()
	writeAt(t, resolve(t, dir, rel), content)
}

func resolveIn(t *testing.T, dir *storefs.Root, rel string) string {
	t.Helper()
	return resolve(t, dir, rel)
}

func resolve(t *testing.T, dir *storefs.Root, rel string) string {
	t.Helper()
	abs, err := dir.Resolve(rel)
	require.NoError(t, err)
	return abs
}

func rels(files []storefs.File) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Rel
	}
	return out
}

func TestFiles_SkipsDerivedAndLockFiles(t *testing.T) {
	files, err := dataDir(t).Files()
	require.NoError(t, err)

	require.Equal(t, []string{
		"dk_candidates/pending/2026/2026-09-12_candidates.jsonl",
		"dk_journal/2026/2026-09-11_journal.jsonl",
		"dk_journal/2026/2026-09-12_journal.jsonl",
	}, rels(files))
}

func TestPush_SecondRunUploadsOnlyChangedFiles(t *testing.T) {
	dir := dataDir(t)
	dest, err := remote.NewLocal(t.TempDir())
	require.NoError(t, err)

	first, err := backup.Push(t.Context(), dir, dest, discard())
	require.NoError(t, err)
	require.Equal(t, 3, first.Uploaded)
	require.Equal(t, 0, first.Skipped)
	require.True(t, first.Since.IsZero(), "first dump has nothing to compare against")

	// Nothing touched: the whole tree is skipped.
	second, err := backup.Push(t.Context(), dir, dest, discard())
	require.NoError(t, err)
	require.Equal(t, 0, second.Uploaded)
	require.Equal(t, 3, second.Skipped)
	require.False(t, second.Since.IsZero(), "second dump compares against the first")

	// One partition appended to: only that file goes.
	appendTo(t, dir, "dk_journal/2026/2026-09-12_journal.jsonl", `{"id":"d","ts":"2026-09-12T11:00:00Z"}`+"\n")
	third, err := backup.Push(t.Context(), dir, dest, discard())
	require.NoError(t, err)
	require.Equal(t, 1, third.Uploaded)
	require.Equal(t, 2, third.Skipped)
}

// A compaction rewrite can leave the size unchanged, so mtime has to catch it.
func TestPush_DetectsRewriteOfSameSize(t *testing.T) {
	dir := dataDir(t)
	dest, err := remote.NewLocal(t.TempDir())
	require.NoError(t, err)

	_, err = backup.Push(t.Context(), dir, dest, discard())
	require.NoError(t, err)

	rel := "dk_journal/2026/2026-09-11_journal.jsonl"
	write(t, dir, rel, `{"id":"z","ts":"2026-09-11T10:00:00Z"}`+"\n")
	abs := resolve(t, dir, rel)
	future := time.Now().Add(time.Second)
	require.NoError(t, os.Chtimes(abs, future, future))

	stats, err := backup.Push(t.Context(), dir, dest, discard())
	require.NoError(t, err)
	require.Equal(t, 1, stats.Uploaded)
}

func TestRestore_RoundTripsTheTree(t *testing.T) {
	src := dataDir(t)
	dest, err := remote.NewLocal(t.TempDir())
	require.NoError(t, err)
	_, err = backup.Push(t.Context(), src, dest, discard())
	require.NoError(t, err)

	into, err := storefs.Open(t.TempDir())
	require.NoError(t, err)
	stats, err := backup.Restore(t.Context(), into, dest, discard())
	require.NoError(t, err)
	require.Equal(t, 3, stats.Downloaded)

	original, err := src.Files()
	require.NoError(t, err)
	restored, err := into.Files()
	require.NoError(t, err)
	require.Equal(t, rels(original), rels(restored))

	want, err := os.ReadFile(resolveIn(t, src, "dk_journal/2026/2026-09-12_journal.jsonl"))
	require.NoError(t, err)
	got, err := os.ReadFile(resolveIn(t, into, "dk_journal/2026/2026-09-12_journal.jsonl"))
	require.NoError(t, err)
	require.Equal(t, want, got)

	// The derived index is deliberately not part of the dump.
	require.NoFileExists(t, resolveIn(t, into, "index.db"))

	// Re-restoring is a no-op: every file already matches the manifest hash.
	again, err := backup.Restore(t.Context(), into, dest, discard())
	require.NoError(t, err)
	require.Equal(t, 0, again.Downloaded)
	require.Equal(t, 3, again.Skipped)
}

func TestPush_WritesContentUnderDataPrefix(t *testing.T) {
	dir := dataDir(t)
	root := t.TempDir()
	dest, err := remote.NewLocal(root)
	require.NoError(t, err)
	_, err = backup.Push(t.Context(), dir, dest, discard())
	require.NoError(t, err)

	require.FileExists(t, filepath.Join(root,
		filepath.FromSlash("data/dk_journal/2026/2026-09-12_journal.jsonl")))
	require.FileExists(t, filepath.Join(root, "manifest.json"))
}

func TestVerify_RepairsTornTrailingLine(t *testing.T) {
	dir := dataDir(t)
	rel := "dk_journal/2026/2026-09-12_journal.jsonl"
	appendTo(t, dir, rel, `{"id":"partial","ts":`) // crash mid-append

	report, err := backup.Verify(dir, false)
	require.NoError(t, err)
	require.False(t, report.OK())
	require.Len(t, report.Problems, 1)
	require.True(t, report.Problems[0].Torn)

	repaired, err := backup.Verify(dir, true)
	require.NoError(t, err)
	require.True(t, repaired.OK())
	require.Equal(t, []string{rel}, repaired.Repaired)

	content, err := os.ReadFile(resolve(t, dir, rel))
	require.NoError(t, err)
	require.Equal(t, `{"id":"b","ts":"2026-09-12T10:00:00Z"}`+"\n", string(content))
}

func TestVerify_ReportsMalformedLineWithoutRewriting(t *testing.T) {
	dir := dataDir(t)
	rel := "dk_journal/2026/2026-09-11_journal.jsonl"
	appendTo(t, dir, rel, "not json\n")

	report, err := backup.Verify(dir, true)
	require.NoError(t, err)
	require.False(t, report.OK(), "a complete but invalid line is never truncated away")
	require.Empty(t, report.Repaired)
}

func appendTo(t *testing.T, dir *storefs.Root, rel, content string) {
	t.Helper()
	abs := resolve(t, dir, rel)
	f, err := os.OpenFile(abs, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	// Ensure the change is visible to a size+mtime comparison on filesystems
	// with coarse timestamps.
	future := time.Now().Add(time.Second)
	require.NoError(t, os.Chtimes(abs, future, future))
}
