package query

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sort"

	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/internal/domain/query/model"
)

func (s *Service) SearchRecords(ctx context.Context, p model.SearchParams) ([]core.Record, error) {
	keys, err := s.metaStorage.Search(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("query: meta search: %w", err)
	}
	records, err := s.storage.Read(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("query: read records: %w", err)
	}
	results := filterRecords(records, p)
	s.log.InfoContext(ctx, "search completed", slog.Any("facets", p.Facets), slog.Any("types", p.Types), slog.Int("files", len(keys)), slog.Int("raw_records", len(records)), slog.Int("count", len(results)), slog.Int("limit", p.Limit))
	return results, nil
}

func filterRecords(records []core.Record, p model.SearchParams) []core.Record {
	limit := p.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	var result []core.Record
	for _, record := range records {
		if !p.From.IsZero() && record.Timestamp.Before(p.From) {
			continue
		}
		if !p.To.IsZero() && record.Timestamp.After(p.To) {
			continue
		}
		if !matchesFacets(record.Facets, p.Facets) {
			continue
		}
		if len(p.Types) > 0 && !slices.Contains(p.Types, record.Type) {
			continue
		}
		result = append(result, record)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Timestamp.After(result[j].Timestamp) })
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

// matchesFacets requires every requested key to match at least one requested value.
func matchesFacets(record, requested map[string][]string) bool {
	for key, values := range requested {
		if len(values) == 0 {
			continue
		}
		actual := record[key]
		if !slices.ContainsFunc(values, func(value string) bool { return slices.Contains(actual, value) }) {
			return false
		}
	}
	return true
}
