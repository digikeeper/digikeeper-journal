package candidate

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/internal/domain/query/model"
)

// Collision represents a record that has unresolved candidates.
type Collision struct {
	RecordID   string            `json:"rec_id"`
	Partition  core.Partition    `json:"partition"`
	Candidates []model.Candidate `json:"candidates"`
}

// ListCollisions returns records that have unresolved candidates in the
// given partition. Each collision includes the record ID and its pending
// candidates.
func (s *Service) ListCollisions(
	ctx context.Context,
	partition core.Partition,
) ([]Collision, error) {
	pending, err := s.storage.ListPending(ctx, partition)
	if err != nil {
		return nil, fmt.Errorf("candidate query: list pending for %s: %w", partition, err)
	}

	if len(pending) == 0 {
		return nil, nil
	}

	// Group candidates by the record they target.
	byRecord := make(map[string][]model.Candidate)
	for _, c := range pending {
		byRecord[c.RecordID] = append(byRecord[c.RecordID], c)
	}

	collisions := make([]Collision, 0, len(byRecord))
	for recordID, candidates := range byRecord {
		collisions = append(collisions, Collision{
			RecordID:   recordID,
			Partition:  partition,
			Candidates: candidates,
		})
	}

	s.logger.InfoContext(ctx, "listed collisions",
		slog.String("partition", partition.String()),
		slog.Int("records_with_candidates", len(collisions)),
		slog.Int("total_candidates", len(pending)),
	)

	return collisions, nil
}
