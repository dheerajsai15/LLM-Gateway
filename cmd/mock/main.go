package main

import (
	"encoding/json"
	"log"
	"net/http"
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

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /v1/chat/completions", chatCompletionHandler)

	if err := http.ListenAndServe(":9090", mux); err != nil {
		log.Fatal(err)
	}
}
