package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dheerajsai15/LLM-Gateway/internal/provider"
)

// helloBody is streamed, because the adapter rejects other requests before
// the send.
const helloBody = `{"model":"claude-opus-5-5","stream":true,"messages":[{"role":"user","content":"hi"}]}`

// upstreamRequest is what the fake Anthropic server got.
type upstreamRequest struct {
	method, path string
	header       http.Header
	body         []byte
}

// fakeUpstream starts a server that records each request on the returned
// channel and answers with status, header and body.
func fakeUpstream(t *testing.T, status int, header http.Header, body string) (*httptest.Server, <-chan upstreamRequest) {
	t.Helper()
	got := make(chan upstreamRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- upstreamRequest{method: r.Method, path: r.URL.Path, header: r.Header.Clone(), body: b}
		for name, values := range header {
			w.Header()[name] = values
		}
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func TestChatCompletionSendsRequest(t *testing.T) {
	srv, got := fakeUpstream(t, http.StatusBadRequest, nil, "{}")
	p := New(srv.URL, "test-key", srv.Client())

	res, err := p.ChatCompletion(context.Background(), provider.Request{RawBody: []byte(helloBody)})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	res.Body.Close()

	up := <-got
	if up.method != http.MethodPost || up.path != "/v1/messages" {
		t.Errorf("request = %s %s, want POST /v1/messages", up.method, up.path)
	}
	wantHeaders := map[string]string{
		"X-Api-Key":         "test-key",
		"Anthropic-Version": apiVersion,
		"Content-Type":      "application/json",
		"Authorization":     "",
	}
	for name, want := range wantHeaders {
		if v := up.header.Get(name); v != want {
			t.Errorf("header %s = %q, want %q", name, v, want)
		}
	}
	want := `{"model":"claude-opus-5-5","max_tokens":4096,"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"stream":true}`
	if string(up.body) != want {
		t.Errorf("body:\ngot  %s\nwant %s", up.body, want)
	}
}

func TestChatCompletionErrorPassThrough(t *testing.T) {
	recorded, err := os.ReadFile("testdata/error_missing_max_tokens.json")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		status     int
		header     http.Header
		wantHeader http.Header
	}{
		{
			name:       "400 invalid request",
			status:     http.StatusBadRequest,
			header:     http.Header{"Content-Type": {"application/json"}, "Request-Id": {"req_1"}},
			wantHeader: http.Header{"Content-Type": {"application/json"}},
		},
		{
			name:   "429 rate limit",
			status: http.StatusTooManyRequests,
			header: http.Header{
				"Content-Type":                           {"application/json"},
				"Retry-After":                            {"7"},
				"Anthropic-Ratelimit-Requests-Remaining": {"0"},
			},
			wantHeader: http.Header{"Content-Type": {"application/json"}, "Retry-After": {"7"}},
		},
		{
			name:       "529 overloaded",
			status:     529,
			header:     http.Header{"Content-Type": {"application/json"}},
			wantHeader: http.Header{"Content-Type": {"application/json"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := fakeUpstream(t, tt.status, tt.header, string(recorded))
			p := New(srv.URL, "test-key", srv.Client())

			reads := 0
			res, err := p.ChatCompletion(context.Background(), provider.Request{
				RawBody:        []byte(helloBody),
				OnUpstreamRead: func() { reads++ },
			})
			if err != nil {
				t.Fatalf("ChatCompletion: %v", err)
			}
			defer res.Body.Close()

			if res.StatusCode != tt.status {
				t.Errorf("status = %d, want %d", res.StatusCode, tt.status)
			}
			for name := range tt.header {
				if got, want := res.Header.Get(name), tt.wantHeader.Get(name); got != want {
					t.Errorf("header %s = %q, want %q", name, got, want)
				}
			}
			body, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if string(body) != string(recorded) {
				t.Errorf("body:\ngot  %s\nwant %s", body, recorded)
			}
			if reads == 0 {
				t.Error("OnUpstreamRead was not called")
			}
		})
	}
}

func TestChatCompletionBadRequest(t *testing.T) {
	srv, got := fakeUpstream(t, http.StatusOK, nil, "")
	p := New(srv.URL, "test-key", srv.Client())

	body := `{"model":"claude-opus-5-5","messages":[{"role":"tool","content":"42"}]}`
	res, err := p.ChatCompletion(context.Background(), provider.Request{RawBody: []byte(body)})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", res.StatusCode)
	}
	var e struct {
		Error struct{ Message, Type string }
	}
	if err := json.NewDecoder(res.Body).Decode(&e); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if e.Error.Type != "invalid_request_error" || !strings.Contains(e.Error.Message, "unsupported role") {
		t.Errorf("error = %+v, want invalid_request_error with unsupported role", e.Error)
	}
	select {
	case <-got:
		t.Error("a request that cannot be translated must not go to Anthropic")
	default:
	}
}

func TestChatCompletionUpstreamDown(t *testing.T) {
	srv, _ := fakeUpstream(t, http.StatusOK, nil, "")
	url := srv.URL
	srv.Close()

	p := New(url, "test-key", http.DefaultClient)
	_, err := p.ChatCompletion(context.Background(), provider.Request{RawBody: []byte(helloBody)})
	if err == nil || !strings.Contains(err.Error(), "upstream request") {
		t.Errorf("err = %v, want an upstream request error", err)
	}
}

func TestChatCompletionMessage(t *testing.T) {
	recording, err := os.ReadFile("testdata/message_text.json")
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile("testdata/message_text.golden")
	if err != nil {
		t.Fatal(err)
	}
	// The golden file is indented. The adapter sends compact JSON.
	var want bytes.Buffer
	if err := json.Compact(&want, golden); err != nil {
		t.Fatal(err)
	}

	upstreamHeader := http.Header{"Content-Type": {"application/json"}, "Request-Id": {"req_1"}}
	srv, got := fakeUpstream(t, http.StatusOK, upstreamHeader, string(recording))
	p := New(srv.URL, "test-key", srv.Client())
	p.now = func() time.Time { return time.Unix(testCreated, 0) }
	body := `{"model":"claude-opus-5-5","messages":[{"role":"user","content":"hi"}]}`
	reads := 0

	res, err := p.ChatCompletion(context.Background(), provider.Request{
		RawBody:        []byte(body),
		OnUpstreamRead: func() { reads++ },
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	defer res.Body.Close()

	if up := <-got; strings.Contains(string(up.body), `"stream"`) {
		t.Errorf("upstream body = %s, want no stream field", up.body)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", res.StatusCode)
	}
	wantHeader := http.Header{"Content-Type": {"application/json"}}
	if !reflect.DeepEqual(res.Header, wantHeader) {
		t.Errorf("header = %v, want %v", res.Header, wantHeader)
	}
	out, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !bytes.Equal(out, want.Bytes()) {
		t.Errorf("body:\ngot  %s\nwant %s", out, want.Bytes())
	}
	if reads == 0 {
		t.Error("OnUpstreamRead was not called")
	}
}

func TestChatCompletionBadMessage(t *testing.T) {
	srv, _ := fakeUpstream(t, http.StatusOK, nil, "not json")
	p := New(srv.URL, "test-key", srv.Client())
	body := `{"model":"claude-opus-5-5","messages":[{"role":"user","content":"hi"}]}`

	res, err := p.ChatCompletion(context.Background(), provider.Request{RawBody: []byte(body)})
	if err == nil {
		res.Body.Close()
		t.Fatal("ChatCompletion() error = nil, want an error")
	}
	if !strings.Contains(err.Error(), "parse anthropic message") {
		t.Errorf("err = %v, want a parse error", err)
	}
}

func TestChatCompletionStream(t *testing.T) {
	recording, err := os.ReadFile("testdata/stream_text.sse")
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile("testdata/stream_text.golden")
	if err != nil {
		t.Fatal(err)
	}
	upstreamHeader := http.Header{"Content-Type": {"text/event-stream"}, "Request-Id": {"req_1"}}

	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "include_usage true",
			body: `{"model":"claude-opus-5-5","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"hi"}]}`,
			want: string(golden),
		},
		{
			name: "include_usage false",
			body: `{"model":"claude-opus-5-5","stream":true,"messages":[{"role":"user","content":"hi"}]}`,
			want: withoutUsageChunk(t, string(golden)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := fakeUpstream(t, http.StatusOK, upstreamHeader, string(recording))
			p := New(srv.URL, "test-key", srv.Client())
			p.now = func() time.Time { return time.Unix(testCreated, 0) }

			reads := 0
			res, err := p.ChatCompletion(context.Background(), provider.Request{
				RawBody:        []byte(tt.body),
				OnUpstreamRead: func() { reads++ },
			})
			if err != nil {
				t.Fatalf("ChatCompletion: %v", err)
			}
			defer res.Body.Close()

			if res.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want 200", res.StatusCode)
			}
			wantHeader := http.Header{"Content-Type": {"text/event-stream"}}
			if !reflect.DeepEqual(res.Header, wantHeader) {
				t.Errorf("header = %v, want %v", res.Header, wantHeader)
			}

			body, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if string(body) != tt.want {
				t.Errorf("body:\ngot:\n%s\nwant:\n%s", body, tt.want)
			}
			if reads == 0 {
				t.Error("OnUpstreamRead was not called")
			}
		})
	}
}

// withoutUsageChunk returns stream with the one event that has "usage"
// removed. The golden file has include_usage true, so this gives the output
// for include_usage false.
func withoutUsageChunk(t *testing.T, stream string) string {
	t.Helper()
	events := strings.SplitAfter(stream, "\n\n")
	var kept []string
	for _, ev := range events {
		if !strings.Contains(ev, `"usage"`) {
			kept = append(kept, ev)
		}
	}
	if len(kept) != len(events)-1 {
		t.Fatalf("want exactly one usage chunk, found %d", len(events)-len(kept))
	}
	return strings.Join(kept, "")
}

func TestChatCompletionStreamCutOff(t *testing.T) {
	srv, _ := fakeUpstream(t, http.StatusOK, nil, sseData(evStart, evText))
	p := New(srv.URL, "test-key", srv.Client())
	p.now = func() time.Time { return time.Unix(testCreated, 0) }

	body := `{"model":"claude-opus-5-5","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	res, err := p.ChatCompletion(context.Background(), provider.Request{RawBody: []byte(body)})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	defer res.Body.Close()

	// The client gets the chunks before the cut, and then the general error.
	// Then Read returns the error from translateStream, not io.EOF.
	out, err := io.ReadAll(res.Body)
	if want := sseData(chRole, chText, chFail); string(out) != want {
		t.Errorf("body:\ngot:\n%s\nwant:\n%s", out, want)
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("read error = %v, want one that wraps io.ErrUnexpectedEOF", err)
	}
}

func TestChatCompletionStreamCloseCancelsUpstream(t *testing.T) {
	// The fake Anthropic sends message_start and then nothing, like a long
	// pause. It reports when its request context ends, which happens when
	// the gateway closes the connection.
	stopped := make(chan struct{})
	testDone := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sseData(evStart))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
			close(stopped)
		case <-testDone:
			// The test failed. Return, so srv.Close does not wait forever.
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(testDone) }) // runs before srv.Close

	p := New(srv.URL, "test-key", srv.Client())
	body := `{"model":"claude-opus-5-5","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	// ctx is never canceled, so only Body.Close can stop the upstream request.
	res, err := p.ChatCompletion(context.Background(), provider.Request{RawBody: []byte(body)})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}

	// Read the role chunk. The goroutine then waits in the upstream Read.
	buf := make([]byte, 4096)
	n, err := res.Body.Read(buf)
	if err != nil || !strings.Contains(string(buf[:n]), `"role":"assistant"`) {
		t.Fatalf("first read = %q, %v; want the role chunk", buf[:n], err)
	}

	res.Body.Close()

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("the upstream request was not canceled after Body.Close")
	}
}

// roundTripFunc lets a test answer HTTP requests with no network.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// signalBody is a response body that closes the closed channel on Close.
type signalBody struct {
	io.Reader
	closed chan struct{}
	once   sync.Once
}

func (b *signalBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

func TestChatCompletionStreamCancelStopsWaitingWrite(t *testing.T) {
	recording, err := os.ReadFile("testdata/stream_text.sse")
	if err != nil {
		t.Fatal(err)
	}
	// The fake transport ignores the context, so a cancel cannot stop the
	// upstream Read. Only the closed pipe can stop the goroutine here.
	upstream := &signalBody{Reader: strings.NewReader(string(recording)), closed: make(chan struct{})}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: upstream}, nil
	})}
	p := New("http://anthropic.test", "test-key", client)

	ctx, cancel := context.WithCancel(context.Background())
	body := `{"model":"claude-opus-5-5","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	res, err := p.ChatCompletion(ctx, provider.Request{RawBody: []byte(body)})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	defer res.Body.Close()

	// Nobody reads, so the goroutine waits in pw.Write with the role chunk.
	cancel()

	// The goroutine ends and closes the upstream body, with no read and no
	// Body.Close from the caller.
	select {
	case <-upstream.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("the goroutine did not end after ctx was canceled")
	}

	// The reader gets the cause of the cancel.
	if _, err := res.Body.Read(make([]byte, 4096)); !errors.Is(err, context.Canceled) {
		t.Errorf("read error = %v, want context.Canceled", err)
	}
}
