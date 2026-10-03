package events

import (
	"context"
	"encoding/json"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type Publisher interface {
	Publish(ctx context.Context, subject, messageID string, payload []byte) error
}

type EventBus struct {
	publisher Publisher
}

func NewEventBus(publisher Publisher) *EventBus {
	return &EventBus{publisher: publisher}
}

func (b *EventBus) Publish(ctx context.Context, event OrderEvent) error {
	if err := event.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return b.publisher.Publish(ctx, Subject(event.TenantID, event.Kind), event.ID, payload)
}

type JetStreamPublisher struct {
	stream jetstream.JetStream
}

func NewJetStreamPublisher(stream jetstream.JetStream) JetStreamPublisher {
	return JetStreamPublisher{stream: stream}
}

func DialJetStream(url string, timeout time.Duration) (*nats.Conn, jetstream.JetStream, error) {
	conn, err := nats.Connect(url, nats.Timeout(timeout), nats.Name("systemsfoundry"))
	if err != nil {
		return nil, nil, err
	}
	stream, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	return conn, stream, nil
}

func (p JetStreamPublisher) Publish(ctx context.Context, subject, messageID string, payload []byte) error {
	_, err := p.stream.Publish(ctx, subject, payload, jetstream.WithMsgID(messageID))
	return err
}
