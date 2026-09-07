package jsonlstore

import (
	stdjson "encoding/json"
	jsonv2 "encoding/json/v2"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
)

var (
	benchBoolSink   bool
	benchBytesSink  []byte
	benchRecordSink core.Record
)

type benchProfile struct {
	name      string
	tags      int
	dataBytes int
}

type benchData struct {
	record         core.Record
	line           []byte
	filtersMatch   ReadFilters
	filtersNoMatch ReadFilters
}

func BenchmarkJSONLFilter(b *testing.B) {
	data := buildBenchData(b)

	b.ReportAllocs()

	b.Run("gjson_match", func(b *testing.B) {
		for b.Loop() {
			benchBoolSink = matchFilters(data.line, &data.filtersMatch)
		}
		if !benchBoolSink {
			b.Fatal("expected match=true")
		}
	})

	b.Run("gjson_no_match", func(b *testing.B) {
		for b.Loop() {
			benchBoolSink = matchFilters(data.line, &data.filtersNoMatch)
		}
		if benchBoolSink {
			b.Fatal("expected match=false")
		}
	})

	b.Run("jsonv2_unmarshal_2field_match", func(b *testing.B) {
		for b.Loop() {
			benchBoolSink = matchFiltersBy2FieldUnmarshalJSONV2(data.line, &data.filtersMatch)
		}
		if !benchBoolSink {
			b.Fatal("expected match=true")
		}
	})

	b.Run("stdjson_unmarshal_2field_match", func(b *testing.B) {
		for b.Loop() {
			benchBoolSink = matchFiltersBy2FieldUnmarshalStdJSON(data.line, &data.filtersMatch)
		}
		if !benchBoolSink {
			b.Fatal("expected match=true")
		}
	})

	b.Run("jsonv2_unmarshal_full_match", func(b *testing.B) {
		for b.Loop() {
			benchBoolSink = matchFiltersByFullUnmarshalJSONV2(data.line, &data.filtersMatch)
		}
		if !benchBoolSink {
			b.Fatal("expected match=true")
		}
	})

	b.Run("stdjson_unmarshal_full_match", func(b *testing.B) {
		for b.Loop() {
			benchBoolSink = matchFiltersByFullUnmarshalStdJSON(data.line, &data.filtersMatch)
		}
		if !benchBoolSink {
			b.Fatal("expected match=true")
		}
	})
}

func BenchmarkRecordMarshal(b *testing.B) {
	data := buildBenchData(b)

	b.ReportAllocs()

	b.Run("jsonv2", func(b *testing.B) {
		for b.Loop() {
			out, err := jsonv2.Marshal(data.record)
			if err != nil {
				b.Fatal(err)
			}
			benchBytesSink = out
		}
	})

	b.Run("stdjson", func(b *testing.B) {
		for b.Loop() {
			out, err := stdjson.Marshal(data.record)
			if err != nil {
				b.Fatal(err)
			}
			benchBytesSink = out
		}
	})
}

func BenchmarkRecordUnmarshal(b *testing.B) {
	data := buildBenchData(b)

	b.ReportAllocs()

	b.Run("jsonv2", func(b *testing.B) {
		for b.Loop() {
			var out core.Record
			if err := jsonv2.Unmarshal(data.line, &out); err != nil {
				b.Fatal(err)
			}
			benchRecordSink = out
		}
	})

	b.Run("stdjson", func(b *testing.B) {
		for b.Loop() {
			var out core.Record
			if err := stdjson.Unmarshal(data.line, &out); err != nil {
				b.Fatal(err)
			}
			benchRecordSink = out
		}
	})
}

func buildBenchData(b *testing.B) benchData {
	b.Helper()

	profile := resolveBenchProfile()

	ts := time.Date(2026, 3, 1, 12, 30, 45, 0, time.UTC)
	tags := make([]string, profile.tags)
	for i := range tags {
		tags[i] = "tag-" + strconv.Itoa(i)
	}

	payload := strings.Repeat("x", profile.dataBytes)
	record := core.Record{
		ID: "bench-record",
		Meta: core.RecordMeta{
			SchemaVersion: 1,
			Src:           1,
		},
		RequestID: "bench-request",
		CreatedAt: ts.Add(15 * time.Second),
		Timestamp: ts,
		Tags:      tags,
		Data: map[string]any{
			"note":    "benchmark",
			"payload": payload,
			"ctx": map[string]any{
				"source": "bench",
				"kind":   "jsonl",
			},
			"nums": []int{1, 2, 3, 4, 5},
		},
	}

	line, err := jsonv2.Marshal(record)
	if err != nil {
		b.Fatalf("jsonv2 marshal bench data: %v", err)
	}

	matchTag := tags[len(tags)/2]
	from := ts.Add(-1 * time.Hour)
	to := ts.Add(1 * time.Hour)

	return benchData{
		record: record,
		line:   line,
		filtersMatch: ReadFilters{
			From: from,
			To:   to,
			Tags: map[string]struct{}{matchTag: {}},
		},
		filtersNoMatch: ReadFilters{
			From: from,
			To:   to,
			Tags: map[string]struct{}{"tag-not-found": {}},
		},
	}
}

func resolveBenchProfile() benchProfile {
	// Tune with env vars:
	// JSONL_BENCH_PROFILE=small|medium|large
	// JSONL_BENCH_TAGS=<int>
	// JSONL_BENCH_DATA_BYTES=<int>
	profileName := strings.ToLower(strings.TrimSpace(os.Getenv("JSONL_BENCH_PROFILE")))
	// Based on provided production shape: tags are in [1..9], median ~= 2.
	profile := benchProfile{name: "medium", tags: 2, dataBytes: 2048}

	switch profileName {
	case "small":
		profile = benchProfile{name: "small", tags: 1, dataBytes: 256}
	case "large":
		profile = benchProfile{name: "large", tags: 9, dataBytes: 32768}
	}

	if n := readPositiveEnvInt("JSONL_BENCH_TAGS"); n > 0 {
		profile.tags = capTagsCount(n)
	}
	if n := readPositiveEnvInt("JSONL_BENCH_DATA_BYTES"); n > 0 {
		profile.dataBytes = n
	}

	return profile
}

func readPositiveEnvInt(key string) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func capTagsCount(n int) int {
	if n < 1 {
		return 1
	}
	if n > 9 {
		return 9
	}
	return n
}

func matchFiltersBy2FieldUnmarshalJSONV2(line []byte, f *ReadFilters) bool {
	var obj struct {
		Timestamp time.Time `json:"ts"`
		Tags      []string  `json:"tags"`
	}
	if err := jsonv2.Unmarshal(line, &obj); err != nil {
		return false
	}
	return isMatchParsed(obj.Timestamp, obj.Tags, f)
}

func matchFiltersBy2FieldUnmarshalStdJSON(line []byte, f *ReadFilters) bool {
	var obj struct {
		Timestamp time.Time `json:"ts"`
		Tags      []string  `json:"tags"`
	}
	if err := stdjson.Unmarshal(line, &obj); err != nil {
		return false
	}
	return isMatchParsed(obj.Timestamp, obj.Tags, f)
}

func matchFiltersByFullUnmarshalJSONV2(line []byte, f *ReadFilters) bool {
	var e core.Record
	if err := jsonv2.Unmarshal(line, &e); err != nil {
		return false
	}
	return isMatchParsed(e.Timestamp, e.Tags, f)
}

func matchFiltersByFullUnmarshalStdJSON(line []byte, f *ReadFilters) bool {
	var e core.Record
	if err := stdjson.Unmarshal(line, &e); err != nil {
		return false
	}
	return isMatchParsed(e.Timestamp, e.Tags, f)
}

func isMatchParsed(ts time.Time, tags []string, f *ReadFilters) bool {
	if !f.From.IsZero() && ts.Before(f.From) {
		return false
	}
	if !f.To.IsZero() && ts.After(f.To) {
		return false
	}

	if len(f.Tags) == 0 {
		return true
	}
	for _, t := range tags {
		if _, ok := f.Tags[t]; ok {
			return true
		}
	}
	return false
}
