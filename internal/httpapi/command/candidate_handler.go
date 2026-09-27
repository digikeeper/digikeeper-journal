package command

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"
	sloghttp "github.com/samber/slog-http"

	domainCandidate "github.com/digikeeper/digikeeper-journal/internal/domain/command/candidate"
	commandmodel "github.com/digikeeper/digikeeper-journal/internal/domain/command/model"
	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/internal/httpapi"
)

type CandidateHandler struct {
	svc *domainCandidate.Service
}

func NewCandidateHandler(svc *domainCandidate.Service) *CandidateHandler {
	return &CandidateHandler{svc: svc}
}

type SubmitCandidateInput struct {
	ClientID string `header:"X-Client-Id" doc:"Client identifier for audit"`
	Body     struct {
		RecordID          string              `json:"rec_id" required:"true"`
		OriginalTimestamp time.Time           `json:"orig_ts" required:"true"`
		Type              string              `json:"type"`
		SchemaVersion     *int                `json:"schm_ver,omitempty"`
		Facets            map[string][]string `json:"facets"`
		Data              map[string]any      `json:"d"`
	}
}

func (i *SubmitCandidateInput) Resolve(ctx huma.Context) []error {
	var errs []error
	if i.Body.RecordID == "" {
		errs = append(errs, &huma.ErrorDetail{
			Location: "body.rec_id",
			Message:  "'rec_id' is required",
		})
	}
	if i.Body.OriginalTimestamp.IsZero() {
		errs = append(errs, &huma.ErrorDetail{
			Location: "body.orig_ts",
			Message:  "'orig_ts' is required",
		})
	}
	if i.Body.Type == "" {
		errs = append(errs, &huma.ErrorDetail{Location: "body.type", Message: "'type' is required"})
	}
	if i.Body.Facets == nil {
		errs = append(errs, &huma.ErrorDetail{Location: "body.facets", Message: "'facets' is required"})
	}
	if i.Body.Data == nil {
		errs = append(errs, &huma.ErrorDetail{Location: "body.d", Message: "'d' is required"})
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

type SubmitCandidateOutput struct {
	Status int
	Body   struct {
		Meta httpapi.ResponseMeta     `json:"meta"`
		Data httpapi.ResourceEnvelope `json:"data"`
	}
}

func (h *CandidateHandler) SubmitCandidate(
	ctx context.Context,
	input *SubmitCandidateInput,
) (*SubmitCandidateOutput, error) {
	c, err := h.svc.Submit(ctx, domainCandidate.SubmitRequest{
		RecordID:          input.Body.RecordID,
		OriginalTimestamp: input.Body.OriginalTimestamp,
		Type:              input.Body.Type,
		SchemaVersion:     input.Body.SchemaVersion,
		Facets:            input.Body.Facets,
		Data:              input.Body.Data,
		ClientID:          input.ClientID,
	}, sloghttp.GetRequestIDFromContext(ctx))
	if err != nil {
		return nil, httpapi.DomainError(ctx, err)
	}

	out := &SubmitCandidateOutput{Status: 201}
	out.Body.Meta = httpapi.ResponseMeta{Type: "candidates"}
	out.Body.Data = httpapi.ToEnvelope(NewCandidateResource(c))
	return out, nil
}

type ResolveCandidatesInput struct {
	ClientID   string `header:"X-Client-Id" doc:"Client identifier for audit"`
	ResolvedBy string `header:"X-Resolved-By" required:"true" doc:"Resolver identifier"`
	Body       struct {
		Partition   string                        `json:"partition" required:"true"`
		Resolutions []domainCandidate.ResolveItem `json:"resolutions" required:"true"`
	}
}

type ListPendingCandidatesInput struct {
	Partition string `query:"partition" required:"true" doc:"Partition to resolve in YYYY-MM-DD format"`
}

func (i *ListPendingCandidatesInput) Resolve(ctx huma.Context) []error {
	if i.Partition == "" {
		return []error{&huma.ErrorDetail{
			Location: "query.partition",
			Message:  "'partition' is required",
		}}
	}
	if _, err := core.ParsePartition(i.Partition); err != nil {
		return []error{&huma.ErrorDetail{
			Location: "query.partition",
			Message:  "'partition' must be YYYY-MM-DD",
			Value:    i.Partition,
		}}
	}
	return nil
}

type ListPendingCandidatesOutput struct {
	Body struct {
		Meta httpapi.ResponseMeta       `json:"meta"`
		Data []httpapi.ResourceEnvelope `json:"data"`
	}
}

func (h *CandidateHandler) ListPendingCandidates(
	ctx context.Context,
	input *ListPendingCandidatesInput,
) (*ListPendingCandidatesOutput, error) {
	partition, err := core.ParsePartition(input.Partition)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}

	candidates, err := h.svc.ListPending(ctx, partition)
	if err != nil {
		return nil, httpapi.DomainError(ctx, err)
	}

	out := &ListPendingCandidatesOutput{}
	out.Body.Meta = httpapi.ResponseMeta{Type: "candidates"}
	out.Body.Data = candidateEnvelopes(candidates)
	return out, nil
}

func (i *ResolveCandidatesInput) Resolve(ctx huma.Context) []error {
	var errs []error
	if i.ResolvedBy == "" {
		errs = append(errs, &huma.ErrorDetail{
			Location: "header.X-Resolved-By",
			Message:  "'X-Resolved-By' is required",
		})
	}
	if i.Body.Partition == "" {
		errs = append(errs, &huma.ErrorDetail{
			Location: "body.partition",
			Message:  "'partition' is required",
		})
	} else if _, err := core.ParsePartition(i.Body.Partition); err != nil {
		errs = append(errs, &huma.ErrorDetail{
			Location: "body.partition",
			Message:  "'partition' must be YYYY-MM-DD",
			Value:    i.Body.Partition,
		})
	}
	if len(i.Body.Resolutions) == 0 {
		errs = append(errs, &huma.ErrorDetail{
			Location: "body.resolutions",
			Message:  "'resolutions' must not be empty",
		})
	}
	for idx, item := range i.Body.Resolutions {
		if item.CandidateID == "" {
			errs = append(errs, &huma.ErrorDetail{
				Location: "body.resolutions",
				Message:  "'candidate_id' is required",
				Value:    idx,
			})
		}
		if !item.Action.IsValid() {
			errs = append(errs, &huma.ErrorDetail{
				Location: "body.resolutions.action",
				Message:  "'action' must be 'apply' or 'deny'",
				Value:    item.Action,
			})
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

type ResolveCandidatesOutput struct {
	Body struct {
		Meta httpapi.ResponseMeta       `json:"meta"`
		Data []httpapi.ResourceEnvelope `json:"data"`
	}
}

func (h *CandidateHandler) ResolveCandidates(
	ctx context.Context,
	input *ResolveCandidatesInput,
) (*ResolveCandidatesOutput, error) {
	partition, err := core.ParsePartition(input.Body.Partition)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}

	candidates, err := h.svc.Resolve(ctx, partition, domainCandidate.ResolveRequest{
		Resolutions: input.Body.Resolutions,
		ResolvedBy:  input.ResolvedBy,
		ClientID:    input.ClientID,
	})
	if err != nil {
		return nil, httpapi.DomainError(ctx, err)
	}

	out := &ResolveCandidatesOutput{}
	out.Body.Meta = httpapi.ResponseMeta{Type: "candidates"}
	out.Body.Data = candidateEnvelopes(candidates)
	return out, nil
}

func candidateEnvelopes(candidates []commandmodel.Candidate) []httpapi.ResourceEnvelope {
	resources := make([]httpapi.ResourceEnvelope, len(candidates))
	for i, c := range candidates {
		resources[i] = httpapi.ToEnvelope(NewCandidateResource(c))
	}
	return resources
}
