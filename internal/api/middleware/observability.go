package middleware

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

type contextKey string

const RequestIDKey contextKey = "request_id"

var (
	requestCount  atomic.Int64
	requestErrors atomic.Int64
	requestTimeNs atomic.Int64
)

// RequestIDMiddleware gives every HTTP request a stable correlation ID. The
// same ID can be forwarded to browser jobs and searched in application logs.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", requestID)
		ctx := context.WithValue(r.Context(), RequestIDKey, requestID)
		// Keep a string-key mirror for infrastructure clients that must not
		// depend on the HTTP middleware package (for example Browser Client).
		ctx = context.WithValue(ctx, "request_id", requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(body)
}

// Preserve streaming through the metrics wrapper without advertising unsupported interfaces.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// MetricsMiddleware records low-cardinality HTTP metrics without persisting
// request bodies or credentials.
func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		writer := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(writer, r)
		status := writer.status
		if status == 0 {
			status = http.StatusOK
		}
		requestCount.Add(1)
		requestTimeNs.Add(time.Since(started).Nanoseconds())
		if status >= http.StatusInternalServerError {
			requestErrors.Add(1)
		}
		log.WithFields(log.Fields{"request_id": RequestID(r.Context()), "status": status, "duration_ms": time.Since(started).Milliseconds()}).Debugf("request completed: %s %s", r.Method, r.URL.Path)
	})
}

func RequestID(ctx context.Context) string {
	if value, ok := ctx.Value(RequestIDKey).(string); ok {
		return value
	}
	return ""
}

type HTTPMetrics struct {
	Requests         int64 `json:"requests"`
	Errors           int64 `json:"errors"`
	AverageLatencyMs int64 `json:"average_latency_ms"`
}

func HTTPMetricsSnapshot() HTTPMetrics {
	count := requestCount.Load()
	average := int64(0)
	if count > 0 {
		average = requestTimeNs.Load() / count / int64(time.Millisecond)
	}
	return HTTPMetrics{Requests: count, Errors: requestErrors.Load(), AverageLatencyMs: average}
}
