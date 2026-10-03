package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func providerStub(t *testing.T, primaryStatus int) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var primaryCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case r.URL.Path == "/v1/embeddings":
			_, _ = w.Write([]byte(`{"data":[{"embedding":[0.6,0.8]}]}`))
		case body.Model == "big":
			primaryCalls.Add(1)
			w.WriteHeader(primaryStatus)
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"from big"}}],"usage":{"total_tokens":12}}`))
		default:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"from small"}}],"usage":{"total_tokens":7}}`))
		}
	}))
	t.Cleanup(server.Close)
	return server, &primaryCalls
}

func TestOpenAICompatibleFailsOverOn503AndCachesByEmbedding(t *testing.T) {
	server, primaryCalls := providerStub(t, http.StatusServiceUnavailable)
	base := server.URL + "/v1"
	primary := &OpenAICompatible{BaseURL: base, APIKey: "test-key", Model: "big"}
	cheap := &OpenAICompatible{BaseURL: base, APIKey: "test-key", Model: "small"}
	proxy := NewProxy(primary, cheap, NewCache(10), NewBudget(10_000), nil)
	proxy.UseEmbedder(&OpenAICompatible{BaseURL: base, APIKey: "test-key", Model: "embed"}, 0.95)
	first := proxy.Complete(context.Background(), Request{TenantID: "acme", Prompt: "summarize my order"})
	if !first.OK || first.Value.Model != "small" || first.Value.Text != "from small" || first.Value.Tokens != 7 {
		t.Fatalf("first = %+v %+v", first.Value, first.Error)
	}
	second := proxy.Complete(context.Background(), Request{TenantID: "acme", Prompt: "please summarize my order"})
	if !second.OK || !second.Value.CacheHit || primaryCalls.Load() != 1 {
		t.Fatalf("second = %+v primaryCalls=%d", second.Value, primaryCalls.Load())
	}
}

func sseStub(t *testing.T, body string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func TestStreamEmitsTokensInOrder(t *testing.T) {
	base := sseStub(t, "data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"}}]}\n\n"+
		"data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n"+
		"data: {\"choices\":[],\"usage\":{\"total_tokens\":9}}\n\ndata: [DONE]\n\n")
	model := &OpenAICompatible{BaseURL: base, Model: "big"}
	proxy := NewProxy(model, model, NewCache(10), NewBudget(10_000), nil)
	var seen []string
	outcome := proxy.Complete(context.Background(), Request{TenantID: "acme", Prompt: "hi", OnToken: func(token string) { seen = append(seen, token) }})
	if !outcome.OK || outcome.Value.Text != "Hello" || outcome.Value.Tokens != 9 || len(seen) != 2 || seen[0] != "Hel" {
		t.Fatalf("outcome=%+v seen=%v", outcome.Value, seen)
	}
}

func TestStreamFailureAfterFirstTokenDoesNotFailOver(t *testing.T) {
	broken := &OpenAICompatible{BaseURL: sseStub(t, "data: {\"choices\":[{\"delta\":{\"content\":\"par\"}}]}\n\ndata: {broken\n\n"), Model: "big"}
	cheap := &scriptModel{name: "cheap", text: "x", tokens: 1}
	proxy := NewProxy(broken, cheap, NewCache(10), NewBudget(10_000), nil)
	outcome := proxy.Complete(context.Background(), Request{TenantID: "acme", Prompt: "hi", OnToken: func(string) {}})
	if outcome.OK || cheap.calls != 0 {
		t.Fatalf("outcome=%+v cheapCalls=%d", outcome.Value, cheap.calls)
	}
}

func TestOpenAICompatibleDoesNotFailOverOn400(t *testing.T) {
	server, _ := providerStub(t, http.StatusBadRequest)
	base := server.URL + "/v1"
	proxy := NewProxy(&OpenAICompatible{BaseURL: base, APIKey: "test-key", Model: "big"},
		&OpenAICompatible{BaseURL: base, APIKey: "test-key", Model: "small"}, NewCache(10), NewBudget(10_000), nil)
	outcome := proxy.Complete(context.Background(), Request{TenantID: "acme", Prompt: "hi"})
	if outcome.OK {
		t.Fatalf("400 failed over: %+v", outcome.Value)
	}
}
