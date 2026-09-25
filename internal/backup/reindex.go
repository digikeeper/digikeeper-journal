package backup

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
)

// JournalReader supplies canonical records without deciding how they are stored.
type JournalReader interface {
	ReadPartition(partition core.Partition) ([]core.Record, error)
}

// PartitionIndexer replaces one partition in a derived index.
type PartitionIndexer interface {
	RebuildPartition(ctx context.Context, partition core.Partition, records []core.Record) error
}

// ReindexStats summarizes one rebuild.
type ReindexStats struct {
	Partitions int
	Records    int
}

// Reindex replays journal files into an index supplied by the composition root.
// Opening, locking, closing, and installing that index belong to the command.
func Reindex(
	ctx context.Context,
	files []storefs.File,
	journal JournalReader,
	index PartitionIndexer,
	logger *slog.Logger,
) (ReindexStats, error) {
	var stats ReindexStats
	for _, file := range files {
		if !file.InJournal() {
			continue
		}
		if err := ctx.Err(); err != nil {
			return stats, err
		}

		partition, ok := file.Partition()
		if !ok {
			logger.Warn("skipping unrecognised journal file", slog.String("file", file.Rel))
			continue
		}

		records, err := journal.ReadPartition(partition)
		if err != nil {
			return stats, fmt.Errorf("backup: read %s: %w", file.Rel, err)
		}
		if err := index.RebuildPartition(ctx, partition, records); err != nil {
			return stats, fmt.Errorf("backup: index %s: %w", file.Rel, err)
		}

		stats.Partitions++
		stats.Records += len(records)
		logger.Info("indexed", slog.String("file", file.Rel), slog.Int("records", len(records)))
	}
	return stats, nil
}
