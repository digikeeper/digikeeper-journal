package commandstore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/digikeeper/digikeeper-journal/internal/domain/appmetric"
	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/internal/domain/errs"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/index"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/jsonlstore"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
	"github.com/digikeeper/digikeeper-journal/pkg/flock"
)

type CMDStore struct {
	dir      *storefs.Dir
	flock    *flock.Lock
	rawStore *jsonlstore.JSONLWriter
	idx      *index.Store
}

// NewStore returns a journal store over an storefs; Fixes drift in journal tree
// and takes server.lock for guarantee single process lifetime.
func NewStore(dir *storefs.Dir, idx *index.Store) (*CMDStore, error) {
	serverLock, err := flock.Acquire(dir.ServerLockPath())
	if err != nil {
		return nil, err
	}

	st := &CMDStore{
		dir:      dir,
		flock:    serverLock,
		rawStore: jsonlstore.NewJSONLWriter(dir.JournalDir(), storefs.JournalKind),
		idx:      idx,
	}
	st.recoverCompaction()

	return st, nil
}

// Append appends a record to the journal.
func (s *CMDStore) Append(ctx context.Context, tx storefs.Tx, record core.Record) error {
	if err := s.dir.Check(tx); err != nil {
		return fmt.Errorf("store: append: %w", err)
	}

	key, err := s.rawStore.Append(record)
	if err != nil {
		return fmt.Errorf("store: write: %w", err)
	}
	if err := s.idx.Insert(ctx, index.Row{
		File:      key,
		Tags:      record.Tags,
		Types:     []string{record.Type},
		Timestamp: record.Timestamp,
	}); err != nil {
		return fmt.Errorf("store: index failed: %w, %w", err, errs.ErrIndexFailed)
	}
	appmetric.RecordsAppended.Add(1)

	return nil
}

// recoverCompaction removes compaction temporaries data.
func (s *CMDStore) recoverCompaction() {
	for _, tmp := range s.dir.CompactTemps() {
		if err := os.Remove(tmp); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to remove orphaned compaction temp file",
				slog.String("file", tmp), slog.Any("error", err))
		} else if err == nil {
			slog.Info("removed orphaned compaction temp file", slog.String("file", tmp))
		}
	}
}

// ReadPartition reads all records from the given partition.
func (s *CMDStore) ReadPartition(_ context.Context, tx storefs.Tx, p core.Partition) ([]core.Record, error) {
	if err := s.dir.Check(tx); err != nil {
		return nil, fmt.Errorf("store: read partition %s: %w", p, err)
	}

	relPath := s.rawStore.BuildRelPath(p)
	records, err := s.rawStore.Read(relPath)
	if err != nil {
		return nil, fmt.Errorf("store: read partition %s: %w", p, err)
	}
	return records, nil
}

// ReadRecord scans one partition for the requested record.
func (s *CMDStore) ReadRecord(ctx context.Context, tx storefs.Tx, recordID string, p core.Partition) (core.Record, error) {
	records, err := s.ReadPartition(ctx, tx, p)
	if err != nil {
		return core.Record{}, err
	}
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return core.Record{}, err
		}
		if record.ID == recordID {
			return record, nil
		}
	}
	return core.Record{}, fmt.Errorf("store: record %s partition %s: %w", recordID, p, errs.ErrRecordNotFound)
}

// ReplacePartition atomically rewrites the partition with records. Satisfies compaction.JournalStorage.
func (s *CMDStore) ReplacePartition(_ context.Context, tx storefs.WriteTx, p core.Partition, records []core.Record) error {
	if err := s.dir.Check(tx); err != nil {
		return fmt.Errorf("store: replace partition %s: %w", p, err)
	}

	relPath := s.rawStore.BuildRelPath(p)
	if err := s.rawStore.ReplaceFile(relPath, records); err != nil {
		return fmt.Errorf("store: replace partition %s: %w", p, err)
	}
	return nil
}

// WithShared is the way to run operations under a shared transaction.
func (s *CMDStore) WithShared(ctx context.Context, fn func(tx storefs.Tx) error) error {
	return s.dir.WithShared(ctx, fn)
}

// WithExclusive is the way to run operations under an exclusive transaction.
func (s *CMDStore) WithExclusive(ctx context.Context, fn func(tx storefs.WriteTx) error) error {
	return s.dir.WithExclusive(ctx, fn)
}

// Close: incapsulate closing of all rawStore and flock resources.
func (s *CMDStore) Close() error {
	return errors.Join(s.rawStore.Close(), s.flock.Release())
}
