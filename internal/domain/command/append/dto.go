package command

import "time"

type AppendRequest struct {
	Type      string              `json:"type"`
	Timestamp time.Time           `json:"ts"`
	Facets    map[string][]string `json:"facets"`
	Data      map[string]any      `json:"d"`
	ClientID  string              `json:"-"`
}
