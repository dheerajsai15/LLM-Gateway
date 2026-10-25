package sse

import (
	"bufio"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

// readAll reads events from input until io.EOF.
func readAll(t *testing.T, input string) []Event {
	t.Helper()
	r := NewReader(strings.NewReader(input))
	var events []Event
	for {
		event, err := r.Next()
		if errors.Is(err, io.EOF) {
			return events
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		events = append(events, event)
	}
}

func TestReader(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []Event
	}{
		{
			name:  "one event",
			input: "event: ping\ndata: {}\n\n",
			want:  []Event{{Event: "ping", Data: []byte("{}")}},
		},
		{
			name:  "two events",
			input: "event: a\ndata: 1\n\nevent: b\ndata: 2\n\n",
			want:  []Event{{Event: "a", Data: []byte("1")}, {Event: "b", Data: []byte("2")}},
		},
		{
			name:  "two data lines",
			input: "data: a\ndata: b\n\n",
			want:  []Event{{Data: []byte("a\nb")}},
		},
		{
			name:  "comment skipped",
			input: ": keep-alive\ndata: x\n\n",
			want:  []Event{{Data: []byte("x")}},
		},
		{
			name:  "no space after colon",
			input: "data:x\n\n",
			want:  []Event{{Data: []byte("x")}},
		},
		{
			name:  "only one space removed",
			input: "data:  x\n\n",
			want:  []Event{{Data: []byte(" x")}},
		},
		{
			name:  "CRLF line endings",
			input: "data: x\r\n\r\n",
			want:  []Event{{Data: []byte("x")}},
		},
		{
			name:  "unknown field ignored",
			input: "id: 7\nretry: 100\ndata: x\n\n",
			want:  []Event{{Data: []byte("x")}},
		},
		{
			name:  "no data, skipped",
			input: "event: ping\n\n",
			want:  nil,
		},
		{
			name:  "event type does not leak into the next event",
			input: "event: ping\n\ndata: x\n\n",
			want:  []Event{{Data: []byte("x")}},
		},
		{
			name:  "cut off at the end",
			input: "data: x\n\ndata: y\n",
			want:  []Event{{Data: []byte("x")}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := readAll(t, tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

// TestReaderLineLimit checks both sides of the limit: a line of maxLine-1
// bytes is read, and a line of maxLine bytes fails.
func TestReaderLineLimit(t *testing.T) {
	prefix := "data: "

	longest := prefix + strings.Repeat("x", maxLine-1-len(prefix))
	if _, err := NewReader(strings.NewReader(longest + "\n\n")).Next(); err != nil {
		t.Fatalf("line of %d bytes: unexpected error: %v", len(longest), err)
	}

	tooLong := longest + "x"
	_, err := NewReader(strings.NewReader(tooLong + "\n\n")).Next()
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("line of %d bytes: error = %v, want bufio.ErrTooLong", len(tooLong), err)
	}
}
