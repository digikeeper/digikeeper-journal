package command

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/internal/domain/errs"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
)

func (s *Service) AppendRecord(ctx context.Context, req AppendRequest, requestID string) (core.Record, error) {
	version, err := s.schemas.LatestVersion(req.Type)
	if err != nil {
		return core.Record{}, fmt.Errorf("append schema: %w: %w", err, errs.ErrInvalidInput)
	}
	record := core.Record{
		ID: uuid.NewV7().String(), Type: req.Type,
		Meta:      core.RecordMeta{SchemaVersion: version, Revision: 1, Source: s.sourceRepo.ResolveID(req.ClientID)},
		RequestID: requestID, CreatedAt: time.Now().UTC(), Timestamp: req.Timestamp,
		Facets: req.Facets, Data: req.Data,
	}
	if record.Facets == nil {
		record.Facets = map[string][]string{}
	}
	if record.Data == nil {
		record.Data = map[string]any{}
	}
	if err := s.schemas.Validate(record.Type, version, record); err != nil {
		return core.Record{}, fmt.Errorf("append schema validation: %w: %w", err, errs.ErrInvalidInput)
	}

	err = s.storage.WithShared(ctx, func(tx storefs.Tx) error { return s.storage.Append(ctx, tx, record) })
	if err != nil {
		if errors.Is(err, errs.ErrIndexFailed) {
			s.logger.ErrorContext(ctx, "meta index failed — record is durable in storage", slog.String("record_id", record.ID), slog.String("request_id", requestID), slog.Any("error", err))
			return record, err
		}
		return core.Record{}, err
	}
	s.logger.InfoContext(ctx, "record appended and indexed", slog.String("record_id", record.ID), slog.String("request_id", requestID))
	return record, nil
}
