package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/dheerajsai15/LLM-Gateway/internal/openai"
)

func chatCompletionHandler(w http.ResponseWriter, req *http.Request) {
	chatReq := openai.Request{}
	if err := json.NewDecoder(req.Body).Decode(&chatReq); err != nil {
		http.Error(w, "JSON supplied is not correct format!", http.StatusBadRequest)
		return
	}
	log.Printf("%+v", chatReq)

	if chatReq.Stream {
		streamResponse(w, chatReq)
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
						Content: "Hello from the mock",
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

func streamResponse(w http.ResponseWriter, chatReq openai.Request) {
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

	words := strings.Fields("Hello from the mock")

	for i, word := range words {
		if i != len(words)-1 {
			word += " "
		}

		chunk.Choices[0].Delta = openai.Delta{Content: word}
		if err := writeChunk(w, rc, chunk); err != nil {
			return
		}

		time.Sleep(200 * time.Millisecond)
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
	mux := http.NewServeMux()

	mux.HandleFunc("POST /v1/chat/completions", chatCompletionHandler)

	if err := http.ListenAndServe(":9090", mux); err != nil {
		log.Fatal(err)
	}
}
