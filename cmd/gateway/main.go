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

	"github.com/dheerajsai15/LLM-Gateway/internal/config"
	"github.com/dheerajsai15/LLM-Gateway/internal/openai"
	"github.com/dheerajsai15/LLM-Gateway/internal/provider"
	openaiprovider "github.com/dheerajsai15/LLM-Gateway/internal/provider/openai"
)

var errIdleTimeout = errors.New("upstream idle timeout")

type gateway struct {
	idleTimeout time.Duration
	provider    provider.Provider
}

const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	serverIdleTimeout = 120 * time.Second
	shutdownTimeout   = 25 * time.Second
	pprofAddr         = "localhost:6060"
)

func healthHandler(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "ok")
}

func (g *gateway) chatCompletionHandler(w http.ResponseWriter, req *http.Request) {
	ctx, cancel := context.WithCancelCause(req.Context())
	defer cancel(nil)

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
			slog.Warn("upstream idle timeout", "stage", "before_upstream_response")
			http.Error(w, "upstream idle timeout before response", http.StatusGatewayTimeout)
			return
		}
		if errors.Is(err, context.Canceled) {
			slog.Info("client disconnected", "stage", "before_upstream_response")
			return
		}
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
				if req.Context().Err() != nil {
					slog.Info("client disconnected", "stage", "during_stream", "err", werr)
					return
				}
				slog.Error("writing response failed", "err", werr)
				return
			}

			if ferr := rc.Flush(); ferr != nil {
				if req.Context().Err() != nil {
					slog.Info("client disconnected", "stage", "during_stream", "err", ferr)
					return
				}
				slog.Error("flushing response failed", "err", ferr)
				return
			}
		}

		if err == io.EOF {
			return
		}

		if err != nil {
			if errors.Is(context.Cause(ctx), errIdleTimeout) {
				slog.Warn("upstream idle timeout", "stage", "during_stream")
				return
			}
			if errors.Is(err, context.Canceled) {
				slog.Info("client disconnected", "stage", "during_stream")
				return
			}
			slog.Error("reading upstream response failed", "err", err, "stage", "during_stream")
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
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)
	configFlag := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configFlag)
	if err != nil {
		slog.Error("load config failed", "err", err)
		os.Exit(1)
	}
	gw := &gateway{
		idleTimeout: cfg.IdleTimeout,
		provider:    openaiprovider.New(cfg.UpstreamURL, cfg.APIKey, http.DefaultClient),
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", healthHandler)

	mux.HandleFunc("POST /v1/chat/completions", gw.chatCompletionHandler)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       serverIdleTimeout,
		ReadTimeout:       readTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)

	slog.Info("gateway started", "listen_addr", cfg.ListenAddr, "upstream", cfg.UpstreamURL, "idle_timeout", cfg.IdleTimeout)
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

	slog.Info("server stopped")
}
