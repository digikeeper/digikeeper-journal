package candidate

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/digikeeper/digikeeper-journal/internal/domain/command/model"
	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/internal/domain/errs"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
)

// Resolve validates caller resolutions, enforces the all-at-once invariant,
// and persists the decision by moving candidates from pending to applied/denied.
//
// The move is atomic
func (s *Service) Resolve(
	ctx context.Context,
	partition core.Partition,
	req ResolveRequest,
) ([]model.Candidate, error) {
	var (
		pending         []model.Candidate
		applied, denied []model.Candidate
	)
	err := s.storage.WithExclusive(ctx, func(tx storefs.WriteTx) error {
		var err error
		pending, err = s.storage.ListPending(ctx, tx, partition)
		if err != nil {
			return fmt.Errorf("candidate: list pending: %w", err)
		}

		pendingByID := make(map[string]*model.Candidate, len(pending))
		for i := range pending {
			pendingByID[pending[i].ID] = &pending[i]
		}

		// Validate each resolution item.
		for _, item := range req.Resolutions {
			if !item.Action.IsValid() {
				return fmt.Errorf("candidate %s action %q: %w", item.CandidateID, item.Action, errs.ErrUnknownAction)
			}
			if _, ok := pendingByID[item.CandidateID]; !ok {
				return fmt.Errorf("candidate %s partition %s: %w", item.CandidateID, partition, errs.ErrCandidateNotPending)
			}
		}

		// Enforce all-at-once: every pending candidate resolved, max one Apply per record.
		if err := validateAllAtOnce(pending, req.Resolutions); err != nil {
			return err
		}

		// Stamp resolution metadata onto each candidate.
		now := time.Now().UTC()
		itemByID := make(map[string]ResolveItem, len(req.Resolutions))
		for _, item := range req.Resolutions {
			itemByID[item.CandidateID] = item
		}

		applied, denied = nil, nil
		for i := range pending {
			c := &pending[i]
			item := itemByID[c.ID]
			c.Action = item.Action
			c.ResolvedBy = req.ResolvedBy
			c.ResolvedAt = now
			c.Reason = item.Reason
			c.ClientID = req.ClientID

			if item.Action == core.Apply {
				applied = append(applied, *c)
			} else {
				denied = append(denied, *c)
			}
		}

		// Atomic move: write destinations → fsync → delete pending.
		// On write/fsync failure, destinations are cleaned up, pending unchanged.
		if err := s.storage.MoveCandidates(ctx, tx, partition, applied, denied); err != nil {
			return fmt.Errorf("candidate: move candidates: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	s.logger.InfoContext(ctx, "candidates resolved",
		slog.String("partition", partition.String()),
		slog.String("resolved_by", req.ResolvedBy),
		slog.Int("applied", len(applied)),
		slog.Int("denied", len(denied)),
	)

	return pending, nil
}

func validateAllAtOnce(pending []model.Candidate, resolutions []ResolveItem) error {
	resolutionsByID := make(map[string]ResolveItem, len(resolutions))
	for _, item := range resolutions {
		resolutionsByID[item.CandidateID] = item
	}

	for _, c := range pending {
		if _, ok := resolutionsByID[c.ID]; !ok {
			return fmt.Errorf(
				"candidate %s record %s: %w",
				c.ID, c.RecordID, errs.ErrIncompleteResolution,
			)
		}
	}

	applyByRecordCounts := make(map[string]int)
	for _, c := range pending {
		item := resolutionsByID[c.ID]
		if item.Action == core.Apply {
			applyByRecordCounts[c.RecordID]++
		}
	}

	for recordID, count := range applyByRecordCounts {
		if count > 1 {
			return fmt.Errorf(
				"record %s count %d: %w",
				recordID, count, errs.ErrMultipleApply,
			)
		}
	}

	return nil
}
