package silog

import "log/slog"

// Level is a log level.
type Level slog.Level

var _ slog.Leveler = Level(0)

// Supported log levels.
const (
	LevelDebug = Level(slog.LevelDebug)
	LevelInfo  = Level(slog.LevelInfo)
	LevelWarn  = Level(slog.LevelWarn)
	LevelError = Level(slog.LevelError)
	LevelFatal = Level(slog.LevelError + 4)
)

// Levels is a list of all supported log levels.
var Levels = []Level{
	LevelDebug,
	LevelInfo,
	LevelWarn,
	LevelError,
	LevelFatal,
}

// String returns the string representation of the log level.
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	case LevelFatal:
		return "fatal"
	default:
		return slog.Level(l).String()
	}
}

// Level returns the level as a slog.Level.
func (l Level) Level() slog.Level {
	return slog.Level(l)
}

// Dec lowers the log level by N steps.
func (l Level) Dec(n int) Level {
	return l - Level(n*4)
}

// ByLevel is a struct that contains fields for each log level.
type ByLevel[T any] struct {
	Debug T
	Info  T
	Warn  T
	Error T
	Fatal T
}
