// Package log wraps slog.Logger with support for:
//
//   - printf-style functions for each level
//     to break out of structured logging when needed
//   - trace and fatal levels
package log

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/mattn/go-isatty"
	"go.abhg.dev/gs/internal/must"
)

// Options defines options for the logger.
type Options struct {
	// Level is the minimum log level to log.
	// It must be one of the supported log levels.
	// The default is LevelInfo.
	Level Level

	// OnFatal is a function that will be called
	// when a fatal log message is logged.
	//
	// This SHOULD stop control flow.
	// If it does not, the default implementation will panic.
	//
	// If unset, the program will exit with a non-zero status code
	// when a fatal log message is logged.
	OnFatal func() // optional

	// Style is the style to use for the logger.
	// If unset, the style will be picked based on whether
	// the output is a terminal or not.
	Style *Style // optional
}

// Logger is a logger that provided structured and printf-style logging.
// It supports the following levels: Trace, Debug, Info, Warn, Error.
// For each level, the logger provides a structured logging method (e.g. Info)
// and a printf-style method (e.g. Infof).
type Logger struct {
	sl      *slog.Logger   // required
	lvl     *slog.LevelVar // required
	onFatal func()         // required
}

// Nop returns a no-op logger that discards all log messages.
func Nop(options ...*Options) *Logger {
	if len(options) > 1 {
		panic("too many options")
	}
	var opts *Options
	if len(options) == 1 {
		opts = options[0]
	}
	return New(io.Discard, opts)
}

// New creates a new logger that writes to the given writer.
// Options customize the behavior of the logger if specified.
func New(w io.Writer, opts *Options) *Logger {
	opts = cmp.Or(opts, &Options{
		Level: LevelInfo,
	})

	must.Bef(opts.Level >= LevelDebug, "level must be >= LevelDebug, got %d", opts.Level)
	must.Bef(opts.Level <= LevelError, "level must be <= LevelError, got %d", opts.Level)

	if opts.Style == nil {
		// The output writer must be file-like to check if it is a TTY.
		var isTTY bool
		if fileLike, ok := w.(interface{ Fd() uintptr }); ok {
			isTTY = isatty.IsTerminal(fileLike.Fd())
		}

		if isTTY {
			opts.Style = DefaultStyle()
		} else {
			opts.Style = PlainStyle()
		}
	}

	var lvl slog.LevelVar
	lvl.Set(opts.Level.Level())
	sl := slog.New(newLogHandler(w, &lvl, opts.Style))

	onFatal := opts.OnFatal
	if onFatal == nil {
		onFatal = exitOnFatal
	}

	return &Logger{
		sl:      sl,
		lvl:     &lvl,
		onFatal: onFatal,
	}
}

// Clone returns a new logger with the same configuration
// as the original logger.
func (l *Logger) Clone() *Logger {
	newL := *l
	return &newL
}

// Level returns the current log level of the logger.
func (l *Logger) Level() Level {
	return Level(l.lvl.Level())
}

// SetLevel changes the log level of the logger
// and all loggers cloned from it.
func (l *Logger) SetLevel(lvl Level) {
	l.lvl.Set(lvl.Level())
}

// WithGroup returns a copy of the logger with the given group name added.
func (l *Logger) WithGroup(name string) *Logger {
	newL := l.Clone()
	newL.sl = newL.sl.WithGroup(name)
	return newL
}

// With returns a copy of the logger with the given attributes added.
func (l *Logger) With(attrs ...any) *Logger {
	if len(attrs) == 0 {
		return l
	}

	newL := l.Clone()
	newL.sl = newL.sl.With(attrs...)
	return newL
}

// Log logs a message at the given level with the given key-value pairs.
func (l *Logger) Log(lvl Level, msg string, kvs ...any) {
	l.sl.Log(context.Background(), lvl.Level(), msg, kvs...)
	if lvl >= LevelFatal {
		l.onFatal()
		panic("unreachable: onFatal should stop control flow")
	}
}

// Logf logs a message at the given level with the given format and arguments.
func (l *Logger) Logf(lvl Level, format string, args ...any) {
	l.Log(lvl, fmt.Sprintf(format, args...))
}

// Debug posts a structured log message with the level [LevelDebug].
func (l *Logger) Debug(msg string, kvs ...any) { l.Log(LevelDebug, msg, kvs...) }

// Info posts a structured log message with the level [LevelInfo].
func (l *Logger) Info(msg string, kvs ...any) { l.Log(LevelInfo, msg, kvs...) }

// Warn posts a structured log message with the level [LevelWarn].
func (l *Logger) Warn(msg string, kvs ...any) { l.Log(LevelWarn, msg, kvs...) }

// Error posts a structured log message with the level [LevelError].
func (l *Logger) Error(msg string, kvs ...any) { l.Log(LevelError, msg, kvs...) }

// Fatal posts a structured log message with the level [LevelFatal].
// It also exits the program with a non-zero status code.
func (l *Logger) Fatal(msg string, kvs ...any) { l.Log(LevelFatal, msg, kvs...) }

// Debugf posts a printf-style log message with the level [LevelDebug].
func (l *Logger) Debugf(format string, args ...any) { l.Logf(LevelDebug, format, args...) }

// Infof posts a printf-style log message with the level [LevelInfo].
func (l *Logger) Infof(format string, args ...any) { l.Logf(LevelInfo, format, args...) }

// Warnf posts a printf-style log message with the level [LevelWarn].
func (l *Logger) Warnf(format string, args ...any) { l.Logf(LevelWarn, format, args...) }

// Errorf posts a printf-style log message with the level [LevelError].
func (l *Logger) Errorf(format string, args ...any) { l.Logf(LevelError, format, args...) }

// Fatalf posts a printf-style log message with the level [LevelFatal].
// It also exits the program with a non-zero status code.
func (l *Logger) Fatalf(format string, args ...any) { l.Logf(LevelFatal, format, args...) }

func exitOnFatal() { os.Exit(1) }
