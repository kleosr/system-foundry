package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type Config struct {
	Addr              string
	MaxInFlight       int
	MaxWaiting        int
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	RequestTimeout    time.Duration
	TimeoutFor        func(*http.Request) time.Duration
	LongLived         func(*http.Request) bool
	Ready             func(context.Context) error
	Handler           http.Handler
	Logger            *slog.Logger
}

type Edge struct {
	cfg      Config
	gate     *Gate
	server   *http.Server
	mu       sync.Mutex
	streams  map[int]context.CancelFunc
	nextID   int
	stopping atomic.Bool
}

func New(cfg Config) (*Edge, error) {
	cfg = cfg.withDefaults()
	if cfg.MaxInFlight < 1 {
		return nil, errors.New("max in flight must be positive")
	}
	gate, err := NewGate(cfg.MaxInFlight, cfg.MaxWaiting)
	if err != nil {
		return nil, err
	}
	edge := &Edge{cfg: cfg, gate: gate}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", edge.live)
	mux.HandleFunc("GET /readyz", edge.ready)
	mux.Handle("/", edge.limit(cfg.Handler))
	edge.server = &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(cfg.Logger.Handler(), slog.LevelError),
	}
	return edge, nil
}

func (c Config) withDefaults() Config {
	if c.Addr == "" {
		c.Addr = "127.0.0.1:8080"
	}
	if c.ReadHeaderTimeout == 0 {
		c.ReadHeaderTimeout = 2 * time.Second
	}
	if c.ReadTimeout == 0 {
		c.ReadTimeout = 10 * time.Second
	}
	if c.IdleTimeout == 0 {
		c.IdleTimeout = 60 * time.Second
	}
	if c.RequestTimeout == 0 {
		c.RequestTimeout = 15 * time.Second
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.Handler == nil {
		c.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
	}
	return c
}

func (e *Edge) Handler() http.Handler {
	return e.server.Handler
}

func (e *Edge) Serve(ln net.Listener) error {
	err := e.server.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (e *Edge) Shutdown(ctx context.Context) error {
	e.stopping.Store(true)
	e.closeStreams()
	return e.server.Shutdown(ctx)
}

func (e *Edge) bindStream(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	e.mu.Lock()
	if e.streams == nil {
		e.streams = map[int]context.CancelFunc{}
	}
	e.nextID++
	id := e.nextID
	e.streams[id] = cancel
	if e.stopping.Load() {
		cancel()
	}
	e.mu.Unlock()
	release := func() {
		cancel()
		e.mu.Lock()
		delete(e.streams, id)
		e.mu.Unlock()
	}
	return ctx, release
}

func (e *Edge) closeStreams() {
	e.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(e.streams))
	for _, cancel := range e.streams {
		cancels = append(cancels, cancel)
	}
	e.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (e *Edge) live(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true,"data":{"live":true}}`))
}

func (e *Edge) ready(w http.ResponseWriter, r *http.Request) {
	if e.cfg.Ready != nil {
		if err := e.cfg.Ready(r.Context()); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"UNAVAILABLE","message":"Not ready."}}`))
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true,"data":{"ready":true}}`))
}

func (e *Edge) limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := incomingID(r)
		w.Header().Set("X-Request-ID", requestID)
		ctx := r.Context()
		var cancel context.CancelFunc
		longLived := e.cfg.LongLived != nil && e.cfg.LongLived(r)
		if longLived {
			var release func()
			ctx, release = e.bindStream(ctx)
			defer release()
			r = r.WithContext(ctx)
		}
		if !longLived {
			timeout := e.cfg.RequestTimeout
			if e.cfg.TimeoutFor != nil {
				if custom := e.cfg.TimeoutFor(r); custom > 0 {
					timeout = custom
				}
			}
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
			r = r.WithContext(ctx)
			if err := e.gate.Enter(ctx); err != nil {
				e.shed(w, r, requestID, err)
				return
			}
			defer e.gate.Leave()
		}
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(sw, r)
		e.cfg.Logger.LogAttrs(ctx, slog.LevelInfo, "request",
			slog.String("event", "request_completed"),
			slog.String("request_id", requestID),
			slog.Int("status", sw.status),
			slog.String("path", r.URL.Path),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
		)
	})
}

func (e *Edge) shed(w http.ResponseWriter, r *http.Request, requestID string, err error) {
	if errors.Is(err, ErrSaturated) {
		w.Header().Set("Retry-After", "1")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"TOO_MANY_REQUESTS","message":"Server is at capacity."}}`))
		e.cfg.Logger.LogAttrs(r.Context(), slog.LevelWarn, "request",
			slog.String("event", "request_shed"),
			slog.String("request_id", requestID),
			slog.String("path", r.URL.Path),
		)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"UNAVAILABLE","message":"Request cancelled."}}`))
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Flush() {
	if flusher, ok := s.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (s *statusWriter) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}
