package query

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	domainQuery "github.com/digikeeper/digikeeper-journal/internal/domain/query"
	"github.com/digikeeper/digikeeper-journal/internal/domain/query/model"
	"github.com/digikeeper/digikeeper-journal/internal/httpapi"
)

type Handler struct {
	svc        *domainQuery.Service
	resolveSrc func(int) string
}

func NewHandler(svc *domainQuery.Service, resolveSrc func(int) string) *Handler {
	return &Handler{svc: svc, resolveSrc: resolveSrc}
}

type QueryInput struct {
	Types  []string            `query:"type" doc:"Filter by record type (OR, repeatable)"`
	From   time.Time           `query:"from" doc:"Start time (RFC3339)"`
	To     time.Time           `query:"to" doc:"End time (RFC3339)"`
	Limit  int                 `query:"limit" minimum:"1" maximum:"1000" default:"100" doc:"Result limit"`
	Facets map[string][]string `json:"-"`
}

func (i *QueryInput) Resolve(ctx huma.Context) []error {
	var errs []error
	if !i.From.IsZero() && !i.To.IsZero() && i.From.After(i.To) {
		errs = append(errs, &huma.ErrorDetail{Location: "query.from", Message: "'from' must not be after 'to'", Value: i.From})
	}
	errs = append(errs, validateQueryValues("type", i.Types)...)
	request := unwrapRequest(ctx)
	i.Facets = make(map[string][]string)
	if request != nil {
		for raw, values := range request.URL.Query() {
			if !strings.HasPrefix(raw, "facet[") || !strings.HasSuffix(raw, "]") {
				continue
			}
			key := strings.TrimSuffix(strings.TrimPrefix(raw, "facet["), "]")
			if strings.TrimSpace(key) == "" {
				errs = append(errs, &huma.ErrorDetail{Location: "query." + raw, Message: "facet key must not be empty"})
				continue
			}
			for _, value := range values {
				if strings.TrimSpace(value) == "" {
					errs = append(errs, &huma.ErrorDetail{Location: "query." + raw, Message: "facet value must not be empty", Value: value})
					continue
				}
				i.Facets[key] = append(i.Facets[key], value)
			}
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// unwrapRequest safely handles resolver tests or adapters other than humago.
func unwrapRequest(ctx huma.Context) (request *http.Request) {
	defer func() {
		if recover() != nil {
			request = nil
		}
	}()
	request, _ = humago.Unwrap(ctx)
	return request
}

func validateQueryValues(name string, values []string) []error {
	var errs []error
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			continue
		}
		errs = append(errs, &huma.ErrorDetail{Location: "query." + name, Message: "'" + name + "' must not be empty", Value: value})
	}
	return errs
}

type QueryOutput struct {
	Body struct {
		Meta httpapi.ResponseMeta       `json:"meta"`
		Data []httpapi.ResourceEnvelope `json:"data"`
	}
}

func (h *Handler) QueryRecords(ctx context.Context, input *QueryInput) (*QueryOutput, error) {
	results, err := h.svc.SearchRecords(ctx, model.SearchParams{Facets: input.Facets, Types: input.Types, From: input.From, To: input.To, Limit: input.Limit})
	if err != nil {
		slog.Default().ErrorContext(ctx, "search failed", slog.Any("error", err))
		return nil, huma.Error500InternalServerError("Query Failure")
	}
	out := &QueryOutput{}
	out.Body.Meta = httpapi.ResponseMeta{Type: "records"}
	out.Body.Data = make([]httpapi.ResourceEnvelope, len(results))
	for i, e := range results {
		out.Body.Data[i] = httpapi.ToEnvelope(NewRecordResource(e, h.resolveSrc))
	}
	return out, nil
}
