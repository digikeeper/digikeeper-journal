package candidate

import (
	"context"
	"fmt"

	"github.com/digikeeper/digikeeper-journal/internal/domain/command/model"
	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
)

// ListPending returns the candidate batch currently awaiting resolution.
func (s *Service) ListPending(ctx context.Context, partition core.Partition) ([]model.Candidate, error) {
	var pending []model.Candidate
	err := s.storage.WithShared(ctx, func(tx storefs.Tx) error {
		var err error
		pending, err = s.storage.ListPending(ctx, tx, partition)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("candidate: list pending: %w", err)
	}

	return pending, nil
}
