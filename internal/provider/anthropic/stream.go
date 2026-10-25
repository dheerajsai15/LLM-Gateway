package anthropic

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/dheerajsai15/LLM-Gateway/internal/openai"
	"github.com/dheerajsai15/LLM-Gateway/internal/sse"
)

// streamEvent holds the fields of all Anthropic stream events that the
// translation reads. Each event type fills only some of them.
type streamEvent struct {
	Type    string `json:"type"`
	Message struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage usage  `json:"usage"`
	} `json:"message"`
	Delta struct {
		Type       string `json:"type"`
		Text       string `json:"text"`
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage usage `json:"usage"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// usage holds the token counts that message_start and message_delta send.
// input_tokens does not include the cache tokens.
type usage struct {
	InputTokens              int `json:"input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	OutputTokensDetails      struct {
		ThinkingTokens int `json:"thinking_tokens"`
	} `json:"output_tokens_details"`
}

// translateStream reads an Anthropic event stream from r and writes it to w
// as an OpenAI chunk stream. created is the Unix time for every chunk. When
// includeUsage is true, a usage chunk comes before [DONE].
func translateStream(r io.Reader, w io.Writer, created int64, includeUsage bool) error {
	events := sse.NewReader(r)
	var id, model string
	var counts usage

	// newChunk returns a chunk with one choice. It reads id and model, which
	// message_start sets before any text arrives.
	newChunk := func(delta openai.Delta) openai.Chunk {
		return openai.Chunk{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
			Choices: []openai.ChunkChoice{{Index: 0, Delta: delta}},
		}
	}

	for {
		raw, err := events.Next()
		if errors.Is(err, io.EOF) {
			// A complete stream returns at message_stop, so EOF here means
			// that the stream was cut off.
			err = fmt.Errorf("stream ended before message_stop: %w", io.ErrUnexpectedEOF)
		}
		if err != nil {
			return failStream(w, err)
		}

		var ev streamEvent
		if err := json.Unmarshal(raw.Data, &ev); err != nil {
			return failStream(w, fmt.Errorf("parse stream event: %w", err))
		}

		switch ev.Type {
		case "message_start":
			id, model = ev.Message.ID, ev.Message.Model
			counts = ev.Message.Usage
			err = writeChunk(w, newChunk(openai.Delta{Role: "assistant"}))
		case "content_block_delta":
			// Only text blocks send text_delta. Thinking blocks send
			// thinking_delta and signature_delta, so they are skipped here.
			if ev.Delta.Type == "text_delta" {
				err = writeChunk(w, newChunk(openai.Delta{Content: ev.Delta.Text}))
			}
		case "message_delta":
			// The counts in message_delta are cumulative, so they replace the
			// output counts. Some API versions send only output_tokens here, so
			// an input count from message_start stays when it is larger.
			counts.InputTokens = max(counts.InputTokens, ev.Usage.InputTokens)
			counts.CacheCreationInputTokens = max(counts.CacheCreationInputTokens, ev.Usage.CacheCreationInputTokens)
			counts.CacheReadInputTokens = max(counts.CacheReadInputTokens, ev.Usage.CacheReadInputTokens)
			counts.OutputTokens = ev.Usage.OutputTokens
			counts.OutputTokensDetails = ev.Usage.OutputTokensDetails

			chunk := newChunk(openai.Delta{})
			chunk.Choices[0].FinishReason = new(mapStopReason(ev.Delta.StopReason))
			err = writeChunk(w, chunk)
		case "message_stop":
			if includeUsage {
				// The usage chunk has no choices. It must be [], not null.
				chunk := newChunk(openai.Delta{})
				chunk.Choices = []openai.ChunkChoice{}
				chunk.Usage = openaiUsage(counts)
				if err := writeChunk(w, chunk); err != nil {
					return err
				}
			}
			// OpenAI ends a stream with [DONE], not with a JSON chunk.
			if _, err := io.WriteString(w, "data: [DONE]\n\n"); err != nil {
				return fmt.Errorf("write done: %w", err)
			}
			return nil
		case "error":
			// Status 200 is already sent, so the error must go in the body. The
			// stream ends here, because Anthropic sends nothing after an error.
			if err := writeError(w, ev.Error.Type, ev.Error.Message); err != nil {
				return err
			}
			return fmt.Errorf("upstream stream error: %s: %s", ev.Error.Type, ev.Error.Message)
		default:
			// Skip ping, content_block_start, content_block_stop and event
			// types that Anthropic adds in the future.
		}
		if err != nil {
			return err
		}
	}
}

// openaiUsage converts Anthropic token counts to OpenAI usage. OpenAI's
// prompt_tokens includes the cache tokens. Anthropic's input_tokens does not,
// so the cache tokens are added here. output_tokens already includes thinking.
func openaiUsage(u usage) *openai.Usage {
	prompt := u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
	return &openai.Usage{
		PromptTokens:     prompt,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      prompt + u.OutputTokens,
		PromptTokensDetails: openai.PromptTokensDetails{
			CachedTokens: u.CacheReadInputTokens,
		},
		CompletionTokensDetails: openai.CompletionTokensDetails{
			ReasoningTokens: u.OutputTokensDetails.ThinkingTokens,
		},
	}
}

// failStream tells the client that the stream failed, and returns err. The
// client gets a general message, because err can contain internal details
// such as upstream addresses.
func failStream(w io.Writer, err error) error {
	if werr := writeError(w, "server_error", "upstream stream failed"); werr != nil {
		return errors.Join(err, werr)
	}
	return err
}

// writeError writes an OpenAI error object to w as one SSE event. The OpenAI
// SDKs raise an exception when a chunk has an error key.
func writeError(w io.Writer, errType, message string) error {
	data, err := json.Marshal(openai.ErrorResponse{
		Error: openai.ErrorDetail{Message: message, Type: errType},
	})
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return fmt.Errorf("write error: %w", err)
	}
	return nil
}

// mapStopReason returns the OpenAI finish_reason for an Anthropic stop_reason.
// An unknown reason gives "stop", because the model did finish its turn.
func mapStopReason(reason string) string {
	switch reason {
	case "max_tokens", "model_context_window_exceeded":
		return "length"
	case "tool_use":
		return "tool_calls"
	case "refusal":
		return "content_filter"
	default:
		// end_turn, stop_sequence, pause_turn and reasons that Anthropic
		// adds in the future.
		return "stop"
	}
}

// writeChunk writes chunk to w as one SSE event. Fprintf formats the whole
// event first and then calls Write once, so each event is one flush.
func writeChunk(w io.Writer, chunk openai.Chunk) error {
	data, err := json.Marshal(chunk)
	if err != nil {
		return fmt.Errorf("marshal chunk: %w", err)
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return fmt.Errorf("write chunk: %w", err)
	}
	return nil
}
