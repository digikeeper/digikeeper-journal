package model

import (
	"time"

	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
)

type Candidate struct {
	ID                string              `json:"id"`
	RecordID          string              `json:"record_id"`
	OriginalTimestamp time.Time           `json:"original_ts"`
	Record            core.Record         `json:"record"`
	CreatedAt         time.Time           `json:"created_at"`
	Action            CandidateResolution `json:"action,omitempty"`
	ResolvedBy        string              `json:"resolved_by,omitempty"`
	ResolvedAt        time.Time           `json:"resolved_at,omitzero"`
	Reason            string              `json:"reason,omitempty"`
	ClientID          string              `json:"client_id,omitempty"`
}

func (c *Candidate) IsResolved() bool {
	return c.Action.IsValid()
}
