package candidate

import (
	"time"

	"github.com/digikeeper/digikeeper-journal/internal/domain/command/model"
)

type SubmitRequest struct {
	RecordID          string              `json:"rec_id"`
	OriginalTimestamp time.Time           `json:"orig_ts"`
	Type              string              `json:"type"`
	SchemaVersion     *int                `json:"schm_ver,omitempty"`
	Facets            map[string][]string `json:"facets"`
	Data              map[string]any      `json:"d"`
	ClientID          string              `json:"-"`
}

type ResolveRequest struct {
	Resolutions []ResolveItem `json:"resolutions"`
	ResolvedBy  string        `json:"-"`
	ClientID    string        `json:"-"`
}

type ResolveItem struct {
	CandidateID string                    `json:"candidate_id"`
	Action      model.CandidateResolution `json:"action"`
	Reason      string                    `json:"reason,omitempty"`
}
