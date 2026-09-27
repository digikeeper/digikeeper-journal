package tests

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	sloghttp "github.com/samber/slog-http"
	"github.com/stretchr/testify/require"

	command "github.com/digikeeper/digikeeper-journal/internal/domain/command/append"
	domainCandidate "github.com/digikeeper/digikeeper-journal/internal/domain/command/candidate"
	domainCompaction "github.com/digikeeper/digikeeper-journal/internal/domain/command/compaction"
	"github.com/digikeeper/digikeeper-journal/internal/domain/query"
	"github.com/digikeeper/digikeeper-journal/internal/httpapi"
	apicmd "github.com/digikeeper/digikeeper-journal/internal/httpapi/command"
	apiqry "github.com/digikeeper/digikeeper-journal/internal/httpapi/query"
	apisreg "github.com/digikeeper/digikeeper-journal/internal/httpapi/schemaregistry"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/candidatestore"
	store "github.com/digikeeper/digikeeper-journal/internal/infrastructure/commandstore"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/index"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/querystore"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/sourcerepo"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
	"github.com/digikeeper/digikeeper-journal/internal/jsonx"
	registry "github.com/digikeeper/digikeeper-journal/internal/schemaregistry"
)

// --- test-local JSON:API response types ---

type singleResponse struct {
	Meta responseMeta   `json:"meta"`
	Data resourceObject `json:"data"`
}

type listResponse struct {
	Meta responseMeta     `json:"meta"`
	Data []resourceObject `json:"data"`
}

type responseMeta struct {
	Type string `json:"type"`
}

type resourceObject struct {
	ID         string      `json:"id"`
	Attributes recordAttrs `json:"attributes"`
}

type recordAttrs struct {
	Type      string              `json:"type"`
	Meta      recordMeta          `json:"m"`
	RequestID string              `json:"request_id"`
	CreatedAt string              `json:"created_at"`
	Timestamp string              `json:"ts"`
	Facets    map[string][]string `json:"facets"`
	Data      map[string]any      `json:"d"`
}

type recordMeta struct {
	SchemaVersion int    `json:"schm_ver"`
	Revision      int    `json:"rev"`
	Source        string `json:"src"`
}

type candidateResponse struct {
	Meta responseMeta            `json:"meta"`
	Data candidateResourceObject `json:"data"`
}

type candidateListResponse struct {
	Meta responseMeta              `json:"meta"`
	Data []candidateResourceObject `json:"data"`
}

type candidateResourceObject struct {
	ID         string         `json:"id"`
	Attributes candidateAttrs `json:"attributes"`
}

type candidateAttrs struct {
	RecordID          string          `json:"rec_id"`
	OriginalTimestamp string          `json:"orig_ts"`
	Record            candidateRecord `json:"record"`
	CreatedAt         string          `json:"created_at"`
	Action            string          `json:"action"`
	ResolvedBy        string          `json:"resolved_by"`
	Reason            string          `json:"reason"`
	ClientID          string          `json:"client_id"`
}

type candidateRecord struct {
	ID     string              `json:"id"`
	Type   string              `json:"type"`
	Facets map[string][]string `json:"facets"`
	Data   map[string]any      `json:"d"`
}

func writeTestSchema(t *testing.T, schemaDir string) {
	t.Helper()
	dir := filepath.Join(schemaDir, "note", "v1")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	schema := `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":true,"required":["type","ts","facets","d"],"properties":{"type":{"const":"note"},"ts":{"type":"string","format":"date-time"},"facets":{"type":"object","additionalProperties":{"type":"array","items":{"type":"string"}}},"d":{"type":"object","additionalProperties":false,"required":["note"],"properties":{"note":{"type":"string","minLength":1}}}}}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.json"), []byte(schema), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "instructions.md"), []byte("# Test note\n"), 0o644))
}

func setupTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	dir, err := storefs.Open(t.TempDir())
	require.NoError(t, err, "open data dir")

	writeTestSchema(t, dir.SchemaDir())
	schemaFS, err := dir.SchemaFS()
	require.NoError(t, err, "open schema directory")
	schemas, err := registry.Load(schemaFS)
	require.NoError(t, err, "init schema registry")

	idx, err := index.NewIdx(dir.IndexPath(), index.Config{})
	require.NoError(t, err, "init index")
	t.Cleanup(func() { _ = idx.Close() })

	journalStore, err := store.NewStore(dir, idx)
	require.NoError(t, err, "init store")
	t.Cleanup(func() { _ = journalStore.Close() })

	candidateStore := candidatestore.New(dir)

	qryStore := querystore.NewStore(dir, idx)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	srcRepo, err := sourcerepo.New()
	require.NoError(t, err, "init sources")

	cmdSvc := command.NewService(journalStore, srcRepo, schemas, logger)
	candidateSvc := domainCandidate.NewService(candidateStore, journalStore, schemas, logger)
	compactionSvc := domainCompaction.NewService(journalStore, candidateStore, idx, journalStore, logger)
	qrySvc := query.NewService(qryStore, qryStore, logger)

	cmdHandler := apicmd.NewHandler(cmdSvc, srcRepo.ResolveName)
	candidateHandler := apicmd.NewCandidateHandler(candidateSvc)
	compactionHandler := apicmd.NewCompactionHandler(compactionSvc)
	qryHandler := apiqry.NewHandler(qrySvc, srcRepo.ResolveName)
	sregHandler := apisreg.NewHandler(schemas)

	mux := http.NewServeMux()
	api := humago.New(mux, httpapi.NewHumaConfig("Digikeeper Journal", "1.0.0"))
	httpapi.InitHumaErrors()

	huma.Register(api, huma.Operation{
		OperationID:   "list-records",
		Method:        http.MethodGet,
		Path:          "/v1/journal",
		Summary:       "Search records",
		DefaultStatus: http.StatusOK,
	}, qryHandler.QueryRecords)

	huma.Register(api, huma.Operation{
		OperationID:   "append-record",
		Method:        http.MethodPost,
		Path:          "/v1/journal",
		Summary:       "Append a record",
		DefaultStatus: http.StatusCreated,
	}, cmdHandler.AppendRecord)
	huma.Register(api, huma.Operation{
		OperationID:   "submit-candidate",
		Method:        http.MethodPost,
		Path:          "/v1/candidates",
		Summary:       "Submit a candidate replacement",
		DefaultStatus: http.StatusCreated,
	}, candidateHandler.SubmitCandidate)
	huma.Register(api, huma.Operation{
		OperationID:   "list-pending-candidates",
		Method:        http.MethodGet,
		Path:          "/v1/candidates/pending",
		Summary:       "List pending candidates",
		DefaultStatus: http.StatusOK,
	}, candidateHandler.ListPendingCandidates)
	huma.Register(api, huma.Operation{
		OperationID:   "resolve-candidates",
		Method:        http.MethodPost,
		Path:          "/v1/candidates/resolve",
		Summary:       "Resolve pending candidates for a partition",
		DefaultStatus: http.StatusOK,
	}, candidateHandler.ResolveCandidates)
	huma.Register(api, huma.Operation{
		OperationID:   "compact-partition",
		Method:        http.MethodPost,
		Path:          "/v1/compaction",
		Summary:       "Compact applied candidates into a record partition",
		DefaultStatus: http.StatusOK,
	}, compactionHandler.CompactPartition)
	huma.Register(api, huma.Operation{
		OperationID:   "list-schemas",
		Method:        http.MethodGet,
		Path:          "/v1/registry",
		Summary:       "List all record type schemas",
		DefaultStatus: http.StatusOK,
	}, sregHandler.ListSchemas)
	huma.Register(api, huma.Operation{
		OperationID:   "get-schema",
		Method:        http.MethodGet,
		Path:          "/v1/registry/{type}",
		Summary:       "Get the latest schema for a record type",
		DefaultStatus: http.StatusOK,
	}, sregHandler.GetSchema)
	huma.Register(api, huma.Operation{
		OperationID:   "get-schema-version",
		Method:        http.MethodGet,
		Path:          "/v1/registry/{type}/{version}",
		Summary:       "Get an immutable schema version for a record type",
		DefaultStatus: http.StatusOK,
	}, sregHandler.GetSchemaVersion)

	sloghttp.RequestIDHeaderKey = httpapi.RequestIDHeader
	handler := sloghttp.NewWithConfig(logger, sloghttp.Config{
		WithRequestID: true,
	})(mux)

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func newTestRequest(t *testing.T, method, url string, body io.Reader) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, body)
	require.NoError(t, err)
	return req
}

func doTestRequest(t *testing.T, req *http.Request) *http.Response {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func postJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	req := newTestRequest(t, http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return doTestRequest(t, req)
}

func postJSONWithHeaders(t *testing.T, url, body string, headers map[string]string) *http.Response {
	t.Helper()
	req := newTestRequest(t, http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	return doTestRequest(t, req)
}

func getURL(t *testing.T, url string) *http.Response {
	t.Helper()
	return doTestRequest(t, newTestRequest(t, http.MethodGet, url, nil))
}

func closeResponseBody(t *testing.T, resp *http.Response) {
	t.Helper()
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}
}

func appendTestRecord(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	resp := postJSON(t, srv.URL+"/v1/journal",
		`{"type":"note","ts":"2026-03-08T10:00:00Z","facets":{"topic":["work"]},"d":{"note":"original"}}`)
	defer closeResponseBody(t, resp)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var appended singleResponse
	require.NoError(t, jsonx.UnmarshalRead(resp.Body, &appended))
	return appended.Data.ID
}

func submitTestCandidate(t *testing.T, srv *httptest.Server, recordID, note string, facets []string) string {
	t.Helper()
	body := `{"rec_id":"` + recordID + `","orig_ts":"2026-03-08T10:00:00Z","type":"note","facets":{"topic":["` +
		strings.Join(facets, `","`) + `"]},"d":{"note":"` + note + `"}}`
	resp := postJSON(t, srv.URL+"/v1/candidates", body)
	defer closeResponseBody(t, resp)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var submitted candidateResponse
	require.NoError(t, jsonx.UnmarshalRead(resp.Body, &submitted))
	return submitted.Data.ID
}
