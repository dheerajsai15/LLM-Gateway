package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dheerajsai15/LLM-Gateway/internal/apikey"
	"github.com/dheerajsai15/LLM-Gateway/internal/config"
	"github.com/dheerajsai15/LLM-Gateway/internal/db"
	"github.com/dheerajsai15/LLM-Gateway/internal/openai"
	"github.com/dheerajsai15/LLM-Gateway/internal/provider"
	"github.com/dheerajsai15/LLM-Gateway/internal/provider/anthropic"
	openaiprovider "github.com/dheerajsai15/LLM-Gateway/internal/provider/openai"
	"github.com/dheerajsai15/LLM-Gateway/internal/usage"
)

var errIdleTimeout = errors.New("upstream idle timeout")

type routingProvider interface {
	provider.Provider
	ProviderName(model string) string
}

type usageSender interface {
	Send(r usage.Record)
}

type gateway struct {
	idleTimeout time.Duration
	provider    routingProvider
	models      openai.ModelList
	usageLog    usageSender
}

const (
	readHeaderTimeout  = 10 * time.Second
	readTimeout        = 30 * time.Second
	serverIdleTimeout  = 120 * time.Second
	shutdownTimeout    = 25 * time.Second
	pprofAddr          = "localhost:6060"
	keyRefreshInterval = 30 * time.Second
	startupTimeout     = 10 * time.Second
	usageBufferSize    = 1024
	usageBatchSize     = 100
	usageFlushInterval = time.Second
)

func healthHandler(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "ok")
}

func newModelList(ids []string) openai.ModelList {
	list := openai.ModelList{Object: "list", Data: make([]openai.Model, 0, len(ids))}
	for _, id := range ids {
		list.Data = append(list.Data, openai.Model{ID: id, Object: "model", OwnedBy: "llm-gateway"})
	}
	return list
}

func (g *gateway) modelsHandler(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(g.models); err != nil {
		slog.Error("writing model list failed", "err", err)
	}
}

func (g *gateway) chatCompletionHandler(w http.ResponseWriter, req *http.Request) {
	record := usage.Record{StartedAt: time.Now()}
	keyID, ok := apiKeyIDFromContext(req.Context())
	if !ok {
		slog.Error("chat completion without an API key id")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	record.APIKeyID = keyID

	sr := &statusRecorder{ResponseWriter: w}
	w = sr

	// A closure, because the arguments of a deferred call are evaluated at
	// the defer line. It is registered first, so it runs last, after the
	// upstream body is closed.
	defer func() {
		record.Duration = time.Since(record.StartedAt)
		record.StatusCode = sr.status
		if record.StatusCode == 0 {
			record.StatusCode = statusClientClosedRequest
		}
		if record.Outcome == "" {
			slog.Error("usage record without an outcome", "status", record.StatusCode)
			record.Outcome = usage.OutcomeUpstreamError
		}
		g.usageLog.Send(record)
	}()

	ctx, cancel := context.WithCancelCause(req.Context())
	defer cancel(nil)

	reqBody, err := io.ReadAll(req.Body)
	if err != nil {
		record.Outcome = usage.OutcomeBadRequest
		http.Error(w, "cannot read request body", http.StatusBadRequest)
		return
	}

	reqObj := openai.Request{}
	if err := json.Unmarshal(reqBody, &reqObj); err != nil {
		record.Outcome = usage.OutcomeBadRequest
		http.Error(w, "bad JSON", http.StatusBadRequest)
		return
	}
	record.Model = reqObj.Model
	record.Stream = reqObj.Stream
	record.Provider = g.provider.ProviderName(reqObj.Model)

	idle := time.AfterFunc(g.idleTimeout, func() { cancel(errIdleTimeout) })
	defer idle.Stop()

	// The adapter resets the idle timer on each read from the upstream. The
	// handler does not, because some adapters read in their own goroutine.
	res, err := g.provider.ChatCompletion(ctx, provider.Request{
		Model:          reqObj.Model,
		Stream:         reqObj.Stream,
		RawBody:        reqBody,
		OnUpstreamRead: func() { idle.Reset(g.idleTimeout) },
	})
	if err != nil {
		if errors.Is(context.Cause(ctx), errIdleTimeout) {
			record.Outcome = usage.OutcomeIdleTimeout
			slog.Warn("upstream idle timeout", "stage", "before_upstream_response")
			http.Error(w, "upstream idle timeout before response", http.StatusGatewayTimeout)
			return
		}
		if errors.Is(err, context.Canceled) {
			record.Outcome = usage.OutcomeClientCanceled
			slog.Info("client disconnected", "stage", "before_upstream_response")
			return
		}
		record.Outcome = usage.OutcomeUpstreamError
		http.Error(w, "upstream service unavailable", http.StatusBadGateway)
		slog.Error("upstream service unavailable", "err", err)
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
				record.Outcome = usage.OutcomeClientCanceled
				if req.Context().Err() != nil {
					slog.Info("client disconnected", "stage", "during_stream", "err", werr)
					return
				}
				slog.Error("writing response failed", "err", werr)
				return
			}

			if ferr := rc.Flush(); ferr != nil {
				record.Outcome = usage.OutcomeClientCanceled
				if req.Context().Err() != nil {
					slog.Info("client disconnected", "stage", "during_stream", "err", ferr)
					return
				}
				slog.Error("flushing response failed", "err", ferr)
				return
			}
		}

		if err == io.EOF {
			record.Outcome = outcomeForStatus(res.StatusCode)
			return
		}

		if err != nil {
			if errors.Is(context.Cause(ctx), errIdleTimeout) {
				record.Outcome = usage.OutcomeIdleTimeout
				slog.Warn("upstream idle timeout", "stage", "during_stream")
				return
			}
			if errors.Is(err, context.Canceled) {
				record.Outcome = usage.OutcomeClientCanceled
				slog.Info("client disconnected", "stage", "during_stream")
				return
			}
			record.Outcome = usage.OutcomeUpstreamError
			slog.Error("reading upstream response failed", "err", err, "stage", "during_stream")
			return
		}
	}

}

func outcomeForStatus(code int) usage.Outcome {
	switch {
	case code == http.StatusBadRequest:
		return usage.OutcomeBadRequest
	case code >= 400:
		return usage.OutcomeUpstreamError
	default:
		return usage.OutcomeOK
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
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)
	configFlag := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configFlag)
	if err != nil {
		slog.Error("load config failed", "err", err)
		os.Exit(1)
	}
	openaiProvider := openaiprovider.New(cfg.UpstreamURL, cfg.APIKey, http.DefaultClient)
	anthropicProvider := anthropic.New(cfg.AnthropicURL, cfg.AnthropicAPIKey, http.DefaultClient)
	gw := &gateway{
		idleTimeout: cfg.IdleTimeout,
		provider: provider.NewRouter(openaiProvider,
			provider.Route{Prefix: "claude-", Provider: anthropicProvider},
		),
		models: newModelList(cfg.Models),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	startupCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	pool, err := db.Connect(startupCtx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("cannot connect to db", "err", err)
		os.Exit(1)
	}
	cache := apikey.NewCache(apikey.NewStore(pool))
	err = cache.Refresh(startupCtx)
	if err != nil {
		slog.Error("cannot load keys from db", "err", err)
		os.Exit(1)
	}
	cancel()
	go cache.RefreshEvery(ctx, keyRefreshInterval)

	usageWriter := usage.NewWriter(usage.NewStore(pool), usageBufferSize, usageBatchSize, usageFlushInterval)
	go usageWriter.Run()
	gw.usageLog = usageWriter

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", healthHandler)

	mux.Handle("GET /v1/models", requireAPIKey(cache, http.HandlerFunc(gw.modelsHandler)))

	mux.Handle("POST /v1/chat/completions", requireAPIKey(cache, http.HandlerFunc(gw.chatCompletionHandler)))

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       serverIdleTimeout,
		ReadTimeout:       readTimeout,
	}

	errCh := make(chan error, 1)

	slog.Info("gateway started", "listen_addr", cfg.ListenAddr, "upstream", cfg.UpstreamURL, "anthropic_url", cfg.AnthropicURL, "idle_timeout", cfg.IdleTimeout)
	go func() {
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	go func() {
		if pprofErr := http.ListenAndServe(pprofAddr, nil); pprofErr != nil {
			slog.Error("pprof server failed", "err", pprofErr)
		}
	}()

	select {
	case err := <-errCh:
		slog.Error("server failed", "err", err)
		os.Exit(1)
	case <-ctx.Done():
	}

	stop()
	slog.Info("shutting down", "cause", context.Cause(ctx))
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown failed", "err", err)
	}
	if err := usageWriter.Close(shutdownCtx); err != nil {
		slog.Error("usage writer close failed", "err", err)
	}
	pool.Close()
	slog.Info("server stopped")
}
