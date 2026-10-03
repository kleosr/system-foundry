package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"systemsfoundry/auth"
	"systemsfoundry/chaos"
	"systemsfoundry/events"
	"systemsfoundry/finops"
	"systemsfoundry/gateway"
	"systemsfoundry/observability"
	ratelimit "systemsfoundry/rate-limiting"
	"systemsfoundry/search"
	"systemsfoundry/server"
	"systemsfoundry/streaming"
)

func main() {
	logger := slog.New(auth.NewRedactingHandler(slog.NewJSONHandler(os.Stdout, nil)))
	cfg, err := configFromEnv()
	if err != nil {
		logger.Error("startup", "event", "startup_refused", "message", err.Error())
		os.Exit(1)
	}
	if err = run(cfg, logger); err != nil {
		logger.Error("shutdown", "event", "exit", "message", err.Error())
		os.Exit(1)
	}
}

type process struct {
	edge    *server.Edge
	cleanup func()
}

func run(cfg runtimeConfig, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	proc, err := assemble(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer proc.cleanup()
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	errCh := make(chan error, 1)
	go func() { errCh <- proc.edge.Serve(ln) }()
	logger.Info("startup", "event", "listening", "addr", ln.Addr().String(), "mode", cfg.Mode)
	select {
	case <-ctx.Done():
		logger.Info("shutdown", "event", "signal")
	case err = <-errCh:
		return err
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return proc.edge.Shutdown(shutdownCtx)
}

func assemble(ctx context.Context, cfg runtimeConfig, logger *slog.Logger) (*process, error) {
	tracerProvider := newTracerProvider(cfg.Langfuse)
	tracing := observability.NewPipeline(tracerProvider)
	stores, err := openBackends(ctx, cfg, tracing)
	if err != nil {
		return nil, err
	}
	cleanups := []func(){stores.close, func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tracerProvider.Shutdown(flushCtx)
	}}
	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}
	proc, err := assembleOnBackends(ctx, cfg, logger, stores, tracing, &cleanups)
	if err != nil {
		cleanup()
		return nil, err
	}
	proc.cleanup = cleanup
	return proc, nil
}

func assembleOnBackends(ctx context.Context, cfg runtimeConfig, logger *slog.Logger, stores *backends, tracing *observability.Pipeline, cleanups *[]func()) (*process, error) {
	provider, err := buildProvider(cfg, stores, logger)
	if err != nil {
		return nil, err
	}
	primary, cheap, embedder := buildModels(cfg.Model)
	proxy := gateway.NewProxy(primary, cheap, gateway.NewCache(10000), gateway.NewBudget(100000), nil)
	if embedder != nil {
		proxy.UseEmbedder(embedder, 0.95)
	}
	hub := streaming.NewHub(64, 250*time.Millisecond)
	ledger := finops.NewLedger(time.Now, 35*24*time.Hour)
	limiter, err := ratelimit.NewLimiter(stores.limits, cfg.RatePerMinute, max(1, cfg.RatePerMinute/10), time.Minute, 50*time.Millisecond)
	if err != nil {
		return nil, err
	}
	bus := events.NewEventBus(events.NewJetStreamPublisher(stores.js))
	onboarding, closeTemporal, err := dialOnboarding(ctx, cfg.Temporal, logger)
	if err != nil {
		return nil, err
	}
	*cleanups = append(*cleanups, closeTemporal)
	webhooks := newWebhooks(cfg.WebhookSecret, bus)
	background, stopBackground := context.WithCancel(ctx)
	*cleanups = append(*cleanups, stopBackground)
	go func() { _ = events.WatchViews(background, stores.orders.Views, orderChanges(hub)) }()
	if cfg.Mode == "local" {
		worker := events.NewWorker(stores.js, stores.orders.Views, logger)
		go func() { _ = worker.Run(background, stores.orders.Consumer) }()
	}
	if webhooks != nil {
		go webhooks.RunDrain(background, time.Second, 100, logger)
	}
	handlers := &api{
		proxy: proxy, catalog: search.NewService(stores.repo), hub: hub,
		alerter: finops.NewAlerter(ledger, costAlerts(hub, logger)),
		prices:  finops.PriceTable{cfg.Model.Primary: cfg.Model.PrimaryPrice, cfg.Model.Cheap: cfg.Model.CheapPrice},
		bus:     bus, views: stores.orders.Views, tracing: tracing, logger: logger,
	}
	board := chaos.NewBoard(time.Now)
	edge, err := server.New(server.Config{
		Addr:        cfg.Addr,
		MaxInFlight: 10000,
		MaxWaiting:  1024,
		Logger:      logger,
		Ready:       stores.ready,
		Handler: buildRoutes(routeDeps{
			cfg: cfg, provider: provider, api: handlers, limiter: limiter, hub: hub, board: board,
			injector: chaos.NewInjector(), ledger: ledger, webhooks: webhooks, onboarding: onboarding, tracing: tracing,
		}),
		LongLived: func(r *http.Request) bool { return r.URL.Path == "/stream" },
		TimeoutFor: func(r *http.Request) time.Duration {
			if r.URL.Path == "/v1/complete" {
				return 17 * time.Second
			}
			return 0
		},
	})
	if err != nil {
		return nil, err
	}
	return &process{edge: edge}, nil
}
