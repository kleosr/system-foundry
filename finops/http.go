package finops

import (
	"encoding/json"
	"net/http"
	"time"

	"systemsfoundry/auth"
)

type rollupView struct {
	Rows  []Rollup `json:"rows"`
	Alert bool     `json:"alert"`
}

func (l *Ledger) ServeCosts(w http.ResponseWriter, r *http.Request) {
	actor, ok := auth.ActorFrom(r.Context())
	if !ok || !auth.Can(actor, auth.ActionRead, actor.TenantID) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"FORBIDDEN","message":"Not allowed."}}`))
		return
	}
	tenantID := actor.TenantID
	now := time.Now().UTC()
	if l.now != nil {
		now = l.now()
	}
	rows := l.Rollup(now.Add(-24*time.Hour), now.Add(time.Hour))
	alert, _ := l.HourAnomaly(tenantID, now)
	filtered := make([]Rollup, 0)
	for _, row := range rows {
		if row.TenantID == tenantID {
			filtered = append(filtered, row)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": rollupView{Rows: filtered, Alert: alert}})
}
