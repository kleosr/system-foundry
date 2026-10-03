package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.temporal.io/sdk/client"
	temporallog "go.temporal.io/sdk/log"

	"systemsfoundry/events"
	"systemsfoundry/finops"
	"systemsfoundry/gateway"
	"systemsfoundry/observability"
	"systemsfoundry/reconciliation"
	"systemsfoundry/streaming"
	"systemsfoundry/workflows"
)

func buildModels(cfg modelConfig) (gateway.Model, gateway.Model, gateway.Embedder) {
	if cfg.BaseURL == "" {
		return echoModel{name: cfg.Primary, gap: 40 * time.Millisecond}, echoModel{name: cfg.Cheap, gap: 40 * time.Millisecond}, nil
	}
	httpClient := &http.Client{Timeout: 60 * time.Second}
	primary := &gateway.OpenAICompatible{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Primary, Client: httpClient}
	cheap := &gateway.OpenAICompatible{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Cheap, Client: httpClient}
	if cfg.Embed == "" {
		return primary, cheap, nil
	}
	return primary, cheap, &gateway.OpenAICompatible{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Embed, Client: &http.Client{Timeout: 5 * time.Second}}
}

func newTracerProvider(cfg langfuseConfig) *sdktrace.TracerProvider {
	options := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", "systemsfoundry"))),
	}
	if cfg.Public != "" && cfg.Secret != "" {
		exporter := &observability.Langfuse{
			Endpoint: strings.TrimRight(cfg.Host, "/") + "/api/public/ingestion",
			Public:   cfg.Public,
			Secret:   cfg.Secret,
			Client:   &http.Client{Timeout: 5 * time.Second},
		}
		options = append(options, sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(2*time.Second)))
	}
	return sdktrace.NewTracerProvider(options...)
}

func publishJSON(hub *streaming.Hub, tenantID string, change any) {
	payload, err := json.Marshal(change)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = hub.Publish(ctx, streaming.Message{TenantID: tenantID, Event: "change", Data: string(payload)})
}

func costAlerts(hub *streaming.Hub, logger *slog.Logger) func(finops.Anomaly) {
	return func(anomaly finops.Anomaly) {
		logger.Warn("finops", "event", "cost_anomaly", "tenant_id", anomaly.TenantID,
			"hour_micros", anomaly.HourMicros, "endpoint", anomaly.TriggeredBy)
		publishJSON(hub, anomaly.TenantID, map[string]any{"type": "cost_anomaly", "hourMicros": anomaly.HourMicros})
	}
}

func orderChanges(hub *streaming.Hub) func(events.OrderView) {
	return func(view events.OrderView) {
		publishJSON(hub, view.TenantID, map[string]any{"type": "order", "orderId": view.OrderID, "status": view.Status})
	}
}

func newWebhooks(secret string, bus *events.EventBus) *reconciliation.Processor {
	if secret == "" {
		return nil
	}
	return reconciliation.NewProcessor(reconciliation.NewMemoryStore(), func(ctx context.Context, payload []byte) error {
		var event events.OrderEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			return events.ErrInvalid
		}
		return bus.Publish(ctx, event)
	}, []byte(secret))
}

func dialOnboarding(ctx context.Context, address string, logger *slog.Logger) (*workflows.Onboarding, func(), error) {
	if address == "" {
		return nil, func() {}, nil
	}
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	temporalClient, err := client.DialContext(dialCtx, client.Options{HostPort: address, Logger: temporallog.NewStructuredLogger(logger)})
	if err != nil {
		return nil, nil, err
	}
	return workflows.NewOnboarding(temporalClient), temporalClient.Close, nil
}
