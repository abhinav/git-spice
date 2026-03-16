package main

import (
	"bytes"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/handler/list"
	"go.abhg.dev/gs/internal/ui"
)

func TestGraphLogPresenter_Present(t *testing.T) {
	var buf bytes.Buffer
	stderrView := ui.NewFileView(&buf)
	presenter := &graphLogPresenter{
		Stderr:            stderrView,
		Theme:             stderrView.Theme(),
		ChangeFormat:      changeFormatID,
		ShowCRStatus:      false,
		ShowCommentCounts: false,
		PushStatusFormat:  pushStatusDisabled,
		CurrentWorktree:   "/repo",
	}

	res := &list.BranchesResponse{
		Branches: []*list.BranchItem{
			{Name: "main", Aboves: []int{1}},
			{
				Name: "feature",
				Base: "main",
			},
		},
		TrunkIdx: 0,
	}

	err := presenter.Present(res, "feature")
	require.NoError(t, err)

	assert.Equal(t, "┏━■ feature ◀\nmain\n", buf.String())
}

func TestGraphLogPresenter_Present_preservesColor(t *testing.T) {
	var buf bytes.Buffer
	stderrView := ui.NewFileView(&colorprofile.Writer{
		Forward: &buf,
		Profile: colorprofile.TrueColor,
	})
	presenter := &graphLogPresenter{
		Stderr:            stderrView,
		Theme:             stderrView.Theme(),
		ChangeFormat:      changeFormatID,
		ShowCRStatus:      false,
		ShowCommentCounts: false,
		PushStatusFormat:  pushStatusDisabled,
		CurrentWorktree:   "/repo",
	}

	res := &list.BranchesResponse{
		Branches: []*list.BranchItem{
			{Name: "main", Aboves: []int{1}},
			{
				Name: "feature",
				Base: "main",
			},
		},
		TrunkIdx: 0,
	}

	err := presenter.Present(res, "feature")
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "\x1b[")
}
