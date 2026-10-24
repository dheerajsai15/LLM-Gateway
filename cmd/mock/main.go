package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/dheerajsai15/LLM-Gateway/internal/openai"
)

const (
	stallModel = "mock-stall"
	stallTime  = 10 * time.Second
)

var mockWords = strings.Fields("Hello from the mock")

type mockConfig struct {
	tokens      int
	tokenDelay  time.Duration
	latency     time.Duration
	errorRate   float64
	errorStatus int
}

func (cfg *mockConfig) validate() error {
	if cfg.tokens < 1 {
		return fmt.Errorf("tokens must be 1 or more, given: %d", cfg.tokens)
	}
	if cfg.tokenDelay < 0 {
		return fmt.Errorf("token-delay must be 0 or more, given: %v", cfg.tokenDelay)
	}
	if cfg.latency < 0 {
		return fmt.Errorf("latency must be 0 or more, given: %v", cfg.latency)
	}
	if cfg.errorRate < 0 || cfg.errorRate > 1 {
		return fmt.Errorf("error-rate must be from 0 to 1, given: %v", cfg.errorRate)
	}
	if cfg.errorStatus < 400 || cfg.errorStatus > 599 {
		return fmt.Errorf("error-status must be from 400 to 599, given: %d", cfg.errorStatus)
	}

	return nil
}

// responseTokens returns the content chunks of a response. It uses the mock
// words in a loop, and each chunk except the last ends with a space.
func (cfg *mockConfig) responseTokens() []string {
	tokens := make([]string, cfg.tokens)
	for i := range tokens {
		tokens[i] = mockWords[i%len(mockWords)]
		if i != len(tokens)-1 {
			tokens[i] += " "
		}
	}
	return tokens
}

func (cfg *mockConfig) chatCompletionHandler(w http.ResponseWriter, req *http.Request) {
	chatReq := openai.Request{}
	if err := json.NewDecoder(req.Body).Decode(&chatReq); err != nil {
		http.Error(w, "JSON supplied is not correct format!", http.StatusBadRequest)
		return
	}
	log.Printf("%+v", chatReq)

	if err := sleepCtx(req.Context(), cfg.latency); err != nil {
		log.Println("latency wait stopped:", err)
		return
	}

	if rand.Float64() < cfg.errorRate {
		log.Println("injected error:", cfg.errorStatus)
		writeError(w, cfg.errorStatus)
		return
	}

	if chatReq.Stream {
		cfg.streamResponse(req.Context(), w, chatReq)
	} else {
		res := openai.Response{
			ID:      "chatcmpl-mock-1",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   chatReq.Model,
			Choices: []openai.Choice{
				{
					Index: 0,
					Message: openai.Message{
						Role:    "assistant",
						Content: strings.Join(cfg.responseTokens(), ""),
					},
					FinishReason: "stop",
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(res); err != nil {
			log.Println("encode error:", err)
		}
	}
}

func (cfg *mockConfig) streamResponse(ctx context.Context, w http.ResponseWriter, chatReq openai.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	rc := http.NewResponseController(w)

	chunk := openai.Chunk{
		ID:      "chatcmpl-mock-1",
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   chatReq.Model,
		Choices: []openai.ChunkChoice{
			{
				Index:        0,
				FinishReason: nil,
			},
		},
	}

	chunk.Choices[0].Delta = openai.Delta{Role: "assistant"}

	if err := writeChunk(w, rc, chunk); err != nil {
		return
	}

	for i, token := range cfg.responseTokens() {
		delay := cfg.tokenDelay

		chunk.Choices[0].Delta = openai.Delta{Content: token}
		if err := writeChunk(w, rc, chunk); err != nil {
			return
		}

		if chatReq.Model == stallModel && i == 0 {
			delay = stallTime
		}

		if err := sleepCtx(ctx, delay); err != nil {
			log.Println("stream wait stopped:", err)
			return
		}
	}

	chunk.Choices[0].Delta = openai.Delta{}
	stop := "stop"
	chunk.Choices[0].FinishReason = &stop
	if err := writeChunk(w, rc, chunk); err != nil {
		return
	}

	if err := writeEvent(w, rc, []byte("[DONE]")); err != nil {
		return
	}
}

func sleepCtx(ctx context.Context, delay time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
	}

	return nil
}

// writeError sends an error in the OpenAI format. 4xx errors have the type
// "invalid_request_error" and 5xx errors have the type "server_error".
func writeError(w http.ResponseWriter, status int) {
	errType := "server_error"
	if status < 500 {
		errType = "invalid_request_error"
	}
	res := openai.ErrorResponse{
		Error: openai.ErrorDetail{
			Message: fmt.Sprintf("mock injected error with status %d", status),
			Type:    errType,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(res); err != nil {
		log.Println("encode error:", err)
	}
}

// writeChunk sends one chunk as an SSE event. It returns the first error, so
// the caller can stop the stream when the client disconnects.
func writeChunk(w http.ResponseWriter, rc *http.ResponseController, chunk openai.Chunk) error {
	data, err := json.Marshal(chunk)
	if err != nil {
		log.Println(err)
		return err
	}

	return writeEvent(w, rc, data)
}

func writeEvent(w http.ResponseWriter, rc *http.ResponseController, data []byte) error {
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		log.Println(err)
		return err
	}

	if err := rc.Flush(); err != nil {
		log.Println(err)
		return err
	}

	return nil
}

func main() {
	addr := flag.String("addr", ":9090", "address to listen on")
	tokens := flag.Int("tokens", 4, "number of content chunks in a response")
	tokenDelay := flag.Duration("token-delay", 200*time.Millisecond, "delay after each content chunk in a stream")
	latency := flag.Duration("latency", 0, "delay before the first byte of a response")
	errorRate := flag.Float64("error-rate", 0, "fraction of requests that fail, from 0 to 1")
	errorStatus := flag.Int("error-status", http.StatusInternalServerError, "HTTP status of a failed request, from 400 to 599")
	flag.Parse()

	cfg := &mockConfig{
		tokens:      *tokens,
		tokenDelay:  *tokenDelay,
		latency:     *latency,
		errorRate:   *errorRate,
		errorStatus: *errorStatus,
	}
	if err := cfg.validate(); err != nil {
		log.Fatal(err)
	}
	log.Printf("mock started: addr=%s tokens=%d token-delay=%v latency=%v error-rate=%v error-status=%d",
		*addr, cfg.tokens, cfg.tokenDelay, cfg.latency, cfg.errorRate, cfg.errorStatus)

	mux := http.NewServeMux()

	mux.HandleFunc("POST /v1/chat/completions", cfg.chatCompletionHandler)

	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}
