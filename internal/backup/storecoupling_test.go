package backup_test

import (
	"context"
	"io/fs"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/digikeeper/digikeeper-journal/internal/backup"
	"github.com/digikeeper/digikeeper-journal/internal/domain/command/compaction"
	commandmodel "github.com/digikeeper/digikeeper-journal/internal/domain/command/model"
	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/candidatestore"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/commandstore"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/index"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/jsonlstore"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
)

// liveDataDir writes through the real stores rather than fixture files, so
// these tests fail if the write path starts producing files the dump does not
// know about. The returned func shuts the stores down, releasing the data
// directory the way stopping the server would.
func liveDataDir(t *testing.T) (*liveStores, func()) {
	t.Helper()
	dir := t.TempDir()

	dd, err := storefs.Open(dir)
	require.NoError(t, err)

	idx, err := index.NewIdx(dd.IndexPath(), index.Config{JournalMode: "WAL", BusyTimeout: time.Second})
	require.NoError(t, err)

	journal, err := commandstore.NewStore(dd, idx)
	require.NoError(t, err)

	var once sync.Once
	shutdown := func() {
		once.Do(func() {
			require.NoError(t, journal.Close()) // releases server.lock
			require.NoError(t, idx.Close())
		})
	}
	t.Cleanup(shutdown)

	// The stores require a transaction, so seeding stands in for the domain
	// services that would normally have opened one.
	ctx := t.Context()
	candidates := candidatestore.New(dd)
	require.NoError(t, journal.WithShared(ctx, func(tx storefs.Tx) error {
		yesterday := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
		today := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
		for i, ts := range []time.Time{yesterday, today, today.Add(time.Hour)} {
			require.NoError(t, journal.Append(ctx, tx, core.Record{
				ID:        string(rune('a' + i)),
				Timestamp: ts,
				Type:      "note",
				Facets:    map[string][]string{"topic": {"t"}},
				Data:      map[string]any{"n": i},
			}))
		}

		require.NoError(t, candidates.AppendCandidate(ctx, tx, commandmodel.Candidate{
			ID:                "cand-1",
			RecordID:          "a",
			OriginalTimestamp: yesterday,
			CreatedAt:         today,
		}))
		require.NoError(t, candidates.AuditAppend(ctx, tx, compaction.CandidateAuditEvent{
			Partition:    core.PartitionFromTime(yesterday),
			AppliedCount: 1,
			CompletedAt:  today,
		}))
		return nil
	}))

	return &liveStores{root: dir, dir: dd, journal: journal, candidates: candidates}, shutdown
}

// liveStores is a data directory together with the stores that wrote it.
type liveStores struct {
	root       string // the data directory's path, for reopening it
	dir        *storefs.Root
	journal    *commandstore.CMDStore
	candidates *candidatestore.Store
}

// allJSONL finds every records file in the tree, whatever produced it.
func allJSONL(t *testing.T, dir string) []string {
	t.Helper()
	var found []string
	require.NoError(t, filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !storefs.IsRecordsFile(d.Name()) {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		require.NoError(t, err)
		found = append(found, filepath.ToSlash(rel))
		return nil
	}))
	sort.Strings(found)
	return found
}

// TestWalk_CoversEverythingTheStoresWrite is the guard against the dump
// silently losing a tree: if a store starts writing records somewhere new,
// Walk has to grow to match, or this fails.
func TestWalk_CoversEverythingTheStoresWrite(t *testing.T) {
	live, _ := liveDataDir(t)

	files, err := live.dir.Files()
	require.NoError(t, err)

	require.Equal(t, allJSONL(t, live.root), rels(files),
		"every records file the stores wrote must be in the dump")
	require.NotEmpty(t, files)
}

// TestPushLock_ContendsWithTheStoreLock pins the quiesce mechanism to the
// stores' own lock: a dump that took a different lock would copy files
// mid-append while believing it was safe. Contention is the only honest test —
// comparing paths would just restate the code.
func TestPushLock_ContendsWithTheStoreLock(t *testing.T) {
	live, _ := liveDataDir(t)
	dumpDir, err := storefs.Open(live.root)
	require.NoError(t, err)

	// Both stores guard the same data directory, so either one stops the world.
	err = live.journal.WithExclusive(t.Context(), func(storefs.WriteTx) error {
		blocked, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()
		return dumpDir.WithExclusive(blocked, func(storefs.WriteTx) error { return nil })
	})
	require.ErrorIs(t, err, context.DeadlineExceeded,
		"a dump would have copied the tree while a store held the data lock")

	require.NoError(t, dumpDir.WithExclusive(t.Context(), func(storefs.WriteTx) error { return nil }),
		"dump cannot lock once the store released")
}

// TestReindex_ReproducesTheIndexTheWritePathBuilt checks the rebuilt index
// against the one the server maintained incrementally, which is the only way to
// know the two agree on partition keys.
func TestReindex_ReproducesTheIndexTheWritePathBuilt(t *testing.T) {
	stores, shutdown := liveDataDir(t)
	dir := stores.dir

	live, err := index.NewIdx(dir.IndexPath(), index.Config{JournalMode: "WAL", BusyTimeout: time.Second})
	require.NoError(t, err)
	before, err := live.Search(t.Context(), index.SearchParams{})
	require.NoError(t, err)
	require.NoError(t, live.Close())
	require.NotEmpty(t, before)

	shutdown()

	files, err := dir.Files()
	require.NoError(t, err)
	journal := jsonlstore.NewJSONLWriter(dir.JournalDir(), storefs.JournalKind)
	t.Cleanup(func() { _ = journal.Close() })
	rebuilt, err := index.NewIdx(filepath.Join(t.TempDir(), "index.db"), index.Config{
		JournalMode: "WAL", BusyTimeout: time.Second,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = rebuilt.Close() })

	stats, err := backup.Reindex(t.Context(), files, journal, rebuilt, discard())
	require.NoError(t, err)
	require.Equal(t, 2, stats.Partitions)
	require.Equal(t, 3, stats.Records)

	after, err := rebuilt.Search(t.Context(), index.SearchParams{})
	require.NoError(t, err)

	require.Equal(t, before, after, "rebuilt index must match the one the write path built")
}
