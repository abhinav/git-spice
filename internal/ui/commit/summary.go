package commit

import (
	"cmp"
	"os"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/dustin/go-humanize"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/ui"
)

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

// Summary is the summary of a single commit.
type Summary struct {
	ShortHash  git.Hash
	Subject    string
	AuthorDate time.Time
}

// SummaryStyle is the style for rendering a Summary.
type SummaryStyle struct {
	Hash    lipgloss.Style
	Subject lipgloss.Style
	Time    lipgloss.Style
}

// Faint returns a copy of the style with the faint attribute set to f.
func (s SummaryStyle) Faint(f bool) SummaryStyle {
	s.Hash = s.Hash.Faint(f)
	s.Subject = s.Subject.Faint(f)
	s.Time = s.Time.Faint(f)
	return s
}

// Bold returns a copy of the style with bold set to true on all fields.
func (s SummaryStyle) Bold(b bool) SummaryStyle {
	s.Hash = s.Hash.Bold(b)
	s.Subject = s.Subject.Bold(b)
	s.Time = s.Time.Bold(b)
	return s
}

// DefaultSummaryStyle is the default style
// for rendering a Summary.
var DefaultSummaryStyle = SummaryStyle{
	Hash:    ui.NewStyle().Foreground(ui.Yellow),
	Subject: ui.NewStyle().Foreground(ui.Plain),
	Time:    ui.NewStyle().Foreground(ui.Gray),
}

// SummaryOptions further customizes the behavior of commit summary rendering.
type SummaryOptions struct {
	Now func() time.Time
}

// Render renders a Summary to the given writer.
func (c *Summary) Render(w ui.Writer, style SummaryStyle, opts *SummaryOptions) {
	opts = cmp.Or(opts, &SummaryOptions{})
	now := opts.Now
	if now == nil {
		now = _timeNow
	}

	w.WriteString(style.Hash.Render(c.ShortHash.String()))
	w.WriteString(" ")
	w.WriteString(style.Subject.Render(c.Subject))
	w.WriteString(" ")
	w.WriteString(style.Time.Render("(" + humanizeTime(now, c.AuthorDate) + ")"))
}

func humanizeTime(now func() time.Time, t time.Time) string {
	return humanize.RelTime(t, now(), "ago", "from now")
}
