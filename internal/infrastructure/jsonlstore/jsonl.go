package jsonlstore

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tidwall/gjson"

	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/internal/domain/errs"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
	"github.com/digikeeper/digikeeper-journal/internal/jsonx"
)

const maxJournalSizeBytes = 10 * 1024 * 1024 // 10 MiB

// JSONLWriter manages JSONL partition files for appending and reading.
//
// Concurrency model:
//   - files (sync.Map) provides lock-free lookup of open file descriptors.
//   - dirMu serializes directory creation only when a new year-directory is needed.
type JSONLWriter struct {
	dir      string
	fileType string
	files    sync.Map   // relPath → *logFD
	dirMu    sync.Mutex // held only during mkdir+open on cache miss
}

type logFD struct {
	fd      *os.File
	relPath string
}

type ReadFilters struct {
	From time.Time
	To   time.Time
	Tags map[string]struct{}
}

type ReadOption func(*ReadFilters)

func WithTimeRange(from, to time.Time) ReadOption {
	return func(f *ReadFilters) { f.From = from; f.To = to }
}

func WithTags(tags ...string) ReadOption {
	return func(f *ReadFilters) {
		f.Tags = make(map[string]struct{}, len(tags))
		for _, t := range tags {
			f.Tags[t] = struct{}{}
		}
	}
}

func NewJSONLWriter(dir, fileType string) *JSONLWriter {
	return &JSONLWriter{
		dir:      dir,
		fileType: fileType,
	}
}

func (w *JSONLWriter) Append(record core.Record) (string, error) {
	line, err := jsonx.Marshal(record)
	if err != nil {
		return "", fmt.Errorf("jsonl: marshal: %w, %w", err, errs.ErrStorageCommon)
	}
	line = append(line, '\n')

	relPath := w.BuildRelPath(core.PartitionFromTime(record.Timestamp))
	lfd, err := w.getOrCreate(relPath)
	if err != nil {
		return "", err
	}

	if _, err := lfd.fd.Write(line); err != nil {
		return "", fmt.Errorf("jsonl: write: %w, %w", err, errs.ErrStorageCommon)
	}
	return relPath, nil
}

// getOrCreate returns the logFD for relPath, creating the file if needed
func (w *JSONLWriter) getOrCreate(relPath string) (*logFD, error) {
	if v, ok := w.files.Load(relPath); ok {
		return v.(*logFD), nil
	}

	w.dirMu.Lock()
	defer w.dirMu.Unlock()

	fpath := filepath.Join(w.dir, relPath)
	dir := filepath.Dir(fpath)
	if _, err := os.Stat(dir); err != nil {
		err := os.MkdirAll(dir, 0o755)
		if err != nil {
			return nil, fmt.Errorf("jsonl: mkdir %s: %w, %w", dir, err, errs.ErrStorageCommon)
		}
	}

	f, err := os.OpenFile(fpath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("jsonl: open %s: %w, %w", fpath, err, errs.ErrStorageCommon)
	}

	lfd := &logFD{fd: f, relPath: relPath}
	if actual, loaded := w.files.LoadOrStore(relPath, lfd); loaded {
		_ = f.Close()
		return actual.(*logFD), nil
	}
	return lfd, nil
}

func (w *JSONLWriter) Read(relPath string, opts ...ReadOption) ([]core.Record, error) {
	var filters ReadFilters
	for _, o := range opts {
		o(&filters)
	}
	hasFilters := len(filters.Tags) > 0 || !filters.From.IsZero() || !filters.To.IsZero()

	fpath := filepath.Join(w.dir, relPath)
	f, err := os.Open(fpath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("jsonl: open %s: %w", relPath, err)
	}
	defer func() { _ = f.Close() }()

	var records []core.Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, bufio.MaxScanTokenSize), maxJournalSizeBytes)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		if hasFilters && !matchFilters(line, &filters) {
			continue
		}
		var r core.Record
		if err := jsonx.Unmarshal(line, &r); err != nil {
			return nil, fmt.Errorf("jsonl: unmarshal in %s: %w", relPath, err)
		}
		records = append(records, r)
	}
	return records, sc.Err()
}

func matchFilters(line []byte, f *ReadFilters) bool {
	if !f.From.IsZero() || !f.To.IsZero() {
		ts := gjson.GetBytes(line, "ts").Time()
		if !f.From.IsZero() && ts.Before(f.From) {
			return false
		}
		if !f.To.IsZero() && ts.After(f.To) {
			return false
		}
	}
	if len(f.Tags) > 0 {
		tagsResult := gjson.GetBytes(line, "tags")
		if !tagsResult.Exists() {
			return false
		}
		matched := false
		tagsResult.ForEach(func(_, v gjson.Result) bool {
			if _, ok := f.Tags[v.String()]; ok {
				matched = true
				return false
			}
			return true
		})
		if !matched {
			return false
		}
	}
	return true
}

// Close closes all open file handles. Call during graceful shutdown.
func (w *JSONLWriter) Close() error {
	var errs []error
	w.files.Range(func(key, value any) bool {
		lfd := value.(*logFD)
		if err := lfd.fd.Close(); err != nil {
			errs = append(errs, err)
		}
		w.files.Delete(key)
		return true
	})
	return errors.Join(errs...)
}

// ReplaceFile atomically rewrites relPath with records:
//
//	write tmp → fsync → rename → evict cache
func (w *JSONLWriter) ReplaceFile(relPath string, records []core.Record) error {
	fpath := filepath.Join(w.dir, relPath)
	tmpPath := fpath + storefs.CompactTmpSuffix

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("jsonl: replace: open tmp: %w", err)
	}

	for _, record := range records {
		line, err := jsonx.Marshal(record)
		if err != nil {
			_ = f.Close()
			_ = os.Remove(tmpPath)
			return fmt.Errorf("jsonl: replace: marshal: %w", err)
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			_ = f.Close()
			_ = os.Remove(tmpPath)
			return fmt.Errorf("jsonl: replace: write: %w", err)
		}
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("jsonl: replace: sync: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("jsonl: replace: close: %w", err)
	}
	if err := os.Rename(tmpPath, fpath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("jsonl: replace: rename: %w", err)
	}

	_ = w.evict(relPath)
	return nil
}

// BuildRelPath returns the partition-relative path for the given partition.
func (w *JSONLWriter) BuildRelPath(p core.Partition) string {
	return storefs.PartitionKey(w.fileType, p)
}

// Dir returns the base directory of the JSONL store.
func (w *JSONLWriter) Dir() string {
	return w.dir
}

// evict closes and removes the cached file descriptor for the given partition path.
func (w *JSONLWriter) evict(relPath string) error {
	v, ok := w.files.LoadAndDelete(relPath)
	if !ok {
		return nil
	}
	lfd := v.(*logFD)
	return lfd.fd.Close()
}
