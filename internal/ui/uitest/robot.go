package uitest

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/ui"
)

const (
	// DefaultWidth is the default terminal width.
	DefaultWidth = 80

	// DefaultHeight is the default terminal height.
	DefaultHeight = 24
)

var (
	_commentPrefix = []byte(">")
	_separator     = []byte("===")
)

// RobotView is an [ui.InteractiveView] that simulates user input in tests.
//
// It is backed by a fixture file that contains the inputs.
// The file is divided into sections by a line containing only `===`.
// Each section corresponds to a prompt and contains JSON-encoded input
// for that prompt.
//
//	===
//	"Alice"
//	===
//	["branch1", "branch2"]
//
// For convenience, the first "===" is optional.
// The JSON input is fed into the corresponding field's UnmarshalValue method.
//
// ">"-prefixed lines before the JSON-encoded input are treated as comments.
//
//	> This is a comment
//	> across multiple lines
//	"Alice"
//
// If an output file is provided, the fixture is reproduced to it,
// along with a comment containing each prompt's rendered form.
// This allows comparing the resulting prompts with the prompts that the input
// was written for.
//
//	===
//	> Enter a name:
//	"Alice"
//	===
//	> Pick branches:
//	>  - branch1
//	>  - branch2
//	>  - branch3
//	["branch1", "branch2"]
//
// Non-prompt output from the application is also written to the output file.
//
// The current position in the fixture file is recorded in a separate file
// to allow resuming from where the test left off.
// This allows reusing the same fixture file across multiple executions.
type RobotView struct {
	fixtureFile  string
	positionFile string
	logger       *log.Logger
	w, h         int

	// outputBuffer holds non-prompt output until the next prompt.
	// This will be made part of the comment that is generated for the prompt.
	outputBuffer bytes.Buffer
	outputWriter outputFileWriter
}

// RobotViewOptions customizes a [RobotView].
type RobotViewOptions struct {
	// OutputFile is the file to write the output to.
	//
	// This will contain all the original inputs from the fixture file,
	// alongside rendered prompts for those inputs.
	//
	// If unset, the output is discarded.
	OutputFile string

	// LogOutput is the writer to log messages to.
	//
	// If unset, messages are discarded.
	LogOutput io.Writer

	// Size of the terminal window.
	//
	// Defaults to DefaultWidth and DefaultHeight.
	Width, Height int
}

// NewRobotView creates a new [RobotView] that reads from the given
// fixture file and writes to the given output file.
// If outputFile is empty, output is discarded.
//
// The returned RobotView must be closed once you're done with it.
func NewRobotView(fixtureFile string, opts *RobotViewOptions) (*RobotView, error) {
	opts = cmp.Or(opts, &RobotViewOptions{})

	var of outputFileWriter = &nopOutputFile{}
	if opts.OutputFile != "" {
		file, err := os.OpenFile(opts.OutputFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, fmt.Errorf("create output file: %w", err)
		}

		of = file
	}

	logOutput := opts.LogOutput
	if logOutput == nil {
		logOutput = io.Discard
	}
	logger := log.NewWithOptions(logOutput, log.Options{
		Level: log.DebugLevel,
	})

	return &RobotView{
		fixtureFile:  fixtureFile,
		positionFile: fixtureFile + ".pos",
		outputWriter: of,
		logger:       logger,
		w:            cmp.Or(opts.Width, DefaultWidth),
		h:            cmp.Or(opts.Height, DefaultHeight),
	}, nil
}

var _ ui.InteractiveView = (*RobotView)(nil)

// Write writes non-prompt output.
//
// This is buffered in-memory until the next prompt,
// or until the view is closed.
func (s *RobotView) Write(bs []byte) (n int, err error) {
	return s.outputBuffer.Write(bs)
}

// Close flushes the output buffer to the output file.
func (s *RobotView) Close() error {
	if s.outputBuffer.Len() > 0 {
		fixture := robotFixture{Comment: s.outputBuffer.String()}
		if err := s.appendOutputFixture(fixture); err != nil {
			return fmt.Errorf("append output fixture: %w", err)
		}
		s.outputBuffer.Reset()
	}

	return s.outputWriter.Close()
}

// Prompt runs the given fields as prompts,
// reading the remaining input values from the fixture file.
// If the fixture file is exhausted, an error is returned.
func (s *RobotView) Prompt(fields ...ui.Field) error {
	log := s.logger

	var inputFixtures robotFixtureFile
	if err := inputFixtures.ReadFile(s.fixtureFile); err != nil {
		return fmt.Errorf("read fixture: %w", err)
	}

fieldLoop:
	for fieldIdx, field := range fields {
		cmd := tea.Sequence(
			field.Init(),
			field.Update(tea.WindowSizeMsg{
				Width:  s.w,
				Height: s.h,
			}),
		)
		msgs := []tea.Msg{cmd()}
		for len(msgs) > 0 {
			var msg tea.Msg
			msg, msgs = msgs[0], msgs[1:]

			switch msg := msg.(type) {
			case tea.Cmd:
				if msg != nil {
					msgs = append(msgs, msg())
				}

			case tea.BatchMsg:
				for _, cmd := range msg {
					msgs = append(msgs, cmd())
				}

			default:
				// HACK:
				// tea.sequenceMsg is private, but it's a slice.
				if v := reflect.ValueOf(msg); v.IsValid() && v.Kind() == reflect.Slice {
					for i := range v.Len() {
						msgs = append(msgs, v.Index(i).Interface())
					}

					continue
				}

				switch msg {
				case ui.SkipField():
					continue fieldLoop

				case tea.QuitMsg{}:
					return nil
				}
			}
		}

		var fieldView strings.Builder
		fieldView.Write(s.outputBuffer.Bytes())
		s.outputBuffer.Reset()
		if title := field.Title(); title != "" {
			fieldView.WriteString(field.Title())
			fieldView.WriteString(": ")
		}
		field.Render(&fieldView)
		if desc := field.Description(); desc != "" {
			fieldView.WriteString("\n")
			fieldView.WriteString(desc)
		}
		if err := field.Err(); err != nil {
			return fmt.Errorf("field [%d]: %w", fieldIdx, err)
		}

		var fixture robotFixture
		for {
			pos, err := s.nextPos()
			if err != nil {
				return fmt.Errorf("field [%d]: next pos: %w", fieldIdx, err)
			}

			if pos >= len(inputFixtures) {
				log.Error("Unexpected prompt",
					"prompt", fieldView.String())
				return fmt.Errorf("field [%d]: no more fixtures", fieldIdx)
			}

			f := inputFixtures[pos]
			if strings.TrimSpace(f.Value) == "" {
				// Skip fixtures that only contain comments.
				continue
			}

			fixture = f
			break
		}

		err := field.UnmarshalValue(func(dst any) error {
			dec := json.NewDecoder(strings.NewReader(fixture.Value))
			dec.DisallowUnknownFields()
			return dec.Decode(dst)
		})
		if err != nil {
			log.Error("Error unmarshalling value into field",
				"field", strings.Trim(fieldView.String(), "\n"),
				"value", fixture.Value,
				"error", err,
			)
			return fmt.Errorf("field [%d]: bad input: %w", fieldIdx, err)
		}

		fixture.Comment = fieldView.String()
		if err := s.appendOutputFixture(fixture); err != nil {
			return fmt.Errorf("append output fixture: %w", err)
		}
	}

	return nil
}

// nextPos returns the next position in the position file,
// and increments the value for the next call.
//
// If there's no file, it will be created.
func (s *RobotView) nextPos() (int, error) {
	var pos int
	if bs, err := os.ReadFile(s.positionFile); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return 0, err
		}

		pos = 0
	} else {
		pos, err = strconv.Atoi(string(bs))
		if err != nil {
			return 0, fmt.Errorf("parse position: %w", err)
		}
	}

	newBs := []byte(strconv.Itoa(pos + 1))
	if err := os.WriteFile(s.positionFile, newBs, 0o644); err != nil {
		return 0, fmt.Errorf("write position: %w", err)
	}

	return pos, nil
}

func (s *RobotView) appendOutputFixture(fixture robotFixture) error {
	p := printer{w: s.outputWriter}
	fixture.Print(&p)
	if err := p.Err(); err != nil {
		return fmt.Errorf("write output fixture: %w", err)
	}

	return nil
}

type robotFixtureFile []robotFixture

func (sf *robotFixtureFile) ReadFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer func() { _ = f.Close() }()

	return sf.Read(f)
}

func (sf *robotFixtureFile) Read(r io.Reader) error {
	scan := bufio.NewScanner(r)
	var (
		comment strings.Builder
		jsonBuf strings.Builder
	)

	flush := func() {
		if jsonBuf.Len() == 0 && comment.Len() == 0 {
			return
		}

		*sf = append(*sf, robotFixture{
			Comment: strings.Trim(comment.String(), "\n"),
			Value:   jsonBuf.String(),
		})

		comment.Reset()
		jsonBuf.Reset()
	}

	for scan.Scan() {
		line := scan.Bytes()

		if bytes.Equal(line, _separator) {
			flush()
			continue
		}

		if bytes.HasPrefix(line, _commentPrefix) {
			line := line[len(_commentPrefix):]
			if len(line) > 0 && line[0] == ' ' {
				line = line[1:] // skip space after ">"
			}

			comment.Write(line)
			comment.WriteByte('\n')
			continue
		}

		jsonBuf.Write(line)
		jsonBuf.WriteByte('\n')
	}

	flush()
	return scan.Err()
}

func (sf robotFixtureFile) Write(w io.Writer) error {
	p := printer{w: w}
	for _, fixture := range sf {
		fixture.Print(&p)
	}
	return p.Err()
}

type robotFixture struct {
	Comment string
	Value   string // JSON
}

func (sf *robotFixture) Print(p *printer) {
	p.Write(_separator)
	p.WriteString("\n")

	comment := strings.Trim(sf.Comment, "\n") // strip extraneous newlines
	for cl := range strings.SplitSeq(comment, "\n") {
		p.Write(_commentPrefix)
		if len(cl) > 0 {
			p.WriteString(" ")
			p.WriteString(cl)
		}
		p.WriteString("\n")
	}

	p.WriteString(sf.Value)
	if len(sf.Value) > 0 && sf.Value[len(sf.Value)-1] != '\n' {
		p.WriteString("\n")
	}
}

type printer struct {
	w   io.Writer
	err error
}

func (p *printer) Write(bs []byte) {
	if p.err == nil {
		_, p.err = p.w.Write(bs)
	}
}

func (p *printer) WriteString(s string) {
	if p.err == nil {
		_, p.err = io.WriteString(p.w, s)
	}
}

func (p *printer) Err() error {
	return p.err
}

type outputFileWriter interface {
	io.WriteCloser

	Sync() error
}

type nopOutputFile struct{}

var _ outputFileWriter = (*nopOutputFile)(nil)

func (nopOutputFile) Write(bs []byte) (int, error) { return len(bs), nil }

func (nopOutputFile) Close() error { return nil }

func (nopOutputFile) Sync() error { return nil }
