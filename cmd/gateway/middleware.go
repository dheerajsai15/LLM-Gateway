package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/dheerajsai15/LLM-Gateway/internal/openai"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

const statusClientClosedRequest = 499

func (rs *statusRecorder) WriteHeader(code int) {
	if rs.status == 0 {
		rs.status = code
	}
	rs.ResponseWriter.WriteHeader(code)
}

func (rs *statusRecorder) Write(data []byte) (int, error) {
	if rs.status == 0 {
		rs.status = http.StatusOK
	}
	return rs.ResponseWriter.Write(data)
}

func (rs *statusRecorder) Unwrap() http.ResponseWriter {
	return rs.ResponseWriter
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		startTime := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 0}
		next.ServeHTTP(rec, req)
		duration := time.Since(startTime)
		if rec.status == 0 {
			if req.Context().Err() != nil {
				rec.status = statusClientClosedRequest
			} else {
				rec.status = http.StatusOK
			}
		}
		slog.Info("request completed", "method", req.Method, "path", req.URL.Path, "status", rec.status, "duration_ms", duration.Milliseconds())
	})
}

func bearerToken(req *http.Request) (string, bool) {
	const prefix = "Bearer "
	h := req.Header.Get("Authorization")
	if len(h) < len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(h[len(prefix):])
	return token, token != ""
}

func writeUnauthorized(w http.ResponseWriter, message string) {
	code := "invalid_api_key"
	w.Header().Set("WWW-Authenticate", "Bearer")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	err := json.NewEncoder(w).Encode(openai.ErrorResponse{
		Error: openai.ErrorDetail{Message: message, Type: "invalid_request_error", Code: &code},
	})
	if err != nil {
		slog.Error("writing unauthorized response failed", "err", err)
	}
}

type apiKeyIDKey struct{}

func apiKeyIDFromContext(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(apiKeyIDKey{}).(int64)
	return id, ok
}

type keyLookup interface {
	Lookup(secret string) (int64, bool)
}

func requireAPIKey(keys keyLookup, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		token, ok := bearerToken(req)
		if !ok {
			writeUnauthorized(w, "missing API key")
			return
		}
		id, ok := keys.Lookup(token)
		if !ok {
			writeUnauthorized(w, "invalid API key")
			return
		}
		ctx := context.WithValue(req.Context(), apiKeyIDKey{}, id)
		next.ServeHTTP(w, req.WithContext(ctx))
	})
}
