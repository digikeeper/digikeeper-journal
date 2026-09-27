package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/digikeeper/digikeeper-journal/internal/domain/appmetric"
	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
	"github.com/digikeeper/digikeeper-journal/internal/jsonx"
	"github.com/digikeeper/digikeeper-journal/pkg/sqlitedsn"
	"github.com/digikeeper/digikeeper-journal/pkg/timefmt"
)

type SearchParams struct {
	Facets   map[string][]string
	Types    []string
	From, To time.Time
}
type Config struct {
	JournalMode string
	BusyTimeout time.Duration
}
type Store struct{ db *sql.DB }

func NewIdx(path string, cfg Config) (*Store, error) {
	db, err := sql.Open("sqlite", sqlitedsn.File(path, sqliteDSNOptions(cfg)...))
	if err != nil {
		return nil, fmt.Errorf("sqlite: open: %w", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		return nil, fmt.Errorf("sqlite: ping: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		return nil, fmt.Errorf("sqlite: migrate: %w", err)
	}
	return s, nil
}
func sqliteDSNOptions(cfg Config) []sqlitedsn.Option {
	var opts []sqlitedsn.Option
	if cfg.JournalMode != "" {
		opts = append(opts, sqlitedsn.Pragma(sqlitedsn.PragmaJournalMode, cfg.JournalMode))
	}
	if cfg.BusyTimeout > 0 {
		opts = append(opts, sqlitedsn.Pragma(sqlitedsn.PragmaBusyTimeout, int(cfg.BusyTimeout/time.Millisecond)))
	}
	return opts
}
func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS file_index (file TEXT NOT NULL PRIMARY KEY, facets TEXT NOT NULL DEFAULT '{}', types TEXT NOT NULL DEFAULT '[]', min_ts TEXT NOT NULL, max_ts TEXT NOT NULL);`)
	return err
}

type Row struct {
	File      string
	Facets    map[string][]string
	Types     []string
	Timestamp time.Time
}

func (s *Store) Insert(ctx context.Context, row Row) error {
	start := time.Now()
	defer func() { appmetric.RecordIndexLatency(time.Since(start)) }()
	ts := timefmt.Format(row.Timestamp)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var rawFacets, rawTypes string
	existingFacets := map[string][]string{}
	var existingTypes []string
	err = tx.QueryRowContext(ctx, `SELECT facets, types FROM file_index WHERE file = ?`, row.File).Scan(&rawFacets, &rawTypes)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return fmt.Errorf("sqlite: select: %w", err)
	default:
		if err := jsonx.Unmarshal([]byte(rawFacets), &existingFacets); err != nil {
			return fmt.Errorf("sqlite: unmarshal facets: %w", err)
		}
		if err := jsonx.Unmarshal([]byte(rawTypes), &existingTypes); err != nil {
			return fmt.Errorf("sqlite: unmarshal types: %w", err)
		}
	}
	facetsJSON, err := jsonx.Marshal(mergeFacets(existingFacets, row.Facets))
	if err != nil {
		return fmt.Errorf("sqlite: marshal facets: %w", err)
	}
	typesJSON, err := jsonx.Marshal(mergeStrings(existingTypes, row.Types))
	if err != nil {
		return fmt.Errorf("sqlite: marshal types: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO file_index (file, facets, types, min_ts, max_ts) VALUES (?, ?, ?, ?, ?) ON CONFLICT(file) DO UPDATE SET facets=?, types=?, min_ts=MIN(file_index.min_ts,excluded.min_ts), max_ts=MAX(file_index.max_ts,excluded.max_ts)`, row.File, string(facetsJSON), string(typesJSON), ts, ts, string(facetsJSON), string(typesJSON))
	if err != nil {
		return fmt.Errorf("sqlite: upsert: %w", err)
	}
	return tx.Commit()
}

type Result struct {
	File   string              `json:"file"`
	Facets map[string][]string `json:"facets"`
	Types  []string            `json:"types"`
	MinTS  time.Time           `json:"min_ts"`
	MaxTS  time.Time           `json:"max_ts"`
}

const maxFilesPerSearch = 366

func (s *Store) Search(ctx context.Context, p SearchParams) ([]Result, error) {
	start := time.Now()
	defer func() { appmetric.RecordIndexLatency(time.Since(start)) }()
	where := []string{"1=1"}
	args := []any{}
	if !p.From.IsZero() {
		where = append(where, "max_ts >= ?")
		args = append(args, timefmt.Format(p.From))
	}
	if !p.To.IsZero() {
		where = append(where, "min_ts <= ?")
		args = append(args, timefmt.Format(p.To))
	}
	for key, values := range p.Facets {
		if len(values) == 0 {
			continue
		}
		where = append(where, facetAnyOf(len(values)))
		args = append(args, key)
		for _, value := range values {
			args = append(args, value)
		}
	}
	if len(p.Types) > 0 {
		where = append(where, jsonAnyOf("types", len(p.Types)))
		for _, typ := range p.Types {
			args = append(args, typ)
		}
	}
	q := fmt.Sprintf(`SELECT file, facets, types, min_ts, max_ts FROM file_index WHERE %s ORDER BY max_ts DESC LIMIT %d`, strings.Join(where, " AND "), maxFilesPerSearch)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: query: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var results []Result
	for rows.Next() {
		var r Result
		var minTS, maxTS, facetsRaw, typesRaw string
		if err := rows.Scan(&r.File, &facetsRaw, &typesRaw, &minTS, &maxTS); err != nil {
			return nil, fmt.Errorf("sqlite: scan: %w", err)
		}
		r.MinTS, err = timefmt.Parse(minTS)
		if err != nil {
			return nil, fmt.Errorf("sqlite: parse min_ts %q: %w", minTS, err)
		}
		r.MaxTS, err = timefmt.Parse(maxTS)
		if err != nil {
			return nil, fmt.Errorf("sqlite: parse max_ts %q: %w", maxTS, err)
		}
		if err := jsonx.Unmarshal([]byte(facetsRaw), &r.Facets); err != nil {
			return nil, fmt.Errorf("sqlite: unmarshal facets: %w", err)
		}
		if err := jsonx.Unmarshal([]byte(typesRaw), &r.Types); err != nil {
			return nil, fmt.Errorf("sqlite: unmarshal types: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}
func (s *Store) RebuildPartition(ctx context.Context, partition core.Partition, records []core.Record) error {
	start := time.Now()
	defer func() { appmetric.RecordIndexLatency(time.Since(start)) }()
	file := storefs.JournalKey(partition)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM file_index WHERE file = ?`, file); err != nil {
		return fmt.Errorf("sqlite: delete partition index: %w", err)
	}
	if len(records) == 0 {
		return tx.Commit()
	}
	var minTS, maxTS time.Time
	facets := map[string][]string{}
	var types []string
	for i, record := range records {
		if i == 0 || record.Timestamp.Before(minTS) {
			minTS = record.Timestamp
		}
		if i == 0 || record.Timestamp.After(maxTS) {
			maxTS = record.Timestamp
		}
		facets = mergeFacets(facets, record.Facets)
		types = append(types, record.Type)
	}
	facetsJSON, err := jsonx.Marshal(facets)
	if err != nil {
		return fmt.Errorf("sqlite: marshal facets: %w", err)
	}
	typesJSON, err := jsonx.Marshal(mergeStrings(nil, types))
	if err != nil {
		return fmt.Errorf("sqlite: marshal types: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO file_index (file, facets, types, min_ts, max_ts) VALUES (?, ?, ?, ?, ?)`, file, string(facetsJSON), string(typesJSON), timefmt.Format(minTS), timefmt.Format(maxTS))
	if err != nil {
		return fmt.Errorf("sqlite: insert partition index: %w", err)
	}
	return tx.Commit()
}
func (s *Store) Close() error { return s.db.Close() }
func jsonAnyOf(col string, n int) string {
	return fmt.Sprintf(`EXISTS (SELECT 1 FROM json_each(%s) WHERE value IN (%s))`, col, strings.Repeat("?,", n)[:n*2-1])
}
func facetAnyOf(n int) string {
	return fmt.Sprintf(`EXISTS (SELECT 1 FROM json_each(facets) AS facet, json_each(facet.value) AS value WHERE facet.key = ? AND value.value IN (%s))`, strings.Repeat("?,", n)[:n*2-1])
}
func mergeStrings(existing, incoming []string) []string {
	set := make(map[string]struct{}, len(existing)+len(incoming))
	for _, v := range existing {
		set[v] = struct{}{}
	}
	for _, v := range incoming {
		set[v] = struct{}{}
	}
	merged := make([]string, 0, len(set))
	for v := range set {
		merged = append(merged, v)
	}
	return merged
}
func mergeFacets(existing, incoming map[string][]string) map[string][]string {
	merged := make(map[string][]string, len(existing)+len(incoming))
	for key, values := range existing {
		merged[key] = mergeStrings(nil, values)
	}
	for key, values := range incoming {
		merged[key] = mergeStrings(merged[key], values)
	}
	return merged
}
