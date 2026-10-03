package server

import (
	"context"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestTenThousandConcurrentConnections(t *testing.T) {
	release := make(chan struct{})
	var entered sync.WaitGroup
	entered.Add(10000)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered.Done()
		<-release
		w.WriteHeader(http.StatusNoContent)
	})
	edge, err := New(Config{
		MaxInFlight:    10000,
		MaxWaiting:     0,
		Handler:        handler,
		RequestTimeout: 30 * time.Second,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   0,
		IdleTimeout:    30 * time.Second,
		Logger:         discardLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go edge.Serve(ln)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = edge.Shutdown(ctx)
	}()
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        20000,
			MaxIdleConnsPerHost: 20000,
			IdleConnTimeout:     30 * time.Second,
		},
	}
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(10000)
	for i := 0; i < 10000; i++ {
		go func() {
			defer wg.Done()
			resp, reqErr := client.Get("http://" + ln.Addr().String() + "/hold")
			if reqErr != nil {
				select {
				case errCh <- reqErr:
				default:
				}
				return
			}
			resp.Body.Close()
		}()
	}
	done := make(chan struct{})
	go func() {
		entered.Wait()
		close(done)
	}()
	select {
	case <-done:
	case err = <-errCh:
		close(release)
		t.Fatal(err)
	case <-time.After(20 * time.Second):
		close(release)
		t.Fatal("did not reach 10000 in-flight requests")
	}
	close(release)
	wg.Wait()
}
