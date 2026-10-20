package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/dheerajsai15/LLM-Gateway/internal/config"
	"github.com/dheerajsai15/LLM-Gateway/internal/openai"
)

type gateway struct {
	upstreamURL string
	apiKey      string
}

func healthHandler(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "ok")
}

func (g *gateway) chatCompletionHandler(w http.ResponseWriter, req *http.Request) {
	reqBody, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, "cannot read request body", http.StatusBadRequest)
		return
	}

	reqObj := openai.Request{}
	if err := json.Unmarshal(reqBody, &reqObj); err != nil {
		http.Error(w, "bad JSON", http.StatusBadRequest)
		return
	}

	upstreamPath := g.upstreamURL + req.RequestURI
	upStreamReq, err := http.NewRequestWithContext(req.Context(), req.Method, upstreamPath, bytes.NewReader(reqBody))
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		log.Println("error during request creation", err)
		return
	}

	copyHeader(upStreamReq.Header, req.Header)
	upStreamReq.Header.Set("Authorization", "Bearer "+g.apiKey)

	res, err := http.DefaultClient.Do(upStreamReq)
	if errors.Is(err, context.Canceled) {
		log.Println("client disconnected before upstream response")
		return
	}
	if err != nil {
		http.Error(w, "upstream service unavailable", http.StatusBadGateway)
		log.Println("upstream service unavailable", err)
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
				if req.Context().Err() != nil {
					log.Println("client disconnected during stream")
					return
				}
				log.Println("error while writing response", werr)
				return
			}

			if ferr := rc.Flush(); ferr != nil {
				if req.Context().Err() != nil {
					log.Println("client disconnected during stream")
					return
				}
				log.Println("error while flushing response", ferr)
				return
			}
		}

		if err == io.EOF {
			return
		}

		if err != nil {
			if errors.Is(err, context.Canceled) {
				log.Println("client disconnected during stream")
				return
			}
			log.Println("error", err)
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
	configFlag := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configFlag)
	if err != nil {
		log.Fatal(err)
	}
	gw := &gateway{upstreamURL: cfg.UpstreamURL, apiKey: cfg.APIKey}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", healthHandler)

	mux.HandleFunc("POST /v1/chat/completions", gw.chatCompletionHandler)

	log.Printf("listening on %s, upstream %s", cfg.ListenAddr, cfg.UpstreamURL)
	if err := http.ListenAndServe(cfg.ListenAddr, mux); err != nil {
		log.Fatal(err)
	}
}
