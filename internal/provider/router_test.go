package provider

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// fakeProvider records the last request and returns res and err.
type fakeProvider struct {
	ctx context.Context
	req Request
	res *Response
	err error
}

func (f *fakeProvider) ChatCompletion(ctx context.Context, req Request) (*Response, error) {
	f.ctx = ctx
	f.req = req
	return f.res, f.err
}

func TestRouterPick(t *testing.T) {
	openai := &fakeProvider{}
	claude := &fakeProvider{}
	haiku := &fakeProvider{}
	r := NewRouter(openai,
		Route{Prefix: "claude-haiku-", Provider: haiku},
		Route{Prefix: "claude-", Provider: claude},
	)

	tests := []struct {
		model string
		want  Provider
	}{
		{model: "claude-sonnet-5-5", want: claude},
		{model: "claude-haiku-5-5", want: haiku},
		{model: "gpt-5", want: openai},
		{model: "mock-stall", want: openai},
		{model: "", want: openai},
		{model: "claude", want: openai},
		{model: "Claude-sonnet-5-5", want: openai},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			// Each want is a pointer, so == checks that pick returned the
			// same fake, not only an equal one.
			if got := r.pick(tt.model); got != tt.want {
				t.Errorf("pick(%q) returned the wrong provider", tt.model)
			}
		})
	}
}

func TestRouterFirstMatchWins(t *testing.T) {
	claude := &fakeProvider{}
	haiku := &fakeProvider{}
	// The route for "claude-" comes first, so it also takes the haiku models.
	r := NewRouter(&fakeProvider{},
		Route{Prefix: "claude-", Provider: claude},
		Route{Prefix: "claude-haiku-", Provider: haiku},
	)

	if got := r.pick("claude-haiku-5-5"); got != claude {
		t.Error(`pick("claude-haiku-5-5") did not return the first matching route`)
	}
}

func TestRouterChatCompletion(t *testing.T) {
	type ctxKey struct{}
	wantRes := &Response{StatusCode: 200}
	wantErr := errors.New("upstream failed")
	claude := &fakeProvider{res: wantRes, err: wantErr}
	r := NewRouter(&fakeProvider{}, Route{Prefix: "claude-", Provider: claude})

	ctx := context.WithValue(context.Background(), ctxKey{}, "marker")
	reads := 0
	req := Request{
		Model:          "claude-sonnet-5-5",
		Stream:         true,
		RawBody:        []byte(`{"model":"claude-sonnet-5-5"}`),
		OnUpstreamRead: func() { reads++ },
	}

	res, err := r.ChatCompletion(ctx, req)

	if res != wantRes || err != wantErr {
		t.Errorf("ChatCompletion() = %v, %v; want the provider's response and error", res, err)
	}
	if claude.ctx != ctx {
		t.Error("the provider did not get the caller's context")
	}
	// Request holds a func, so it cannot be compared with ==. Check each field.
	if claude.req.Model != req.Model || claude.req.Stream != req.Stream || !bytes.Equal(claude.req.RawBody, req.RawBody) {
		t.Errorf("the provider got %+v, want %+v", claude.req, req)
	}
	if claude.req.OnUpstreamRead == nil {
		t.Fatal("the provider got no OnUpstreamRead callback")
	}
	claude.req.OnUpstreamRead()
	if reads != 1 {
		t.Errorf("calling the provider's callback ran the caller's callback %d times, want 1", reads)
	}
}
