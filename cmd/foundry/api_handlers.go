package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"systemsfoundry/auth"
	"systemsfoundry/events"
	"systemsfoundry/finops"
	"systemsfoundry/gateway"
	"systemsfoundry/observability"
	"systemsfoundry/platform"
	"systemsfoundry/search"
	"systemsfoundry/streaming"
)

var idempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

type api struct {
	proxy   *gateway.Proxy
	catalog *search.Service
	hub     *streaming.Hub
	alerter *finops.Alerter
	prices  finops.PriceTable
	bus     *events.EventBus
	views   jetstream.KeyValue
	tracing *observability.Pipeline
	logger  *slog.Logger
}

type acceptedEvent struct {
	EventID string `json:"eventId"`
}

func decodeBody(w http.ResponseWriter, r *http.Request, limit int64, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		_ = platform.WriteResult(w, platform.FailFields[bool]("Invalid JSON body.", map[string][]string{"body": {err.Error()}}))
		return false
	}
	return true
}

func actorWith(w http.ResponseWriter, r *http.Request, action string) (platform.Actor, bool) {
	actor, ok := auth.ActorFrom(r.Context())
	if !ok || !auth.Can(actor, action, actor.TenantID) {
		_ = platform.WriteResult(w, platform.Fail[bool](platform.CodeForbidden, "Not allowed."))
		return platform.Actor{}, false
	}
	return actor, true
}

func (a *api) complete(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorWith(w, r, auth.ActionRead)
	var body struct {
		Prompt    string `json:"prompt"`
		MaxTokens int    `json:"maxTokens"`
	}
	if !ok || !decodeBody(w, r, 64<<10, &body) {
		return
	}
	ctx, endLLM := a.tracing.StartLLM(r.Context(), "gateway", body.Prompt)
	outcome := a.proxy.Complete(ctx, gateway.Request{
		TenantID: actor.TenantID, Prompt: body.Prompt, MaxTokens: body.MaxTokens,
		OnToken: func(token string) {
			_ = a.hub.Publish(ctx, streaming.Message{TenantID: actor.TenantID, Event: "token", Data: token})
		},
	})
	if !outcome.OK {
		endLLM(errors.New(outcome.Error.Message))
		_ = platform.WriteResult(w, outcome)
		return
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.String("llm.model", outcome.Value.Model),
		attribute.Int("llm.tokens", outcome.Value.Tokens),
		attribute.Bool("llm.cache_hit", outcome.Value.CacheHit),
	)
	endLLM(nil)
	if outcome.Value.CacheHit {
		_ = a.hub.Publish(ctx, streaming.Message{TenantID: actor.TenantID, Event: "token", Data: outcome.Value.Text})
	}
	publishJSON(a.hub, actor.TenantID, map[string]any{"type": "completion", "model": outcome.Value.Model, "tokens": outcome.Value.Tokens, "cacheHit": outcome.Value.CacheHit})
	cost := finops.Event{
		TenantID: actor.TenantID, Endpoint: "/v1/complete", Model: outcome.Value.Model,
		USDMicros: a.prices.CostMicros(outcome.Value.Model, outcome.Value.Tokens), At: time.Now().UTC(),
	}
	if err := a.alerter.Record(cost); err != nil {
		a.logger.Error("finops", "event", "cost_record_failed", "message", err.Error())
	}
	_ = platform.WriteResult(w, outcome)
}

func (a *api) indexDocument(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorWith(w, r, auth.ActionWrite)
	var body struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if !ok || !decodeBody(w, r, 256<<10, &body) {
		return
	}
	outcome := a.catalog.Index(r.Context(), actor, body.ID, body.Title, body.Body)
	if outcome.OK {
		publishJSON(a.hub, actor.TenantID, map[string]any{"type": "document", "id": outcome.Value.ID})
	}
	_ = platform.WriteResult(w, outcome)
}

func (a *api) searchDocuments(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorWith(w, r, auth.ActionRead)
	if !ok {
		return
	}
	query := r.URL.Query()
	limit, _ := strconv.Atoi(query.Get("limit"))
	_ = platform.WriteResult(w, a.catalog.Find(r.Context(), actor, query.Get("q"), query.Get("cursor"), limit))
}

func (a *api) appendOrderEvent(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorWith(w, r, auth.ActionWrite)
	var body struct {
		Kind events.Kind `json:"kind"`
	}
	if !ok || !decodeBody(w, r, 4<<10, &body) {
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if !idempotencyPattern.MatchString(key) {
		_ = platform.WriteResult(w, platform.FailFields[acceptedEvent]("Idempotency-Key is required.", map[string][]string{"Idempotency-Key": {"8 to 64 of A-Z a-z 0-9 _ -"}}))
		return
	}
	event := events.OrderEvent{
		ID: actor.TenantID + "-" + key, TenantID: actor.TenantID, OrderID: r.PathValue("order"),
		Kind: body.Kind, Occurred: time.Now().UTC(),
	}
	err := a.bus.Publish(r.Context(), event)
	if errors.Is(err, events.ErrInvalid) {
		_ = platform.WriteResult(w, platform.FailFields[acceptedEvent]("Invalid order event.", map[string][]string{"kind": {"order.placed, order.paid, or order.cancelled"}}))
		return
	}
	if err != nil {
		_ = platform.WriteResult(w, platform.Fail[acceptedEvent](platform.CodeUnavailable, "Event log unavailable."))
		return
	}
	_ = platform.WriteStatus(w, http.StatusAccepted, platform.Succeed(acceptedEvent{EventID: event.ID}))
}

func (a *api) readOrder(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorWith(w, r, auth.ActionRead)
	if !ok {
		return
	}
	entry, err := a.views.Get(r.Context(), events.ViewKey(actor.TenantID, r.PathValue("order")))
	if errors.Is(err, jetstream.ErrKeyNotFound) || errors.Is(err, jetstream.ErrInvalidKey) {
		_ = platform.WriteResult(w, platform.Fail[events.OrderView](platform.CodeNotFound, "Order not found."))
		return
	}
	var view events.OrderView
	if err == nil {
		err = json.Unmarshal(entry.Value(), &view)
	}
	if err != nil {
		_ = platform.WriteResult(w, platform.Fail[events.OrderView](platform.CodeUnavailable, "Order view unavailable."))
		return
	}
	_ = platform.WriteResult(w, platform.Succeed(view))
}
