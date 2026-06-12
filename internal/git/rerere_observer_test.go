package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseRerereReplay(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantPath string
		wantOK   bool
	}{
		{
			name:     "ResolvedForm",
			line:     "Resolved 'foo/bar.go' using previous resolution.",
			wantPath: "foo/bar.go",
			wantOK:   true,
		},
		{
			name:     "StagedForm",
			line:     "Staged 'foo/bar.go' using previous resolution.",
			wantPath: "foo/bar.go",
			wantOK:   true,
		},
		{
			name: "OtherLine",
			line: "Auto-merging foo/bar.go",
		},
		{
			name: "PartialMatch",
			line: "Staged 'foo/bar.go'",
		},
		{
			name: "Empty",
			line: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, ok := parseRerereReplay([]byte(tt.line))
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantPath, path)
		})
	}
}

func TestRerereReplayObserver_ChunkedWrites(t *testing.T) {
	var got []string
	o := &rerereReplayObserver{cb: func(path string) {
		got = append(got, path)
	}}
	// Split across writes mid-line and mid-prefix.
	chunks := []string{
		"Auto-merging f.txt\nStag",
		"ed 'f.txt' using previous resolution.\nCONFLICT\n",
		"Resolved 'g.txt' using previous resolution.\n",
	}
	for _, c := range chunks {
		_, err := o.Write([]byte(c))
		assert.NoError(t, err)
	}
	assert.Equal(t, []string{"f.txt", "g.txt"}, got)
}
