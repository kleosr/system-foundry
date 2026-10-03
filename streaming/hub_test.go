package streaming

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPublishReachesSubscriber(t *testing.T) {
	hub := NewHub(2, time.Second)
	sub := hub.Subscribe("acme")
	defer sub.Cancel()
	if err := hub.Publish(context.Background(), Message{TenantID: "acme", Event: "token", Data: "hi"}); err != nil {
		t.Fatal(err)
	}
	msg := <-sub.Messages
	if msg.Data != "hi" || msg.Event != "token" {
		t.Fatalf("msg = %+v", msg)
	}
}

func TestRegressionStreamLeaksOtherTenantMessages(t *testing.T) {
	hub := NewHub(2, time.Second)
	beta := hub.Subscribe("beta")
	defer beta.Cancel()
	if err := hub.Publish(context.Background(), Message{TenantID: "acme", Event: "token", Data: "secret"}); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-beta.Messages:
		t.Fatalf("beta received %+v", msg)
	default:
	}
	if err := hub.Publish(context.Background(), Message{Event: "token", Data: "x"}); err != ErrNoTenant {
		t.Fatalf("tenantless publish err = %v", err)
	}
}

func TestSlowSubscriberIsEvictedAndPublishStaysBounded(t *testing.T) {
	hub := NewHub(1, 20*time.Millisecond)
	slow := hub.Subscribe("acme")
	fast := hub.Subscribe("acme")
	defer fast.Cancel()
	go func() {
		for range fast.Messages {
		}
	}()
	if err := hub.Publish(context.Background(), Message{TenantID: "acme", Event: "token", Data: "one"}); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := hub.Publish(context.Background(), Message{TenantID: "acme", Event: "token", Data: "two"}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("publish blocked %s", elapsed)
	}
	select {
	case <-slow.Closed:
	default:
		t.Fatal("slow subscriber was not evicted")
	}
	if hub.Evicted() != 1 {
		t.Fatalf("evicted = %d", hub.Evicted())
	}
}

func TestStreamWithoutActorIsForbidden(t *testing.T) {
	rec := httptest.NewRecorder()
	NewHub(1, time.Second).ServeHTTP(rec, httptest.NewRequest("GET", "/stream", nil))
	if rec.Code != 403 {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestWriteEventFormatsSSE(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := WriteEvent(rec, Message{Event: "token", Data: "tok"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Body.String(), "event: token\ndata: tok\n\n") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}
