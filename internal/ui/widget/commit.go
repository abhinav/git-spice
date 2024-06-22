package widget

import (
	"os"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/dustin/go-humanize"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/ui"
)

// CommitSummary is the summary of a single commit.
type CommitSummary struct {
	ShortHash  git.Hash
	Subject    string
	AuthorDate time.Time
}

// CommitSummaryStyle is the style for rendering a CommitSummary.
type CommitSummaryStyle struct {
	Hash    lipgloss.Style
	Subject lipgloss.Style
	Time    lipgloss.Style
}

// Faint returns a copy of the style with the faint attribute set to f.
func (s CommitSummaryStyle) Faint(f bool) CommitSummaryStyle {
	s.Hash = s.Hash.Faint(f)
	s.Subject = s.Subject.Faint(f)
	s.Time = s.Time.Faint(f)
	return s
}

// DefaultCommitSummaryStyle is the default style
// for rendering a CommitSummary.
var DefaultCommitSummaryStyle = CommitSummaryStyle{
	Hash:    ui.NewStyle().Foreground(ui.Yellow),
	Subject: ui.NewStyle().Foreground(ui.Plain),
	Time:    ui.NewStyle().Foreground(ui.Gray),
}

// Render renders a CommitSummary to the given writer.
func (c *CommitSummary) Render(w ui.Writer, style CommitSummaryStyle) {
	w.WriteString(style.Hash.Render(c.ShortHash.String()))
	w.WriteString(" ")
	w.WriteString(style.Subject.Render(c.Subject))
	w.WriteString(" ")
	w.WriteString(style.Time.Render("(" + humanizeTime(c.AuthorDate) + ")"))
}

var _timeNow = time.Now

func init() {
	now := os.Getenv("GIT_SPICE_NOW")
	if now != "" {
		t, err := time.Parse(time.RFC3339, now)
		if err == nil {
			_timeNow = func() time.Time {
				return t
			}
		}
	}
}

func humanizeTime(t time.Time) string {
	return humanize.RelTime(t, _timeNow(), "ago", "from now")
}
