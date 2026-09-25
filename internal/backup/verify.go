package backup

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
	"github.com/digikeeper/digikeeper-journal/internal/jsonx"
)

// Problem is one defect found in a JSONL file.
type Problem struct {
	File   string
	Line   int // 1-based; 0 when the problem is not line-scoped
	Detail string
	Torn   bool // a truncated trailing line, repairable by dropping it
}

func (p Problem) String() string {
	if p.Line == 0 {
		return fmt.Sprintf("%s: %s", p.File, p.Detail)
	}
	return fmt.Sprintf("%s:%d: %s", p.File, p.Line, p.Detail)
}

// VerifyReport is the result of one verify pass.
type VerifyReport struct {
	Files    int
	Records  int
	Problems []Problem
	Repaired []string // files truncated back to their last complete record
}

// OK reports whether the tree is free of defects.
func (r VerifyReport) OK() bool { return len(r.Problems) == 0 }

// Verify parses every JSONL file under dataPath. A crash mid-append leaves a
// partial trailing line; with repair set, such a line is truncated away, which
// is safe because the format is line-oriented and the record was never
// acknowledged. Any other malformed line is reported, never rewritten.
func Verify(dir *storefs.Root, repair bool) (VerifyReport, error) {
	files, err := dir.Files()
	if err != nil {
		return VerifyReport{}, err
	}

	report := VerifyReport{Files: len(files)}
	for _, f := range files {
		abs, err := dir.Resolve(f.Rel)
		if err != nil {
			return report, err
		}
		content, err := os.ReadFile(abs)
		if err != nil {
			return report, fmt.Errorf("backup: read %s: %w", f.Rel, err)
		}

		records, problems, keep := scanJSONL(f.Rel, content)
		report.Records += records
		report.Problems = append(report.Problems, problems...)

		if repair && keep < int64(len(content)) && anyTorn(problems) {
			if err := os.Truncate(abs, keep); err != nil {
				return report, fmt.Errorf("backup: truncate %s: %w", f.Rel, err)
			}
			report.Repaired = append(report.Repaired, f.Rel)
			report.Problems = dropTorn(report.Problems)
		}
	}

	// Compaction temporaries mean a compaction died midway; the server removes
	// them at startup, but a restored tree should not carry them.
	for _, tmp := range dir.CompactTemps() {
		report.Problems = append(report.Problems, Problem{
			File:   filepath.ToSlash(filepath.Base(tmp)),
			Detail: "orphaned compaction temporary; remove before starting the server",
		})
	}
	return report, nil
}

// scanJSONL validates each line and returns the record count, the problems
// found, and the offset of the end of the last complete, valid line.
func scanJSONL(rel string, content []byte) (records int, problems []Problem, keep int64) {
	var offset int64
	for line := 1; len(content) > 0; line++ {
		i := bytes.IndexByte(content, '\n')
		if i < 0 {
			// No terminator: the write did not finish.
			problems = append(problems, Problem{
				File: rel, Line: line, Torn: true,
				Detail: "truncated trailing line (no newline terminator)",
			})
			return records, problems, keep
		}

		raw := content[:i]
		content = content[i+1:]
		offset += int64(i) + 1

		if len(bytes.TrimSpace(raw)) == 0 {
			problems = append(problems, Problem{File: rel, Line: line, Detail: "blank line"})
			continue
		}
		var probe map[string]any
		if err := jsonx.Unmarshal(raw, &probe); err != nil {
			problems = append(problems, Problem{
				File: rel, Line: line,
				Detail: fmt.Sprintf("invalid JSON: %v", err),
			})
			continue
		}
		records++
		keep = offset
	}
	return records, problems, keep
}

func anyTorn(problems []Problem) bool {
	return slices.ContainsFunc(problems, func(p Problem) bool { return p.Torn })
}

func dropTorn(problems []Problem) []Problem {
	return slices.DeleteFunc(problems, func(p Problem) bool { return p.Torn })
}
