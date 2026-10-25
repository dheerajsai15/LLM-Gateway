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
