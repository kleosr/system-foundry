package events

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

var errViewContention = errors.New("order view changed during apply")

type Disposition int

const (
	DispositionAck Disposition = iota
	DispositionRetry
	DispositionDead
)

func Decide(applyErr error, delivered uint64) Disposition {
	switch {
	case applyErr == nil:
		return DispositionAck
	case errors.Is(applyErr, ErrInvalid):
		return DispositionDead
	case delivered >= MaxDeliveries:
		return DispositionDead
	default:
		return DispositionRetry
	}
}

type Worker struct {
	js      jetstream.JetStream
	views   jetstream.KeyValue
	logger  *slog.Logger
	Applied atomic.Int64
	Dead    atomic.Int64
}

func NewWorker(js jetstream.JetStream, views jetstream.KeyValue, logger *slog.Logger) *Worker {
	return &Worker{js: js, views: views, logger: logger}
}

func (w *Worker) Run(ctx context.Context, consumer jetstream.Consumer) error {
	consumeCtx, err := consumer.Consume(w.Handle)
	if err != nil {
		return err
	}
	<-ctx.Done()
	consumeCtx.Drain()
	<-consumeCtx.Closed()
	return nil
}

func (w *Worker) Handle(msg jetstream.Msg) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var delivered uint64 = 1
	if meta, err := msg.Metadata(); err == nil {
		delivered = meta.NumDelivered
	}
	applyErr := w.applyMessage(ctx, msg.Data())
	switch Decide(applyErr, delivered) {
	case DispositionAck:
		if err := msg.DoubleAck(ctx); err != nil {
			w.logger.Warn("orders", "event", "ack_failed", "subject", msg.Subject())
			return
		}
		w.Applied.Add(1)
	case DispositionRetry:
		_ = msg.NakWithDelay(redeliveryBackoff[min(int(delivered), len(redeliveryBackoff))-1])
	case DispositionDead:
		w.deadLetter(ctx, msg, applyErr)
	}
}

func (w *Worker) applyMessage(ctx context.Context, data []byte) error {
	var event OrderEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return ErrInvalid
	}
	if err := event.Validate(); err != nil {
		return err
	}
	for attempt := 0; attempt < 3; attempt++ {
		err := w.applyOnce(ctx, event)
		if !errors.Is(err, errViewContention) {
			return err
		}
	}
	return errViewContention
}

func (w *Worker) applyOnce(ctx context.Context, event OrderEvent) error {
	key := ViewKey(event.TenantID, event.OrderID)
	var view OrderView
	var revision uint64
	entry, err := w.views.Get(ctx, key)
	switch {
	case errors.Is(err, jetstream.ErrKeyNotFound):
	case err != nil:
		return err
	default:
		revision = entry.Revision()
		if err = json.Unmarshal(entry.Value(), &view); err != nil {
			return err
		}
	}
	next, changed := Fold(view, event)
	if !changed {
		return nil
	}
	payload, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if revision == 0 {
		_, err = w.views.Create(ctx, key, payload)
	} else {
		_, err = w.views.Update(ctx, key, payload, revision)
	}
	var apiErr *jetstream.APIError
	if errors.Is(err, jetstream.ErrKeyExists) || (errors.As(err, &apiErr) && apiErr.ErrorCode == jetstream.JSErrCodeStreamWrongLastSequence) {
		return errViewContention
	}
	return err
}

func ViewKey(tenantID, orderID string) string {
	return tenantID + "." + orderID
}

func (w *Worker) deadLetter(ctx context.Context, msg jetstream.Msg, cause error) {
	reason := "apply failed"
	if errors.Is(cause, ErrInvalid) {
		reason = "invalid event"
	}
	dead := nats.NewMsg(deadPrefix + msg.Subject())
	dead.Data = msg.Data()
	dead.Header.Set("Foundry-Dead-Reason", reason)
	messageID := msg.Headers().Get(nats.MsgIdHdr)
	if meta, err := msg.Metadata(); err == nil && messageID == "" {
		messageID = "seq-" + strconv.FormatUint(meta.Sequence.Stream, 10)
	}
	if _, err := w.js.PublishMsg(ctx, dead, jetstream.WithMsgID(messageID)); err != nil {
		w.logger.Error("orders", "event", "dead_letter_failed", "subject", msg.Subject())
		_ = msg.Nak()
		return
	}
	_ = msg.TermWithReason(reason)
	w.Dead.Add(1)
	w.logger.Warn("orders", "event", "dead_lettered", "subject", msg.Subject(), "reason", reason)
}
