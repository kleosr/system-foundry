package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSaturatedRequestReturns429(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		w.WriteHeader(http.StatusNoContent)
	})
	edge, err := New(Config{
		MaxInFlight:    1,
		MaxWaiting:     0,
		Handler:        handler,
		RequestTimeout: time.Second,
		Logger:         discardLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := newTestServer(edge)
	defer ts.Close()
	go func() {
		resp, reqErr := ts.Client().Get(ts.URL + "/hold")
		if reqErr == nil {
			resp.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	resp, err := ts.Client().Get(ts.URL + "/overflow")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") != "1" {
		t.Fatalf("retry-after = %q", resp.Header.Get("Retry-After"))
	}
	close(release)
}

func TestShutdownDrainsInFlightRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})
	edge, err := New(Config{
		MaxInFlight:    4,
		MaxWaiting:     4,
		Handler:        handler,
		RequestTimeout: 5 * time.Second,
		WriteTimeout:   0,
		Logger:         discardLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- edge.Serve(ln) }()
	client := &http.Client{Timeout: 5 * time.Second}
	respCh := make(chan *http.Response, 1)
	errCh := make(chan error, 1)
	go func() {
		resp, reqErr := client.Get("http://" + ln.Addr().String() + "/hold")
		if reqErr != nil {
			errCh <- reqErr
			return
		}
		respCh <- resp
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- edge.Shutdown(context.Background())
	}()
	select {
	case err = <-shutdownDone:
		t.Fatal("shutdown finished before the request")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case err = <-shutdownDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not finish")
	}
	select {
	case resp := <-respCh:
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("status = %d", resp.StatusCode)
		}
	case err = <-errCh:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("response missing")
	}
	if err = <-serveDone; err != nil {
		t.Fatal(err)
	}
	_, err = client.Get("http://" + ln.Addr().String() + "/again")
	if err == nil {
		t.Fatal("listener still accepted a request")
	}
}

func TestRegressionOpenStreamBlocksShutdown(t *testing.T) {
	started := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	})
	edge, err := New(Config{
		MaxInFlight:  1,
		MaxWaiting:   0,
		Handler:      handler,
		WriteTimeout: 0,
		LongLived: func(r *http.Request) bool {
			return r.URL.Path == "/stream"
		},
		Logger: discardLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go edge.Serve(ln)
	go http.Get("http://" + ln.Addr().String() + "/stream")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("stream did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	startedAt := time.Now()
	if err = edge.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if time.Since(startedAt) > 500*time.Millisecond {
		t.Fatalf("shutdown took %s", time.Since(startedAt))
	}
}

func TestLiveIgnoresReadyFailure(t *testing.T) {
	edge, err := New(Config{
		MaxInFlight: 2,
		MaxWaiting:  0,
		Ready: func(context.Context) error {
			return errors.New("db down")
		},
		Logger: discardLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := newTestServer(edge)
	defer ts.Close()
	live, err := ts.Client().Get(ts.URL + "/livez")
	if err != nil {
		t.Fatal(err)
	}
	live.Body.Close()
	if live.StatusCode != http.StatusOK {
		t.Fatalf("live = %d", live.StatusCode)
	}
	ready, err := ts.Client().Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	ready.Body.Close()
	if ready.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("ready = %d", ready.StatusCode)
	}
}

func TestNewRejectsZeroInFlight(t *testing.T) {
	_, err := New(Config{MaxInFlight: 0, Logger: discardLogger()})
	if err == nil {
		t.Fatal("expected error")
	}
}

type testServer struct {
	URL    string
	client *http.Client
	close  func()
}

func (s testServer) Client() *http.Client { return s.client }
func (s testServer) Close()               { s.close() }

func newTestServer(edge *Edge) testServer {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	go edge.Serve(ln)
	return testServer{
		URL:    "http://" + ln.Addr().String(),
		client: &http.Client{Timeout: 2 * time.Second},
		close: func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = edge.Shutdown(ctx)
		},
	}
}
