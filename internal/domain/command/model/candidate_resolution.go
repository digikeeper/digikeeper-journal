package model

import "github.com/digikeeper/digikeeper-journal/internal/domain/core"

// CandidateResolution is the command decision for a pending candidate.
type CandidateResolution string

const (
	ApplyResolution CandidateResolution = "apply"
	DenyResolution  CandidateResolution = "deny"
)

func (r CandidateResolution) IsValid() bool {
	return r == ApplyResolution || r == DenyResolution
}

// EndState maps a valid command decision to the candidate's lifecycle state.
func (r CandidateResolution) EndState() (core.CandidateState, bool) {
	switch r {
	case ApplyResolution:
		return core.CandidateApplied, true
	case DenyResolution:
		return core.CandidateDenied, true
	default:
		return "", false
	}
}
