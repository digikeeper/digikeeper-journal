package command

import (
	"time"

	commandmodel "github.com/digikeeper/digikeeper-journal/internal/domain/command/model"
	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
)

type RecordResource struct {
	id    string
	attrs RecordResourceAttrs
}

type RecordResourceMeta struct {
	SchemaVersion int    `json:"schema_version"`
	Revision      int    `json:"revision"`
	Source        string `json:"source"`
}

type RecordResourceAttrs struct {
	RequestID string    `json:"request_id"`
	CreatedAt time.Time `json:"created_at"`
	Timestamp time.Time `json:"timestamp"`

	Meta RecordResourceMeta `json:"meta"`

	Type string         `json:"type"`
	Tags []string       `json:"tags"`
	Data map[string]any `json:"data"`
}

func (r RecordResource) GetID() string      { return r.id }
func (r RecordResource) GetAttributes() any { return r.attrs }

func NewRecordResource(e core.Record, resolve func(int) string) RecordResource {
	return RecordResource{
		id: e.ID,
		attrs: RecordResourceAttrs{
			Type: e.Type,
			Meta: RecordResourceMeta{
				SchemaVersion: e.Meta.SchemaVersion,
				Revision:      e.Meta.Revision,
				Source:        resolve(e.Meta.Src),
			},
			RequestID: e.RequestID,
			CreatedAt: e.CreatedAt,
			Timestamp: e.Timestamp,
			Tags:      e.Tags,
			Data:      e.Data,
		},
	}
}

type CandidateResource struct {
	id    string
	attrs CandidateResourceAttrs
}

type CandidateResourceAttrs struct {
	RecordID          string                           `json:"record_id"`
	OriginalTimestamp time.Time                        `json:"original_timestamp"`
	Record            core.Record                      `json:"record"`
	CreatedAt         time.Time                        `json:"created_at"`
	Action            commandmodel.CandidateResolution `json:"action,omitempty"`
	Reason            string                           `json:"reason,omitempty"`
	ClientID          string                           `json:"client_id,omitempty"`

	ResolvedBy string    `json:"resolved_by,omitempty"`
	ResolvedAt time.Time `json:"resolved_at,omitzero"`
}

func (r CandidateResource) GetID() string      { return r.id }
func (r CandidateResource) GetAttributes() any { return r.attrs }

func NewCandidateResource(c commandmodel.Candidate) CandidateResource {
	return CandidateResource{
		id: c.ID,
		attrs: CandidateResourceAttrs{
			RecordID:          c.RecordID,
			OriginalTimestamp: c.OriginalTimestamp,
			Record:            c.Record,
			CreatedAt:         c.CreatedAt,
			Action:            c.Action,
			Reason:            c.Reason,
			ClientID:          c.ClientID,

			ResolvedBy: c.ResolvedBy,
			ResolvedAt: c.ResolvedAt,
		},
	}
}
