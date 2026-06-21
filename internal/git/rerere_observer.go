package git

import "bytes"

// rerereReplayObserver scans a git stderr byte stream for the rerere
// replay line — git prints "Resolved 'PATH' using previous
// resolution." when rerere.autoupdate is false, or "Staged 'PATH'
// using previous resolution." when autoupdate is true (the merge
// is auto-staged after replay). gs always sets autoupdate=true when
// rerere is enabled, so "Staged" is the dominant form, but we match
// both for robustness across configurations.
//
// It is intended for use with [gitCmd.TeeStderr]. Each matching line
// invokes cb with the captured PATH. Non-matching output is ignored.
// The observer accumulates bytes across Write calls so it correctly
// handles arbitrary chunking.
type rerereReplayObserver struct {
	cb  func(path string)
	buf []byte
}

var (
	_rerereReplayPrefixResolved = []byte("Resolved '")
	_rerereReplayPrefixStaged   = []byte("Staged '")
	_rerereReplaySuffix         = []byte("' using previous resolution.")
)

func (o *rerereReplayObserver) Write(p []byte) (int, error) {
	o.buf = append(o.buf, p...)
	for {
		idx := bytes.IndexByte(o.buf, '\n')
		if idx < 0 {
			break
		}
		line := o.buf[:idx]
		o.buf = o.buf[idx+1:]
		if path, ok := parseRerereReplay(line); ok {
			o.cb(path)
		}
	}
	return len(p), nil
}

func parseRerereReplay(line []byte) (string, bool) {
	var rest []byte
	switch {
	case bytes.HasPrefix(line, _rerereReplayPrefixResolved):
		rest = line[len(_rerereReplayPrefixResolved):]
	case bytes.HasPrefix(line, _rerereReplayPrefixStaged):
		rest = line[len(_rerereReplayPrefixStaged):]
	default:
		return "", false
	}
	if !bytes.HasSuffix(rest, _rerereReplaySuffix) {
		return "", false
	}
	return string(rest[:len(rest)-len(_rerereReplaySuffix)]), true
}
