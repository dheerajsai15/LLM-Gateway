package anthropic

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// openaiRequest holds only the fields of an OpenAI chat request that the
// translation reads. All other fields, such as temperature, are dropped.
type openaiRequest struct {
	Model               string          `json:"model"`
	Messages            []openaiMessage `json:"messages"`
	MaxTokens           *int            `json:"max_tokens"`
	MaxCompletionTokens *int            `json:"max_completion_tokens"`
	Stop                json.RawMessage `json:"stop"`
	Stream              bool            `json:"stream"`
}

// openaiMessage is one message in an OpenAI chat request. Content is a string
// or an array of parts, so it stays raw until the translation reads it.
type openaiMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// anthropicRequest is the body of a request to the Anthropic Messages API.
type anthropicRequest struct {
	Model         string             `json:"model"`
	MaxTokens     int                `json:"max_tokens"`
	System        string             `json:"system,omitempty"`
	Messages      []anthropicMessage `json:"messages"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Stream        bool               `json:"stream,omitempty"`
}

// anthropicMessage is one message in an Anthropic request. Content is always
// an array of blocks, even when the client sent a plain string.
type anthropicMessage struct {
	Role    string           `json:"role"`
	Content []anthropicBlock `json:"content"`
}

// anthropicBlock is one content block. Only text blocks are supported.
type anthropicBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// translateRequest converts an OpenAI chat request body into an Anthropic
// request. It uses defaultMaxTokens when the client sets no token limit.
func translateRequest(body []byte, defaultMaxTokens int) (anthropicRequest, error) {
	var in openaiRequest
	if err := json.Unmarshal(body, &in); err != nil {
		return anthropicRequest{}, fmt.Errorf("parse request: %w", err)
	}

	out := anthropicRequest{Model: in.Model, Stream: in.Stream}

	switch {
	case in.MaxCompletionTokens != nil:
		out.MaxTokens = *in.MaxCompletionTokens
	case in.MaxTokens != nil:
		out.MaxTokens = *in.MaxTokens
	default:
		out.MaxTokens = defaultMaxTokens
	}

	stop, err := parseStop(in.Stop)
	if err != nil {
		return anthropicRequest{}, err
	}
	out.StopSequences = stop

	var system []string
	for _, msg := range in.Messages {
		blocks, err := contentBlocks(msg.Content)
		if err != nil {
			return anthropicRequest{}, err
		}
		switch msg.Role {
		case "system", "developer":
			for _, block := range blocks {
				system = append(system, block.Text)
			}
		case "user", "assistant":
			out.Messages = append(out.Messages, anthropicMessage{Role: msg.Role, Content: blocks})
		default:
			return anthropicRequest{}, fmt.Errorf("unsupported role %q", msg.Role)
		}
	}
	out.System = strings.Join(system, "\n\n")

	return out, nil
}

// parseStop converts the OpenAI stop field, a string or an array of strings,
// into a slice. It returns nil when the field is absent or null.
func parseStop(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("parse stop: %w", err)
		}
		return []string{s}, nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("parse stop: %w", err)
	}
	return list, nil
}

// contentBlocks converts the content of one OpenAI message, a string or an
// array of parts, into Anthropic blocks. Only text parts are supported.
func contentBlocks(raw json.RawMessage) ([]anthropicBlock, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, errors.New("message has no content")
	}

	switch raw[0] {
	case '"':
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("parse content: %w", err)
		}
		return []anthropicBlock{{Type: "text", Text: s}}, nil
	case '[':
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(raw, &parts); err != nil {
			return nil, fmt.Errorf("parse content: %w", err)
		}
		blocks := make([]anthropicBlock, 0, len(parts))
		for _, part := range parts {
			if part.Type != "text" {
				return nil, fmt.Errorf("unsupported content part type %q", part.Type)
			}
			blocks = append(blocks, anthropicBlock{Type: "text", Text: part.Text})
		}
		return blocks, nil
	default:
		return nil, errors.New("content must be a string or an array")
	}
}
