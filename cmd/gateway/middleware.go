package main

import (
	"log/slog"
	"net/http"
	"time"
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
