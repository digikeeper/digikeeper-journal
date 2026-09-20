package querystore

import (
	"context"
	"fmt"

	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/internal/domain/query/model"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/index"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/jsonlstore"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
)

type Store struct {
	rawStore *jsonlstore.JSONLWriter
	idx      *index.Store
}

func NewStore(jsonJournalDir string, idx *index.Store) *Store {
	return &Store{
		rawStore: jsonlstore.NewJSONLWriter(jsonJournalDir, storefs.JournalKind),
		idx:      idx,
	}
}

// Search satisfies query.MetaStorage.
func (s *Store) Search(ctx context.Context, p model.SearchParams) ([]string, error) {
	results, err := s.idx.Search(ctx, index.SearchParams{
		Tags:  p.Tags,
		Types: p.Types,
		From:  p.From,
		To:    p.To,
	})
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(results))
	for i, r := range results {
		keys[i] = r.File
	}
	return keys, nil
}

// Read satisfies query.Storage.
func (s *Store) Read(ctx context.Context, keys []string) ([]core.Record, error) {
	var records []core.Record
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("store: read cancelled: %w", err)
		}
		fileRecords, err := s.rawStore.Read(key)
		if err != nil {
			return nil, fmt.Errorf("store: read %s: %w", key, err)
		}
		records = append(records, fileRecords...)
	}
	return records, nil
}
