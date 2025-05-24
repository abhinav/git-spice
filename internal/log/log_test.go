package log_test

import (
	"log/slog"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/log"
)

func TestNop(*testing.T) {
	log := log.Nop(nil)
	log.Info("foo")
}

func TestLogger_changeLevel(t *testing.T) {
	var buffer strings.Builder
	logger := log.New(&buffer, nil)

	assert.Equal(t, log.LevelInfo, logger.Level(),
		"default level should be Info")

	logger.Debug("foo")
	assert.Empty(t, buffer.String())

	logger.SetLevel(log.LevelDebug)

	logger.Debug("foo")
	assert.Equal(t, "DBG foo\n", buffer.String())
}

func TestLogger_formatting(t *testing.T) {
	var buffer strings.Builder
	log := log.New(&buffer, &log.Options{
		Level: log.LevelTrace,
	})

	assertLines := func(t *testing.T, lines ...string) bool {
		t.Helper()

		defer buffer.Reset()

		want := strings.Join(lines, "\n") + "\n"
		got := buffer.String()
		return assert.Equal(t, want, got)
	}

	t.Run("Message", func(t *testing.T) {
		log.Info("foo")
		assertLines(t, "INF foo")
	})

	t.Run("Levels", func(t *testing.T) {
		tests := []struct {
			name  string
			logFn func(string, ...any)
			want  string
		}{
			{"trace", log.Trace, "TRC hello"},
			{"debug", log.Debug, "DBG hello"},
			{"info", log.Info, "INF hello"},
			{"warn", log.Warn, "WRN hello"},
			{"error", log.Error, "ERR hello"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				tt.logFn("hello")
				assertLines(t, tt.want)
			})
		}
	})

	t.Run("Printf", func(t *testing.T) {
		tests := []struct {
			name  string
			logFn func(string, ...any)
			want  string
		}{
			{"trace", log.Tracef, "TRC hello world"},
			{"debug", log.Debugf, "DBG hello world"},
			{"info", log.Infof, "INF hello world"},
			{"warn", log.Warnf, "WRN hello world"},
			{"error", log.Errorf, "ERR hello world"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				tt.logFn("hello %s", "world")
				assertLines(t, tt.want)
			})
		}
	})

	t.Run("Attrs", func(t *testing.T) {
		someDate := time.Date(2025, 5, 20, 21, 0, 0, 0, time.UTC)

		tests := []struct {
			name  string
			value any
			want  string
		}{
			{"Bool", true, "true"},
			{"Duration", time.Second, "1s"},
			{"Float64", 3.14, "3.14"},
			{"Int64", int64(42), "42"},
			{"String", "foo", "foo"},
			{"Time", someDate, "9:00PM"},
			{"Uint64", uint64(42), "42"},
			{"Stringer", &testStringer{"foo"}, "foo"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				log.Info("foo", "k1", tt.value)
				assertLines(t, "INF foo  k1="+tt.want)
			})
		}
	})

	t.Run("EmptyAttr", func(t *testing.T) {
		log.Info("foo", slog.Attr{}, "foo", "bar")
		assertLines(t, "INF foo  foo=bar")
	})

	t.Run("MultilineMessage", func(t *testing.T) {
		log.Info("foo\nbar\nbaz")
		assertLines(t,
			"INF foo",
			"INF bar",
			"INF baz",
		)
	})

	t.Run("MultilineMessageWithLeadingSpaces", func(t *testing.T) {
		var s strings.Builder
		s.WriteString("foo\n")
		s.WriteString("  bar\n")
		s.WriteString("    baz\n")
		s.WriteString("qux\n")
		log.Info(s.String())

		assertLines(t,
			"INF foo",
			"INF   bar",
			"INF     baz",
			"INF qux",
		)
	})

	t.Run("WithAttrs", func(t *testing.T) {
		log := log.With("k1", true, "k2", 2, "k3", 3.0, "k4", "foo")
		log.Info("bar")

		assertLines(t, "INF bar  k1=true k2=2 k3=3 k4=foo")
	})

	t.Run("WithAttrsEmpty", func(t *testing.T) {
		log := log.With()
		log.Info("bar")
		assertLines(t, "INF bar")
	})

	t.Run("MultilineMessageWithAttrs", func(t *testing.T) {
		log := log.With("k1", true, "k2", 2, "k3", 3.0, "k4", "foo")
		log.Info("bar\nbaz")

		assertLines(t,
			"INF bar",
			"INF baz  k1=true k2=2 k3=3 k4=foo",
		)
	})

	t.Run("MultilineMessageWithAttrsAndLeadingNewline", func(t *testing.T) {
		log := log.With("k1", true, "k2", 2, "k3", 3.0, "k4", "foo")
		log.Info("bar\nbaz\n")

		assertLines(t,
			"INF bar",
			"INF baz",
			"  k1=true k2=2 k3=3 k4=foo",
		)
	})

	t.Run("Attrs", func(t *testing.T) {
		log.Info("foo", "k1", true, "k2", 2, "k3", 3.0, "k4", "bar")
		assertLines(t, "INF foo  k1=true k2=2 k3=3 k4=bar")
	})

	t.Run("AttrsWithAttrs", func(t *testing.T) {
		log := log.With("k1", true, "k2", 2)
		log.Info("foo", "k3", 3.0, "k4", "bar")
		log.Warn("baz", "k5", 5.0, "k6", "qux")

		assertLines(t,
			"INF foo  k1=true k2=2 k3=3 k4=bar",
			"WRN baz  k1=true k2=2 k5=5 k6=qux",
		)
	})

	t.Run("WithGroup", func(t *testing.T) {
		log := log.WithGroup("g")
		log.Info("foo", "k1", true, "k2", 2, "k3", 3.0, "k4", "bar")
		assertLines(t, "INF foo  g.k1=true g.k2=2 g.k3=3 g.k4=bar")
	})

	t.Run("WithGroupEmpty", func(t *testing.T) {
		log := log.WithGroup("")
		log.Info("foo", "k1", true)
		assertLines(t, "INF foo  k1=true")
	})

	t.Run("WithGroupWithAttrs", func(t *testing.T) {
		log := log.WithGroup("g").With("k1", true, "k2", 2)
		log.Info("foo", "k3", 3.0, "k4", "bar")
		log.Warn("baz", "k5", 5.0, "k6", "qux")

		assertLines(t,
			"INF foo  g.k1=true g.k2=2 g.k3=3 g.k4=bar",
			"WRN baz  g.k1=true g.k2=2 g.k5=5 g.k6=qux",
		)
	})

	t.Run("AttrGroup", func(t *testing.T) {
		log.Info("foo", slog.Group("bar", "k1", true, "k2", 2, "k3", 3.0, "k4", "bar"))
		assertLines(t, "INF foo  bar.k1=true bar.k2=2 bar.k3=3 bar.k4=bar")
	})

	t.Run("AttrGroupEmptyAttr", func(t *testing.T) {
		log.Info("foo", slog.Group("bar", slog.Attr{}, "k1", true, "k2", 2, "k3", 3.0, "k4", "bar")) //nolint:loggercheck
		assertLines(t, "INF foo  bar.k1=true bar.k2=2 bar.k3=3 bar.k4=bar")
	})

	t.Run("TrailingNewlineMessage", func(t *testing.T) {
		log.Info("foo\n")
		assertLines(t, "INF foo")
	})

	t.Run("TrailingNewlineMessageWithAttr", func(t *testing.T) {
		log.Info("foo\n", "k1", true)
		assertLines(t,
			"INF foo",
			"  k1=true",
		)
	})

	t.Run("MultilineAttrValue", func(t *testing.T) {
		log.Info("foo", "k1", "bar\nbaz\nqux", "k2", "quux")
		assertLines(t,
			"INF foo  ",
			"  k1=",
			"    | bar",
			"    | baz",
			"    | qux",
			"  k2=quux",
		)
	})

	t.Run("MultlineAttrValueNestedInGroup", func(t *testing.T) {
		log := log.WithGroup("a").WithGroup("b")
		log.Info("foo", slog.Group("c", "d", "foo\nbar\nbaz", "e", "qux"))

		assertLines(t,
			"INF foo  ",
			"  a.b.c.d=",
			"    | foo",
			"    | bar",
			"    | baz",
			"  a.b.c.e=qux",
		)
	})

	t.Run("LeadingWhitespace", func(t *testing.T) {
		log.Info(" foo")
		assertLines(t, "INF  foo")
	})

	t.Run("TrailingWhitespace", func(t *testing.T) {
		log.Info("foo ")
		assertLines(t, "INF foo")
	})
}

func TestLogger_Fatal(t *testing.T) {
	var buffer strings.Builder

	done := make(chan struct{})
	go func() {
		defer close(done)

		logger := log.New(&buffer, &log.Options{
			OnFatal: runtime.Goexit,
		})

		logger.Fatal("foo")

		t.Errorf("logger should not return")
	}()
	<-done

	assert.Equal(t, "FTL foo\n", buffer.String())
}

func TestLogger_Fatalf(t *testing.T) {
	var buffer strings.Builder

	done := make(chan struct{})
	go func() {
		defer close(done)

		logger := log.New(&buffer, &log.Options{
			OnFatal: runtime.Goexit,
		})

		logger.Fatalf("foo %s", "bar")

		t.Errorf("logger should not return")
	}()
	<-done

	assert.Equal(t, "FTL foo bar\n", buffer.String())
}

type testStringer struct{ v string }

func (s *testStringer) String() string { return s.v }
