package candidate

import (
	"time"

	"github.com/digikeeper/digikeeper-journal/internal/domain/command/model"
)

type SubmitRequest struct {
	//  ID of the original record.
	RecordID string `json:"record_id"`
	// OriginalTimestamp is the Timestamp of the original record.
	OriginalTimestamp time.Time `json:"original_timestamp"`

	Type string         `json:"type"`
	Tags []string       `json:"tags"`
	Data map[string]any `json:"data"`

	ClientID string `json:"-"`
}

// ResolveRequest is the input for resolving one or more candidates.
type ResolveRequest struct {
	Resolutions []ResolveItem `json:"resolutions"`

	// ResolvedBy identifies who is performing the resolution (user/system ID).
	ResolvedBy string `json:"-"`

	ClientID string `json:"-"`
}

// ResolveItem is a single resolution decision from the caller.
type ResolveItem struct {
	CandidateID string                    `json:"candidate_id"`
	Action      model.CandidateResolution `json:"action"`
	Reason      string                    `json:"reason,omitempty"`
}
