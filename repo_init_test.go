package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
)

func TestRepoInitCmd_resolveRemote(t *testing.T) {
	tests := []struct {
		name string

		upstream string
		push     string

		guesser fakeRepoInitRemoteGuesser

		want    state.Remote
		wantErr string
	}{
		{
			name: "NoFlags",
			guesser: fakeRepoInitRemoteGuesser{
				upstreams: []remoteGuessResult{{value: "upstream"}},
				pushes:    []remoteGuessResult{{value: "origin"}},
			},
			want: state.Remote{
				Upstream: "upstream",
				Push:     "origin",
			},
		},
		{
			name: "RemoteOnly",
			push: "origin",
			want: state.Remote{
				Upstream: "origin",
				Push:     "origin",
			},
		},
		{
			name:     "UpstreamOnly",
			upstream: "upstream",
			want: state.Remote{
				Upstream: "upstream",
				Push:     "upstream",
			},
		},
		{
			name:     "ForkMode",
			upstream: "upstream",
			push:     "origin",
			want: state.Remote{
				Upstream: "upstream",
				Push:     "origin",
			},
		},
		{
			name: "UpstreamGuessError",
			guesser: fakeRepoInitRemoteGuesser{
				upstreams: []remoteGuessResult{{err: errors.New("no upstream")}},
			},
			wantErr: "guess upstream remote: no upstream",
		},
		{
			name: "PushGuessError",
			guesser: fakeRepoInitRemoteGuesser{
				upstreams: []remoteGuessResult{{value: "upstream"}},
				pushes:    []remoteGuessResult{{err: errors.New("no push")}},
			},
			wantErr: "guess push remote: no push",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &repoInitCmd{
				Upstream: tt.upstream,
				Remote:   tt.push,
			}
			guesser := tt.guesser
			got, err := cmd.resolveRemote(
				t.Context(),
				nil,
				&guesser,
			)

			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.want.Upstream, cmd.Upstream)
			assert.Equal(t, tt.want.Push, cmd.Remote)
		})
	}
}

type remoteGuessResult struct {
	value string
	err   error
}

type fakeRepoInitRemoteGuesser struct {
	upstreams []remoteGuessResult
	pushes    []remoteGuessResult
}

func (g *fakeRepoInitRemoteGuesser) GuessUpstreamRemote(
	context.Context,
	spice.GitRepository,
) (string, error) {
	return g.nextUpstream()
}

func (g *fakeRepoInitRemoteGuesser) GuessPushRemote(
	_ context.Context,
	_ spice.GitRepository,
	_ string,
) (string, error) {
	return g.nextPush()
}

func (g *fakeRepoInitRemoteGuesser) nextUpstream() (string, error) {
	if len(g.upstreams) == 0 {
		return "", errors.New("unexpected upstream guess")
	}
	next := g.upstreams[0]
	g.upstreams = g.upstreams[1:]
	return next.value, next.err
}

func (g *fakeRepoInitRemoteGuesser) nextPush() (string, error) {
	if len(g.pushes) == 0 {
		return "", errors.New("unexpected push guess")
	}
	next := g.pushes[0]
	g.pushes = g.pushes[1:]
	return next.value, next.err
}
