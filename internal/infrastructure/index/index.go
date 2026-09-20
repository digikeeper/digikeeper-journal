package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // register "sqlite" driver

	"github.com/digikeeper/digikeeper-journal/internal/domain/appmetric"
	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
	"github.com/digikeeper/digikeeper-journal/internal/jsonx"
	"github.com/digikeeper/digikeeper-journal/pkg/sqlitedsn"
	"github.com/digikeeper/digikeeper-journal/pkg/timefmt"
)

// SearchParams filters a [Store.Search] query.
// Tags and Types use OR semantics: a file matches if it contains any of the listed values.
type SearchParams struct {
	Tags  []string
	Types []string
	From  time.Time
	To    time.Time
}

type Config struct {
	JournalMode string
	BusyTimeout time.Duration
}

// Store is a file-level metadata index over JSONL files backed by SQLite.
// Each row represents one JSONL file with aggregated tags, types, and time range.
type Store struct {
	db *sql.DB
}

// New opens (or creates) a SQLite database at path and runs migrations.
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
		opts = append(opts, sqlitedsn.Pragma(
			sqlitedsn.PragmaBusyTimeout,
			int(cfg.BusyTimeout/time.Millisecond),
		))
	}
	return opts
}

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS file_index (
			file   TEXT NOT NULL PRIMARY KEY,
			tags   TEXT NOT NULL DEFAULT '[]',
			types  TEXT NOT NULL DEFAULT '[]',
			min_ts TEXT NOT NULL,
			max_ts TEXT NOT NULL
		);
	`)
	return err
}

// Row is the input to [Store.Insert].
// It carries metadata from a single append that gets merged into the file's row.
type Row struct {
	File      string
	Tags      []string
	Types     []string
	Timestamp time.Time
}

// Insert upserts file-level metadata: merges tags/types and expands the time range.
func (s *Store) Insert(ctx context.Context, row Row) error {
	start := time.Now()
	defer func() { appmetric.RecordIndexLatency(time.Since(start)) }()

	ts := timefmt.Format(row.Timestamp)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var existingTags, existingTypes []string
	var rawTags, rawTypes string
	err = tx.QueryRowContext(ctx,
		`SELECT tags, types FROM file_index WHERE file = ?`, row.File,
	).Scan(&rawTags, &rawTypes)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		// first insert — nothing to merge
	case err != nil:
		return fmt.Errorf("sqlite: select: %w", err)
	default:
		if err := jsonx.Unmarshal([]byte(rawTags), &existingTags); err != nil {
			return fmt.Errorf("sqlite: unmarshal tags: %w", err)
		}
		if err := jsonx.Unmarshal([]byte(rawTypes), &existingTypes); err != nil {
			return fmt.Errorf("sqlite: unmarshal types: %w", err)
		}
	}

	tagsJSON, err := jsonx.Marshal(mergeStrings(existingTags, row.Tags))
	if err != nil {
		return fmt.Errorf("sqlite: marshal tags: %w", err)
	}
	typesJSON, err := jsonx.Marshal(mergeStrings(existingTypes, row.Types))
	if err != nil {
		return fmt.Errorf("sqlite: marshal types: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO file_index (file, tags, types, min_ts, max_ts) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(file) DO UPDATE SET
			tags   = ?,
			types  = ?,
			min_ts = MIN(file_index.min_ts, excluded.min_ts),
			max_ts = MAX(file_index.max_ts, excluded.max_ts)
	`, row.File, string(tagsJSON), string(typesJSON), ts, ts,
		string(tagsJSON), string(typesJSON))
	if err != nil {
		return fmt.Errorf("sqlite: upsert: %w", err)
	}
	return tx.Commit()
}

// Result is a single file returned by [Store.Search].
type Result struct {
	File  string    `json:"file"`
	Tags  []string  `json:"tags"`
	Types []string  `json:"types"`
	MinTS time.Time `json:"min_ts"`
	MaxTS time.Time `json:"max_ts"`
}

// maxFilesPerSearch caps the number of JSONL files scanned per query.
// Each file is one day of journal; 366 covers a full rolling year.
const maxFilesPerSearch = 366

// Search finds JSONL files whose metadata matches the given filters.
// Tags and Types use OR semantics: a file matches if it contains any listed value.
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
	if len(p.Tags) > 0 {
		where = append(where, jsonAnyOf("tags", len(p.Tags)))
		for _, t := range p.Tags {
			args = append(args, t)
		}
	}
	if len(p.Types) > 0 {
		where = append(where, jsonAnyOf("types", len(p.Types)))
		for _, t := range p.Types {
			args = append(args, t)
		}
	}

	q := fmt.Sprintf(
		`SELECT file, tags, types, min_ts, max_ts
		   FROM file_index
		  WHERE %s
		  ORDER BY max_ts DESC
		  LIMIT %d`,
		strings.Join(where, " AND "),
		maxFilesPerSearch,
	)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []Result
	for rows.Next() {
		var r Result
		var minTS, maxTS, tagsRaw, typesRaw string
		if err := rows.Scan(&r.File, &tagsRaw, &typesRaw, &minTS, &maxTS); err != nil {
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
		if err := jsonx.Unmarshal([]byte(tagsRaw), &r.Tags); err != nil {
			return nil, fmt.Errorf("sqlite: unmarshal tags: %w", err)
		}
		if err := jsonx.Unmarshal([]byte(typesRaw), &r.Types); err != nil {
			return nil, fmt.Errorf("sqlite: unmarshal types: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// RebuildPartition replaces the file-level index row for a rewritten partition.
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
	var tags, types []string
	for i, record := range records {
		if i == 0 || record.Timestamp.Before(minTS) {
			minTS = record.Timestamp
		}
		if i == 0 || record.Timestamp.After(maxTS) {
			maxTS = record.Timestamp
		}
		tags = append(tags, record.Tags...)
		types = append(types, record.Type)
	}

	tagsJSON, err := jsonx.Marshal(mergeStrings(nil, tags))
	if err != nil {
		return fmt.Errorf("sqlite: marshal tags: %w", err)
	}
	typesJSON, err := jsonx.Marshal(mergeStrings(nil, types))
	if err != nil {
		return fmt.Errorf("sqlite: marshal types: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO file_index (file, tags, types, min_ts, max_ts)
		VALUES (?, ?, ?, ?, ?)
	`, file, string(tagsJSON), string(typesJSON), timefmt.Format(minTS), timefmt.Format(maxTS))
	if err != nil {
		return fmt.Errorf("sqlite: insert partition index: %w", err)
	}
	return tx.Commit()
}

// Close closes the underlying database connection pool.
func (s *Store) Close() error {
	return s.db.Close()
}

// jsonAnyOf builds an EXISTS clause that matches if any value in the named
// JSON array column equals one of n placeholders.
func jsonAnyOf(col string, n int) string {
	return fmt.Sprintf(
		`EXISTS (SELECT 1 FROM json_each(%s) WHERE value IN (%s))`,
		col, strings.Repeat("?,", n)[:n*2-1],
	)
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
