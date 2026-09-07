package httpapi

import (
	"maps"

	"github.com/danielgtaylor/huma/v2"
)

const ContentType = "application/vnd.api+json"

// Resource represents a JSON:API resource object with id and attributes.
type Resource interface {
	GetID() string
	GetAttributes() any
}

type ResponseMeta struct {
	Type string `json:"type"`
}

type ResourceEnvelope struct {
	ID         string `json:"id"`
	Attributes any    `json:"attributes"`
}

type ErrorDetail struct {
	Status string `json:"status"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}

// NewHumaConfig returns a huma.Config with application/vnd.api+json as the
// default response content type.
func NewHumaConfig(title, version string) huma.Config {
	cfg := huma.DefaultConfig(title, version)

	// Copy: DefaultConfig returns the shared DefaultFormats map, and writing to
	// it races with the range over config.Formats in huma.NewAPI.
	formats := make(map[string]huma.Format, len(cfg.Formats)+1)
	maps.Copy(formats, cfg.Formats)
	formats[ContentType] = huma.DefaultJSONFormat
	cfg.Formats = formats

	cfg.DefaultFormat = ContentType
	return cfg
}

func ToEnvelope(r Resource) ResourceEnvelope {
	return ResourceEnvelope{ID: r.GetID(), Attributes: r.GetAttributes()}
}
