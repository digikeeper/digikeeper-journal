package compaction

import (
	"context"
	"log/slog"

	"github.com/digikeeper/digikeeper-journal/internal/domain/command/model"
	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
)

// DataLock guards the whole data directory.
// Compaction rewrites journal and candidate files together.
type DataLock interface {
	// WithExclusive stops the world for the whole of fn and yields the transaction
	// the stores require, so none of them can be reached without it.
	WithExclusive(ctx context.Context, fn func(tx storefs.WriteTx) error) error
}

// JournalStorage reads and rewrites journal partitions.
type JournalStorage interface {
	ReadPartition(ctx context.Context, tx storefs.Tx, partition core.Partition) ([]core.Record, error)
	ReplacePartition(ctx context.Context, tx storefs.WriteTx, partition core.Partition, records []core.Record) error
}

// CandidateStorage reads, cleans up, and audits candidate partitions.
type CandidateStorage interface {
	// ListApplied returns resolved-apply candidates awaiting compaction.
	ListApplied(ctx context.Context, tx storefs.Tx, partition core.Partition) ([]model.Candidate, error)
	// DeleteApplied removes candidates from applied/ after successful compaction.
	DeleteApplied(ctx context.Context, tx storefs.WriteTx, partition core.Partition, candidateIDs []string) error
	// AuditAppend records a completed compaction event for the audit trail.
	AuditAppend(ctx context.Context, tx storefs.Tx, event CandidateAuditEvent) error
}

// IndexRebuilder updates the file-level index after a partition rewrite.
type IndexRebuilder interface {
	RebuildPartition(ctx context.Context, partition core.Partition, records []core.Record) error
}

// Service drains applied candidates by rewriting journal partitions.
type Service struct {
	journalStorage JournalStorage
	candidates     CandidateStorage
	index          IndexRebuilder
	lock           DataLock
	logger         *slog.Logger
}

func NewService(
	journalStorage JournalStorage,
	candidates CandidateStorage,
	index IndexRebuilder,
	lock DataLock,
	logger *slog.Logger,
) *Service {
	return &Service{
		journalStorage: journalStorage,
		candidates:     candidates,
		index:          index,
		lock:           lock,
		logger:         logger,
	}
}
