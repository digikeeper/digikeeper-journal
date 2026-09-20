package command

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/internal/domain/errs"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
)

func (s *Service) AppendRecord(
	ctx context.Context,
	req AppendRequest,
	requestID string,
) (core.Record, error) {
	record := core.Record{
		ID:   uuid.NewString(),
		Type: req.Type,
		Meta: core.RecordMeta{
			SchemaVersion: 1,
			Revision:      1,
			Src:           s.sourceRepo.ResolveID(req.ClientID),
		},
		RequestID: requestID,
		CreatedAt: time.Now().UTC(),
		Timestamp: req.Timestamp,
		Tags:      req.Tags,
		Data:      req.Data,
	}
	if record.Tags == nil {
		record.Tags = []string{}
	}
	if record.Data == nil {
		record.Data = map[string]any{}
	}

	err := s.storage.WithShared(ctx, func(tx storefs.Tx) error {
		return s.storage.Append(ctx, tx, record)
	})
	if err != nil {
		if errors.Is(err, errs.ErrIndexFailed) {
			s.logger.ErrorContext(ctx, "meta index failed — record is durable in storage",
				slog.String("record_id", record.ID),
				slog.String("request_id", requestID),
				slog.Any("error", err),
			)
			return record, err
		}
		return core.Record{}, err
	}

	s.logger.InfoContext(ctx, "record appended and indexed",
		slog.String("record_id", record.ID),
		slog.String("request_id", requestID),
	)

	return record, nil
}
