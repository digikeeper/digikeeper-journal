package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/digikeeper/digikeeper-journal/internal/backup"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/index"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/jsonlstore"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
	"github.com/digikeeper/digikeeper-journal/pkg/flock"
)

func runPush(ctx context.Context, args []string, cfg config) error {
	fs := flag.NewFlagSet("push", flag.ContinueOnError)
	data := dataFlag(fs, cfg.JournalStorage.Path)
	spec := remoteFlag(fs, cfg.Backup.Remote)
	verbose := verboseFlag(fs, cfg.Backup.Verbose)
	if err := fs.Parse(args); err != nil {
		return err
	}

	dest, err := openRemote(*spec, cfg.Backup)
	if err != nil {
		return err
	}
	dir, err := storefs.Open(*data)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()

	stats, err := backup.Push(ctx, dir, dest, logger(*verbose))
	if err != nil {
		return err
	}

	since := "never"
	if !stats.Since.IsZero() {
		since = stats.Since.Format(time.RFC3339)
	}
	fmt.Printf("pushed %d/%d files (%s), %d unchanged, %d gone; last dump: %s\n",
		stats.Uploaded, stats.Total, humanBytes(stats.BytesSent),
		stats.Skipped, stats.Removed, since)
	return nil
}

func runRestore(ctx context.Context, args []string, cfg config) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	data := dataFlag(fs, cfg.JournalStorage.Path)
	spec := remoteFlag(fs, cfg.Backup.Remote)
	verbose := verboseFlag(fs, cfg.Backup.Verbose)
	if err := fs.Parse(args); err != nil {
		return err
	}

	src, err := openRemote(*spec, cfg.Backup)
	if err != nil {
		return err
	}
	dir, err := storefs.Open(*data)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()

	stats, err := backup.Restore(ctx, dir, src, logger(*verbose))
	if err != nil {
		return err
	}
	fmt.Printf("restored %d/%d files (%s), %d already current; dump of %s\n",
		stats.Downloaded, stats.Total, humanBytes(stats.BytesReceived),
		stats.Skipped, stats.DumpedAt.Format(time.RFC3339))
	fmt.Println("next: dkbackup verify -repair, then dkbackup reindex")
	return nil
}

func runVerify(args []string, cfg config) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	data := dataFlag(fs, cfg.JournalStorage.Path)
	repair := fs.Bool("repair", false,
		"truncate files back to their last complete record (drops a torn trailing line)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	dir, err := storefs.Open(*data)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()

	report, err := backup.Verify(dir, *repair)
	if err != nil {
		return err
	}

	for _, rel := range report.Repaired {
		fmt.Printf("repaired %s\n", rel)
	}
	for _, problem := range report.Problems {
		fmt.Fprintln(os.Stderr, problem)
	}
	fmt.Printf("checked %d files, %d records, %d problems\n",
		report.Files, report.Records, len(report.Problems))

	if !report.OK() {
		return fmt.Errorf("%d problems found", len(report.Problems))
	}
	return nil
}

func runReindex(ctx context.Context, args []string, cfg config) error {
	fs := flag.NewFlagSet("reindex", flag.ContinueOnError)
	data := dataFlag(fs, cfg.JournalStorage.Path)
	verbose := verboseFlag(fs, cfg.Backup.Verbose)
	journalMode := fs.String("journal-mode", cfg.SQLite.JournalMode, "SQLite journal_mode for the rebuilt index")
	busyTimeout := fs.Duration("busy-timeout", cfg.SQLite.BusyTimeout, "SQLite busy_timeout for the rebuilt index")
	if err := fs.Parse(args); err != nil {
		return err
	}

	root, err := storefs.Open(*data)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	serverLock, err := flock.Acquire(root.ServerLockPath())
	if err != nil {
		if errors.Is(err, flock.ErrLocked) {
			return fmt.Errorf("reindex needs the server stopped: %w", err)
		}
		return err
	}
	defer func() { _ = serverLock.Release() }()

	tmpPath := root.IndexPath() + ".rebuild"
	if err := removeSQLite(tmpPath); err != nil {
		return err
	}
	defer func() { _ = removeSQLite(tmpPath) }()

	idx, err := index.NewIdx(tmpPath, index.Config{
		JournalMode: *journalMode,
		BusyTimeout: *busyTimeout,
	})
	if err != nil {
		return fmt.Errorf("open rebuild index: %w", err)
	}
	journal := jsonlstore.NewJSONLWriter(root.JournalDir(), storefs.JournalKind)

	var stats backup.ReindexStats
	reindexErr := root.WithShared(ctx, func(storefs.Tx) error {
		files, err := root.Files()
		if err != nil {
			return err
		}
		stats, err = backup.Reindex(ctx, files, journal, idx, logger(*verbose))
		return err
	})
	closeErr := errors.Join(journal.Close(), idx.Close())
	if err := errors.Join(reindexErr, closeErr); err != nil {
		return err
	}

	if err := removeSQLiteSidecars(root.IndexPath()); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, root.IndexPath()); err != nil {
		return fmt.Errorf("install rebuilt index: %w", err)
	}

	fmt.Printf("rebuilt %s from %d partitions, %d records\n",
		root.IndexPath(), stats.Partitions, stats.Records)
	return nil
}

func removeSQLite(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return removeSQLiteSidecars(path)
}

func removeSQLiteSidecars(path string) error {
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s%s: %w", path, suffix, err)
		}
	}
	return nil
}

// humanBytes renders a byte count for a one-line summary.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for size := n / unit; size >= unit; size /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
