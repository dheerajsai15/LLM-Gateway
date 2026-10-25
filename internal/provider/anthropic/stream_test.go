package anthropic

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dheerajsai15/LLM-Gateway/internal/openai"
)

// update writes the current output into the golden files. Review the files
// with git diff before you commit them, because update saves bugs too.
var update = flag.Bool("update", false, "write the golden files")

// testCreated is the fixed Unix time for golden output, so each run gives
// the same bytes.
const testCreated = 1700000000

// TestTranslateStreamGolden runs translateStream on each recorded stream in
// testdata and compares the output with testdata/<name>.golden.
func TestTranslateStreamGolden(t *testing.T) {
	inputs, err := filepath.Glob("testdata/*.sse")
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) == 0 {
		t.Fatal("no testdata/*.sse files")
	}

	for _, input := range inputs {
		name := strings.TrimSuffix(filepath.Base(input), ".sse")
		t.Run(name, func(t *testing.T) {
			in, err := os.ReadFile(input)
			if err != nil {
				t.Fatal(err)
			}

			var out bytes.Buffer
			if err := translateStream(bytes.NewReader(in), &out, testCreated, true); err != nil {
				t.Fatalf("translateStream: %v", err)
			}

			golden := filepath.Join("testdata", name+".golden")
			if *update {
				if err := os.WriteFile(golden, out.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run the test with -update to create it)", err)
			}
			if !bytes.Equal(out.Bytes(), want) {
				t.Errorf("output does not match %s\ngot:\n%s\nwant:\n%s", golden, out.Bytes(), want)
			}
		})
	}
}

// Anthropic events for the table tests. translateStream reads only the data
// lines, so the inputs have no event lines.
const (
	evStart   = `{"type":"message_start","message":{"id":"msg_1","model":"m","usage":{"input_tokens":10,"output_tokens":1}}}`
	evText    = `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`
	evDelta   = `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`
	evStop    = `{"type":"message_stop"}`
	evPing    = `{"type":"ping"}`
	evFuture  = `{"type":"future_event","text":"lost"}`
	evError   = `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`
	evBadJSON = `{"type":`
)

// OpenAI chunks that translateStream writes for the events above.
const (
	chRole   = `{"id":"msg_1","object":"chat.completion.chunk","created":1700000000,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`
	chText   = `{"id":"msg_1","object":"chat.completion.chunk","created":1700000000,"model":"m","choices":[{"index":0,"delta":{"content":"Hi"},"finish_reason":null}]}`
	chFinish = `{"id":"msg_1","object":"chat.completion.chunk","created":1700000000,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`
	chUsage  = `{"id":"msg_1","object":"chat.completion.chunk","created":1700000000,"model":"m","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}}`
	chDone   = `[DONE]`
	// chFail is the general error for a failed stream. It must not contain
	// internal details.
	chFail  = `{"error":{"message":"upstream stream failed","type":"server_error","param":null,"code":null}}`
	chError = `{"error":{"message":"Overloaded","type":"overloaded_error","param":null,"code":null}}`
)

// sseData returns one SSE event for each data string.
func sseData(data ...string) string {
	var b strings.Builder
	for _, d := range data {
		b.WriteString("data: ")
		b.WriteString(d)
		b.WriteString("\n\n")
	}
	return b.String()
}

func TestTranslateStream(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		includeUsage bool
		want         string
		wantErr      string // a part of the error text; "" means no error
		wantErrIs    error  // when not nil, errors.Is must be true
	}{
		{
			name:         "usage on",
			input:        sseData(evStart, evText, evDelta, evStop),
			includeUsage: true,
			want:         sseData(chRole, chText, chFinish, chUsage, chDone),
		},
		{
			name:  "usage off",
			input: sseData(evStart, evText, evDelta, evStop),
			want:  sseData(chRole, chText, chFinish, chDone),
		},
		{
			name:  "ping and unknown events skipped",
			input: sseData(evStart, evPing, evFuture, evText, evDelta, evStop),
			want:  sseData(chRole, chText, chFinish, chDone),
		},
		{
			name:    "error event",
			input:   sseData(evStart, evError),
			want:    sseData(chRole, chError),
			wantErr: "overloaded_error",
		},
		{
			name:      "cut off after text",
			input:     sseData(evStart, evText),
			want:      sseData(chRole, chText, chFail),
			wantErr:   "before message_stop",
			wantErrIs: io.ErrUnexpectedEOF,
		},
		{
			name:      "empty stream",
			input:     "",
			want:      sseData(chFail),
			wantErr:   "before message_stop",
			wantErrIs: io.ErrUnexpectedEOF,
		},
		{
			name:    "bad JSON",
			input:   sseData(evStart, evBadJSON),
			want:    sseData(chRole, chFail),
			wantErr: "parse stream event",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			err := translateStream(strings.NewReader(tt.input), &out, testCreated, tt.includeUsage)

			if got := out.String(); got != tt.want {
				t.Errorf("output:\ngot:\n%s\nwant:\n%s", got, tt.want)
			}

			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("unexpected error: %v", err)
			case tt.wantErr != "" && err == nil:
				t.Errorf("got no error, want an error with %q", tt.wantErr)
			case tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr):
				t.Errorf("error %q does not contain %q", err, tt.wantErr)
			}
			if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
				t.Errorf("errors.Is(%v, %v) is false", err, tt.wantErrIs)
			}
		})
	}
}

func TestOpenAIUsage(t *testing.T) {
	tests := []struct {
		name                            string
		input, cacheCreation, cacheRead int
		output, thinking                int
		want                            openai.Usage
	}{
		{
			name: "zero",
		},
		{
			name:  "no cache, with thinking",
			input: 30, output: 67, thinking: 32,
			want: openai.Usage{
				PromptTokens: 30, CompletionTokens: 67, TotalTokens: 97,
				CompletionTokensDetails: openai.CompletionTokensDetails{ReasoningTokens: 32},
			},
		},
		{
			name:  "cache read",
			input: 5, cacheRead: 100, output: 10,
			want: openai.Usage{
				PromptTokens: 105, CompletionTokens: 10, TotalTokens: 115,
				PromptTokensDetails: openai.PromptTokensDetails{CachedTokens: 100},
			},
		},
		{
			// Tokens written to the cache count as prompt tokens, but they
			// were not read from the cache, so they are not cached tokens.
			name:  "cache creation",
			input: 5, cacheCreation: 200, output: 10,
			want: openai.Usage{
				PromptTokens: 205, CompletionTokens: 10, TotalTokens: 215,
			},
		},
		{
			name:  "all counts",
			input: 5, cacheCreation: 200, cacheRead: 100, output: 10, thinking: 4,
			want: openai.Usage{
				PromptTokens: 305, CompletionTokens: 10, TotalTokens: 315,
				PromptTokensDetails:     openai.PromptTokensDetails{CachedTokens: 100},
				CompletionTokensDetails: openai.CompletionTokensDetails{ReasoningTokens: 4},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := usage{
				InputTokens:              tt.input,
				CacheCreationInputTokens: tt.cacheCreation,
				CacheReadInputTokens:     tt.cacheRead,
				OutputTokens:             tt.output,
			}
			u.OutputTokensDetails.ThinkingTokens = tt.thinking

			if got := openaiUsage(u); *got != tt.want {
				t.Errorf("got %+v, want %+v", *got, tt.want)
			}
		})
	}
}

func TestMapStopReason(t *testing.T) {
	tests := []struct {
		reason string
		want   string
	}{
		{"end_turn", "stop"},
		{"stop_sequence", "stop"},
		{"pause_turn", "stop"},
		{"max_tokens", "length"},
		{"model_context_window_exceeded", "length"},
		{"tool_use", "tool_calls"},
		{"refusal", "content_filter"},
		{"future_reason", "stop"},
		{"", "stop"},
	}

	for _, tt := range tests {
		if got := mapStopReason(tt.reason); got != tt.want {
			t.Errorf("mapStopReason(%q) = %q, want %q", tt.reason, got, tt.want)
		}
	}
}
