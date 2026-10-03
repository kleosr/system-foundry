package observability

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestLangfuseBodyKeepsRedactedPrompt(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
	pipeline := NewPipeline(provider)
	_, endLLM := pipeline.StartLLM(context.Background(), "primary", "ada@example.com")
	endLLM(nil)
	payload, err := EncodeIngestion(recorder.Ended())
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	if strings.Contains(text, "ada@example.com") || !strings.Contains(text, "[redacted-email]") {
		t.Fatalf("payload = %s", text)
	}
	if !strings.Contains(text, "llm.chat") {
		t.Fatal(text)
	}
}

func TestLangfuseTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	exporter := &Langfuse{
		Endpoint: server.URL,
		Public:   "pk",
		Secret:   "sk",
		Client:   &http.Client{Timeout: 30 * time.Millisecond},
	}
	recorder := tracetest.NewSpanRecorder()
	provider := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
	_, end := NewPipeline(provider).StartHTTP(context.Background(), "GET", "/livez")
	end(200)
	err := exporter.ExportSpans(context.Background(), recorder.Ended())
	if err == nil {
		t.Fatal("expected timeout")
	}
	_, _ = io.Copy(io.Discard, strings.NewReader(""))
}
