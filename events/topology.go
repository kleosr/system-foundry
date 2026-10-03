package events

import (
	"context"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

const (
	StreamOrders     = "ORDERS"
	StreamDead       = "ORDERS_DLQ"
	ConsumerOrders   = "orders-worker"
	BucketOrderViews = "order-views"
	MaxDeliveries    = 5
	deadPrefix       = "dlq."
)

var redeliveryBackoff = []time.Duration{time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second, time.Minute}

type Topology struct {
	Consumer jetstream.Consumer
	Views    jetstream.KeyValue
}

func EnsureTopology(ctx context.Context, js jetstream.JetStream) (Topology, error) {
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:       StreamOrders,
		Subjects:   []string{"orders.>"},
		Storage:    jetstream.FileStorage,
		Retention:  jetstream.LimitsPolicy,
		Duplicates: 2 * time.Minute,
		DenyDelete: true,
		DenyPurge:  true,
	}); err != nil {
		return Topology{}, err
	}
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:       StreamDead,
		Subjects:   []string{deadPrefix + "orders.>"},
		Storage:    jetstream.FileStorage,
		Duplicates: 2 * time.Minute,
	}); err != nil {
		return Topology{}, err
	}
	consumer, err := js.CreateOrUpdateConsumer(ctx, StreamOrders, jetstream.ConsumerConfig{
		Durable:       ConsumerOrders,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    MaxDeliveries,
		BackOff:       redeliveryBackoff,
		MaxAckPending: 256,
		FilterSubject: "orders.>",
	})
	if err != nil {
		return Topology{}, err
	}
	views, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: BucketOrderViews, History: 1})
	if err != nil {
		return Topology{}, err
	}
	return Topology{Consumer: consumer, Views: views}, nil
}
