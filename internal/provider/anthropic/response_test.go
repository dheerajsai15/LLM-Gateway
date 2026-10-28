package anthropic

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTranslateResponseGolden(t *testing.T) {
	inputs, err := filepath.Glob("testdata/message_*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) == 0 {
		t.Fatal("no testdata/message_*.json files")
	}

	for _, input := range inputs {
		name := strings.TrimSuffix(filepath.Base(input), ".json")
		t.Run(name, func(t *testing.T) {
			in, err := os.ReadFile(input)
			if err != nil {
				t.Fatal(err)
			}

			res, err := translateResponse(in, testCreated)
			if err != nil {
				t.Fatalf("translateResponse: %v", err)
			}
			// Indented JSON makes a golden file that a person can review
			// in git diff.
			out, err := json.MarshalIndent(res, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, '\n')

			golden := filepath.Join("testdata", name+".golden")
			if *update {
				if err := os.WriteFile(golden, out, 0o644); err != nil {
					t.Fatal(err)
				}
			}

			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run the test with -update to create it)", err)
			}
			if !bytes.Equal(out, want) {
				t.Errorf("output does not match %s\ngot:\n%s\nwant:\n%s", golden, out, want)
			}
		})
	}
}

func TestTranslateResponse(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantContent string
		wantFinish  string
		wantErr     bool
	}{
		{
			name:        "text blocks joined in order, thinking skipped",
			body:        `{"content":[{"type":"text","text":"Hello, "},{"type":"thinking","thinking":"hmm"},{"type":"text","text":"Gopher!"}],"stop_reason":"end_turn"}`,
			wantContent: "Hello, Gopher!",
			wantFinish:  "stop",
		},
		{
			// Only the type decides. A thinking block keeps its text in
			// "thinking", so this row needs a block that has a "text" field.
			name:        "a block of another type with a text field is skipped",
			body:        `{"content":[{"type":"future_block","text":"must not appear"},{"type":"text","text":"Hi"}],"stop_reason":"end_turn"}`,
			wantContent: "Hi",
			wantFinish:  "stop",
		},
		{
			name:        "no text block",
			body:        `{"content":[{"type":"thinking","thinking":"hmm"}],"stop_reason":"max_tokens"}`,
			wantContent: "",
			wantFinish:  "length",
		},
		{
			name:        "empty content",
			body:        `{"content":[],"stop_reason":"end_turn"}`,
			wantContent: "",
			wantFinish:  "stop",
		},
		{
			name:    "bad JSON",
			body:    `{"content":`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := translateResponse([]byte(tt.body), testCreated)
			if tt.wantErr {
				if err == nil {
					t.Fatal("translateResponse() error = nil, want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("translateResponse() error = %v", err)
			}
			if len(res.Choices) != 1 {
				t.Fatalf("got %d choices, want 1", len(res.Choices))
			}
			choice := res.Choices[0]
			if choice.Message.Content != tt.wantContent {
				t.Errorf("content = %q, want %q", choice.Message.Content, tt.wantContent)
			}
			if choice.Message.Role != "assistant" {
				t.Errorf("role = %q, want assistant", choice.Message.Role)
			}
			if choice.FinishReason != tt.wantFinish {
				t.Errorf("finish_reason = %q, want %q", choice.FinishReason, tt.wantFinish)
			}
		})
	}
}
