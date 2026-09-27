package candidate

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/digikeeper/digikeeper-journal/internal/domain/command/model"
	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/internal/domain/errs"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
)

// Submit creates a schema-validated candidate replacement for an existing journal record.
func (s *Service) Submit(ctx context.Context, req SubmitRequest, requestID string) (model.Candidate, error) {
	candidateID := uuid.NewString()
	partition := core.PartitionFromTime(req.OriginalTimestamp)
	var c model.Candidate
	err := s.storage.WithShared(ctx, func(tx storefs.Tx) error {
		original, err := s.journalStorage.ReadRecord(ctx, tx, req.RecordID, partition)
		if err != nil {
			return fmt.Errorf("candidate: lookup original %s: %w", req.RecordID, err)
		}
		typeName := req.Type
		if typeName == "" {
			typeName = original.Type
		}
		version := original.Meta.SchemaVersion
		if req.SchemaVersion != nil {
			version = *req.SchemaVersion
		} else if typeName != original.Type {
			version, err = s.schemas.LatestVersion(typeName)
			if err != nil {
				return fmt.Errorf("candidate schema: %w: %w", err, errs.ErrInvalidInput)
			}
		}
		replacement := original
		replacement.Type, replacement.Meta.SchemaVersion = typeName, version
		replacement.Facets, replacement.Data = req.Facets, req.Data
		if replacement.Facets == nil {
			replacement.Facets = map[string][]string{}
		}
		if replacement.Data == nil {
			replacement.Data = map[string]any{}
		}
		if err := s.schemas.Validate(typeName, version, replacement); err != nil {
			return fmt.Errorf("candidate schema validation: %w: %w", err, errs.ErrInvalidInput)
		}
		c = model.Candidate{ID: candidateID, RecordID: req.RecordID, Record: replacement, OriginalTimestamp: req.OriginalTimestamp, CreatedAt: time.Now().UTC()}
		if err := s.storage.AppendCandidate(ctx, tx, c); err != nil {
			return fmt.Errorf("candidate: append: %w", err)
		}
		return nil
	})
	if err != nil {
		return model.Candidate{}, err
	}
	s.logger.InfoContext(ctx, "candidate submitted", slog.String("candidate_id", c.ID), slog.String("record_id", c.RecordID), slog.String("request_id", requestID))
	return c, nil
}
