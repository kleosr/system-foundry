package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type Langfuse struct {
	Endpoint string
	Public   string
	Secret   string
	Client   *http.Client
}

type ingestionBatch struct {
	Batch []ingestionItem `json:"batch"`
}

type ingestionItem struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Timestamp string         `json:"timestamp"`
	Body      map[string]any `json:"body"`
}

func (l *Langfuse) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if l == nil || l.Public == "" || l.Secret == "" || len(spans) == 0 {
		return nil
	}
	payload, err := EncodeIngestion(spans)
	if err != nil {
		return err
	}
	client := l.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.SetBasicAuth(l.Public, l.Secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return errStatus(resp.StatusCode)
	}
	return nil
}

func (l *Langfuse) Shutdown(context.Context) error {
	return nil
}

func EncodeIngestion(spans []sdktrace.ReadOnlySpan) ([]byte, error) {
	batch := ingestionBatch{Batch: make([]ingestionItem, 0, len(spans)+1)}
	for _, span := range spans {
		spanID := span.SpanContext().SpanID().String()
		traceID := span.SpanContext().TraceID().String()
		started := span.StartTime().UTC().Format(time.RFC3339Nano)
		metadata := map[string]string{}
		body := map[string]any{
			"id":        spanID,
			"traceId":   traceID,
			"name":      span.Name(),
			"startTime": started,
			"endTime":   span.EndTime().UTC().Format(time.RFC3339Nano),
			"metadata":  metadata,
		}
		eventType := "span-create"
		for _, attr := range span.Attributes() {
			switch attr.Key {
			case "llm.prompt":
				body["input"] = attr.Value.AsString()
			case "llm.model":
				body["model"] = attr.Value.AsString()
				eventType = "generation-create"
			default:
				metadata[string(attr.Key)] = attr.Value.Emit()
			}
		}
		if span.Parent().IsValid() {
			body["parentObservationId"] = span.Parent().SpanID().String()
		} else {
			batch.Batch = append(batch.Batch, ingestionItem{
				ID: "trace-" + traceID, Type: "trace-create", Timestamp: started,
				Body: map[string]any{"id": traceID, "name": span.Name(), "metadata": metadata},
			})
		}
		batch.Batch = append(batch.Batch, ingestionItem{ID: spanID, Type: eventType, Timestamp: started, Body: body})
	}
	return json.Marshal(batch)
}

type errStatus int

func (e errStatus) Error() string { return "langfuse export failed" }
