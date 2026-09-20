package compaction

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/digikeeper/digikeeper-journal/internal/domain/command/model"
	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
)

// errNothingToCompact unwinds the transaction when there is no work, so the
// empty case releases the lock through the same path as every other exit.
var errNothingToCompact = errors.New("compact: nothing to compact")

// Compact drains applied candidates for a partition by rewriting the records.
//
//  1. Take the data directory's exclusive lock.
//  2. Read applied/{partition} — if empty, nothing to do.
//  3. Read all records from journal/{partition}.
//  4. Substitute: replace matching records; append unmatched applied candidates.
//  5. Atomic rewrite (write-temp → fsync → rename via renameio).
//  6. Delete compacted candidates from applied/.
//  7. Rebuild index for the rewritten partition.
//  8. Append event to candidate audit.
//
// Steps 6–8 are best-effort. If they fail, next compaction sees the same
// applied candidates; the revision CAS in applyCandidate detects them as
// already applied, skips them (not counted in appliedCount/audit), and
// deletes them. The end state is idempotent.
func (s *Service) Compact(ctx context.Context, req CompactRequest) error {
	var appliedCount, totalRecords int

	// Stop the world for the whole sequence:
	//  read a partition and rewrite it
	err := s.lock.WithExclusive(ctx, func(tx storefs.WriteTx) error {
		// 2. Read applied candidates. Inside the lock, so the set compacted is
		// the set that was there when the journal was read.
		applied, err := s.candidates.ListApplied(ctx, tx, req.Partition)
		if err != nil {
			return fmt.Errorf("compact: list applied: %w", err)
		}
		if len(applied) == 0 {
			s.logger.InfoContext(ctx, "no applied candidates, nothing to compact",
				slog.String("partition", req.Partition.String()))
			return errNothingToCompact
		}

		// 3. Read all records.
		records, err := s.journalStorage.ReadPartition(ctx, tx, req.Partition)
		if err != nil {
			return fmt.Errorf("compact: read partition %s: %w", req.Partition, err)
		}

		// 4. Substitute.
		rewritten, count := s.applySubstitutions(records, applied)
		appliedCount, totalRecords = count, len(rewritten)

		// 5. Atomic rewrite.
		if err := s.journalStorage.ReplacePartition(ctx, tx, req.Partition, rewritten); err != nil {
			return fmt.Errorf("compact: rewrite partition %s: %w", req.Partition, err)
		}

		// 6. Delete compacted candidates (best-effort).
		ids := make([]string, len(applied))
		for i, c := range applied {
			ids[i] = c.ID
		}
		if err := s.candidates.DeleteApplied(ctx, tx, req.Partition, ids); err != nil {
			s.logger.ErrorContext(ctx, "delete applied failed, will re-apply on next run",
				slog.String("partition", req.Partition.String()),
				slog.Any("error", err))
		}

		// 7. Rebuild index (best-effort).
		if err := s.index.RebuildPartition(ctx, req.Partition, rewritten); err != nil {
			s.logger.ErrorContext(ctx, "index rebuild failed, partition is consistent but index may be stale",
				slog.String("partition", req.Partition.String()),
				slog.Any("error", err))
		}

		// 8. Candidate audit (best-effort).
		event := CandidateAuditEvent{
			Partition:    req.Partition,
			AppliedCount: appliedCount,
			CompletedAt:  time.Now().UTC(),
		}
		if err := s.candidates.AuditAppend(ctx, tx, event); err != nil {
			s.logger.ErrorContext(ctx, "candidate audit append failed",
				slog.String("partition", req.Partition.String()),
				slog.Any("error", err))
		}
		return nil
	})
	switch {
	case errors.Is(err, errNothingToCompact):
		return nil
	case err != nil:
		return err
	}

	s.logger.InfoContext(ctx, "compaction completed",
		slog.String("partition", req.Partition.String()),
		slog.Int("applied", appliedCount),
		slog.Int("total_records", totalRecords),
	)

	return nil
}

// applySubstitutions replaces records whose ID matches an applied candidate and
// appends applied candidates with no matching record to the end of the partition.
func (s *Service) applySubstitutions(
	records []core.Record,
	applied []model.Candidate,
) ([]core.Record, int) {
	candidatesByRecordID := make(map[string]model.Candidate, len(applied))
	for _, candidate := range applied {
		candidatesByRecordID[candidate.RecordID] = candidate
	}

	appliedCount := 0
	seen := make(map[string]struct{}, len(applied))
	result := make([]core.Record, 0, len(records)+len(applied))
	for _, record := range records {
		candidate, ok := candidatesByRecordID[record.ID]
		if !ok {
			result = append(result, record)
			continue
		}

		replacement, applied := s.applyCandidate(record, candidate)
		result = append(result, replacement)
		seen[record.ID] = struct{}{}
		if applied {
			appliedCount++
		}
	}

	for _, candidate := range applied {
		if _, ok := seen[candidate.RecordID]; ok {
			continue
		}
		replacement := candidate.Record
		replacement.Meta.Revision = nextRevision(replacement.Meta.Revision)
		result = append(result, replacement)
		seen[candidate.RecordID] = struct{}{}
		appliedCount++
	}

	return result, appliedCount
}

func (s *Service) applyCandidate(current core.Record, candidate model.Candidate) (core.Record, bool) {
	replacement := candidate.Record
	currentRevision := normalizedRevision(current.Meta.Revision)
	candidateRevision := normalizedRevision(replacement.Meta.Revision)

	// A candidate is a compare-and-swap operation over the revision it copied
	// at submission. It is stale after a successful rewrite whose applied-file
	// cleanup failed, and it is invalid if it was somehow created from a future
	// revision. In both cases, preserve the current record.
	if candidateRevision != currentRevision {
		s.logger.Warn("skipping candidate with mismatched record revision",
			slog.String("candidate_id", candidate.ID),
			slog.String("record_id", current.ID),
			slog.Int("candidate_revision", candidateRevision),
			slog.Int("current_revision", currentRevision),
		)
		return current, false
	}

	replacement.Meta.Revision = nextRevision(current.Meta.Revision)
	return replacement, true
}

func nextRevision(revision int) int {
	return normalizedRevision(revision) + 1
}

func normalizedRevision(revision int) int {
	// Records written before revisions existed omit "r". Treat them as the
	// initial logical revision so their first replacement becomes revision 2.
	if revision < 1 {
		return 1
	}
	return revision
}
