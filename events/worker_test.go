package events

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func embeddedJetStream(t *testing.T) jetstream.JetStream {
	t.Helper()
	srv, err := StartEmbedded(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Shutdown)
	conn, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	js, err := jetstream.New(conn)
	if err != nil {
		t.Fatal(err)
	}
	return js
}

func waitFor(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestJetStreamPipelineDedupesProjectsAndDeadLetters(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	js := embeddedJetStream(t)
	topology, err := EnsureTopology(ctx, js)
	if err != nil {
		t.Fatal(err)
	}
	bus := NewEventBus(NewJetStreamPublisher(js))
	placed, paid := orderEvent("e1", KindPlaced), orderEvent("e2", KindPaid)
	for _, event := range []OrderEvent{paid, placed, placed} {
		if err = bus.Publish(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = js.Publish(ctx, "orders.acme.order.placed", []byte("{not json")); err != nil {
		t.Fatal(err)
	}
	stream, err := js.Stream(ctx, StreamOrders)
	if err != nil {
		t.Fatal(err)
	}
	info, err := stream.Info(ctx)
	if err != nil || info.State.Msgs != 3 {
		t.Fatalf("stream msgs = %d err=%v (duplicate publish was stored)", info.State.Msgs, err)
	}
	worker := NewWorker(js, topology.Views, slog.New(slog.NewTextHandler(io.Discard, nil)))
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx, topology.Consumer) }()
	waitFor(t, "two applied and one dead", func() bool { return worker.Applied.Load() == 2 && worker.Dead.Load() == 1 })

	entry, err := topology.Views.Get(ctx, "acme.o-1")
	if err != nil {
		t.Fatal(err)
	}
	var view OrderView
	if err = json.Unmarshal(entry.Value(), &view); err != nil || view.Status != StatusPaid || len(view.Applied) != 2 {
		t.Fatalf("view = %+v err=%v", view, err)
	}
	dead, err := js.Stream(ctx, StreamDead)
	if err != nil {
		t.Fatal(err)
	}
	deadMsg, err := dead.GetLastMsgForSubject(ctx, "dlq.orders.acme.order.placed")
	if err != nil || deadMsg.Header.Get("Foundry-Dead-Reason") != "invalid event" {
		t.Fatalf("dead letter = %+v err=%v", deadMsg, err)
	}
	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRedeliveredEventDoesNotChangeView(t *testing.T) {
	ctx := context.Background()
	js := embeddedJetStream(t)
	topology, err := EnsureTopology(ctx, js)
	if err != nil {
		t.Fatal(err)
	}
	worker := NewWorker(js, topology.Views, slog.New(slog.NewTextHandler(io.Discard, nil)))
	payload, _ := json.Marshal(orderEvent("e1", KindPlaced))
	for i := 0; i < 2; i++ {
		if err = worker.applyMessage(ctx, payload); err != nil {
			t.Fatal(err)
		}
	}
	entry, err := topology.Views.Get(ctx, "acme.o-1")
	if err != nil || entry.Revision() != 1 {
		t.Fatalf("revision = %d err=%v", entry.Revision(), err)
	}
}

func TestDecideRetriesThenDeadLetters(t *testing.T) {
	failure := errors.New("store down")
	cases := []struct {
		err       error
		delivered uint64
		want      Disposition
	}{
		{nil, 1, DispositionAck},
		{failure, 1, DispositionRetry},
		{failure, MaxDeliveries - 1, DispositionRetry},
		{failure, MaxDeliveries, DispositionDead},
		{ErrInvalid, 1, DispositionDead},
	}
	for _, tc := range cases {
		if got := Decide(tc.err, tc.delivered); got != tc.want {
			t.Fatalf("Decide(%v, %d) = %d want %d", tc.err, tc.delivered, got, tc.want)
		}
	}
}
