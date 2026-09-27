package query

import (
	"time"

	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
)

type RecordResource struct {
	id    string
	attrs RecordResourceAttrs
}
type RecordResourceMeta struct {
	SchemaVersion int    `json:"schm_ver"`
	Revision      int    `json:"rev"`
	Source        string `json:"src"`
}
type RecordResourceAttrs struct {
	RequestID string              `json:"request_id"`
	CreatedAt time.Time           `json:"created_at"`
	Timestamp time.Time           `json:"ts"`
	Meta      RecordResourceMeta  `json:"m"`
	Type      string              `json:"type"`
	Facets    map[string][]string `json:"facets"`
	Data      map[string]any      `json:"d"`
}

func (r RecordResource) GetID() string      { return r.id }
func (r RecordResource) GetAttributes() any { return r.attrs }
func NewRecordResource(e core.Record, resolve func(int) string) RecordResource {
	return RecordResource{id: e.ID, attrs: RecordResourceAttrs{Type: e.Type, Meta: RecordResourceMeta{SchemaVersion: e.Meta.SchemaVersion, Revision: e.Meta.Revision, Source: resolve(e.Meta.Source)}, RequestID: e.RequestID, CreatedAt: e.CreatedAt, Timestamp: e.Timestamp, Facets: e.Facets, Data: e.Data}}
}
