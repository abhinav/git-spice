package bitbucket_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
	"go.abhg.dev/gs/internal/gateway/bitbucket/cloud"
	"go.abhg.dev/gs/internal/gateway/bitbucket/server"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
)

const (
	productCloud  = "Cloud"
	productServer = "DataCenter"
)

var conformanceProducts = [...]string{productCloud, productServer}

const (
	// Bitbucket Cloud (REST 2.0) wire states.
	stateOpen       = "OPEN"
	stateMerged     = "MERGED"
	stateDeclined   = "DECLINED"
	stateSuperseded = "SUPERSEDED"

	// Bitbucket Data Center (REST 1.0) wire states.
	statePROpen     = "OPEN"
	statePRMerged   = "MERGED"
	statePRDeclined = "DECLINED"
)

const (
	testProjectKey = "ENG"
	testSlug       = "warp-core"

	cloudRepositoryPath    = "/repositories/workspace/repo"
	cloudPullRequestsPath  = cloudRepositoryPath + "/pullrequests"
	serverPullRequestsPath = "/rest/api/1.0/projects/" + testProjectKey +
		"/repos/" + testSlug + "/pull-requests"
)

// This file holds the gateway conformance suite.
// Each scenario runs once per Bitbucket product
// against a fake server speaking that product's wire protocol,
// and asserts that both gateways normalize equivalent product data
// into identical neutral results (see gateway.go for the contract).
//
// Differences documented in the gateway contract
// (URL format, comment Version, SetChangeDraft support, pending comments)
// are asserted as product-specific, never papered over.

// TestGatewayConformance_GetChange verifies that both gateways
// normalize the same logical pull request,
// served in each product's wire shape,
// into an identical neutral PullRequest.
//
// URL is product-specific by contract
// and asserted non-empty on each product.
func TestGatewayConformance_GetChange(t *testing.T) {
	// The same logical pull request in each product's wire shape.
	// Both products spell the open wire state "OPEN".
	pr := conformancePR{
		Number:       42,
		WireState:    "OPEN",
		Title:        "Refit the warp core",
		HeadName:     "feature",
		BaseName:     "main",
		SourceCommit: "abc123def456",
		Draft:        true,
		Reviewers:    []string{"spock", "uhura"},
	}

	for _, product := range conformanceProducts {
		t.Run(product, func(t *testing.T) {
			mux := http.NewServeMux()
			stubPullRequestGet(t, product, mux, pr)

			gw := newConformanceGateway(t, product, mux)
			change, err := gw.GetChange(t.Context(), pr.Number)
			require.NoError(t, err)

			// URL is product-specific by contract (see PullRequest.URL);
			// only its presence is product-neutral.
			assert.NotEmpty(t, change.URL)
			neutral := *change
			neutral.URL = ""

			// All other fields must normalize identically.
			assert.Equal(t, bitbucket.PullRequest{
				Number:    42,
				State:     forge.ChangeOpen,
				Subject:   "Refit the warp core",
				BaseName:  "main",
				HeadHash:  "abc123def456",
				Draft:     true,
				Reviewers: []string{"spock", "uhura"},
			}, neutral)
		})
	}
}

// TestGatewayConformance_GetChange_states verifies that
// equivalent product wire states normalize
// to the same forge.ChangeState on both products.
//
// Cloud's extra wire states (DRAFT, SUPERSEDED)
// have no Data Center spelling;
// each is paired with the Data Center state
// that shares its normalized meaning.
func TestGatewayConformance_GetChange_states(t *testing.T) {
	tests := []struct {
		name        string
		cloudState  string
		serverState string
		want        forge.ChangeState
	}{
		{"Open", stateOpen, statePROpen, forge.ChangeOpen},
		{"Merged", stateMerged, statePRMerged, forge.ChangeMerged},
		{"Declined", stateDeclined, statePRDeclined, forge.ChangeClosed},
		{"Draft", "DRAFT", statePROpen, forge.ChangeOpen},
		{"Superseded", stateSuperseded, statePRDeclined, forge.ChangeClosed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, product := range conformanceProducts {
				t.Run(product, func(t *testing.T) {
					pr := conformancePR{
						Number:    1,
						WireState: tt.cloudState,
					}
					if product == productServer {
						pr.WireState = tt.serverState
					}

					mux := http.NewServeMux()
					stubPullRequestGet(t, product, mux, pr)

					gw := newConformanceGateway(t, product, mux)
					change, err := gw.GetChange(t.Context(), pr.Number)
					require.NoError(t, err)
					assert.Equal(t, tt.want, change.State)
				})
			}
		})
	}
}

// TestGatewayConformance_FindChangesByBranch verifies that an
// open-state filter with a limit yields equivalent neutral result
// lists on both products. Each product's wire query encoding is
// covered by its per-gateway tests; only neutral equivalence
// is asserted here.
func TestGatewayConformance_FindChangesByBranch(t *testing.T) {
	// Three equivalent open pull requests on the same source branch;
	// with a limit of two, both products must surface the first two.
	prs := []conformancePR{
		{Number: 1, WireState: "OPEN", HeadName: "feature", BaseName: "main"},
		{Number: 2, WireState: "OPEN", HeadName: "feature", BaseName: "main"},
		{Number: 3, WireState: "OPEN", HeadName: "feature", BaseName: "main"},
	}

	for _, product := range conformanceProducts {
		t.Run(product, func(t *testing.T) {
			mux := http.NewServeMux()
			switch product {
			case productCloud:
				mux.HandleFunc("GET "+cloudPullRequestsPath,
					func(w http.ResponseWriter, r *http.Request) {
						limit := len(prs)
						pagelen, err := strconv.Atoi(r.URL.Query().Get("pagelen"))
						if err == nil {
							limit = min(limit, pagelen)
						}

						values := make([]map[string]any, limit)
						for i, pr := range prs[:limit] {
							values[i] = pr.cloudWirePR()
						}
						writeJSON(t, w, http.StatusOK,
							map[string]any{"values": values})
					})
			case productServer:
				mux.HandleFunc("GET "+serverPullRequestsPath,
					func(w http.ResponseWriter, _ *http.Request) {
						values := make([]map[string]any, len(prs))
						for i, pr := range prs {
							values[i] = pr.serverWirePR()
						}
						writeJSON(t, w, http.StatusOK, map[string]any{
							"isLastPage": true,
							"values":     values,
						})
					})
			}

			gw := newConformanceGateway(t, product, mux)
			changes, err := gw.FindChangesByBranch(t.Context(), "feature",
				bitbucket.FindChangesOptions{State: forge.ChangeOpen, Limit: 2})
			require.NoError(t, err)

			type changeSummary struct {
				Number int64
				State  forge.ChangeState
			}

			summaries := make([]changeSummary, len(changes))
			for i, change := range changes {
				summaries[i] = changeSummary{
					Number: change.Number,
					State:  change.State,
				}
			}
			assert.Equal(t, []changeSummary{
				{Number: 1, State: forge.ChangeOpen},
				{Number: 2, State: forge.ChangeOpen},
			}, summaries)
		})
	}
}

// TestGatewayConformance_SetChangeDraft pins the documented capability
// split (see Gateway.SetChangeDraft):
// Bitbucket Cloud toggles the draft flag with a single-field wire PUT,
// while Bitbucket Data Center cannot change it after creation
// and must report ErrUnsupported without talking to the server.
func TestGatewayConformance_SetChangeDraft(t *testing.T) {
	t.Run(productCloud, func(t *testing.T) {
		var gotDraft *bool
		mux := http.NewServeMux()
		mux.HandleFunc("PUT "+cloudPullRequestsPath+"/1",
			func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Draft *bool `json:"draft"`
				}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				gotDraft = body.Draft
				writeJSON(t, w, http.StatusOK, cloud.PullRequest{ID: 1})
			})

		gw := newConformanceGateway(t, productCloud, mux)
		require.NoError(t, gw.SetChangeDraft(t.Context(), 1, true))

		require.NotNil(t, gotDraft, "expected a draft PUT")
		assert.True(t, *gotDraft)
	})

	t.Run(productServer, func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(_ http.ResponseWriter, r *http.Request) {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		})

		gw := newConformanceGateway(t, productServer, mux)
		err := gw.SetChangeDraft(t.Context(), 1, true)
		require.Error(t, err)
		assert.ErrorIs(t, err, bitbucket.ErrUnsupported)
	})
}

// TestGatewayConformance_ListCommitChecks verifies that equivalent
// build statuses map to the same forge.ChangeCheck multiset
// on both products.
func TestGatewayConformance_ListCommitChecks(t *testing.T) {
	const commitSHA = "feedface"

	// Both products spell these build states identically on the wire.
	wireStates := []string{"SUCCESSFUL", "INPROGRESS", "FAILED"}

	for _, product := range conformanceProducts {
		t.Run(product, func(t *testing.T) {
			mux := http.NewServeMux()
			switch product {
			case productCloud:
				statuses := make([]cloud.CommitStatus, len(wireStates))
				for i, state := range wireStates {
					statuses[i] = cloud.CommitStatus{
						Key:   "build-" + strconv.Itoa(i),
						State: state,
					}
				}

				path := cloudRepositoryPath + "/commit/" + commitSHA + "/statuses"
				mux.HandleFunc("GET "+path,
					func(w http.ResponseWriter, _ *http.Request) {
						writeJSON(t, w, http.StatusOK,
							cloud.CommitStatusList{Values: statuses})
					})
			case productServer:
				values := make([]map[string]any, len(wireStates))
				for i, state := range wireStates {
					values[i] = map[string]any{
						"key":   "build-" + strconv.Itoa(i),
						"state": state,
					}
				}

				mux.HandleFunc("GET "+"/rest/build-status/1.0/commits/"+commitSHA,
					func(w http.ResponseWriter, _ *http.Request) {
						writeJSON(t, w, http.StatusOK, map[string]any{
							"isLastPage": true,
							"values":     values,
						})
					})
			}

			gw := newConformanceGateway(t, product, mux)
			got, err := gw.ListCommitChecks(
				t.Context(), git.Hash(commitSHA),
			)
			require.NoError(t, err)
			assert.ElementsMatch(t, []forge.ChangeCheck{
				{Name: "build-0", State: forge.ChangeCheckPassed},
				{Name: "build-1", State: forge.ChangeCheckPending},
				{Name: "build-2", State: forge.ChangeCheckFailed},
			}, got)
		})
	}
}

// TestGatewayConformance_ChangeTemplate verifies that both gateways
// return identical template contents for an existing file,
// and an error matching forge.ErrNotFound for a missing one.
func TestGatewayConformance_ChangeTemplate(t *testing.T) {
	const templatePath = "PULL_REQUEST_TEMPLATE.md"

	t.Run("Found", func(t *testing.T) {
		for _, product := range conformanceProducts {
			t.Run(product, func(t *testing.T) {
				mux := http.NewServeMux()
				switch product {
				case productCloud:
					mux.HandleFunc("GET "+cloudRepositoryPath,
						func(w http.ResponseWriter, _ *http.Request) {
							writeJSON(t, w, http.StatusOK, cloud.Repository{
								MainBranch: cloud.Branch{Name: "main"},
							})
						})
					mux.HandleFunc("GET "+cloudRepositoryPath+"/src/main/"+templatePath,
						func(w http.ResponseWriter, _ *http.Request) {
							_, err := w.Write([]byte("## Summary\n"))
							assert.NoError(t, err)
						})
				case productServer:
					mux.HandleFunc("GET "+"/rest/api/1.0/projects/"+
						testProjectKey+"/repos/"+testSlug+"/raw/"+templatePath,
						func(w http.ResponseWriter, _ *http.Request) {
							_, err := w.Write([]byte("## Summary\n"))
							assert.NoError(t, err)
						})
				}

				gw := newConformanceGateway(t, product, mux)
				body, err := gw.ChangeTemplate(
					t.Context(), templatePath,
				)
				require.NoError(t, err)
				assert.Equal(t, "## Summary\n", body)
			})
		}
	})

	t.Run("Missing", func(t *testing.T) {
		for _, product := range conformanceProducts {
			t.Run(product, func(t *testing.T) {
				// The repository exists, but the template file is
				// absent on both products: the fake servers answer
				// 404 for the unregistered file paths.
				mux := http.NewServeMux()
				if product == productCloud {
					mux.HandleFunc("GET "+cloudRepositoryPath,
						func(w http.ResponseWriter, _ *http.Request) {
							writeJSON(t, w, http.StatusOK, cloud.Repository{
								MainBranch: cloud.Branch{Name: "main"},
							})
						})
				}

				gw := newConformanceGateway(t, product, mux)
				_, err := gw.ChangeTemplate(
					t.Context(), templatePath,
				)
				require.Error(t, err)
				assert.ErrorIs(t, err, forge.ErrNotFound)
			})
		}
	})
}

// TestGatewayConformance_commentRoundTrip verifies that
// CreateComment, UpdateComment, and DeleteComment transmit
// the same comment texts and succeed on both products,
// and that the created comment carries the same identifiers
// and body.
func TestGatewayConformance_commentRoundTrip(t *testing.T) {
	const (
		prID                 int64 = 7
		commentID            int64 = 101
		serverCommentVersion       = 3
	)

	for _, product := range conformanceProducts {
		t.Run(product, func(t *testing.T) {
			mux := http.NewServeMux()
			var bodies []string
			var deleted bool
			switch product {
			case productCloud:
				commentsPath := cloudPullRequestsPath + "/" +
					strconv.FormatInt(prID, 10) + "/comments"
				commentPath := commentsPath + "/" + strconv.FormatInt(commentID, 10)
				echo := func(w http.ResponseWriter, r *http.Request) {
					var req cloud.CommentCreateRequest
					require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
					bodies = append(bodies, req.Content.Raw)
					writeJSON(t, w, http.StatusOK, cloud.Comment{
						ID:      commentID,
						Content: req.Content,
					})
				}
				mux.HandleFunc("POST "+commentsPath, echo)
				mux.HandleFunc("PUT "+commentPath, echo)
				mux.HandleFunc("DELETE "+commentPath,
					func(w http.ResponseWriter, _ *http.Request) {
						deleted = true
						w.WriteHeader(http.StatusNoContent)
					})
			case productServer:
				prPath := serverPullRequestsPath + "/" + strconv.FormatInt(prID, 10)
				commentsPath := prPath + "/comments"
				commentPath := commentsPath + "/" + strconv.FormatInt(commentID, 10)
				mux.HandleFunc("POST "+commentsPath,
					func(w http.ResponseWriter, r *http.Request) {
						var body struct {
							Text string `json:"text"`
						}
						require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
						bodies = append(bodies, body.Text)
						writeJSON(t, w, http.StatusCreated, map[string]any{
							"id":      commentID,
							"version": serverCommentVersion,
							"text":    body.Text,
						})
					})
				mux.HandleFunc("PUT "+commentPath,
					func(w http.ResponseWriter, r *http.Request) {
						var body struct {
							Text string `json:"text"`
						}
						require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
						bodies = append(bodies, body.Text)
						writeJSON(t, w, http.StatusOK, map[string]any{
							"id":      commentID,
							"version": serverCommentVersion + 1,
						})
					})
				mux.HandleFunc("DELETE "+commentPath,
					func(w http.ResponseWriter, _ *http.Request) {
						deleted = true
						w.WriteHeader(http.StatusNoContent)
					})
			}

			gw := newConformanceGateway(t, product, mux)
			ctx := t.Context()

			comment, err := gw.CreateComment(ctx, prID, "v1")
			require.NoError(t, err)
			assert.Equal(t, commentID, comment.ID)
			assert.Equal(t, prID, comment.PRID)
			assert.Equal(t, "v1", comment.Body)

			// Version is product-specific by contract
			// (see ChangeComment.Version):
			// Bitbucket Cloud comments are unversioned (always zero),
			// while Data Center reports the optimistic-locking
			// version that its mutations must carry.
			switch product {
			case productCloud:
				assert.Zero(t, comment.Version)
			case productServer:
				assert.Positive(t, comment.Version)
			}

			require.NoError(t, gw.UpdateComment(ctx, comment, "v2"))
			require.NoError(t, gw.DeleteComment(ctx, comment))

			// Both products must have transmitted the same comment
			// texts in the same order, and then the delete.
			assert.Equal(t, []string{"v1", "v2"}, bodies)
			assert.True(t, deleted)
		})
	}
}

// TestGatewayConformance_ResolvableComments verifies that one resolved
// and one unresolved inline review comment yield the same
// ResolvableComment multiset on both products.
//
// Inline comments are the resolution feature the products share:
// Bitbucket Cloud can resolve only inline comment threads,
// while Data Center additionally resolves general threads
// (covered by the per-gateway tests).
//
// Pending is product-specific by contract
// (see ResolvableComment.Pending):
// only Data Center reports unpublished drafts,
// so Pending is always false on Cloud.
// These fixtures contain no drafts,
// making false the conforming value on both products.
func TestGatewayConformance_ResolvableComments(t *testing.T) {
	const prID int64 = 7

	want := []bitbucket.ResolvableComment{
		{ID: 1, Body: "needs work", Resolved: true},
		{ID: 2, Body: "looks off"},
	}

	for _, product := range conformanceProducts {
		t.Run(product, func(t *testing.T) {
			mux := http.NewServeMux()
			switch product {
			case productCloud:
				path := cloudPullRequestsPath + "/" +
					strconv.FormatInt(prID, 10) + "/comments"
				mux.HandleFunc("GET "+path,
					func(w http.ResponseWriter, _ *http.Request) {
						writeJSON(t, w, http.StatusOK, cloud.CommentList{
							Values: []cloud.Comment{
								{
									ID:      1,
									Content: cloud.Content{Raw: "needs work"},
									Inline:  &cloud.Inline{Path: "main.go"},
									Resolution: &cloud.Resolution{
										Type: "comment_resolution",
									},
								},
								{
									ID:      2,
									Content: cloud.Content{Raw: "looks off"},
									Inline:  &cloud.Inline{Path: "main.go"},
								},
							},
						})
					})
			case productServer:
				prPath := serverPullRequestsPath + "/" + strconv.FormatInt(prID, 10)
				mux.HandleFunc("GET "+prPath+"/activities",
					func(w http.ResponseWriter, _ *http.Request) {
						writeJSON(t, w, http.StatusOK, map[string]any{
							"isLastPage": true,
							"values": []map[string]any{
								{"action": "COMMENTED", "comment": map[string]any{
									"id": 1, "text": "needs work",
									"severity": "NORMAL", "state": "OPEN",
									"threadResolved": true,
									"anchor": map[string]any{
										"path": "main.go", "line": 10,
									},
								}},
								{"action": "COMMENTED", "comment": map[string]any{
									"id": 2, "text": "looks off",
									"severity": "NORMAL", "state": "OPEN",
									"threadResolved": false,
									"anchor": map[string]any{
										"path": "main.go", "line": 20,
									},
								}},
							},
						})
					})
				// No tasks nested as replies; the flat task list is empty.
				mux.HandleFunc("GET "+prPath+"/blocker-comments",
					func(w http.ResponseWriter, _ *http.Request) {
						writeJSON(t, w, http.StatusOK, map[string]any{
							"isLastPage": true,
							"values":     []map[string]any{},
						})
					})
			}

			gw := newConformanceGateway(t, product, mux)

			var got []bitbucket.ResolvableComment
			for comment, err := range gw.ResolvableComments(
				t.Context(), prID,
			) {
				require.NoError(t, err)
				got = append(got, *comment)
			}
			assert.ElementsMatch(t, want, got)
		})
	}
}

// newConformanceGateway builds the gateway for product,
// backed by a fake product server that serves mux.
func newConformanceGateway(
	t *testing.T,
	product string,
	mux *http.ServeMux,
) bitbucket.Gateway {
	t.Helper()

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	switch product {
	case productCloud:
		gw, err := cloud.New(
			srv.URL,
			srv.URL,
			"workspace", "repo",
			silog.Nop(),
			&cloud.Token{AccessToken: "test"},
			http.DefaultClient,
		)
		require.NoError(t, err)
		return gw
	case productServer:
		gw, err := server.New(
			srv.URL+"/rest/api/1.0", srv.URL,
			testProjectKey, testSlug, false,
			silog.Nop(),
			&server.Token{AccessToken: "test-token"},
		)
		require.NoError(t, err)
		return gw
	default:
		t.Fatalf("unknown product %q", product)
		return nil
	}
}

// conformancePR describes one logical pull request
// that the product stubs serve in their own wire shapes,
// so that scenarios can assert both gateways normalize it identically.
type conformancePR struct {
	Number       int64
	WireState    string // product wire state, e.g. "OPEN"
	Title        string
	HeadName     string
	BaseName     string
	SourceCommit string
	Draft        bool
	Reviewers    []string
}

// stubPullRequestGet serves pr
// from the product's single-pull-request GET endpoint.
func stubPullRequestGet(
	t *testing.T,
	product string,
	mux *http.ServeMux,
	pr conformancePR,
) {
	t.Helper()

	switch product {
	case productCloud:
		path := cloudPullRequestsPath + "/" + strconv.FormatInt(pr.Number, 10)
		mux.HandleFunc("GET "+path,
			func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(t, w, http.StatusOK, pr.cloudWirePR())
			})
	case productServer:
		path := serverPullRequestsPath + "/" + strconv.FormatInt(pr.Number, 10)
		mux.HandleFunc("GET "+path,
			func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(t, w, http.StatusOK, pr.serverWirePR())
			})
	default:
		t.Fatalf("unknown product %q", product)
	}
}

// cloudWirePR renders pr in Bitbucket Cloud's REST 2.0 wire shape.
func (pr conformancePR) cloudWirePR() map[string]any {
	source := map[string]any{
		"branch": map[string]any{"name": pr.HeadName},
	}
	if pr.SourceCommit != "" {
		source["commit"] = map[string]any{"hash": pr.SourceCommit}
	}

	wire := map[string]any{
		"id":     pr.Number,
		"title":  pr.Title,
		"state":  pr.WireState,
		"draft":  pr.Draft,
		"source": source,
		"destination": map[string]any{
			"branch": map[string]any{"name": pr.BaseName},
		},
		"links": map[string]any{
			"html": map[string]any{
				"href": "https://bitbucket.org/workspace/repo/pull-requests/" +
					strconv.FormatInt(pr.Number, 10),
			},
		},
	}

	reviewers := make([]map[string]any, 0, len(pr.Reviewers))
	for _, name := range pr.Reviewers {
		reviewers = append(reviewers, map[string]any{"nickname": name})
	}
	if len(reviewers) > 0 {
		wire["reviewers"] = reviewers
	}
	return wire
}

// serverWirePR renders pr in Bitbucket Data Center's REST 1.0 wire shape.
func (pr conformancePR) serverWirePR() map[string]any {
	reviewers := make([]map[string]any, len(pr.Reviewers))
	for i, name := range pr.Reviewers {
		reviewers[i] = map[string]any{
			"user": map[string]any{"name": name},
		}
	}
	return map[string]any{
		"id":      pr.Number,
		"version": 1,
		"title":   pr.Title,
		"state":   pr.WireState,
		"draft":   pr.Draft,
		"fromRef": map[string]any{
			"displayId":    pr.HeadName,
			"latestCommit": pr.SourceCommit,
		},
		"toRef":     map[string]any{"displayId": pr.BaseName},
		"reviewers": reviewers,
		"links": map[string]any{
			"self": []map[string]any{{
				"href": "https://bitbucket.example.com/projects/" +
					testProjectKey + "/repos/" + testSlug +
					"/pull-requests/" +
					strconv.FormatInt(pr.Number, 10) + "/overview",
			}},
		},
	}
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(t *testing.T, w http.ResponseWriter, code int, v any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	require.NoError(t, json.NewEncoder(w).Encode(v))
}
