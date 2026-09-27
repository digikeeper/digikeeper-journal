package candidate

import (
	"context"
	"log/slog"

	"github.com/digikeeper/digikeeper-journal/internal/domain/command/model"
	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
)

type Storage interface {
	WithShared(ctx context.Context, fn func(tx storefs.Tx) error) error
	WithExclusive(ctx context.Context, fn func(tx storefs.WriteTx) error) error
	AppendCandidate(ctx context.Context, tx storefs.Tx, c model.Candidate) error
	ListPending(ctx context.Context, tx storefs.Tx, partition core.Partition) ([]model.Candidate, error)
	MoveCandidates(ctx context.Context, tx storefs.WriteTx, partition core.Partition, applied, denied []model.Candidate) error
}

type JournalStorage interface {
	ReadRecord(ctx context.Context, tx storefs.Tx, recordID string, partition core.Partition) (core.Record, error)
}

type SchemaRegistry interface {
	LatestVersion(typeName string) (int, error)
	Validate(typeName string, version int, record core.Record) error
}

type Service struct {
	storage        Storage
	journalStorage JournalStorage
	schemas        SchemaRegistry
	logger         *slog.Logger
}

func NewService(s Storage, ls JournalStorage, schemas SchemaRegistry, logger *slog.Logger) *Service {
	return &Service{storage: s, journalStorage: ls, schemas: schemas, logger: logger}
}
