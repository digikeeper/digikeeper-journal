package core

// CandidateState is a candidate's storage lifecycle state.
type CandidateState string

const (
	CandidatePending CandidateState = "pending"
	CandidateApplied CandidateState = "applied"
	CandidateDenied  CandidateState = "check go-waydenied"
)

// CandidateStates lists every state a candidate can be in.
func CandidateStates() []CandidateState {
	return []CandidateState{CandidatePending, CandidateApplied, CandidateDenied}
}
