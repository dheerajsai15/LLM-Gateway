package anthropic

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const testModel = "claude-opus-5-5"

// userHi is the Anthropic form of {"role":"user","content":"hi"}.
var userHi = anthropicMessage{Role: "user", Content: textBlocks("hi")}

// textBlocks returns one text block for each string.
func textBlocks(texts ...string) []anthropicBlock {
	blocks := make([]anthropicBlock, 0, len(texts))
	for _, text := range texts {
		blocks = append(blocks, anthropicBlock{Type: "text", Text: text})
	}
	return blocks
}

func TestTranslateRequest(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		want      anthropicRequest
		wantUsage bool
		wantErr   string
	}{
		{
			name: "default max tokens",
			body: `{"model":"claude-opus-5-5","messages":[{"role":"user","content":"hi"}]}`,
			want: anthropicRequest{Model: testModel, MaxTokens: 1024, Messages: []anthropicMessage{userHi}},
		},
		{
			name: "client max_tokens",
			body: `{"model":"claude-opus-5-5","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}`,
			want: anthropicRequest{Model: testModel, MaxTokens: 50, Messages: []anthropicMessage{userHi}},
		},
		{
			name: "max_completion_tokens wins",
			body: `{"model":"claude-opus-5-5","max_tokens":50,"max_completion_tokens":80,"messages":[{"role":"user","content":"hi"}]}`,
			want: anthropicRequest{Model: testModel, MaxTokens: 80, Messages: []anthropicMessage{userHi}},
		},
		{
			name: "explicit zero is kept",
			body: `{"model":"claude-opus-5-5","max_tokens":0,"messages":[{"role":"user","content":"hi"}]}`,
			want: anthropicRequest{Model: testModel, MaxTokens: 0, Messages: []anthropicMessage{userHi}},
		},
		{
			name: "system and developer joined",
			body: `{"model":"claude-opus-5-5","messages":[
				{"role":"system","content":"Be brief."},
				{"role":"developer","content":"Use English."},
				{"role":"user","content":"hi"}]}`,
			want: anthropicRequest{
				Model:     testModel,
				MaxTokens: 1024,
				System:    "Be brief.\n\nUse English.",
				Messages:  []anthropicMessage{userHi},
			},
		},
		{
			// Anthropic has only a top-level system field, so a system message
			// loses its position in the conversation.
			name: "system in the middle moves to top",
			body: `{"model":"claude-opus-5-5","messages":[
				{"role":"user","content":"hi"},
				{"role":"assistant","content":"Hello."},
				{"role":"system","content":"Answer in French."},
				{"role":"user","content":"And Berlin?"}]}`,
			want: anthropicRequest{
				Model:     testModel,
				MaxTokens: 1024,
				System:    "Answer in French.",
				Messages: []anthropicMessage{
					userHi,
					{Role: "assistant", Content: textBlocks("Hello.")},
					{Role: "user", Content: textBlocks("And Berlin?")},
				},
			},
		},
		{
			name: "stop as string",
			body: `{"model":"claude-opus-5-5","stop":"END","messages":[{"role":"user","content":"hi"}]}`,
			want: anthropicRequest{Model: testModel, MaxTokens: 1024, Messages: []anthropicMessage{userHi}, StopSequences: []string{"END"}},
		},
		{
			name: "stop as array",
			body: `{"model":"claude-opus-5-5","stop":["A","B"],"messages":[{"role":"user","content":"hi"}]}`,
			want: anthropicRequest{Model: testModel, MaxTokens: 1024, Messages: []anthropicMessage{userHi}, StopSequences: []string{"A", "B"}},
		},
		{
			name: "unknown fields dropped",
			body: `{"model":"claude-opus-5-5","temperature":0.7,"top_p":0.9,"n":1,"messages":[{"role":"user","content":"hi"}]}`,
			want: anthropicRequest{Model: testModel, MaxTokens: 1024, Messages: []anthropicMessage{userHi}},
		},
		{
			name: "stream copied",
			body: `{"model":"claude-opus-5-5","stream":true,"messages":[{"role":"user","content":"hi"}]}`,
			want: anthropicRequest{Model: testModel, MaxTokens: 1024, Messages: []anthropicMessage{userHi}, Stream: true},
		},
		{
			name:      "include_usage true",
			body:      `{"model":"claude-opus-5-5","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"hi"}]}`,
			want:      anthropicRequest{Model: testModel, MaxTokens: 1024, Messages: []anthropicMessage{userHi}, Stream: true},
			wantUsage: true,
		},
		{
			name: "include_usage false",
			body: `{"model":"claude-opus-5-5","stream":true,"stream_options":{"include_usage":false},"messages":[{"role":"user","content":"hi"}]}`,
			want: anthropicRequest{Model: testModel, MaxTokens: 1024, Messages: []anthropicMessage{userHi}, Stream: true},
		},
		{
			name: "stream_options null",
			body: `{"model":"claude-opus-5-5","stream":true,"stream_options":null,"messages":[{"role":"user","content":"hi"}]}`,
			want: anthropicRequest{Model: testModel, MaxTokens: 1024, Messages: []anthropicMessage{userHi}, Stream: true},
		},
		{
			name: "array content",
			body: `{"model":"claude-opus-5-5","messages":[{"role":"user","content":[{"type":"text","text":"hi"},{"type":"text","text":"there"}]}]}`,
			want: anthropicRequest{
				Model:     testModel,
				MaxTokens: 1024,
				Messages:  []anthropicMessage{{Role: "user", Content: textBlocks("hi", "there")}},
			},
		},
		{
			name:    "image part rejected",
			body:    `{"model":"claude-opus-5-5","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://x"}}]}]}`,
			wantErr: "unsupported content part type",
		},
		{
			name:    "tool role rejected",
			body:    `{"model":"claude-opus-5-5","messages":[{"role":"tool","content":"42"}]}`,
			wantErr: "unsupported role",
		},
		{
			name:    "null content rejected",
			body:    `{"model":"claude-opus-5-5","messages":[{"role":"assistant","content":null}]}`,
			wantErr: "message has no content",
		},
		{
			name:    "number content rejected",
			body:    `{"model":"claude-opus-5-5","messages":[{"role":"user","content":42}]}`,
			wantErr: "content must be a string or an array",
		},
		{
			name:    "bad stop type",
			body:    `{"model":"claude-opus-5-5","stop":5,"messages":[{"role":"user","content":"hi"}]}`,
			wantErr: "parse stop",
		},
		{
			name:    "bad include_usage type",
			body:    `{"model":"claude-opus-5-5","stream":true,"stream_options":{"include_usage":"yes"},"messages":[{"role":"user","content":"hi"}]}`,
			wantErr: "parse request",
		},
		{
			name:    "invalid JSON",
			body:    `{"model":`,
			wantErr: "parse request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotUsage, err := translateRequest([]byte(tt.body), 1024)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got  %+v\nwant %+v", got, tt.want)
			}
			if gotUsage != tt.wantUsage {
				t.Errorf("includeUsage = %v, want %v", gotUsage, tt.wantUsage)
			}
		})
	}
}

// TestTranslateRequestJSON checks the JSON tags and omitempty, which
// reflect.DeepEqual in TestTranslateRequest does not see.
func TestTranslateRequestJSON(t *testing.T) {
	body := `{"model":"claude-opus-5-5","stop":"END","messages":[{"role":"user","content":"hi"}]}`
	req, _, err := translateRequest([]byte(body), 1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"model":"claude-opus-5-5","max_tokens":1024,"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"stop_sequences":["END"]}`
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
