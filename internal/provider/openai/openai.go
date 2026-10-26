package openai

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	"github.com/dheerajsai15/LLM-Gateway/internal/provider"
)

// Provider sends chat completion requests to OpenAI, or to any server with
// the same API, such as the mock.
type Provider struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

var _ provider.Provider = (*Provider)(nil)

// New returns a Provider that sends requests to baseURL with apiKey, using client.
func New(baseURL string, apiKey string, client *http.Client) *Provider {
	return &Provider{baseURL: baseURL, apiKey: apiKey, client: client}
}

func (p *Provider) newRequest(ctx context.Context, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create openai request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	return req, nil
}

// ChatCompletion sends req to the OpenAI API and returns the upstream response
// unchanged: same status, headers and body. The body calls req.OnUpstreamRead
// on each read with bytes. The caller must close the body.
func (p *Provider) ChatCompletion(ctx context.Context, req provider.Request) (*provider.Response, error) {
	upReq, err := p.newRequest(ctx, req.RawBody)
	if err != nil {
		return nil, err
	}

	res, err := p.client.Do(upReq)
	if err != nil {
		return nil, fmt.Errorf("upstream request: %w", err)
	}

	return &provider.Response{
		StatusCode: res.StatusCode,
		Header:     res.Header,
		Body:       provider.WithReadActivity(res.Body, req.OnUpstreamRead),
	}, nil
}
