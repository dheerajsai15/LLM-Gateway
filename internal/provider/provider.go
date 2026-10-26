package provider

import (
	"context"
	"io"
	"net/http"
)

// Request is what every provider gets. RawBody is the client's original JSON,
// so the OpenAI adapter can forward it unchanged.
type Request struct {
	Model   string
	Stream  bool
	RawBody []byte
	// OnUpstreamRead is called each time the adapter reads bytes from the
	// upstream. The handler uses it to reset its idle timer, so every adapter
	// must call it, for example with WithReadActivity. It can be nil.
	OnUpstreamRead func()
}

// Response is always in OpenAI format, whatever the upstream sent.
type Response struct {
	StatusCode int
	Header     http.Header
	Body       io.ReadCloser
}

// Provider sends a chat completion request to one upstream, such as OpenAI or
// Anthropic. It always returns the response in OpenAI format, so the handler
// can stream it to the client without knowing which upstream answered.
// When ctx is canceled, the upstream request stops.
type Provider interface {
	ChatCompletion(ctx context.Context, req Request) (*Response, error)
}

// WithReadActivity returns body with one change: onRead is called each time
// Read returns bytes. Close closes body. If onRead is nil, body is returned
// unchanged.
func WithReadActivity(body io.ReadCloser, onRead func()) io.ReadCloser {
	if onRead == nil {
		return body
	}
	return &activityReader{ReadCloser: body, onRead: onRead}
}

// activityReader embeds body, so Close goes straight to body. It does not
// read on its own. It reads only when its caller calls Read.
type activityReader struct {
	io.ReadCloser
	onRead func()
}

func (a *activityReader) Read(p []byte) (int, error) {
	n, err := a.ReadCloser.Read(p)
	if n > 0 {
		a.onRead()
	}
	return n, err
}
