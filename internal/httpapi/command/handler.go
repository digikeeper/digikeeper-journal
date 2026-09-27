package command

import (
	"context"
	"errors"
	"time"

	"github.com/danielgtaylor/huma/v2"
	sloghttp "github.com/samber/slog-http"

	domainAppend "github.com/digikeeper/digikeeper-journal/internal/domain/command/append"
	"github.com/digikeeper/digikeeper-journal/internal/domain/errs"
	"github.com/digikeeper/digikeeper-journal/internal/httpapi"
)

type Handler struct {
	svc        *domainAppend.Service
	resolveSrc func(int) string
}

func NewHandler(svc *domainAppend.Service, resolveSrc func(int) string) *Handler {
	return &Handler{svc: svc, resolveSrc: resolveSrc}
}

type AppendInput struct {
	ClientID string `header:"X-Client-Id" doc:"Client identifier for source tracking"`
	Body     struct {
		Type      string              `json:"type" required:"true" doc:"Record type"`
		Timestamp time.Time           `json:"ts" required:"true" doc:"Event timestamp"`
		Facets    map[string][]string `json:"facets" required:"true" doc:"Named facet values"`
		Data      map[string]any      `json:"d" required:"true" doc:"Record data"`
	}
}

func (i *AppendInput) Resolve(ctx huma.Context) []error {
	if i.Body.Type == "" {
		return []error{&huma.ErrorDetail{Location: "body.type", Message: "'type' is required"}}
	}
	if i.Body.Timestamp.IsZero() {
		return []error{&huma.ErrorDetail{Location: "body.ts", Message: "'ts' is required"}}
	}
	if i.Body.Facets == nil {
		return []error{&huma.ErrorDetail{Location: "body.facets", Message: "'facets' is required"}}
	}
	if i.Body.Data == nil {
		return []error{&huma.ErrorDetail{Location: "body.d", Message: "'d' is required"}}
	}
	return nil
}

type AppendOutput struct {
	Status int
	Body   struct {
		Meta httpapi.ResponseMeta     `json:"meta"`
		Data httpapi.ResourceEnvelope `json:"data"`
	}
}

func (h *Handler) AppendRecord(ctx context.Context, input *AppendInput) (*AppendOutput, error) {
	record, err := h.svc.AppendRecord(ctx, domainAppend.AppendRequest{Type: input.Body.Type, Timestamp: input.Body.Timestamp, Facets: input.Body.Facets, Data: input.Body.Data, ClientID: input.ClientID}, sloghttp.GetRequestIDFromContext(ctx))
	switch {
	case err == nil:
		out := &AppendOutput{Status: 201}
		out.Body.Meta = httpapi.ResponseMeta{Type: "records"}
		out.Body.Data = httpapi.ToEnvelope(NewRecordResource(record, h.resolveSrc))
		return out, nil
	case errors.Is(err, errs.ErrIndexFailed):
		out := &AppendOutput{Status: 202}
		out.Body.Meta = httpapi.ResponseMeta{Type: "records"}
		out.Body.Data = httpapi.ToEnvelope(NewRecordResource(record, h.resolveSrc))
		return out, nil
	default:
		return nil, httpapi.DomainError(ctx, err)
	}
}
