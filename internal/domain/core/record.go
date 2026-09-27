package core

import "time"

type RecordMeta struct {
	SchemaVersion int `json:"schm_ver"`
	Revision      int `json:"rev"`
	Source        int `json:"src"`
	// Src is retained only for in-package benchmark literals; it is never serialized.
	Src int `json:"-"`
}

type Record struct {
	ID        string              `json:"id"`
	RequestID string              `json:"request_id"`
	CreatedAt time.Time           `json:"created_at"`
	Meta      RecordMeta          `json:"m"`
	Timestamp time.Time           `json:"ts"`
	Type      string              `json:"type"`
	Facets    map[string][]string `json:"facets"`
	Data      map[string]any      `json:"d"`
}
