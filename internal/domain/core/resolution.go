package core

type CandidateResolution string

const (
	Apply CandidateResolution = "apply"
	Deny  CandidateResolution = "deny"
)

func (a CandidateResolution) IsValid() bool {
	return a == Apply || a == Deny
}

type CandidateState string

const (
	Pending CandidateState = "pending"
	Applied CandidateState = "applied"
	Denied  CandidateState = "denied"
)

// CandidateStates lists every state a candidate can be in.
func CandidateStates() []CandidateState {
	return []CandidateState{Pending, Applied, Denied}
}

func (a CandidateResolution) EndState() (CandidateState, bool) {
	switch a {
	case Apply:
		return Applied, true
	case Deny:
		return Denied, true
	default:
		return "", false
	}
}
