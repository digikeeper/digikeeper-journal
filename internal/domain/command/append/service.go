package command

import (
	"context"
	"log/slog"

	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
)

type Storage interface {
	WithShared(ctx context.Context, fn func(tx storefs.Tx) error) error
	Append(ctx context.Context, tx storefs.Tx, record core.Record) error
}

type SourceRepo interface{ ResolveID(clientName string) int }

type SchemaRegistry interface {
	LatestVersion(typeName string) (int, error)
	Validate(typeName string, version int, record core.Record) error
}

type Service struct {
	storage    Storage
	sourceRepo SourceRepo
	schemas    SchemaRegistry
	logger     *slog.Logger
}

func NewService(s Storage, sr SourceRepo, schemas SchemaRegistry, logger *slog.Logger) *Service {
	return &Service{storage: s, sourceRepo: sr, schemas: schemas, logger: logger}
}
