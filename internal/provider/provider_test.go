package provider

import (
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

// trackingBody is a body that records whether Close was called.
type trackingBody struct {
	io.Reader
	closed bool
}

func (b *trackingBody) Close() error {
	b.closed = true
	return nil
}

func TestWithReadActivity(t *testing.T) {
	errBoom := errors.New("boom")

	tests := []struct {
		name      string
		r         io.Reader
		bufSize   int
		wantData  string
		wantCalls int
		wantErr   error
	}{
		{name: "one byte per read", r: strings.NewReader("abc"), bufSize: 1, wantData: "abc", wantCalls: 3, wantErr: io.EOF},
		{name: "all bytes in one read", r: strings.NewReader("abc"), bufSize: 32, wantData: "abc", wantCalls: 1, wantErr: io.EOF},
		{name: "bytes and EOF in one read", r: iotest.DataErrReader(strings.NewReader("abc")), bufSize: 32, wantData: "abc", wantCalls: 1, wantErr: io.EOF},
		{name: "empty body", r: strings.NewReader(""), bufSize: 32, wantCalls: 0, wantErr: io.EOF},
		{name: "error with no bytes", r: iotest.ErrReader(errBoom), bufSize: 32, wantCalls: 0, wantErr: errBoom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			body := WithReadActivity(&trackingBody{Reader: tt.r}, func() { calls++ })

			// Read until the first error, as the handler does.
			var data []byte
			buf := make([]byte, tt.bufSize)
			var err error
			for err == nil {
				var n int
				n, err = body.Read(buf)
				data = append(data, buf[:n]...)
			}

			if string(data) != tt.wantData {
				t.Errorf("data = %q, want %q", data, tt.wantData)
			}
			if calls != tt.wantCalls {
				t.Errorf("onRead calls = %d, want %d", calls, tt.wantCalls)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestWithReadActivityClose(t *testing.T) {
	inner := &trackingBody{Reader: strings.NewReader("")}
	body := WithReadActivity(inner, func() {})

	if err := body.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !inner.closed {
		t.Error("Close did not reach the inner body")
	}
}

func TestWithReadActivityNilCallback(t *testing.T) {
	inner := &trackingBody{Reader: strings.NewReader("")}
	if got := WithReadActivity(inner, nil); got != io.ReadCloser(inner) {
		t.Error("with a nil callback, the body must be returned unchanged")
	}
}
