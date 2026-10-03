package server

import (
	"net/http"
	"testing"
	"time"
)

func TestCompleteRouteGetsFailoverDeadline(t *testing.T) {
	var fast, slow time.Duration
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, ok := r.Context().Deadline()
		if !ok {
			t.Error("missing deadline")
			return
		}
		left := time.Until(deadline)
		if r.URL.Path == "/v1/complete" {
			slow = left
		} else {
			fast = left
		}
		w.WriteHeader(http.StatusNoContent)
	})
	edge, err := New(Config{
		MaxInFlight:    2,
		MaxWaiting:     0,
		Handler:        handler,
		RequestTimeout: 40 * time.Millisecond,
		TimeoutFor: func(r *http.Request) time.Duration {
			if r.URL.Path == "/v1/complete" {
				return 200 * time.Millisecond
			}
			return 0
		},
		Logger: discardLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := newTestServer(edge)
	defer ts.Close()
	for _, path := range []string{"/v1/complete", "/other"} {
		res, err := ts.Client().Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
	}
	if slow < 150*time.Millisecond {
		t.Fatalf("complete deadline %s", slow)
	}
	if fast > 80*time.Millisecond {
		t.Fatalf("default deadline %s", fast)
	}
}

func TestStreamDoesNotHoldInFlightSlot(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stream" {
			close(started)
			<-release
		}
		w.WriteHeader(http.StatusNoContent)
	})
	edge, err := New(Config{
		MaxInFlight:    1,
		MaxWaiting:     0,
		Handler:        handler,
		RequestTimeout: time.Second,
		LongLived: func(r *http.Request) bool {
			return r.URL.Path == "/stream"
		},
		Logger: discardLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := newTestServer(edge)
	defer ts.Close()
	go ts.Client().Get(ts.URL + "/stream")
	<-started
	res, err := ts.Client().Get(ts.URL + "/other")
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d", res.StatusCode)
	}
}
