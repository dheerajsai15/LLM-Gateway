package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/dheerajsai15/LLM-Gateway/internal/openai"
	"github.com/dheerajsai15/LLM-Gateway/internal/provider"
)

const (
	// apiVersion is the Anthropic API version that the translation is written
	// for. Anthropic needs it in each request.
	apiVersion = "2026-10-21"
	// defaultMaxTokens is used when the client sets no token limit, because
	// Anthropic needs max_tokens in each request.
	defaultMaxTokens = 4096
)

// passHeaders are the upstream headers that go to the client with an error
// status. Other headers, such as anthropic-ratelimit-*, are dropped.
var passHeaders = []string{"Content-Type", "Retry-After"}

// Provider sends chat completion requests to the Anthropic Messages API and
// returns the responses in OpenAI format.
type Provider struct {
	baseURL string
	apiKey  string
	client  *http.Client
	// now gives the created time of the OpenAI chunks. Tests replace it with
	// a fixed time, so the output can be compared with a golden file.
	now func() time.Time
}

var _ provider.Provider = (*Provider)(nil)

// New returns a Provider that sends requests to baseURL with apiKey, using client.
func New(baseURL string, apiKey string, client *http.Client) *Provider {
	return &Provider{baseURL: baseURL, apiKey: apiKey, client: client, now: time.Now}
}

func (p *Provider) newRequest(ctx context.Context, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create anthropic request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", p.apiKey)
	req.Header.Set("Anthropic-Version", apiVersion)
	return req, nil
}

// ChatCompletion translates req to an Anthropic request and sends it. A
// request that cannot be translated gets a 400 in OpenAI format, and nothing
// goes to Anthropic. An error status from Anthropic passes through with its
// body unchanged. A stream is translated to OpenAI chunks while the caller
// reads the body. The caller must close the body. Closing the body also
// cancels the upstream request, so the caller does not need to cancel ctx.
func (p *Provider) ChatCompletion(ctx context.Context, req provider.Request) (*provider.Response, error) {
	anthReq, includeUsage, err := translateRequest(req.RawBody, defaultMaxTokens)
	if err != nil {
		// The error describes the client's own request, so it is safe to show.
		return badRequest(err.Error())
	}

	if !anthReq.Stream {
		// The translation of responses that are not streamed is not written
		// yet. The check comes before the send, so Anthropic does not
		// generate an answer that the gateway then drops.
		return nil, errors.New("anthropic: responses that are not streamed are not supported yet")
	}

	body, err := json.Marshal(anthReq)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	// upCtx belongs to this upstream request. Every path below must call
	// upCancel: at once on an error, or later when the caller closes the body.
	upCtx, upCancel := context.WithCancel(ctx)

	upReq, err := p.newRequest(upCtx, body)
	if err != nil {
		upCancel()
		return nil, err
	}

	res, err := p.client.Do(upReq)
	if err != nil {
		upCancel()
		return nil, fmt.Errorf("upstream request: %w", err)
	}

	if res.StatusCode != http.StatusOK {
		header := make(http.Header)
		for _, name := range passHeaders {
			if v := res.Header.Get(name); v != "" {
				header.Set(name, v)
			}
		}
		return &provider.Response{
			StatusCode: res.StatusCode,
			Header:     header,
			Body:       cancelOnClose{provider.WithReadActivity(res.Body, req.OnUpstreamRead), upCancel},
		}, nil
	}

	// The goroutine translates the Anthropic events and writes the OpenAI
	// chunks into the pipe. The caller reads them from the other end. Each
	// pipe Write waits until the caller reads, so no chunks pile up in memory.
	pr, pw := io.Pipe()
	upstream := provider.WithReadActivity(res.Body, req.OnUpstreamRead)
	created := p.now().Unix()
	// When upCtx ends, close the writer end with the cause. A pipe Write that
	// waits for a reader then returns at once, so the goroutine can end even
	// when the caller neither reads nor closes the body. The reader gets the
	// cause. If the goroutine closed the pipe first, this does nothing.
	stop := context.AfterFunc(upCtx, func() {
		pw.CloseWithError(context.Cause(upCtx))
	})
	go func() {
		defer stop()
		defer upstream.Close()
		err := translateStream(upstream, pw, created, includeUsage)
		// A nil error gives the reader io.EOF. Any other error is returned by
		// the reader's next Read, so a failed stream does not look complete.
		pw.CloseWithError(err)
	}()

	// The Anthropic headers are not copied. The body is new, so their
	// Content-Length is wrong, and the anthropic-* headers are dropped.
	return &provider.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       cancelOnClose{pr, upCancel},
	}, nil
}

// cancelOnClose is a body that also cancels the upstream request when it is
// closed. For a stream, closing the pipe stops a waiting pipe Write, and the
// cancel stops a waiting upstream Read. The goroutine needs both.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b cancelOnClose) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

// badRequest returns a 400 response with an OpenAI error body.
func badRequest(message string) (*provider.Response, error) {
	body, err := json.Marshal(openai.ErrorResponse{
		Error: openai.ErrorDetail{Message: message, Type: "invalid_request_error"},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal error: %w", err)
	}
	return &provider.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
	}, nil
}
