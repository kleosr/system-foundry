package observability

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestMiddlewareJoinsIncomingTraceAndLinksLLMAndDBSpans(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	pipeline := NewPipeline(trace.NewTracerProvider(trace.WithSpanProcessor(recorder)))
	handler := pipeline.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, endLLM := pipeline.StartLLM(r.Context(), "primary", "hello")
		_, endDB := pipeline.StartDB(ctx, "SELECT 1")
		endDB(nil)
		endLLM(nil)
		if _, ok := w.(http.Flusher); !ok {
			t.Error("writer lost Flusher")
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	req := httptest.NewRequest(http.MethodPost, "/v1/complete", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("X-Trace-ID") != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace id = %s", rec.Header().Get("X-Trace-ID"))
	}
	spans := recorder.Ended()
	if len(spans) != 3 {
		t.Fatalf("spans = %d", len(spans))
	}
	payload, err := EncodeIngestion(spans)
	if err != nil {
		t.Fatal(err)
	}
	var batch struct {
		Batch []struct {
			Type string         `json:"type"`
			Body map[string]any `json:"body"`
		} `json:"batch"`
	}
	if err = json.Unmarshal(payload, &batch); err != nil {
		t.Fatal(err)
	}
	types := map[string]int{}
	for _, item := range batch.Batch {
		types[item.Type]++
		if item.Body["traceId"] != nil && item.Body["traceId"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
			t.Fatalf("foreign trace in %+v", item)
		}
	}
	if types["generation-create"] != 1 || types["span-create"] != 2 {
		t.Fatalf("types = %v", types)
	}
}
