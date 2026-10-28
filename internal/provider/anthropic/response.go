package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dheerajsai15/LLM-Gateway/internal/openai"
)

// messageResponse holds the fields of an Anthropic message that the
// translation reads.
type messageResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      usage  `json:"usage"`
}

// translateResponse turns an Anthropic message into an OpenAI chat
// completion. created is its Unix time. The text blocks are joined in order,
// and other blocks, such as thinking, are skipped. A stream does the same.
func translateResponse(body []byte, created int64) (openai.Response, error) {
	var msg messageResponse
	if err := json.Unmarshal(body, &msg); err != nil {
		return openai.Response{}, fmt.Errorf("parse anthropic message: %w", err)
	}

	var text strings.Builder
	for _, block := range msg.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}

	return openai.Response{
		ID:      msg.ID,
		Object:  "chat.completion",
		Created: created,
		Model:   msg.Model,
		Choices: []openai.Choice{{
			Index:        0,
			Message:      openai.Message{Role: "assistant", Content: text.String()},
			FinishReason: mapStopReason(msg.StopReason),
		}},
		Usage: openaiUsage(msg.Usage),
	}, nil
}
