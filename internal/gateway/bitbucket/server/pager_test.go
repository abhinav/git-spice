package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type pagerItem struct {
	ID int `json:"id"`
}

func TestGetPaged_walksPages(t *testing.T) {
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RawQuery)
		switch r.URL.Query().Get("start") {
		case "0":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"values":        []pagerItem{{ID: 1}, {ID: 2}},
				"size":          2,
				"limit":         2,
				"start":         0,
				"isLastPage":    false,
				"nextPageStart": 2,
			})
		case "2":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"values":        []pagerItem{{ID: 3}, {ID: 4}},
				"size":          2,
				"limit":         2,
				"start":         2,
				"isLastPage":    false,
				"nextPageStart": 4,
			})
		case "4":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"values":     []pagerItem{{ID: 5}},
				"size":       1,
				"limit":      2,
				"start":      4,
				"isLastPage": true,
			})
		default:
			t.Errorf("unexpected start: %q", r.URL.Query().Get("start"))
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	var ids []int
	for item, err := range getPaged[pagerItem](t.Context(), client, "/items", nil) {
		require.NoError(t, err)
		ids = append(ids, item.ID)
	}

	assert.Equal(t, []int{1, 2, 3, 4, 5}, ids)
	require.Len(t, requests, 3)
	// The default limit is applied when the caller does not set one.
	assert.Equal(t, "limit=100&start=0", requests[0])
	assert.Equal(t, "limit=100&start=2", requests[1])
	assert.Equal(t, "limit=100&start=4", requests[2])
}

func TestGetPaged_singlePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{
			"values":     []pagerItem{{ID: 7}},
			"size":       1,
			"isLastPage": true,
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	var ids []int
	for item, err := range getPaged[pagerItem](t.Context(), client, "/items", nil) {
		require.NoError(t, err)
		ids = append(ids, item.ID)
	}
	assert.Equal(t, []int{7}, ids)
}

func TestGetPaged_error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	var (
		gotErr error
		count  int
	)
	for _, err := range getPaged[pagerItem](t.Context(), client, "/items", nil) {
		count++
		gotErr = err
	}
	require.Equal(t, 1, count)
	require.ErrorIs(t, gotErr, ErrNotFound)
}

func TestGetPaged_missingNextPageStart(t *testing.T) {
	// A server that reports more pages but omits (or under-reports)
	// nextPageStart must not trap the pager in an infinite loop: the
	// start offset advances by the number of values returned instead.
	var starts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := r.URL.Query().Get("start")
		starts = append(starts, start)
		switch start {
		case "0":
			// isLastPage false, but nextPageStart is omitted (decodes to
			// 0, i.e. <= start), so the guard must advance by len(values).
			writeJSON(t, w, http.StatusOK, map[string]any{
				"values":     []pagerItem{{ID: 1}, {ID: 2}},
				"isLastPage": false,
			})
		case "2":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"values":     []pagerItem{{ID: 3}},
				"isLastPage": true,
			})
		default:
			t.Errorf("unexpected start: %q", start)
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	var ids []int
	for item, err := range getPaged[pagerItem](t.Context(), client, "/items", nil) {
		require.NoError(t, err)
		ids = append(ids, item.ID)
	}

	// The pager terminates (rather than looping on start=0) and collects
	// every item.
	assert.Equal(t, []int{1, 2, 3}, ids)
	assert.Equal(t, []string{"0", "2"}, starts)
}

func TestGetPaged_earlyStop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always claims another page exists; the caller must stop on
		// its own without hanging.
		start := r.URL.Query().Get("start")
		var next int
		switch start {
		case "0":
			next = 2
		default:
			next = 4
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"values":        []pagerItem{{ID: 1}, {ID: 2}},
			"isLastPage":    false,
			"nextPageStart": next,
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	var ids []int
	for item, err := range getPaged[pagerItem](t.Context(), client, "/items", nil) {
		require.NoError(t, err)
		ids = append(ids, item.ID)
		if len(ids) == 3 {
			break
		}
	}
	assert.Equal(t, []int{1, 2, 1}, ids)
}
