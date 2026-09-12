package tests

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
	"github.com/digikeeper/digikeeper-journal/internal/jsonx"
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
	Type      string         `json:"type"`
	Meta      recordMeta     `json:"meta"`
	RequestID string         `json:"request_id"`
	CreatedAt string         `json:"created_at"`
	Timestamp string         `json:"timestamp"`
	Tags      []string       `json:"tags"`
	Data      map[string]any `json:"data"`
}

type recordMeta struct {
	SchemaVersion int    `json:"schema_version"`
	Revision      int    `json:"revision"`
	Source        string `json:"source"`
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
	RecordID          string          `json:"record_id"`
	OriginalTimestamp string          `json:"original_timestamp"`
	Record            candidateRecord `json:"record"`
	CreatedAt         string          `json:"created_at"`
	Action            string          `json:"action"`
	ResolvedBy        string          `json:"resolved_by"`
	Reason            string          `json:"reason"`
	ClientID          string          `json:"client_id"`
}

type candidateRecord struct {
	ID   string         `json:"id"`
	Type string         `json:"type"`
	Tags []string       `json:"tags"`
	Data map[string]any `json:"d"`
}

func setupTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	dir := t.TempDir()

	idx, err := index.New(filepath.Join(dir, "index.db"), index.Config{})
	require.NoError(t, err, "init index")
	t.Cleanup(func() { _ = idx.Close() })

	journalStore, err := store.NewStore(dir, idx)
	require.NoError(t, err, "init store")
	t.Cleanup(func() { _ = journalStore.Close() })

	candidateStore, err := candidatestore.New(dir)
	require.NoError(t, err, "init candidate store")

	qryStore := querystore.NewStore(filepath.Join(dir, "dk_journal"), idx)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	srcRepo, err := sourcerepo.New()
	require.NoError(t, err, "init sources")

	cmdSvc := command.NewService(journalStore, srcRepo, logger)
	candidateSvc := domainCandidate.NewService(candidateStore, journalStore, logger)
	compactionSvc := domainCompaction.NewService(journalStore, candidateStore, idx, logger)
	qrySvc := query.NewService(qryStore, qryStore, logger)

	cmdHandler := apicmd.NewHandler(cmdSvc, srcRepo.ResolveName)
	candidateHandler := apicmd.NewCandidateHandler(candidateSvc)
	compactionHandler := apicmd.NewCompactionHandler(compactionSvc)
	qryHandler := apiqry.NewHandler(qrySvc, srcRepo.ResolveName)
	sregHandler, err := apisreg.NewHandler()
	require.NoError(t, err, "init schema registry")

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
		`{"type":"note","timestamp":"2026-03-08T10:00:00Z","tags":["work"],"data":{"note":"original"}}`)
	defer closeResponseBody(t, resp)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var appended singleResponse
	require.NoError(t, jsonx.UnmarshalRead(resp.Body, &appended))
	return appended.Data.ID
}

func submitTestCandidate(t *testing.T, srv *httptest.Server, recordID, note string, tags []string) string {
	t.Helper()
	body := `{"record_id":"` + recordID + `","original_timestamp":"2026-03-08T10:00:00Z","type":"note","tags":["` +
		strings.Join(tags, `","`) + `"],"data":{"note":"` + note + `"}}`
	resp := postJSON(t, srv.URL+"/v1/candidates", body)
	defer closeResponseBody(t, resp)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var submitted candidateResponse
	require.NoError(t, jsonx.UnmarshalRead(resp.Body, &submitted))
	return submitted.Data.ID
}
