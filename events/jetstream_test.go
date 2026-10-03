package events

import (
	"context"
	"testing"
	"time"
)

type recordingPublisher struct {
	subject string
	id      string
	payload []byte
}

func (r *recordingPublisher) Publish(_ context.Context, subject, messageID string, payload []byte) error {
	r.subject = subject
	r.id = messageID
	r.payload = append([]byte(nil), payload...)
	return nil
}

func TestEventBusPublishesImmutableSubject(t *testing.T) {
	recorder := &recordingPublisher{}
	bus := NewEventBus(recorder)
	event := sample("evt-9")
	if err := bus.Publish(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if recorder.subject != "orders.acme.order.placed" || recorder.id != "evt-9" {
		t.Fatalf("subject=%s id=%s", recorder.subject, recorder.id)
	}
	event.Payload[0] = 'X'
	if recorder.payload[0] == 'X' {
		t.Fatal("publisher kept a live buffer")
	}
}

func TestEventBusRejectsOversizedPayload(t *testing.T) {
	bus := NewEventBus(&recordingPublisher{})
	event := sample("evt-9")
	event.Payload = make([]byte, MaxPayload+1)
	event.Occurred = time.Unix(1_700_000_000, 0).UTC()
	if err := bus.Publish(context.Background(), event); err == nil {
		t.Fatal("expected invalid event")
	}
}
