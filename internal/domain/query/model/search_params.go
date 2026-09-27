package model

import "time"

type SearchParams struct {
	Facets map[string][]string
	Types  []string
	From   time.Time
	To     time.Time
	Limit  int
}
