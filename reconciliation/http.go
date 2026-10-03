package reconciliation

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"systemsfoundry/platform"
)

const maxWebhookBody = 64 << 10

type deadView struct {
	Provider  string `json:"provider"`
	EventID   string `json:"eventId"`
	Attempts  int    `json:"attempts"`
	LastError string `json:"lastError"`
}

func (p *Processor) ServeWebhook(w http.ResponseWriter, r *http.Request) {
	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBody))
	if err != nil {
		_ = platform.WriteResult(w, platform.FailFields[Status]("Body is too large.", map[string][]string{"body": {"max 65536 bytes"}}))
		return
	}
	outcome := p.Accept(r.Context(), r.PathValue("provider"), r.Header.Get("X-Webhook-Id"), payload, r.Header.Get("X-Webhook-Signature"))
	if !outcome.OK {
		_ = platform.WriteResult(w, outcome)
		return
	}
	_ = platform.WriteStatus(w, http.StatusAccepted, outcome)
}

func (p *Processor) ServeDead(w http.ResponseWriter, r *http.Request) {
	rows := p.store.ListDead(r.Context(), 100)
	views := make([]deadView, 0, len(rows))
	for _, row := range rows {
		views = append(views, deadView{Provider: row.Provider, EventID: row.EventID, Attempts: row.Attempts, LastError: row.LastError})
	}
	_ = platform.WriteResult(w, platform.Succeed(views))
}

func (p *Processor) ServeReplay(w http.ResponseWriter, r *http.Request) {
	_ = platform.WriteResult(w, p.Replay(r.Context(), r.PathValue("provider"), r.PathValue("id")))
}

func (p *Processor) RunDrain(ctx context.Context, every time.Duration, batch int, logger *slog.Logger) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			applied, err := p.Drain(ctx, batch)
			if err != nil {
				logger.Error("webhooks", "event", "drain_failed", "message", err.Error())
				continue
			}
			if applied > 0 {
				logger.Info("webhooks", "event", "drained", "applied", applied)
			}
		}
	}
}
