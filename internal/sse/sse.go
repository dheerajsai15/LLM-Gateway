// Package sse reads server-sent events, the stream format that OpenAI and
// Anthropic both use for streaming responses.
package sse

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
)

// maxLine is the scanner buffer size. The buffer must also hold the newline, so
// the longest line the reader accepts is maxLine-1 bytes. A thinking signature
// is one long line, so the 64 KB default of bufio.Scanner is too small.
const maxLine = 1024 * 1024

// Event is one server-sent event.
type Event struct {
	Event string
	Data  []byte
}

// Reader reads server-sent events from a stream, one at a time.
type Reader struct {
	scanner *bufio.Scanner
}

// NewReader returns a Reader that reads from r.
func NewReader(r io.Reader) *Reader {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLine)
	return &Reader{scanner: scanner}
}

// Next returns the next event. It skips comments and events with no data. It
// returns io.EOF when the stream ends, and drops an event that has no blank
// line after it, because the stream was cut in the middle of that event.
func (r *Reader) Next() (Event, error) {
	var event Event
	hasData := false

	for r.scanner.Scan() {
		line := r.scanner.Bytes()

		// A blank line ends the event.
		if len(line) == 0 {
			if hasData {
				return event, nil
			}
			event = Event{}
			continue
		}

		// A line that starts with a colon is a comment.
		if line[0] == ':' {
			continue
		}

		field, value, _ := bytes.Cut(line, []byte(":"))
		value = bytes.TrimPrefix(value, []byte(" "))

		// The scanner reuses the memory of line, so copy the value out of it.
		switch string(field) {
		case "event":
			event.Event = string(value)
		case "data":
			if hasData {
				event.Data = append(event.Data, '\n')
			}
			event.Data = append(event.Data, value...)
			hasData = true
		}
	}

	if err := r.scanner.Err(); err != nil {
		return Event{}, fmt.Errorf("read sse: %w", err)
	}
	return Event{}, io.EOF
}
