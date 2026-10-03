package chaos

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

type Rule struct {
	LatencyMs  int     `json:"latencyMs"`
	Kill       bool    `json:"kill"`
	Partition  bool    `json:"partition"`
	Ratio      float64 `json:"ratio"`
	PathPrefix string  `json:"pathPrefix"`
}

func (r Rule) valid() bool {
	return r.Ratio > 0 && r.Ratio <= 1 && r.LatencyMs >= 0 && r.LatencyMs <= 30000 && strings.HasPrefix(r.PathPrefix, "/")
}

type Injector struct {
	rule   atomic.Pointer[Rule]
	chance func() float64
}

func NewInjector() *Injector {
	return &Injector{chance: rand.Float64}
}

func (i *Injector) Set(rule Rule) bool {
	if !rule.valid() {
		return false
	}
	i.rule.Store(&rule)
	return true
}

func (i *Injector) Clear() {
	i.rule.Store(nil)
}

func (i *Injector) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rule := i.rule.Load()
		if rule == nil || !strings.HasPrefix(r.URL.Path, rule.PathPrefix) || i.chance() >= rule.Ratio {
			next.ServeHTTP(w, r)
			return
		}
		plan := Plan{Latency: time.Duration(rule.LatencyMs) * time.Millisecond, Kill: rule.Kill, Partition: rule.Partition}
		err := Apply(r.Context(), plan, func(ctx context.Context) error {
			next.ServeHTTP(w, r.WithContext(ctx))
			return nil
		})
		switch {
		case err == nil:
		case errors.Is(err, ErrReplicaKilled):
			w.Header().Set("Connection", "close")
			writeFault(w, http.StatusServiceUnavailable, "Injected fault: replica killed.")
		case errors.Is(err, ErrPartition):
			writeFault(w, http.StatusGatewayTimeout, "Injected fault: network partition.")
		default:
			writeFault(w, http.StatusServiceUnavailable, "Request cancelled during injected latency.")
		}
	})
}

func (i *Injector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPut:
		var rule Rule
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&rule); err != nil || !i.Set(rule) {
			writeFault(w, http.StatusUnprocessableEntity, "Rule needs ratio in (0,1], latencyMs in [0,30000], and a pathPrefix that starts with /.")
			return
		}
	case http.MethodDelete:
		i.Clear()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": i.rule.Load()})
}

func writeFault(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": map[string]string{"code": "UNAVAILABLE", "message": message}})
}
