package mergeprogress

import (
	"math"
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	_animationTick             = 80 * time.Millisecond
	_animationMinCycle         = 1400 * time.Millisecond
	_animationMaxCellsPerFrame = 2
)

// animationTickMsg carries the elapsed animation time for the next frame.
type animationTickMsg time.Duration

// animationState owns the active-segment animation clock.
//
// It keeps the timing policy together with the state Bubble Tea mutates
// as ticks are scheduled and delivered.
type animationState struct {
	enabled bool // whether animation may schedule ticks

	elapsed time.Duration // elapsed animation time

	// tickScheduled prevents ordinary progress events from starting
	// overlapping animation clocks while an earlier tick is still pending.
	tickScheduled bool
}

func (a animationState) marker(
	width int,
	glyphs AnimationGlyphSet,
) (position int, glyph string, ok bool) {
	if !a.enabled || width <= 0 {
		return 0, "", false
	}

	cycle := a.cycleForWidth(width)
	phase := float64(a.elapsed%cycle) / float64(cycle)
	// A cosine wave maps one cycle into a smooth ping-pong path:
	// 0 at the start,
	// 1 halfway through,
	// and 0 again at the end.
	eased := (1 - math.Cos(2*math.Pi*phase)) / 2
	position = int(math.Round(eased * float64(width-1)))

	if phase < 0.05 || phase >= 0.95 {
		return position, glyphs.LeftEdge, true
	}
	if phase > 0.45 && phase < 0.55 {
		return position, glyphs.RightEdge, true
	}

	speed := math.Abs(math.Sin(2 * math.Pi * phase))
	switch {
	case speed > 0.85:
		return position, glyphs.Fast, true
	case speed > 0.45:
		return position, glyphs.Medium, true
	default:
		return position, glyphs.Slow, true
	}
}

func (a *animationState) resetSchedule() {
	a.tickScheduled = false
}

func (a *animationState) advance(msg animationTickMsg) {
	a.tickScheduled = false
	a.elapsed = time.Duration(msg)
}

func (a *animationState) scheduleTick(hasActiveItem bool) tea.Cmd {
	// Only one timer may be outstanding,
	// or progress events can start competing animation clocks.
	if !a.enabled || !hasActiveItem || a.tickScheduled {
		return nil
	}

	nextElapsed := a.elapsed + _animationTick
	a.tickScheduled = true
	return tea.Tick(_animationTick, func(time.Time) tea.Msg {
		return animationTickMsg(nextElapsed)
	})
}

func (animationState) cycleForWidth(width int) time.Duration {
	if width <= 1 {
		return _animationMinCycle
	}

	// The cosine easing moves fastest at the center of the segment.
	// Size the cycle for that peak speed instead of the average speed
	// so wide segments do not jump too many cells between frames.
	cycle := time.Duration(math.Ceil(
		float64(width-1) *
			math.Pi *
			float64(_animationTick) /
			float64(_animationMaxCellsPerFrame),
	))
	return max(_animationMinCycle, cycle)
}
