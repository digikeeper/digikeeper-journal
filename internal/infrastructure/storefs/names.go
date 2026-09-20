package storefs

import (
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
)

// On-disk names are unexported so callers cannot assemble paths independently.
const (
	journalDirName        = "dk_journal"
	candidatesDirName     = "dk_candidates"
	candidateAuditDirName = "candidateaudit"
	indexFileName         = "index.db"
	serverLockName        = "server.lock"
	dataLockName          = "dk.lock"
)

// File kinds are the suffixes after the partition name.
const (
	JournalKind        = "journal"
	CandidateKind      = "candidates"
	CandidateAuditKind = "candidateaudit"
)

func CandidateDir(state core.CandidateState) (string, bool) {
	switch state {
	case core.CandidatePending, core.CandidateApplied, core.CandidateDenied:
		return string(state), true
	default:
		return "", false
	}
}

// Extensions and suffixes shared by the JSONL layer and Dir.CompactTemps.
const (
	JSONLExt         = ".jsonl"
	CompactTmpSuffix = ".compact.tmp"
)

// candidateDirs lists the directories in the candidate tree.
func candidateDirs() []string {
	dirs := make([]string, 0, len(core.CandidateStates())+1)
	for _, state := range core.CandidateStates() {
		if dir, ok := CandidateDir(state); ok {
			dirs = append(dirs, dir)
		}
	}
	return append(dirs, candidateAuditDirName)
}

// PartitionKey returns a partition's slash-separated path within its tree:
//
//	2026/2026-09-12_journal.jsonl
func PartitionKey(kind string, p core.Partition) string {
	year := strconv.Itoa(p.Year())
	fileName := p.String() + "_" + string(kind) + JSONLExt
	return path.Join(year, fileName)
}

// JournalKey returns the index key for a journal partition.
func JournalKey(p core.Partition) string {
	return PartitionKey(JournalKind, p)
}

// candidateKey returns a candidate partition's path within the candidate tree.
func candidateKey(dir string, p core.Partition) string {
	return path.Join(dir, PartitionKey(CandidateKind, p))
}

// auditKey returns a partition's audit log within the candidate tree.
func auditKey(p core.Partition) string {
	return path.Join(candidateAuditDirName, PartitionKey(CandidateAuditKind, p))
}

// IsRecordsFile reports whether a file name holds records, as opposed to a lock
// or a compaction temporary.
func IsRecordsFile(name string) bool {
	return strings.HasSuffix(name, JSONLExt)
}

// PartitionOfFile extracts the partition from any path whose base name is
// <partition>_<kind>.jsonl. It reports false for names not of that shape.
func PartitionOfFile(name string) (core.Partition, bool) {
	base := path.Base(filepath.ToSlash(name))
	prefix, _, found := strings.Cut(base, "_")
	if !found {
		return core.Partition{}, false
	}
	p, err := core.ParsePartition(prefix)
	if err != nil {
		return core.Partition{}, false
	}
	return p, true
}
