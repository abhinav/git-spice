package github

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_LabelID(t *testing.T) {
	gateway := newResponseGateway(t, `{"data":{"repository":{"label":{"id":"L_1"}}}}`)
	id, err := gateway.LabelID(t.Context(), "acme", "repo", "bug")
	require.NoError(t, err)
	assert.Equal(t, ID("L_1"), id)
}

func TestGateway_LabelIDs(t *testing.T) {
	gateway := newTestGateway(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Query     string          `json:"query"`
			Variables json.RawMessage `json:"variables"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		assert.Equal(t, "query($label0:String!$label1:String!$name:String!$owner:String!){repository(owner: $owner, name: $name){label0:label(name: $label0){id},label1:label(name: $label1){id}}}", request.Query)
		assert.JSONEq(t, `{
			"label0": "bug",
			"label1": "needs review: urgent",
			"name": "repo",
			"owner": "acme"
		}`, string(request.Variables))
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body: io.NopCloser(strings.NewReader(`{
				"data": {"repository": {
					"label0": {"id": "L_1"},
					"label1": null
				}}
			}`)),
		}, nil
	}))

	ids, err := gateway.LabelIDs(
		t.Context(), "acme", "repo",
		[]string{"bug", "needs review: urgent", "bug"},
	)
	require.NoError(t, err)
	assert.Equal(t, []ID{"L_1", "", "L_1"}, ids)
}

func TestGateway_LabelIDs_empty(t *testing.T) {
	gateway := newTestGateway(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("unexpected request")
		return nil, nil
	}))

	ids, err := gateway.LabelIDs(t.Context(), "acme", "repo", nil)
	require.NoError(t, err)
	assert.Nil(t, ids)
}

func TestGateway_CreateLabel(t *testing.T) {
	gateway := newResponseGateway(t, `{"data":{"createLabel":{"label":{"id":"L_1"}}}}`)
	id, err := gateway.CreateLabel(t.Context(), "R_1", "ffffff", "bug")
	require.NoError(t, err)
	assert.Equal(t, ID("L_1"), id)
}

func TestGateway_AddLabelsToLabelable(t *testing.T) {
	gateway := newResponseGateway(t, `{"data":{"addLabelsToLabelable":{}}}`)
	require.NoError(t, gateway.AddLabelsToLabelable(t.Context(), "PR_1", []ID{"L_1"}))
}

func TestGateway_DeleteLabel(t *testing.T) {
	gateway := newResponseGateway(t, `{"data":{"deleteLabel":{}}}`)
	require.NoError(t, gateway.DeleteLabel(t.Context(), "L_1"))
}
