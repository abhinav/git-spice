package silog

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.abhg.dev/gs/internal/must"
)

// logHandler is a slog.Handler that writes to an io.Writer
// with colored output.
//
// Output is in a logfmt-style format, with colored levels.
// Other features include:
//
//   - rendering of trace level
//   - multi-line fields are indented and aligned
type logHandler struct {
	lvl   slog.Leveler // required
	style *Style       // required
	outMu *sync.Mutex  // required
	out   io.Writer    // required

	// attrs holds attributes that have already been serialized
	// with WithAttrs.
	//
	// This is set only at construction time (e.g. WithAttrs)
	// and not modified afterwards.
	attrs []byte

	// groups is the current group stack.
	groups []string

	// prefix is the prefix to use for the logger.
	prefix string
}

var _ slog.Handler = (*logHandler)(nil)

func newLogHandler(out io.Writer, lvl slog.Leveler, style *Style) *logHandler {
	must.NotBeNilf(out, "output writer cannot be nil")
	must.NotBeNilf(lvl, "leveler cannot be nil")
	must.NotBeNilf(style, "style cannot be nil")

	return &logHandler{
		lvl:   lvl,
		style: style,
		outMu: new(sync.Mutex),
		out:   out,
	}
}

func (l *logHandler) Enabled(_ context.Context, lvl slog.Level) bool {
	return l.lvl.Level() <= lvl
}

const (
	lvlDelim     = " "  // separator between level and message
	groupDelim   = "."  // separator between group names
	msgAttrDelim = "  " // separator between message and attributes
	attrDelim    = " "  // separator between attributes
	indent       = "  " // indentation for multi-line attributes
)

func (l *logHandler) Handle(_ context.Context, rec slog.Record) error {
	bs := *takeBuf()
	defer releaseBuf(&bs)

	lvlString := l.style.LevelLabels.Get(Level(rec.Level)).String()
	// If the message is multi-line, we'll need to prepend the level
	// to each line.
	for line := range strings.Lines(rec.Message) {
		bs = append(bs, lvlString...)
		bs = append(bs, lvlDelim...)

		if l.prefix != "" {
			bs = append(bs, l.prefix...)
			bs = append(bs, l.style.PrefixDelimiter.Render()...)
		}

		// line may end with \n.
		// That should not be included in the rendering logic.
		var trailingNewline bool
		if line[len(line)-1] == '\n' {
			trailingNewline = true
			line = line[:len(line)-1]
		}
		line = l.style.Messages.Get(Level(rec.Level)).Render(line)
		bs = append(bs, line...)
		if trailingNewline {
			bs = append(bs, '\n')
		}
	}

	// First attribute after the message is separated by two spaces.
	bs = append(bs, msgAttrDelim...)

	// withAttrs attributes are serialized into the buffer
	if len(l.attrs) > 0 {
		bs = append(bs, l.attrs...)
	}

	// Write the attributes.
	formatter := attrFormatter{
		buf:    bs,
		style:  l.style,
		groups: slices.Clone(l.groups),
	}
	rec.Attrs(func(attr slog.Attr) bool {
		formatter.FormatAttr(attr)
		return true
	})
	bs = formatter.buf

	// Always a single trailing newline.
	bs = append(bytes.TrimRight(bs, " \n"), '\n')

	l.outMu.Lock()
	defer l.outMu.Unlock()
	_, err := l.out.Write(bs)
	return err
}

func (l *logHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	f := attrFormatter{
		buf:    slices.Clone(l.attrs),
		groups: slices.Clone(l.groups),
		style:  l.style,
	}
	for _, attr := range attrs {
		f.FormatAttr(attr)
	}
	bs := f.buf

	newL := *l
	newL.attrs = bs
	return &newL
}

func (l *logHandler) WithGroup(name string) slog.Handler {
	newL := *l
	newL.groups = append(slices.Clone(l.groups), name)
	return &newL
}

// WithLeveler returns a new handler with the given leveler
// but with the same attributes and groups as this handler.
// It will write to the same output writer as this handler.
func (l *logHandler) WithLeveler(lvl slog.Leveler) slog.Handler {
	newL := *l
	newL.lvl = lvl
	return &newL
}

// WithPrefix returns a new handler with the given prefix
func (l *logHandler) WithPrefix(prefix string) slog.Handler {
	newL := *l
	newL.prefix = prefix
	return &newL
}

type attrFormatter struct {
	buf    []byte
	style  *Style
	groups []string
}

func (f *attrFormatter) FormatAttr(attr slog.Attr) {
	if attr.Equal(slog.Attr{}) {
		return // skip empty attributes
	}

	value := attr.Value.Resolve()
	if value.Kind() == slog.KindGroup {
		// Groups just get splatted into their attributes
		// prefixed with the group name.
		f.groups = append(f.groups, attr.Key)
		for _, a := range value.Group() {
			f.FormatAttr(a)
		}
		f.groups = f.groups[:len(f.groups)-1]
		return
	}

	// We serialize the attribute into a byte slice,
	// and then decide how it goes into the output.
	// This is because we need to handle multi-line attributes
	// and indent them.
	valbs := *takeBuf()
	defer releaseBuf(&valbs)

	switch value.Kind() {
	case slog.KindBool:
		valbs = strconv.AppendBool(valbs, value.Bool())
	case slog.KindDuration:
		valbs = append(valbs, value.Duration().String()...)
	case slog.KindFloat64:
		valbs = strconv.AppendFloat(valbs, value.Float64(), 'g', -1, 64)
	case slog.KindInt64:
		valbs = strconv.AppendInt(valbs, value.Int64(), 10)
	case slog.KindString:
		valbs = append(valbs, value.String()...)
	case slog.KindTime:
		valbs = value.Time().AppendFormat(valbs, time.Kitchen)
	case slog.KindUint64:
		valbs = strconv.AppendUint(valbs, value.Uint64(), 10)
	default:
		// TODO: reflection to handle structs, maps, slices, etc.
		valbs = append(valbs, value.String()...)
	}

	// Add delimiter between attrs.
	if len(f.buf) > 0 {
		switch {
		case f.buf[len(f.buf)-1] == '\n':
			// If the last thing we wrote was multi-line,
			// then we need to indent the next attribute.
			f.buf = append(f.buf, indent...)
		case f.buf[len(f.buf)-1] != ' ':
			// All other attributes are separated by a space.
			f.buf = append(f.buf, attrDelim...)
		}
	}

	// Single-line attributes are rendered as:
	//
	//   key=value
	//
	// Multi-line attributes are rendered as:
	//
	//   key=
	//     | line 1
	//     | line 2
	isMultiline := bytes.ContainsAny(valbs, "\r\n")
	if isMultiline {
		f.buf = append(f.buf, '\n')
		f.buf = append(f.buf, indent...)
	}

	f.formatKey(attr.Key)
	f.buf = append(f.buf, f.style.KeyValueDelimiter.Render()...) // =

	valueStyle, hasStyle := f.style.Values[attr.Key]
	if isMultiline {
		prefixStyle := f.style.MultilinePrefix
		if hasStyle {
			prefixStyle = prefixStyle.Foreground(valueStyle.GetForeground())
		}
		prefix := indent + prefixStyle.Render()

		// TODO: \r handling
		f.buf = append(f.buf, '\n')
		for line := range bytes.Lines(valbs) {
			f.buf = append(f.buf, prefix...)
			line = bytes.TrimRight(line, "\r\n")
			if hasStyle {
				f.buf = append(f.buf, valueStyle.Render(string(line))...)
			} else {
				f.buf = append(f.buf, line...)
			}
			f.buf = append(f.buf, '\n')
		}

		// If multi-line attribute value does not end with a newline,
		// add one.
		if f.buf[len(f.buf)-1] != '\n' {
			f.buf = append(f.buf, '\n')
		}
	} else {
		if hasStyle {
			f.buf = append(f.buf, valueStyle.Render(string(valbs))...)
		} else {
			f.buf = append(f.buf, valbs...)
		}
	}
}

// formatKey writes a group-prefixed key to the buffer.
func (f *attrFormatter) formatKey(key string) {
	for _, group := range f.groups {
		if group != "" {
			f.buf = append(f.buf, f.style.Key.Render(group)...)
			f.buf = append(f.buf, groupDelim...)
		}
	}
	f.buf = append(f.buf, f.style.Key.Render(key)...)
}

var _bufPool = &sync.Pool{
	New: func() any {
		bs := make([]byte, 0, 1024)
		return &bs
	},
}

func takeBuf() *[]byte {
	bs := _bufPool.Get().(*[]byte)
	*bs = (*bs)[:0]
	return bs
}

func releaseBuf(bs *[]byte) {
	_bufPool.Put(bs)
}
