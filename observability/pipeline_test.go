package observability

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestHTTPLLMAndDBShareOneTrace(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
	pipeline := NewPipeline(provider)
	ctx, endHTTP := pipeline.StartHTTP(context.Background(), "POST", "/llm")
	ctx, endLLM := pipeline.StartLLM(ctx, "primary", "mail ada@example.com")
	_, endDB := pipeline.StartDB(ctx, "SELECT document_id FROM documents WHERE title = $1")
	endDB(nil)
	endLLM(nil)
	endHTTP(200)
	spans := recorder.Ended()
	if len(spans) != 3 {
		t.Fatalf("spans = %d", len(spans))
	}
	traceID := spans[0].SpanContext().TraceID()
	names := map[string]bool{}
	for _, span := range spans {
		if span.SpanContext().TraceID() != traceID {
			t.Fatal("trace ids differ")
		}
		names[span.Name()] = true
		for _, attr := range span.Attributes() {
			if attr.Key == "llm.prompt" && attr.Value.AsString() != "mail [redacted-email]" {
				t.Fatalf("prompt = %s", attr.Value.AsString())
			}
		}
	}
	if !names["http.server"] || !names["llm.chat"] || !names["db.query"] {
		t.Fatalf("names = %v", names)
	}
}
