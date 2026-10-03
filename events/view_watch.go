package events

import (
	"context"
	"encoding/json"

	"github.com/nats-io/nats.go/jetstream"
)

func WatchViews(ctx context.Context, views jetstream.KeyValue, changed func(OrderView)) error {
	watcher, err := views.WatchAll(ctx, jetstream.UpdatesOnly())
	if err != nil {
		return err
	}
	defer func() { _ = watcher.Stop() }()
	for {
		select {
		case <-ctx.Done():
			return nil
		case entry, open := <-watcher.Updates():
			if !open {
				return nil
			}
			if entry == nil || entry.Operation() != jetstream.KeyValuePut {
				continue
			}
			var view OrderView
			if json.Unmarshal(entry.Value(), &view) == nil {
				changed(view)
			}
		}
	}
}
