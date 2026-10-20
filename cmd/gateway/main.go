package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/dheerajsai15/LLM-Gateway/internal/config"
	"github.com/dheerajsai15/LLM-Gateway/internal/openai"
)

func healthHandler(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "ok")
}

func chatCompletionHandler(w http.ResponseWriter, req *http.Request) {
	reqBody, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, "cannot read request body", http.StatusBadRequest)
		return
	}

	reqObj := openai.Request{}
	if err := json.Unmarshal(reqBody, &reqObj); err != nil {
		http.Error(w, "Bad JSON", http.StatusBadRequest)
		return
	}

	const upstreamURL = "http://localhost:9090"
	upstreamPath := upstreamURL + req.RequestURI
	upStreamReq, err := http.NewRequestWithContext(req.Context(), req.Method, upstreamPath, bytes.NewReader(reqBody))
	if err != nil {
		http.Error(w, "Server error", http.StatusInternalServerError)
		log.Println("Error during request creation", err)
		return
	}

	copyHeader(upStreamReq.Header, req.Header)

	res, err := http.DefaultClient.Do(upStreamReq)
	if err != nil {
		http.Error(w, "Upstream service unavailable", http.StatusBadGateway)
		log.Println("Upstream service unavailable", err)
		return
	}
	defer res.Body.Close()

	copyHeader(w.Header(), res.Header)
	w.WriteHeader(res.StatusCode)

	buf := make([]byte, 32*1024)
	rc := http.NewResponseController(w)

	for {
		n, err := res.Body.Read(buf)

		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				log.Println("Error while writing response", werr)
				return
			}

			if ferr := rc.Flush(); ferr != nil {
				log.Println("Error while flushing response", ferr)
				return
			}
		}

		if err == io.EOF {
			return
		}

		if err != nil {
			log.Println("Error", err)
			return
		}
	}

}

func copyHeader(dst, src http.Header) {
	for name, headers := range src {
		for _, val := range headers {
			dst.Add(name, val)
		}
	}
}

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("%+v", cfg)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", healthHandler)

	mux.HandleFunc("POST /v1/chat/completions", chatCompletionHandler)

	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}
