package command

import (
	"context"
	"log/slog"

	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
)

type Storage interface {
	// WithShared is sufficient for appending.
	WithShared(ctx context.Context, fn func(tx storefs.Tx) error) error
	Append(ctx context.Context, tx storefs.Tx, record core.Record) error
}

type SourceRepo interface {
	ResolveID(clientName string) int
}

type Service struct {
	storage    Storage
	sourceRepo SourceRepo
	logger     *slog.Logger
}

func NewService(s Storage, sr SourceRepo, logger *slog.Logger) *Service {
	return &Service{storage: s, sourceRepo: sr, logger: logger}
}
