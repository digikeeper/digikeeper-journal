package candidate

import (
	"context"
	"log/slog"

	"github.com/digikeeper/digikeeper-journal/internal/domain/command/model"
	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
)

type Storage interface {
	// WithShared is sufficient for reading operations.
	WithShared(ctx context.Context, fn func(tx storefs.Tx) error) error
	// WithExclusive is sufficient for multiple partition actions.
	WithExclusive(ctx context.Context, fn func(tx storefs.WriteTx) error) error
	AppendCandidate(ctx context.Context, tx storefs.Tx, c model.Candidate) error
	ListPending(ctx context.Context, tx storefs.Tx, partition core.Partition) ([]model.Candidate, error)
	MoveCandidates(ctx context.Context, tx storefs.WriteTx, partition core.Partition, applied, denied []model.Candidate) error
}

// JournalStorage reads existing journal records to verify the original exists.
type JournalStorage interface {
	ReadRecord(ctx context.Context, tx storefs.Tx, recordID string, partition core.Partition) (core.Record, error)
}

// Service handles candidate commands: submit and resolve.
type Service struct {
	storage        Storage
	journalStorage JournalStorage
	logger         *slog.Logger
}

func NewService(s Storage, ls JournalStorage, logger *slog.Logger) *Service {
	return &Service{storage: s, journalStorage: ls, logger: logger}
}
