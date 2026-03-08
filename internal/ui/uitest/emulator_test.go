package uitest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmulatorView_Write_mapsBareNewlineToCarriageReturn(t *testing.T) {
	view := NewEmulatorView(nil)

	_, err := view.Write([]byte("Select a branch:\n\x1b[10Drow"))
	require.NoError(t, err)

	assert.Equal(t, []string{
		"Select a branch:",
		"      row",
	}, view.Rows())
}

func TestRenderedRow_zeroRunesBecomeSpaces(t *testing.T) {
	row := []rune{0, 'a', 0}

	assert.Equal(t, []rune{' ', 'a'}, trimRightWS(row))
	assert.Equal(t, []rune{0, 'a', 0}, row)
}

func TestEmulatorView_Write_tabMovesCursorWithoutErasing(t *testing.T) {
	view := NewEmulatorView(nil)

	_, err := view.Write([]byte("      ┏━■ qux ◀"))
	require.NoError(t, err)

	_, err = view.Write([]byte("\r\t□ qux\x1b[K"))
	require.NoError(t, err)

	assert.Equal(t, []string{"      ┏━□ qux"}, view.Rows())
}

func TestEmulatorView_Write_preservesCRLFAcrossWrites(t *testing.T) {
	view := NewEmulatorView(nil)

	_, err := view.Write([]byte("hello\r"))
	require.NoError(t, err)

	_, err = view.Write([]byte("\nworld"))
	require.NoError(t, err)

	assert.Equal(t, []string{
		"hello",
		"world",
	}, view.Rows())
}
