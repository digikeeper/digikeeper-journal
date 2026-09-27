package model

import (
	"time"

	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
)

type Candidate struct {
	ID                string      `json:"id"`
	RecordID          string      `json:"rec_id"`
	OriginalTimestamp time.Time   `json:"original_ts"`
	Record            core.Record `json:"record"`
	CreatedAt         time.Time   `json:"created_at"`
}
