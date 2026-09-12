package main

import (
	"log/slog"
	"net/http"
	_ "net/http/pprof" // /debug/pprof/* and /debug/vars

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	sloghttp "github.com/samber/slog-http"

	"github.com/digikeeper/digikeeper-journal/internal/httpapi"
	apicmd "github.com/digikeeper/digikeeper-journal/internal/httpapi/command"
	apiqry "github.com/digikeeper/digikeeper-journal/internal/httpapi/query"
	apisreg "github.com/digikeeper/digikeeper-journal/internal/httpapi/schemaregistry"
	"github.com/digikeeper/digikeeper-journal/pkg/chain"
	"github.com/digikeeper/digikeeper-journal/pkg/healthz"
)

// handlers bundles the HTTP handlers the API routes are registered against.
type handlers struct {
	Command    *apicmd.Handler
	Candidate  *apicmd.CandidateHandler
	Compaction *apicmd.CompactionHandler
	Query      *apiqry.Handler
	Schema     *apisreg.Handler
}

// newHTTPHandler builds the API routes plus the ops endpoints and wraps them in
// the middleware chain.
func newHTTPHandler(cfg Config, logger *slog.Logger, h handlers) http.Handler {
	mux := http.NewServeMux()
	api := humago.New(mux, httpapi.NewHumaConfig("Digikeeper Journal", "1.0.0"))
	httpapi.InitHumaErrors()

	registerRoutes(api, h)

	mux.HandleFunc("GET /healthz", healthz.Handle)
	if cfg.Debug.Enabled {
		mux.Handle("/debug/", http.DefaultServeMux)
		logger.Info("debug endpoints enabled", slog.String("path", "/debug/"))
	}

	sloghttp.RequestIDHeaderKey = httpapi.RequestIDHeader
	return chain.New(
		httpapi.Recovery,
		sloghttp.NewWithConfig(logger, sloghttp.Config{
			WithRequestID: true,
		}),
	).Then(mux)
}

func registerRoutes(api huma.API, h handlers) {
	// Top-level tags control the grouping and order of sections in /docs.
	api.OpenAPI().Tags = []*huma.Tag{
		{Name: "Records", Description: "Append and search journal records, and compact partitions."},
		{Name: "Candidates", Description: "Submit, list, and resolve candidate record replacements."},
		{Name: "Schemas", Description: "Record type schema registry."},
	}

	// One group per docs section: the shared modifier documents X-Request-ID on
	// every operation, and each subgroup stamps its tag.
	v1 := huma.NewGroup(api, "/v1")
	v1.UseSimpleModifier(func(op *huma.Operation) {
		op.Parameters = append(op.Parameters, httpapi.RequestIDParam())
	})
	records := tagged(v1, "Records")
	candidates := tagged(v1, "Candidates")
	schemas := tagged(v1, "Schemas")

	huma.Register(records, huma.Operation{
		OperationID:   "list-records",
		Method:        http.MethodGet,
		Path:          "/journal",
		Summary:       "Search records",
		DefaultStatus: http.StatusOK,
	}, h.Query.QueryRecords)
	huma.Register(records, huma.Operation{
		OperationID:   "append-record",
		Method:        http.MethodPost,
		Path:          "/journal",
		Summary:       "Append a record",
		DefaultStatus: http.StatusCreated,
	}, h.Command.AppendRecord)
	huma.Register(records, huma.Operation{
		OperationID:   "compact-partition",
		Method:        http.MethodPost,
		Path:          "/compaction",
		Summary:       "Compact applied candidates into a record partition",
		DefaultStatus: http.StatusOK,
	}, h.Compaction.CompactPartition)

	huma.Register(candidates, huma.Operation{
		OperationID:   "submit-candidate",
		Method:        http.MethodPost,
		Path:          "/candidates",
		Summary:       "Submit a candidate replacement",
		DefaultStatus: http.StatusCreated,
	}, h.Candidate.SubmitCandidate)
	huma.Register(candidates, huma.Operation{
		OperationID:   "list-pending-candidates",
		Method:        http.MethodGet,
		Path:          "/candidates/pending",
		Summary:       "List pending candidates",
		DefaultStatus: http.StatusOK,
	}, h.Candidate.ListPendingCandidates)
	huma.Register(candidates, huma.Operation{
		OperationID:   "resolve-candidates",
		Method:        http.MethodPost,
		Path:          "/candidates/resolve",
		Summary:       "Resolve pending candidates for a partition",
		DefaultStatus: http.StatusOK,
	}, h.Candidate.ResolveCandidates)

	huma.Register(schemas, huma.Operation{
		OperationID:   "list-schemas",
		Method:        http.MethodGet,
		Path:          "/registry",
		Summary:       "List all record type schemas",
		DefaultStatus: http.StatusOK,
	}, h.Schema.ListSchemas)
	huma.Register(schemas, huma.Operation{
		OperationID:   "get-schema",
		Method:        http.MethodGet,
		Path:          "/registry/{type}",
		Summary:       "Get the latest schema for a record type",
		DefaultStatus: http.StatusOK,
	}, h.Schema.GetSchema)
	huma.Register(schemas, huma.Operation{
		OperationID:   "get-schema-version",
		Method:        http.MethodGet,
		Path:          "/registry/{type}/{version}",
		Summary:       "Get an immutable schema version for a record type",
		DefaultStatus: http.StatusOK,
	}, h.Schema.GetSchemaVersion)
}

// tagged returns a subgroup whose operations all carry the given docs tag.
func tagged(parent huma.API, tag string) *huma.Group {
	g := huma.NewGroup(parent)
	g.UseSimpleModifier(func(op *huma.Operation) {
		op.Tags = append(op.Tags, tag)
	})
	return g
}
