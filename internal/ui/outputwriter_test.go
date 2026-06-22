package ui

import (
	"bytes"
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOutputWriter_Write_inactiveWritesDirect(t *testing.T) {
	var buf bytes.Buffer
	w := NewOutputWriter(&buf)

	_, err := w.Write([]byte("direct output\n"))

	require.NoError(t, err)
	assert.Equal(t, "direct output\n", buf.String())
}

func TestOutputWriter_Write_activeSendsCompletedLines(t *testing.T) {
	var buf bytes.Buffer
	var program captureProgram
	w := NewOutputWriter(&buf)
	stopPrinting := w.printTo(&program)

	_, err := w.Write([]byte("one"))
	require.NoError(t, err)
	assert.Empty(t, program.msgs)
	assert.Empty(t, buf.String())

	_, err = w.Write([]byte("\r\ntwo\npartial"))
	require.NoError(t, err)

	require.Len(t, program.msgs, 2)
	assertPrintLineMessage(t, program.msgs[0], "one")
	assertPrintLineMessage(t, program.msgs[1], "two")
	assert.Empty(t, buf.String())

	stopPrinting()
	_, err = w.Write([]byte("direct output\n"))
	require.NoError(t, err)
	assert.Equal(t, "partialdirect output\n", buf.String())
}

type captureProgram struct {
	msgs []tea.Msg
}

func (p *captureProgram) Send(msg tea.Msg) {
	p.msgs = append(p.msgs, msg)
}

func assertPrintLineMessage(t *testing.T, msg tea.Msg, want string) {
	t.Helper()

	expected := tea.Println(want)()
	require.Equal(t, reflect.TypeOf(expected), reflect.TypeOf(msg))
	assert.Equal(t, want,
		reflect.ValueOf(msg).FieldByName("messageBody").String())
}
