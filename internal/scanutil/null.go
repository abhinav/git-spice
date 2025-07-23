// Package scanutil offers tools to use with a bufio.Scanner.
package scanutil

import "bytes"

// SplitNull is a [bufio.SplitFunc] that splits input on null bytes (0x00).
func SplitNull(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	if i := bytes.IndexByte(data, 0); i >= 0 {
		// Have a null-byte separated section.
		return i + 1, data[:i], nil
	}

	// No null-byte found, but end of input,
	// so consume the rest as one section.
	if atEOF {
		return len(data), data, nil
	}

	// Request more data.
	return 0, nil, nil
}
