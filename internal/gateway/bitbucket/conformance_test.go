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

			// All other fields must normalize identically.
			assert.Equal(t, bitbucket.PullRequest{
				Number:    42,
				State:     forge.ChangeOpen,
				Subject:   "Refit the warp core",
				BaseName:  "main",
				HeadHash:  "abc123def456",
				Draft:     true,
				Reviewers: []string{"spock", "uhura"},
			}, withoutProductFields(change))
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
			stubPullRequestList(t, product, mux, prs)

			gw := newConformanceGateway(t, product, mux)
			changes, err := gw.FindChangesByBranch(t.Context(), "feature",
				bitbucket.FindChangesOptions{State: forge.ChangeOpen, Limit: 2})
			require.NoError(t, err)

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
		mux.HandleFunc("PUT "+cloudPRPath(1),
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
		gw := newConformanceGateway(t, productServer, noRequestMux(t))
		err := gw.SetChangeDraft(t.Context(), 1, true)
		require.Error(t, err)
		assert.ErrorIs(t, err, bitbucket.ErrUnsupported)
	})
}

// TestGatewayConformance_ListCommitChecks verifies that equivalent
// build statuses map to the same forge.ChecksState multiset
// on both products.
func TestGatewayConformance_ListCommitChecks(t *testing.T) {
	// Both products spell these build states identically on the wire.
	wireStates := []string{"SUCCESSFUL", "INPROGRESS", "FAILED"}

	for _, product := range conformanceProducts {
		t.Run(product, func(t *testing.T) {
			mux := http.NewServeMux()
			stubCommitStatuses(t, product, mux, wireStates)

			gw := newConformanceGateway(t, product, mux)
			got, err := gw.ListCommitChecks(
				t.Context(), git.Hash(conformanceCommitSHA),
			)
			require.NoError(t, err)
			assert.ElementsMatch(t, []forge.ChecksState{
				forge.ChecksPassed,
				forge.ChecksPending,
				forge.ChecksFailed,
			}, got)
		})
	}
}

// TestGatewayConformance_ChangeTemplate verifies that both gateways
// return identical template contents for an existing file,
// and an error matching forge.ErrNotFound for a missing one.
func TestGatewayConformance_ChangeTemplate(t *testing.T) {
	t.Run("Found", func(t *testing.T) {
		for _, product := range conformanceProducts {
			t.Run(product, func(t *testing.T) {
				mux := http.NewServeMux()
				stubChangeTemplateRepo(t, product, mux)
				stubChangeTemplateFile(t, product, mux, "## Summary\n")

				gw := newConformanceGateway(t, product, mux)
				body, err := gw.ChangeTemplate(
					t.Context(), conformanceTemplatePath,
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
				stubChangeTemplateRepo(t, product, mux)

				gw := newConformanceGateway(t, product, mux)
				_, err := gw.ChangeTemplate(
					t.Context(), conformanceTemplatePath,
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
	for _, product := range conformanceProducts {
		t.Run(product, func(t *testing.T) {
			mux := http.NewServeMux()
			recorder := stubCommentLifecycle(t, product, mux)

			gw := newConformanceGateway(t, product, mux)
			ctx := t.Context()

			comment, err := gw.CreateComment(ctx, conformancePRID, "v1")
			require.NoError(t, err)
			assert.Equal(t, conformanceCommentID, comment.ID)
			assert.Equal(t, conformancePRID, comment.PRID)
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
			assert.Equal(t, []string{"v1", "v2"}, recorder.bodies)
			assert.True(t, recorder.deleted)
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
	want := []bitbucket.ResolvableComment{
		{ID: 1, Body: "needs work", Resolved: true},
		{ID: 2, Body: "looks off"},
	}

	for _, product := range conformanceProducts {
		t.Run(product, func(t *testing.T) {
			mux := http.NewServeMux()
			stubResolvableComments(t, product, mux)

			gw := newConformanceGateway(t, product, mux)

			var got []bitbucket.ResolvableComment
			for comment, err := range gw.ResolvableComments(
				t.Context(), conformancePRID,
			) {
				require.NoError(t, err)
				got = append(got, *comment)
			}
			assert.ElementsMatch(t, want, got)
		})
	}
}

// Conformance harness.

// Conformance subtest names, one per Bitbucket product.
const (
	productCloud  = "Cloud"
	productServer = "DataCenter"
)

// conformanceProducts lists the products under conformance test.
var conformanceProducts = []string{productCloud, productServer}

// Shared fixture identifiers used by the conformance scenarios.
const (
	conformancePRID         int64 = 7
	conformanceCommentID    int64 = 101
	conformanceCommitSHA          = "feedface"
	conformanceTemplatePath       = "PULL_REQUEST_TEMPLATE.md"

	// serverCommentVersion is the comment version served by the
	// Data Center comment stub. It is non-zero to prove that the
	// gateway carries the product's optimistic-locking version
	// through to the neutral ChangeComment.
	serverCommentVersion = 3
)

// Product wire spellings of the pull-request states,
// mirroring the unexported state constants of the cloud adapter
// (stateOpen and friends) and the server adapter (statePR*).
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

// newConformanceGateway builds the gateway for product,
// backed by a fake product server that serves mux.
//
// The Cloud gateway targets the workspace/repo test repository
// (see newTestCloudGateway), and the Data Center gateway targets
// testProjectKey/testSlug (see newOpsTestServerGateway);
// product stubs must register handlers on the matching paths.
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
		return newTestCloudGateway(t, srv.URL)
	case productServer:
		return newOpsTestServerGateway(t, srv)
	default:
		t.Fatalf("unknown product %q", product)
		return nil
	}
}

// newTestCloudGateway builds a Bitbucket Cloud gateway
// for the workspace/repo test repository,
// talking to the fake Bitbucket Cloud server at baseURL.
func newTestCloudGateway(t *testing.T, baseURL string) *cloud.Gateway {
	t.Helper()

	gw, err := cloud.New(
		baseURL,
		baseURL,
		"workspace", "repo",
		silog.Nop(),
		&cloud.Token{AccessToken: "test"},
		http.DefaultClient,
	)
	require.NoError(t, err)
	return gw
}

// newOpsTestServerGateway builds a Bitbucket Data Center gateway
// for the shared testProjectKey/testSlug repository served by srv,
// wired exactly as in production
// (API rooted at srv.URL + "/rest/api/1.0").
func newOpsTestServerGateway(t *testing.T, srv *httptest.Server) *server.Gateway {
	t.Helper()

	gw, err := server.New(
		srv.URL+"/rest/api/1.0", srv.URL,
		testProjectKey, testSlug, false,
		silog.Nop(),
		&server.Token{AccessToken: "test-token"},
	)
	require.NoError(t, err)
	return gw
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

// changeSummary is the product-neutral identity of a found pull
// request: the fields the FindChangesByBranch scenario compares.
type changeSummary struct {
	Number int64
	State  forge.ChangeState
}

// commentRecorder captures the comment mutations
// that a fake product server received.
type commentRecorder struct {
	bodies  []string // texts received by create and update, in order
	deleted bool
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
		mux.HandleFunc("GET "+cloudPRPath(pr.Number),
			func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(t, w, http.StatusOK, cloudWirePR(pr))
			})
	case productServer:
		mux.HandleFunc("GET "+prItemPath(pr.Number),
			func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(t, w, http.StatusOK, serverWirePR(pr))
			})
	default:
		t.Fatalf("unknown product %q", product)
	}
}

// stubPullRequestList serves prs
// from the product's pull-request list endpoint.
//
// The Cloud stub honors the pagelen query parameter,
// because Bitbucket Cloud applies the result limit server-side;
// the Data Center stub returns everything
// and relies on the gateway's client-side truncation.
func stubPullRequestList(
	t *testing.T,
	product string,
	mux *http.ServeMux,
	prs []conformancePR,
) {
	t.Helper()

	switch product {
	case productCloud:
		mux.HandleFunc("GET "+cloudPRsPath(),
			func(w http.ResponseWriter, r *http.Request) {
				limit := len(prs)
				pagelen, err := strconv.Atoi(r.URL.Query().Get("pagelen"))
				if err == nil {
					limit = min(limit, pagelen)
				}

				values := make([]cloud.PullRequest, limit)
				for i, pr := range prs[:limit] {
					values[i] = cloudWirePR(pr)
				}
				writeJSON(t, w, http.StatusOK,
					cloud.PullRequestList{Values: values})
			})
	case productServer:
		mux.HandleFunc("GET "+prListPath(),
			func(w http.ResponseWriter, _ *http.Request) {
				values := make([]map[string]any, len(prs))
				for i, pr := range prs {
					values[i] = serverWirePR(pr)
				}
				writeJSON(t, w, http.StatusOK, map[string]any{
					"isLastPage": true,
					"values":     values,
				})
			})
	default:
		t.Fatalf("unknown product %q", product)
	}
}

// stubCommitStatuses serves one build status per wire state
// from the product's commit-status endpoint for conformanceCommitSHA.
func stubCommitStatuses(
	t *testing.T,
	product string,
	mux *http.ServeMux,
	states []string,
) {
	t.Helper()

	switch product {
	case productCloud:
		statuses := make([]cloud.CommitStatus, len(states))
		for i, state := range states {
			statuses[i] = cloud.CommitStatus{State: state}
		}
		mux.HandleFunc("GET "+cloudStatusesPath(conformanceCommitSHA),
			func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(t, w, http.StatusOK,
					cloud.CommitStatusList{Values: statuses})
			})
	case productServer:
		values := make([]map[string]any, len(states))
		for i, state := range states {
			values[i] = map[string]any{
				"key":   "build-" + strconv.Itoa(i),
				"state": state,
			}
		}
		mux.HandleFunc("GET "+buildStatusPath(conformanceCommitSHA),
			func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(t, w, http.StatusOK, map[string]any{
					"isLastPage": true,
					"values":     values,
				})
			})
	default:
		t.Fatalf("unknown product %q", product)
	}
}

// stubChangeTemplateRepo registers the repository-level fixtures
// a template lookup needs before fetching the file:
// Bitbucket Cloud first resolves the default branch,
// while Data Center addresses the default branch implicitly.
func stubChangeTemplateRepo(t *testing.T, product string, mux *http.ServeMux) {
	t.Helper()

	switch product {
	case productCloud:
		mux.HandleFunc("GET "+cloudRepoPath(),
			func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(t, w, http.StatusOK, cloud.Repository{
					MainBranch: cloud.Branch{Name: "main"},
				})
			})
	case productServer:
		// Nothing to register.
	default:
		t.Fatalf("unknown product %q", product)
	}
}

// stubChangeTemplateFile serves content for conformanceTemplatePath
// from the product's raw-file endpoint on the default branch.
func stubChangeTemplateFile(
	t *testing.T,
	product string,
	mux *http.ServeMux,
	content string,
) {
	t.Helper()

	serve := func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(content))
		assert.NoError(t, err)
	}
	switch product {
	case productCloud:
		mux.HandleFunc("GET "+cloudSrcPath(conformanceTemplatePath), serve)
	case productServer:
		mux.HandleFunc("GET "+serverRawPath(conformanceTemplatePath), serve)
	default:
		t.Fatalf("unknown product %q", product)
	}
}

// stubCommentLifecycle registers create, update, and delete handlers
// for a single comment (conformanceCommentID on PR conformancePRID)
// in the product's wire protocol,
// and returns a recorder of what the fake server saw.
//
// Both stubs echo the created comment's text back,
// as the real products do.
// The Data Center stub serves a non-zero comment version
// (serverCommentVersion) to prove that the gateway
// carries the product version through.
func stubCommentLifecycle(
	t *testing.T,
	product string,
	mux *http.ServeMux,
) *commentRecorder {
	t.Helper()

	recorder := &commentRecorder{}
	switch product {
	case productCloud:
		echo := func(w http.ResponseWriter, r *http.Request) {
			var req cloud.CommentCreateRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			recorder.bodies = append(recorder.bodies, req.Content.Raw)
			writeJSON(t, w, http.StatusOK, cloud.Comment{
				ID:      conformanceCommentID,
				Content: req.Content,
			})
		}
		mux.HandleFunc("POST "+cloudCommentsPath(conformancePRID), echo)
		mux.HandleFunc(
			"PUT "+cloudCommentItemPath(conformancePRID, conformanceCommentID),
			echo,
		)
		mux.HandleFunc(
			"DELETE "+cloudCommentItemPath(conformancePRID, conformanceCommentID),
			func(w http.ResponseWriter, _ *http.Request) {
				recorder.deleted = true
				w.WriteHeader(http.StatusNoContent)
			})
	case productServer:
		mux.HandleFunc("POST "+commentsPath(conformancePRID),
			func(w http.ResponseWriter, r *http.Request) {
				text := decodeServerCommentText(t, r)
				recorder.bodies = append(recorder.bodies, text)
				writeJSON(t, w, http.StatusCreated, map[string]any{
					"id":      conformanceCommentID,
					"version": serverCommentVersion,
					"text":    text,
				})
			})
		mux.HandleFunc(
			"PUT "+commentItemPath(conformancePRID, conformanceCommentID),
			func(w http.ResponseWriter, r *http.Request) {
				recorder.bodies = append(
					recorder.bodies, decodeServerCommentText(t, r),
				)
				writeJSON(t, w, http.StatusOK, map[string]any{
					"id":      conformanceCommentID,
					"version": serverCommentVersion + 1,
				})
			})
		mux.HandleFunc(
			"DELETE "+commentItemPath(conformancePRID, conformanceCommentID),
			func(w http.ResponseWriter, _ *http.Request) {
				recorder.deleted = true
				w.WriteHeader(http.StatusNoContent)
			})
	default:
		t.Fatalf("unknown product %q", product)
	}
	return recorder
}

// stubResolvableComments serves one resolved and one unresolved
// inline review comment from the product's comment source
// for PR conformancePRID.
func stubResolvableComments(t *testing.T, product string, mux *http.ServeMux) {
	t.Helper()

	switch product {
	case productCloud:
		mux.HandleFunc("GET "+cloudCommentsPath(conformancePRID),
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
		mux.HandleFunc("GET "+activitiesPath(conformancePRID),
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
		mux.HandleFunc("GET "+blockerCommentsPath(conformancePRID),
			func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(t, w, http.StatusOK, map[string]any{
					"isLastPage": true,
					"values":     []map[string]any{},
				})
			})
	default:
		t.Fatalf("unknown product %q", product)
	}
}

// cloudWirePR renders pr in Bitbucket Cloud's REST 2.0 wire shape.
func cloudWirePR(pr conformancePR) cloud.PullRequest {
	wire := cloud.PullRequest{
		ID:    pr.Number,
		Title: pr.Title,
		State: pr.WireState,
		Draft: pr.Draft,
		Source: cloud.BranchRef{
			Branch: cloud.Branch{Name: pr.HeadName},
		},
		Destination: cloud.BranchRef{
			Branch: cloud.Branch{Name: pr.BaseName},
		},
		Links: cloud.PullRequestLinks{
			HTML: cloud.Link{
				Href: "https://bitbucket.org/workspace/repo/pull-requests/" +
					strconv.FormatInt(pr.Number, 10),
			},
		},
	}
	if pr.SourceCommit != "" {
		wire.Source.Commit = &cloud.Commit{Hash: pr.SourceCommit}
	}
	for _, name := range pr.Reviewers {
		wire.Reviewers = append(wire.Reviewers, cloud.User{Nickname: name})
	}
	return wire
}

// serverWirePR renders pr in Bitbucket Data Center's REST 1.0 wire shape.
func serverWirePR(pr conformancePR) map[string]any {
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

// decodeServerCommentText extracts the "text" field from a Data Center
// comment create or update request body.
func decodeServerCommentText(t *testing.T, r *http.Request) string {
	t.Helper()

	var body struct {
		Text string `json:"text"`
	}
	require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
	return body.Text
}

// withoutProductFields strips the PullRequest fields
// that are product-specific by contract (see PullRequest).
// The remaining fields must agree across products.
func withoutProductFields(pr *bitbucket.PullRequest) bitbucket.PullRequest {
	neutral := *pr
	neutral.URL = ""
	return neutral
}

// noRequestMux returns a mux that fails the test on any request,
// for capabilities that must be rejected without a wire call.
func noRequestMux(t *testing.T) *http.ServeMux {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
	})
	return mux
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(t *testing.T, w http.ResponseWriter, code int, v any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	require.NoError(t, json.NewEncoder(w).Encode(v))
}

// Bitbucket Cloud REST paths for the workspace/repo test repository,
// the coordinates baked into newTestCloudGateway.
// They parallel the Data Center helpers prItemPath, commentsPath, etc.

func cloudRepoPath() string {
	return "/repositories/workspace/repo"
}

func cloudPRsPath() string {
	return cloudRepoPath() + "/pullrequests"
}

func cloudPRPath(id int64) string {
	return cloudPRsPath() + "/" + strconv.FormatInt(id, 10)
}

func cloudCommentsPath(prID int64) string {
	return cloudPRPath(prID) + "/comments"
}

func cloudCommentItemPath(prID, commentID int64) string {
	return cloudCommentsPath(prID) + "/" + strconv.FormatInt(commentID, 10)
}

func cloudStatusesPath(sha string) string {
	return cloudRepoPath() + "/commit/" + sha + "/statuses"
}

func cloudSrcPath(path string) string {
	return cloudRepoPath() + "/src/main/" + path
}

// Bitbucket Data Center REST paths and coordinates,
// the shared test repository baked into newOpsTestServerGateway.
const (
	testProjectKey = "ENG"
	testSlug       = "warp-core"
)

// prListPath and prItemPath build the REST paths the server gateway
// hits for the test repository.
func prListPath() string {
	return "/rest/api/1.0/projects/" + testProjectKey + "/repos/" + testSlug + "/pull-requests"
}

func prItemPath(id int64) string {
	return prListPath() + "/" + strconv.FormatInt(id, 10)
}

// commentsPath is the REST path for creating/listing comments on a PR.
func commentsPath(prID int64) string {
	return prItemPath(prID) + "/comments"
}

// commentItemPath is the REST path for a single comment on a PR.
func commentItemPath(prID, commentID int64) string {
	return commentsPath(prID) + "/" + strconv.FormatInt(commentID, 10)
}

// activitiesPath is the REST path for a PR's activity feed.
func activitiesPath(prID int64) string {
	return prItemPath(prID) + "/activities"
}

// blockerCommentsPath is the REST path for a PR's flat task list.
func blockerCommentsPath(prID int64) string {
	return prItemPath(prID) + "/blocker-comments"
}

// buildStatusPath is the REST path for a commit's build statuses.
func buildStatusPath(sha string) string {
	return "/rest/build-status/1.0/commits/" + sha
}

// serverRawPath is the Data Center REST path for a raw file
// on the default branch of the shared test repository.
func serverRawPath(path string) string {
	return "/rest/api/1.0/projects/" + testProjectKey +
		"/repos/" + testSlug + "/raw/" + path
}
