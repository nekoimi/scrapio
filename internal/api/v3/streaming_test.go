package v3

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nekoimi/scrapio/internal/api/middleware"
)

func TestEventStreamFlushThroughProductionWrappers(t *testing.T) {
	for _, path := range []string{"/api/v3/trials/trial/events", "/api/v3/runs/run/events"} {
		t.Run(path, func(t *testing.T) {
			stream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !supportsFlush(w) {
					t.Fatal("metrics wrapper hides supported streaming")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				// Exercise both wrappers; mutation audit skips GET in production.
				writer := &operationWriter{ResponseWriter: w}
				fmt.Fprint(writer, "id: 1\ndata: {\"status\":\"queued\"}\n\n")
				if err := http.NewResponseController(writer).Flush(); err != nil {
					t.Fatalf("flush through wrapper chain: %v", err)
				}
			})
			recorder := httptest.NewRecorder()
			middleware.MetricsMiddleware(OperationAudit(stream)).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			if !recorder.Flushed || recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "text/event-stream" || !bytes.Contains(recorder.Body.Bytes(), []byte("\"queued\"")) {
				t.Fatalf("event stream was not flushed: %+v", recorder)
			}
		})
	}
}

type nonStreamingWriter struct {
	header http.Header
}

func (w *nonStreamingWriter) Header() http.Header       { return w.header }
func (*nonStreamingWriter) WriteHeader(int)             {}
func (*nonStreamingWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestEventStreamRejectsUnderlyingWriterWithoutFlush(t *testing.T) {
	w := &operationWriter{ResponseWriter: &nonStreamingWriter{header: http.Header{}}}
	if supportsFlush(w) {
		t.Fatal("wrapper incorrectly advertises streaming")
	}
}
