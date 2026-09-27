package schemaregistry_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digikeeper/digikeeper-journal/internal/httpapi"
	apisreg "github.com/digikeeper/digikeeper-journal/internal/httpapi/schemaregistry"
	registry "github.com/digikeeper/digikeeper-journal/internal/schemaregistry"
)

func setupRegistryServer(t *testing.T) *httptest.Server {
	t.Helper()

	schemas, err := registry.Load(os.DirFS("../../../examples/schemas"))
	require.NoError(t, err)
	h := apisreg.NewHandler(schemas)

	mux := http.NewServeMux()
	api := humago.New(mux, httpapi.NewHumaConfig("test", "0.0.0"))
	httpapi.InitHumaErrors()

	huma.Register(api, huma.Operation{
		OperationID:   "get-schema",
		Method:        http.MethodGet,
		Path:          "/v1/registry/{type}",
		DefaultStatus: http.StatusOK,
	}, h.GetSchema)
	huma.Register(api, huma.Operation{
		OperationID:   "get-schema-version",
		Method:        http.MethodGet,
		Path:          "/v1/registry/{type}/{version}",
		DefaultStatus: http.StatusOK,
	}, h.GetSchemaVersion)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func TestGetSchema_UnknownType_Returns404(t *testing.T) {
	t.Parallel()
	srv := setupRegistryServer(t)

	resp := get(t, srv.URL+"/v1/registry/missing")
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	var body struct {
		Errors []any `json:"errors"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.NotEmpty(t, body.Errors)
}

func TestGetSchema_KnownType_ReturnsLatestVersion(t *testing.T) {
	t.Parallel()
	srv := setupRegistryServer(t)

	resp := get(t, srv.URL+"/v1/registry/note")
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body struct {
		Type    string `json:"type"`
		Version int    `json:"version"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "note", body.Type)
	assert.Equal(t, 1, body.Version)
}

func TestGetSchemaVersion_Returns404ForUnknownVersion(t *testing.T) {
	t.Parallel()
	srv := setupRegistryServer(t)

	resp := get(t, srv.URL+"/v1/registry/note/2")
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestGetSchemaVersion_Returns200(t *testing.T) {
	t.Parallel()
	srv := setupRegistryServer(t)

	resp := get(t, srv.URL+"/v1/registry/note/1")
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body struct {
		Type    string `json:"type"`
		Version int    `json:"version"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "note", body.Type)
	assert.Equal(t, 1, body.Version)
}
