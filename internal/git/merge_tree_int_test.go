package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SetConflictStyle exports test-only functionality
// for external tests to set the conflict marker style
// for a merge-tree operation.
func SetConflictStyle(req *MergeTreeRequest, style string) {
	req.conflictStyle = style
}

func TestParseConflictStage(t *testing.T) {
	tests := []struct {
		name string
		give string
		want ConflictStage
	}{
		{name: "Ok", give: "0", want: ConflictStageOk},
		{name: "Base", give: "1", want: ConflictStageBase},
		{name: "Ours", give: "2", want: ConflictStageOurs},
		{name: "Theirs", give: "3", want: ConflictStageTheirs},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseConflictStage(tt.give)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseConflictStage_invalidInput(t *testing.T) {
	tests := []struct {
		name string
		give string
	}{
		{name: "InvalidNumber", give: "4"},
		{name: "NegativeNumber", give: "-1"},
		{name: "NonNumeric", give: "abc"},
		{name: "Empty", give: ""},
		{name: "MultipleDigits", give: "10"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseConflictStage(tt.give)
			assert.Error(t, err)
			assert.ErrorContains(t, err, "invalid conflict stage")
		})
	}
}

func TestConflictStage_String(t *testing.T) {
	tests := []struct {
		name string
		give ConflictStage
		want string
	}{
		{name: "Ok", give: ConflictStageOk, want: "ok"},
		{name: "Base", give: ConflictStageBase, want: "base"},
		{name: "Ours", give: ConflictStageOurs, want: "ours"},
		{name: "Theirs", give: ConflictStageTheirs, want: "theirs"},
		{name: "Unknown", give: ConflictStage(99), want: "unknown(99)"},
		{name: "NegativeUnknown", give: ConflictStage(-1), want: "unknown(-1)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.give.String()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseMergeTreeConflictFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		give string
		want MergeTreeConflictFile
	}{
		{
			name: "RegularFile",
			give: "100644 abcdef1234567890abcdef1234567890abcdef12 1\tfile.txt",
			want: MergeTreeConflictFile{
				Mode:   RegularMode,
				Object: Hash("abcdef1234567890abcdef1234567890abcdef12"),
				Stage:  ConflictStageBase,
				Path:   "file.txt",
			},
		},
		{
			name: "DirectoryMode",
			give: "40000 1234567890abcdef1234567890abcdef12345678 2\tsubdir",
			want: MergeTreeConflictFile{
				Mode:   DirMode,
				Object: Hash("1234567890abcdef1234567890abcdef12345678"),
				Stage:  ConflictStageOurs,
				Path:   "subdir",
			},
		},
		{
			name: "FileWithSpaces",
			give: "100644 fedcba0987654321fedcba0987654321fedcba09 3\tfile with spaces.txt",
			want: MergeTreeConflictFile{
				Mode:   RegularMode,
				Object: Hash("fedcba0987654321fedcba0987654321fedcba09"),
				Stage:  ConflictStageTheirs,
				Path:   "file with spaces.txt",
			},
		},
		{
			name: "DeepPath",
			give: "100644 0123456789abcdef0123456789abcdef01234567 0\tpath/to/deep/file.go",
			want: MergeTreeConflictFile{
				Mode:   RegularMode,
				Object: Hash("0123456789abcdef0123456789abcdef01234567"),
				Stage:  ConflictStageOk,
				Path:   "path/to/deep/file.go",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseMergeTreeConflictFile(tt.give)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseMergeTreeConflictFile_invalidInput(t *testing.T) {
	tests := []struct {
		name string
		give string
		want string // expected error message substring
	}{
		{
			name: "NoSpaceAfterMode",
			give: "100644abcdef",
			want: "expected <mode>, got EOL",
		},
		{
			name: "InvalidMode",
			give: "invalid abcdef1234567890abcdef1234567890abcdef12 1\tfile.txt",
			want: "invalid mode",
		},
		{
			name: "NoSpaceAfterObject",
			give: "100644 abcdef1234567890abcdef1234567890abcdef12",
			want: "expected <object>, got EOL",
		},
		{
			name: "NoTabBeforeFilename",
			give: "100644 abcdef1234567890abcdef1234567890abcdef12 1",
			want: "expected <stage> and <filename>, got EOL",
		},
		{
			name: "InvalidStage",
			give: "100644 abcdef1234567890abcdef1234567890abcdef12 9\tfile.txt",
			want: "invalid stage",
		},
		{
			name: "EmptyString",
			give: "",
			want: "expected <mode>, got EOL",
		},
		{
			name: "OnlyMode",
			give: "100644",
			want: "expected <mode>, got EOL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseMergeTreeConflictFile(tt.give)
			assert.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}
