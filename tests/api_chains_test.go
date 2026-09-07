package tests

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digikeeper/digikeeper-journal/internal/jsonx"
)

func TestPostThenGet(t *testing.T) {
	srv := setupTestServer(t)

	// act POST
	for _, postBody := range []string{
		`{"type":"note","timestamp":"2026-03-08T14:30:00Z","tags":["fitness","health"],"data":{"exercise":"running"}}`,
		`{"type":"note","timestamp":"2026-03-08T14:29:00Z","tags":["health"],"data":{"exercise":"pre-running"}}`,
	} {
		postResp := postJSON(t, srv.URL+"/v1/journal", postBody)
		defer closeResponseBody(t, postResp)
		require.Equal(t, http.StatusCreated, postResp.StatusCode)

		var postResult singleResponse
		require.NoError(t, jsonx.UnmarshalRead(postResp.Body, &postResult))
	}

	// act GET
	getResp := getURL(t, srv.URL+"/v1/journal?tag=fitness")
	defer closeResponseBody(t, getResp)

	require.Equal(t, http.StatusOK, getResp.StatusCode)
	assert.Equal(t, "application/vnd.api+json", getResp.Header.Get("Content-Type"))

	// assert
	var getResult listResponse
	require.NoError(t, jsonx.UnmarshalRead(getResp.Body, &getResult))

	assert.Equal(t, "records", getResult.Meta.Type)
	require.NotEmpty(t, getResult.Data)

	var found *resourceObject
	assert.Len(t, getResult.Data, 1)
	found = &getResult.Data[0]

	assert.Contains(t, found.Attributes.Tags, "fitness")
	assert.Equal(t, "running", found.Attributes.Data["exercise"])
	assert.Equal(t, "2026-03-08T14:30:00Z", found.Attributes.Timestamp)
}

func TestCandidateResolveAndCompactFlow(t *testing.T) {
	srv := setupTestServer(t)

	appendResp := postJSON(t, srv.URL+"/v1/journal",
		`{"type":"note","timestamp":"2026-03-08T10:00:00Z","tags":["work"],"data":{"note":"original"}}`)
	defer closeResponseBody(t, appendResp)
	require.Equal(t, http.StatusCreated, appendResp.StatusCode)
	var appended singleResponse
	require.NoError(t, jsonx.UnmarshalRead(appendResp.Body, &appended))

	submitBody := `{"record_id":"` + appended.Data.ID + `","original_timestamp":"2026-03-08T10:00:00Z","type":"note","tags":["corrected"],"data":{"note":"corrected text"}}`
	submitResp := postJSONWithHeaders(t, srv.URL+"/v1/candidates", submitBody, map[string]string{
		"X-Client-Id": "mobile",
	})
	defer closeResponseBody(t, submitResp)
	require.Equal(t, http.StatusCreated, submitResp.StatusCode)
	var submitted candidateResponse
	require.NoError(t, jsonx.UnmarshalRead(submitResp.Body, &submitted))
	assert.Equal(t, "candidates", submitted.Meta.Type)
	assert.NotEmpty(t, submitted.Data.ID)
	assert.Equal(t, appended.Data.ID, submitted.Data.Attributes.RecordID)
	assert.Equal(t, "corrected text", submitted.Data.Attributes.Record.Data["note"])

	batchResp := getURL(t, srv.URL+"/v1/candidates/pending?partition=2026-03-08")
	defer closeResponseBody(t, batchResp)
	require.Equal(t, http.StatusOK, batchResp.StatusCode)
	var batch candidateListResponse
	require.NoError(t, jsonx.UnmarshalRead(batchResp.Body, &batch))
	require.Len(t, batch.Data, 1)
	assert.Equal(t, submitted.Data.ID, batch.Data[0].ID)
	assert.Equal(t, appended.Data.ID, batch.Data[0].Attributes.RecordID)

	resolveBody := `{"partition":"2026-03-08","resolutions":[{"candidate_id":"` + batch.Data[0].ID + `","action":"apply","reason":"best correction"}]}`
	resolveResp := postJSONWithHeaders(t, srv.URL+"/v1/candidates/resolve", resolveBody, map[string]string{
		"X-Resolved-By": "tester",
	})
	defer closeResponseBody(t, resolveResp)
	require.Equal(t, http.StatusOK, resolveResp.StatusCode)
	var resolved candidateListResponse
	require.NoError(t, jsonx.UnmarshalRead(resolveResp.Body, &resolved))
	require.Len(t, resolved.Data, 1)
	assert.Equal(t, "apply", resolved.Data[0].Attributes.Action)
	assert.Equal(t, "tester", resolved.Data[0].Attributes.ResolvedBy)

	compactResp := postJSON(t, srv.URL+"/v1/compaction", `{"partition":"2026-03-08"}`)
	defer closeResponseBody(t, compactResp)
	require.Equal(t, http.StatusOK, compactResp.StatusCode)

	queryResp := getURL(t, srv.URL+"/v1/journal?tag=corrected")
	defer closeResponseBody(t, queryResp)
	require.Equal(t, http.StatusOK, queryResp.StatusCode)
	var queried listResponse
	require.NoError(t, jsonx.UnmarshalRead(queryResp.Body, &queried))
	require.Len(t, queried.Data, 1)
	assert.Equal(t, appended.Data.ID, queried.Data[0].ID)
	assert.Equal(t, "corrected text", queried.Data[0].Attributes.Data["note"])
	assert.Equal(t, 2, queried.Data[0].Attributes.Meta.Revision)

	compactAgainResp := postJSON(t, srv.URL+"/v1/compaction", `{"partition":"2026-03-08"}`)
	defer closeResponseBody(t, compactAgainResp)
	require.Equal(t, http.StatusOK, compactAgainResp.StatusCode)
}

func TestResolveCandidatesIncompleteResolution(t *testing.T) {
	srv := setupTestServer(t)
	recordID := appendTestRecord(t, srv)
	candidateA := submitTestCandidate(t, srv, recordID, "a", []string{"a"})
	_ = submitTestCandidate(t, srv, recordID, "b", []string{"b"})

	resolveBody := `{"partition":"2026-03-08","resolutions":[{"candidate_id":"` + candidateA + `","action":"deny"}]}`
	resp := postJSONWithHeaders(t, srv.URL+"/v1/candidates/resolve", resolveBody, map[string]string{
		"X-Resolved-By": "tester",
	})
	defer closeResponseBody(t, resp)
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestResolveCandidatesRejectsMultipleApplyForRecord(t *testing.T) {
	srv := setupTestServer(t)
	recordID := appendTestRecord(t, srv)
	candidateA := submitTestCandidate(t, srv, recordID, "a", []string{"a"})
	candidateB := submitTestCandidate(t, srv, recordID, "b", []string{"b"})

	resolveBody := `{"partition":"2026-03-08","resolutions":[{"candidate_id":"` + candidateA + `","action":"apply"},{"candidate_id":"` + candidateB + `","action":"apply"}]}`
	resp := postJSONWithHeaders(t, srv.URL+"/v1/candidates/resolve", resolveBody, map[string]string{
		"X-Resolved-By": "tester",
	})
	defer closeResponseBody(t, resp)
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}
