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
	dir      *storefs.Dir
	rawStore *jsonlstore.JSONLWriter
	idx      *index.Store
}

func NewStore(dir *storefs.Dir, idx *index.Store) *Store {
	return &Store{dir: dir, rawStore: jsonlstore.NewJSONLWriter(dir.JournalDir(), storefs.JournalKind), idx: idx}
}

func (s *Store) Search(ctx context.Context, p model.SearchParams) ([]string, error) {
	results, err := s.idx.Search(ctx, index.SearchParams{Facets: p.Facets, Types: p.Types, From: p.From, To: p.To})
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(results))
	for i, r := range results {
		keys[i] = r.File
	}
	return keys, nil
}

// Read takes the same shared data-directory lock as append readers, preventing
// compaction from replacing a partition while JSONL is being scanned.
func (s *Store) Read(ctx context.Context, keys []string) ([]core.Record, error) {
	var records []core.Record
	err := s.dir.WithShared(ctx, func(_ storefs.Tx) error {
		for _, key := range keys {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("store: read cancelled: %w", err)
			}
			fileRecords, err := s.rawStore.Read(key)
			if err != nil {
				return fmt.Errorf("store: read %s: %w", key, err)
			}
			records = append(records, fileRecords...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return records, nil
}
