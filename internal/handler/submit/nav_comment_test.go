package submit

import (
	"cmp"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.abhg.dev/gs/internal/forge/shamhub"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/statetest"
	gomock "go.uber.org/mock/gomock"
)

func TestUpdateNavigationComments(t *testing.T) {
	type trackedBranch struct {
		Name            string
		Base            string // empty = trunk
		ChangeID        int    // 0 = unsubmitted
		MergedDownstack []int  // list of merged downstack change IDs
	}

	tests := []struct {
		name            string
		trackedBranches []trackedBranch
		when            NavCommentWhen
		sync            NavCommentSync
		downstack       NavCommentDownstack

		// branches from trackedBranches that were just submitted.
		submit []string

		wantComments map[int]string // change ID -> comment body (without header/footer/marker)
	}{
		{
			name: "NoSubmittedBranches",
			trackedBranches: []trackedBranch{
				{Name: "feat1", ChangeID: 123},
			},
			// no comments
		},
		{
			name: "NavCommentNever",
			trackedBranches: []trackedBranch{
				{Name: "feat1", ChangeID: 123},
			},
			when:   NavCommentNever,
			submit: []string{"feat1"},
			// no comments even if submitted
		},
		{
			name: "SingleBranch",
			trackedBranches: []trackedBranch{
				{Name: "feat1", ChangeID: 123},
			},
			submit: []string{"feat1"},
			wantComments: map[int]string{
				123: joinLines("- #123 ◀"),
			},
		},
		{
			name: "SingleBranch/OnMultiple",
			trackedBranches: []trackedBranch{
				{Name: "feat1", ChangeID: 123},
			},
			when:   NavCommentOnMultiple,
			submit: []string{"feat1"},
			// no comment, as there's only one branch
		},
		{
			name: "LinearStack",
			trackedBranches: []trackedBranch{
				{Name: "feat1", ChangeID: 123},
				{Name: "feat2", Base: "feat1", ChangeID: 124},
				{Name: "feat3", Base: "feat2", ChangeID: 125},
			},
			submit: []string{"feat2"},
			wantComments: map[int]string{
				124: joinLines(
					"- #123",
					"    - #124 ◀",
					"        - #125",
				),
			},
		},
		{
			name: "LinearStack/SyncDownstack",
			trackedBranches: []trackedBranch{
				{Name: "feat1", ChangeID: 123},
				{Name: "feat2", Base: "feat1", ChangeID: 124},
				{Name: "feat3", Base: "feat2", ChangeID: 125},
			},
			sync:   NavCommentSyncDownstack,
			submit: []string{"feat3"},
			// topmost branch was submitted, so all get comments
			wantComments: map[int]string{
				123: joinLines(
					"- #123 ◀",
					"    - #124",
					"        - #125",
				),
				124: joinLines(
					"- #123",
					"    - #124 ◀",
					"        - #125",
				),
				125: joinLines(
					"- #123",
					"    - #124",
					"        - #125 ◀",
				),
			},
		},
		{
			name: "MultipleSubmissions/SyncDownstack",
			trackedBranches: []trackedBranch{
				{Name: "feat1", ChangeID: 123},
				{Name: "feat2", Base: "feat1", ChangeID: 124},
				{Name: "feat3", ChangeID: 125},
				{Name: "feat4", Base: "feat3", ChangeID: 126},
			},
			sync:   NavCommentSyncDownstack,
			submit: []string{"feat2", "feat4"},
			wantComments: map[int]string{
				123: joinLines(
					"- #123 ◀",
					"    - #124",
				),
				124: joinLines(
					"- #123",
					"    - #124 ◀",
				),
				125: joinLines(
					"- #125 ◀",
					"    - #126",
				),
				126: joinLines(
					"- #125",
					"    - #126 ◀",
				),
			},
		},
		{
			name: "LinearStack/SyncDownstack/OnMultiple",
			trackedBranches: []trackedBranch{
				{Name: "feat1", ChangeID: 123},
				{Name: "feat2", Base: "feat1", ChangeID: 124},
				{Name: "feat3", Base: "feat2", ChangeID: 125},
			},
			when:   NavCommentOnMultiple,
			sync:   NavCommentSyncDownstack,
			submit: []string{"feat2"},
			wantComments: map[int]string{
				123: joinLines(
					"- #123 ◀",
					"    - #124",
					"        - #125",
				),
				124: joinLines(
					"- #123",
					"    - #124 ◀",
					"        - #125",
				),
			},
		},
		{
			name: "NonLinearStack/SyncDownstack",
			trackedBranches: []trackedBranch{
				{Name: "feat1", ChangeID: 123},
				{Name: "feat2", Base: "feat1", ChangeID: 124},
				{Name: "feat3", Base: "feat1", ChangeID: 125},
				{Name: "feat4", Base: "feat2", ChangeID: 126},
			},
			sync:   NavCommentSyncDownstack,
			submit: []string{"feat3", "feat4"},
			wantComments: map[int]string{
				123: joinLines(
					"- #123 ◀",
					"    - #124",
					"        - #126",
					"    - #125",
				),
				124: joinLines(
					"- #123",
					"    - #124 ◀",
					"        - #126",
				),
				125: joinLines(
					"- #123",
					"    - #125 ◀",
				),
				126: joinLines(
					"- #123",
					"    - #124",
					"        - #126 ◀",
				),
			},
		},
		{
			name: "UnsubmittedBranches/SyncDownstack",
			trackedBranches: []trackedBranch{
				{Name: "feat1", ChangeID: 123},
				{Name: "feat2", Base: "feat1", ChangeID: 0}, // unsubmitted
				{Name: "feat3", Base: "feat2", ChangeID: 124},
			},
			sync:   NavCommentSyncDownstack,
			submit: []string{"feat3"},
			wantComments: map[int]string{
				124: joinLines(
					"- #124 ◀",
				),
			},
		},
		{
			name: "MultipleSubmissions/SyncBranch",
			trackedBranches: []trackedBranch{
				{Name: "feat1", ChangeID: 123},
				{Name: "feat2", Base: "feat1", ChangeID: 124},
				{Name: "feat3", ChangeID: 125},
			},
			submit: []string{"feat1", "feat3"},
			wantComments: map[int]string{
				123: joinLines(
					"- #123 ◀",
					"    - #124",
				),
				125: joinLines(
					"- #125 ◀",
				),
			},
		},
		{
			name: "NonLinearStack/OnMultiple",
			trackedBranches: []trackedBranch{
				{Name: "feat1", ChangeID: 123},
				{Name: "feat2", Base: "feat1", ChangeID: 124},
				{Name: "feat3", Base: "feat1", ChangeID: 125},
				{Name: "feat4", Base: "feat2", ChangeID: 126},
			},
			when:   NavCommentOnMultiple,
			submit: []string{"feat1", "feat4"},
			wantComments: map[int]string{
				123: joinLines(
					"- #123 ◀",
					"    - #124",
					"        - #126",
					"    - #125",
				),
				126: joinLines(
					"- #123",
					"    - #124",
					"        - #126 ◀",
				),
			},
		},
		{
			name: "UnsubmittedBranches",
			trackedBranches: []trackedBranch{
				{Name: "feat1", ChangeID: 123},
				{Name: "feat2", Base: "feat1", ChangeID: 0}, // unsubmitted
				{Name: "feat3", Base: "feat2", ChangeID: 124},
			},
			submit: []string{"feat1"},
			wantComments: map[int]string{
				123: joinLines(
					"- #123 ◀",
				),
			},
		},
		{
			// Regression test for https://github.com/abhinav/git-spice/issues/788
			name: "MergedDownstack",
			trackedBranches: []trackedBranch{
				{Name: "feat1", ChangeID: 123, MergedDownstack: []int{100, 101}},
				{Name: "feat2", Base: "feat1", ChangeID: 124},
				{Name: "feat3", Base: "feat2", ChangeID: 125},
			},
			sync:   NavCommentSyncDownstack,
			submit: []string{"feat3"},
			// This should not panic when accessing infos[idx]
			// where idx corresponds to merged downstack nodes
			wantComments: map[int]string{
				123: joinLines(
					"- #100",
					"    - #101",
					"        - #123 ◀",
					"            - #124",
					"                - #125",
				),
				124: joinLines(
					"- #100",
					"    - #101",
					"        - #123",
					"            - #124 ◀",
					"                - #125",
				),
				125: joinLines(
					"- #100",
					"    - #101",
					"        - #123",
					"            - #124",
					"                - #125 ◀",
				),
			},
		},
		{
			name: "MergedDownstack/FilterOpen",
			trackedBranches: []trackedBranch{
				{Name: "feat1", ChangeID: 123, MergedDownstack: []int{100, 101}},
				{Name: "feat2", Base: "feat1", ChangeID: 124},
				{Name: "feat3", Base: "feat2", ChangeID: 125},
			},
			sync:      NavCommentSyncDownstack,
			downstack: NavCommentDownstackOpen,
			submit:    []string{"feat3"},
			// Merged CRs (#100, #101) should not appear
			wantComments: map[int]string{
				123: joinLines(
					"- #123 ◀",
					"    - #124",
					"        - #125",
				),
				124: joinLines(
					"- #123",
					"    - #124 ◀",
					"        - #125",
				),
				125: joinLines(
					"- #123",
					"    - #124",
					"        - #125 ◀",
				),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := silogtest.New(t)
			ctrl := gomock.NewController(t)
			store := statetest.NewMemoryStore(t, "main", "origin", log)

			mockService := NewMockService(ctrl)
			mockService.EXPECT().
				LoadBranches(gomock.Any()).
				DoAndReturn(func(context.Context) ([]spice.LoadBranchItem, error) {
					items := make([]spice.LoadBranchItem, len(tt.trackedBranches))
					for i, b := range tt.trackedBranches {
						item := spice.LoadBranchItem{
							Name:           b.Name,
							Base:           cmp.Or(b.Base, "main"),
							Head:           "abcd1234",
							BaseHash:       "efgh5678",
							UpstreamBranch: b.Name,
						}

						if b.ChangeID > 0 {
							item.Change = &shamhub.ChangeMetadata{
								Number: b.ChangeID,
							}
						}

						// Add merged downstack entries if any
						for _, mergedID := range b.MergedDownstack {
							mergedMeta := &shamhub.ChangeMetadata{Number: mergedID}
							mergedJSON, err := json.Marshal(mergedMeta)
							require.NoError(t, err)
							item.MergedDownstack = append(item.MergedDownstack, mergedJSON)
						}

						items[i] = item
					}

					return items, nil
				}).
				AnyTimes()

			mockForge := forgetest.NewMockForge(ctrl)
			mockForge.EXPECT().ID().Return("shamhub").AnyTimes()
			mockForge.EXPECT().
				MarshalChangeMetadata(gomock.Any()).
				DoAndReturn(func(m forge.ChangeMetadata) (json.RawMessage, error) {
					md, ok := m.(*shamhub.ChangeMetadata)
					require.True(t, ok, "unexpected change metadata type: %T", m)
					return json.Marshal(md)
				}).
				AnyTimes()
			mockForge.EXPECT().
				UnmarshalChangeID(gomock.Any()).
				DoAndReturn(func(data json.RawMessage) (forge.ChangeID, error) {
					var md shamhub.ChangeMetadata
					if err := json.Unmarshal(data, &md); err != nil {
						return nil, err
					}
					return shamhub.ChangeID(md.Number), nil
				}).
				AnyTimes()

			mockRemoteRepo := forgetest.NewMockRepository(ctrl)
			mockRemoteRepo.EXPECT().Forge().Return(mockForge).AnyTimes()

			var (
				mu               sync.Mutex
				commentIDCounter atomic.Int64
			)
			comments := make(map[shamhub.ChangeCommentID]string)                   // comment ID => body
			changeComments := make(map[shamhub.ChangeID][]shamhub.ChangeCommentID) // change => comments
			mockRemoteRepo.EXPECT().
				PostChangeComment(gomock.Any(), gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, cid forge.ChangeID, body string) (forge.ChangeCommentID, error) {
					changeID, ok := cid.(shamhub.ChangeID)
					require.True(t, ok, "unexpected change ID type: %T", cid)
					commentID := shamhub.ChangeCommentID(commentIDCounter.Add(1))

					mu.Lock()
					comments[commentID] = body
					changeComments[changeID] = append(changeComments[changeID], commentID)
					mu.Unlock()
					return commentID, nil
				}).
				AnyTimes()

			mockRemoteRepo.EXPECT().
				UpdateChangeComment(gomock.Any(), gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, ccid forge.ChangeCommentID, body string) error {
					commentID, ok := ccid.(shamhub.ChangeCommentID)
					require.True(t, ok, "unexpected change comment ID type: %T", ccid)
					mu.Lock()
					comments[commentID] = body
					mu.Unlock()
					return nil
				}).
				AnyTimes()

			err := updateNavigationComments(
				t.Context(),
				store,
				mockService,
				log,
				tt.when,
				tt.sync,
				tt.downstack,
				"",
				tt.submit,
				func(context.Context) (forge.Repository, error) {
					return mockRemoteRepo, nil
				},
			)
			require.NoError(t, err)

			gotComments := make(map[int]string) // change => comments
			for changeID, commentIDs := range changeComments {
				if assert.Len(t, commentIDs, 1, "change %v doesn't have exactly one comment", changeID) {
					body, ok := comments[commentIDs[0]]
					if assert.True(t, ok, "comment %v on change %v has no body", commentIDs[0], changeID) {
						// Strip header, footer, and marker to get just the navigation content
						stripped := strings.TrimPrefix(body, _commentHeader+"\n\n")
						stripped = strings.TrimSuffix(stripped, "\n"+_commentFooter+"\n"+_commentMarker+"\n")
						gotComments[int(changeID)] = stripped
					}
				}
			}

			for changeID, wantComment := range tt.wantComments {
				assert.Equal(t, gotComments[changeID], wantComment, "changeID=%d", changeID)
				delete(gotComments, changeID)
			}

			for changeID, comment := range gotComments {
				assert.Fail(t, "unexpected comment", "changeID=%d\ncomment=\n%v", changeID, comment)
			}
		})
	}
}

func TestUpdateNavigationComments_deletedExternally(t *testing.T) {
	t.Run("SingleBranch", func(t *testing.T) {
		log := silogtest.New(t)
		ctrl := gomock.NewController(t)
		store := statetest.NewMemoryStore(t, "main", "origin", log)

		// Set up a branch with an existing navigation comment.
		existingCommentID := shamhub.ChangeCommentID(42)
		existingMeta := &shamhub.ChangeMetadata{
			Number:            123,
			NavigationComment: int(existingCommentID),
		}
		existingMetaJSON, err := json.Marshal(existingMeta)
		require.NoError(t, err)

		// Pre-populate the store with the branch.
		err = statetest.UpdateBranch(t.Context(), store, &statetest.UpdateRequest{
			Upserts: []state.UpsertRequest{
				{
					Name:           "feat1",
					Base:           "main",
					ChangeMetadata: existingMetaJSON,
					ChangeForge:    "shamhub",
				},
			},
			Message: "setup",
		})
		require.NoError(t, err)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			LoadBranches(gomock.Any()).
			Return([]spice.LoadBranchItem{
				{
					Name:           "feat1",
					Base:           "main",
					Head:           "abcd1234",
					BaseHash:       "efgh5678",
					UpstreamBranch: "feat1",
					Change:         existingMeta,
				},
			}, nil)

		mockForge := forgetest.NewMockForge(ctrl)
		mockForge.EXPECT().ID().Return("shamhub").AnyTimes()
		mockForge.EXPECT().
			MarshalChangeMetadata(gomock.Any()).
			DoAndReturn(func(m forge.ChangeMetadata) (json.RawMessage, error) {
				md, ok := m.(*shamhub.ChangeMetadata)
				require.True(t, ok, "unexpected change metadata type: %T", m)
				return json.Marshal(md)
			}).
			AnyTimes()

		mockRemoteRepo := forgetest.NewMockRepository(ctrl)
		mockRemoteRepo.EXPECT().Forge().Return(mockForge).AnyTimes()

		// UpdateChangeComment returns ErrNotFound,
		// simulating the comment being deleted externally.
		mockRemoteRepo.EXPECT().
			UpdateChangeComment(gomock.Any(), existingCommentID, gomock.Any()).
			Return(forge.ErrNotFound)

		// PostChangeComment should be called as recovery.
		newCommentID := shamhub.ChangeCommentID(100)
		mockRemoteRepo.EXPECT().
			PostChangeComment(gomock.Any(), shamhub.ChangeID(123), gomock.Any()).
			DoAndReturn(func(_ context.Context, _ forge.ChangeID, body string) (forge.ChangeCommentID, error) {
				assert.Contains(t, body, "#123")
				return newCommentID, nil
			})

		err = updateNavigationComments(
			t.Context(),
			store,
			mockService, log,
			NavCommentAlways,
			NavCommentSyncBranch,
			NavCommentDownstackAll,
			"",
			[]string{"feat1"},
			func(context.Context) (forge.Repository, error) {
				return mockRemoteRepo, nil
			},
		)
		require.NoError(t, err)
	})

	t.Run("MultipleComments", func(t *testing.T) {
		log := silogtest.New(t)
		ctrl := gomock.NewController(t)
		store := statetest.NewMemoryStore(t, "main", "origin", log)

		// Set up a stack of 3 branches, each with an existing comment.
		existingMetas := []*shamhub.ChangeMetadata{
			{Number: 123, NavigationComment: 42},
			{Number: 124, NavigationComment: 43},
			{Number: 125, NavigationComment: 44},
		}

		// Pre-populate the store with the branches.
		var upserts []state.UpsertRequest
		for i, meta := range existingMetas {
			metaJSON, err := json.Marshal(meta)
			require.NoError(t, err)

			bases := []string{"main", "feat1", "feat2"}
			upserts = append(upserts, state.UpsertRequest{
				Name:           []string{"feat1", "feat2", "feat3"}[i],
				Base:           bases[i],
				ChangeMetadata: metaJSON,
				ChangeForge:    "shamhub",
			})
		}
		err := statetest.UpdateBranch(t.Context(), store, &statetest.UpdateRequest{
			Upserts: upserts,
			Message: "setup",
		})
		require.NoError(t, err)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			LoadBranches(gomock.Any()).
			Return([]spice.LoadBranchItem{
				{
					Name:           "feat1",
					Base:           "main",
					Head:           "abcd1234",
					BaseHash:       "efgh5678",
					UpstreamBranch: "feat1",
					Change:         existingMetas[0],
				},
				{
					Name:           "feat2",
					Base:           "feat1",
					Head:           "ijkl9012",
					BaseHash:       "abcd1234",
					UpstreamBranch: "feat2",
					Change:         existingMetas[1],
				},
				{
					Name:           "feat3",
					Base:           "feat2",
					Head:           "mnop3456",
					BaseHash:       "ijkl9012",
					UpstreamBranch: "feat3",
					Change:         existingMetas[2],
				},
			}, nil)

		mockForge := forgetest.NewMockForge(ctrl)
		mockForge.EXPECT().ID().Return("shamhub").AnyTimes()
		mockForge.EXPECT().
			MarshalChangeMetadata(gomock.Any()).
			DoAndReturn(func(m forge.ChangeMetadata) (json.RawMessage, error) {
				md, ok := m.(*shamhub.ChangeMetadata)
				require.True(t, ok, "unexpected change metadata type: %T", m)
				return json.Marshal(md)
			}).
			AnyTimes()

		mockRemoteRepo := forgetest.NewMockRepository(ctrl)
		mockRemoteRepo.EXPECT().Forge().Return(mockForge).AnyTimes()

		// All UpdateChangeComment calls return ErrNotFound.
		mockRemoteRepo.EXPECT().
			UpdateChangeComment(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(forge.ErrNotFound).
			Times(3)

		var (
			mu            sync.Mutex
			commentIDNext = int64(100)
		)
		mockRemoteRepo.EXPECT().
			PostChangeComment(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(context.Context, forge.ChangeID, string) (forge.ChangeCommentID, error) {
				mu.Lock()
				defer mu.Unlock()

				id := shamhub.ChangeCommentID(commentIDNext)
				commentIDNext++
				return id, nil
			}).
			Times(3)

		err = updateNavigationComments(
			t.Context(),
			store,
			mockService,
			log,
			NavCommentAlways,
			NavCommentSyncDownstack,
			NavCommentDownstackAll,
			"",
			[]string{"feat3"},
			func(context.Context) (forge.Repository, error) {
				return mockRemoteRepo, nil
			},
		)
		require.NoError(t, err)
	})
}

func TestGenerateStackNavigationComment(t *testing.T) {
	tests := []struct {
		name    string
		graph   []*stackedChange
		current int
		want    string
	}{
		{
			name: "Single",
			graph: []*stackedChange{
				{Change: _changeID("123"), Base: -1},
			},
			current: 0,
			want: joinLines(
				"- #123 ◀",
			),
		},
		{
			name: "Downstack",
			graph: []*stackedChange{
				{Change: _changeID("123"), Base: -1},
				{Change: _changeID("124"), Base: 0},
				{Change: _changeID("125"), Base: 1},
			},
			current: 2,
			want: joinLines(
				"- #123",
				"    - #124",
				"        - #125 ◀",
			),
		},
		{
			name: "Upstack/Linear",
			graph: []*stackedChange{
				{Change: _changeID("123"), Base: -1},
				{Change: _changeID("124"), Base: 0},
				{Change: _changeID("125"), Base: 1},
			},
			current: 0,
			want: joinLines(
				"- #123 ◀",
				"    - #124",
				"        - #125",
			),
		},
		{
			name: "Upstack/NonLinear",
			graph: []*stackedChange{
				{Change: _changeID("123"), Base: -1},
				{Change: _changeID("124"), Base: 0}, // 1
				{Change: _changeID("125"), Base: 0}, // 2
				{Change: _changeID("126"), Base: 1},
				{Change: _changeID("127"), Base: 2},
			},
			current: 0,
			want: joinLines(
				"- #123 ◀",
				"    - #124",
				"        - #126",
				"    - #125",
				"        - #127",
			),
		},
		{
			name: "MidStack",
			graph: []*stackedChange{
				{Change: _changeID("123"), Base: -1}, // 0
				{Change: _changeID("124"), Base: 0},  // 1
				{Change: _changeID("125"), Base: 1},  // 2
				{Change: _changeID("126"), Base: 0},  // 3
				{Change: _changeID("127"), Base: 3},  // 4
			},
			// 1 has a sibling (3), but that won't be shown
			// as it's not in the path to the current branch.
			current: 1,
			want: joinLines(
				"- #123",
				"    - #124 ◀",
				"        - #125",
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Connect the upstacks.
			// Easier to write the test cases this way.
			for i, n := range tt.graph {
				if n.Base == -1 {
					continue
				}
				tt.graph[n.Base].Aboves = append(tt.graph[n.Base].Aboves, i)
			}

			want := _commentHeader + "\n\n" +
				tt.want + "\n" +
				_commentFooter + "\n" +
				_commentMarker + "\n"
			got := generateStackNavigationComment(tt.graph, tt.current, "", nil)
			assert.Equal(t, want, got)

			// Sanity check: All generated comments must match
			// these regular expressions.
			t.Run("Regexp", func(t *testing.T) {
				for _, re := range _navCommentRegexes {
					assert.True(t, re.MatchString(got), "regexp %q failed", re)
				}
			})
		})
	}

	t.Run("CustomMarker", func(t *testing.T) {
		graph := []*stackedChange{
			{Change: _changeID("123"), Base: -1},
			{Change: _changeID("124"), Base: 0},
		}
		graph[0].Aboves = []int{1}

		got := generateStackNavigationComment(graph, 1, "<-- you are here", nil)
		want := _commentHeader + "\n\n" +
			joinLines(
				"- #123",
				"    - #124 <-- you are here",
			) + "\n" +
			_commentFooter + "\n" +
			_commentMarker + "\n"
		assert.Equal(t, want, got)
	})
}

func TestNavigationCommentWhen_StringMarshal(t *testing.T) {
	tests := []struct {
		give string
		want NavCommentWhen
		str  string
	}{
		{
			give: "true",
			want: NavCommentAlways,
			str:  "true",
		},
		{
			give: "false",
			want: NavCommentNever,
			str:  "false",
		},
		{
			give: "multiple",
			want: NavCommentOnMultiple,
			str:  "multiple",
		},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			var got NavCommentWhen
			require.NoError(t, got.UnmarshalText([]byte(tt.give)))
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.give, got.String())
		})
	}

	t.Run("unknown", func(t *testing.T) {
		var f NavCommentWhen
		require.Error(t, f.UnmarshalText([]byte("unknown")))
		assert.Equal(t, "unknown", NavCommentWhen(42).String())
	})
}

func TestNavCommentSync_UnmarshalText(t *testing.T) {
	tests := []struct {
		give string
		want NavCommentSync
	}{
		{"branch", NavCommentSyncBranch},
		{"downstack", NavCommentSyncDownstack},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			var got NavCommentSync
			require.NoError(t, got.UnmarshalText([]byte(tt.give)))
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.give, got.String())
		})
	}

	t.Run("unknown", func(t *testing.T) {
		var s NavCommentSync
		require.Error(t, s.UnmarshalText([]byte("unknown")))
		assert.Equal(t, "unknown", NavCommentSync(42).String())
	})
}

func TestNavCommentSync_StringMarshal(t *testing.T) {
	tests := []struct {
		give string
		want NavCommentSync
		str  string
	}{
		{
			give: "branch",
			want: NavCommentSyncBranch,
			str:  "branch",
		},
		{
			give: "downstack",
			want: NavCommentSyncDownstack,
			str:  "downstack",
		},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			var got NavCommentSync
			require.NoError(t, got.UnmarshalText([]byte(tt.give)))
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.give, got.String())
		})
	}

	t.Run("unknown", func(t *testing.T) {
		var s NavCommentSync
		require.Error(t, s.UnmarshalText([]byte("unknown")))
		assert.Equal(t, "unknown", NavCommentSync(42).String())
	})
}

func TestNavCommentDownstack_UnmarshalText(t *testing.T) {
	tests := []struct {
		give string
		want NavCommentDownstack
	}{
		{"all", NavCommentDownstackAll},
		{"open", NavCommentDownstackOpen},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			var got NavCommentDownstack
			require.NoError(t, got.UnmarshalText([]byte(tt.give)))
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.give, got.String())
		})
	}

	t.Run("unknown", func(t *testing.T) {
		var d NavCommentDownstack
		require.Error(t, d.UnmarshalText([]byte("unknown")))
		assert.Equal(t, "unknown", NavCommentDownstack(42).String())
	})
}

type _changeID string

func (s _changeID) String() string {
	return "#" + string(s)
}

func joinLines(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}
