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
		Type      string         `json:"type" doc:"Record type"`
		Timestamp time.Time      `json:"timestamp" required:"true" doc:"Event timestamp"`
		Tags      []string       `json:"tags" doc:"Record tags"`
		Data      map[string]any `json:"data" doc:"Record data"`
	}
}

func (i *AppendInput) Resolve(ctx huma.Context) []error {
	if len(i.Body.Tags) == 0 && len(i.Body.Data) == 0 {
		return []error{&huma.ErrorDetail{
			Message: "at least one of 'tags' or 'data' must be provided",
		}}
	}
	if i.Body.Timestamp.IsZero() {
		return []error{&huma.ErrorDetail{
			Location: "body.timestamp",
			Message:  "'timestamp' is required",
		}}
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
	req := domainAppend.AppendRequest{
		Type:      input.Body.Type,
		Timestamp: input.Body.Timestamp,
		Tags:      input.Body.Tags,
		Data:      input.Body.Data,
		ClientID:  input.ClientID,
	}

	requestID := sloghttp.GetRequestIDFromContext(ctx)
	record, err := h.svc.AppendRecord(ctx, req, requestID)
	switch {
	case err == nil:
		out := &AppendOutput{}
		out.Status = 201
		out.Body.Meta = httpapi.ResponseMeta{Type: "records"}
		out.Body.Data = httpapi.ToEnvelope(NewRecordResource(record, h.resolveSrc))
		return out, nil

	case errors.Is(err, errs.ErrIndexFailed):
		out := &AppendOutput{}
		out.Status = 202
		out.Body.Meta = httpapi.ResponseMeta{Type: "records"}
		out.Body.Data = httpapi.ToEnvelope(NewRecordResource(record, h.resolveSrc))
		return out, nil

	default:
		return nil, httpapi.DomainError(ctx, err)
	}
}
