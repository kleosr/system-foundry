package chaos

import (
	"encoding/json"
	"net/http"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Flush() {
	if flusher, ok := s.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (s *statusRecorder) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}

type Board struct {
	objective float64
	window    *Window
}

func NewBoard(now func() time.Time) *Board {
	return &Board{objective: 0.999, window: NewWindow(now)}
}

func (b *Board) Observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(observed, r)
		b.window.Observe(observed.status >= 500)
	})
}

type burnView struct {
	ShortRate  float64 `json:"shortRate"`
	LongRate   float64 `json:"longRate"`
	Page       bool    `json:"page"`
	ShortTotal int64   `json:"shortTotal"`
	LongTotal  int64   `json:"longTotal"`
}

func (b *Board) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/slo/view" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(sloPage))
		return
	}
	short := b.window.Sample(5 * time.Minute)
	long := b.window.Sample(time.Hour)
	view := burnView{
		ShortRate:  BurnRate(short, b.objective),
		LongRate:   BurnRate(long, b.objective),
		ShortTotal: short.Total,
		LongTotal:  long.Total,
	}
	view.Page = ShouldPage(view.ShortRate, view.LongRate)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": view})
}

const sloPage = `<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>SLO burn</title></head>
<body>
<p id="burn">waiting</p>
<script>
async function refreshBurn() {
  const response = await fetch("/slo");
  const body = await response.json();
  const view = body.data;
  document.querySelector("#burn").textContent =
    "short " + view.shortRate + " long " + view.longRate + " page " + view.page;
}
refreshBurn();
setInterval(refreshBurn, 2000);
</script>
</body>
</html>
`
