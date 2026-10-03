package main

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"systemsfoundry/events"
	ratelimit "systemsfoundry/rate-limiting"
	"systemsfoundry/search"
)

type readyCheck func(context.Context) error

type backends struct {
	pool    *pgxpool.Pool
	repo    search.Repo
	limits  ratelimit.Store
	js      jetstream.JetStream
	orders  events.Topology
	checks  []readyCheck
	closers []func()
}

func (b *backends) close() {
	for i := len(b.closers) - 1; i >= 0; i-- {
		b.closers[i]()
	}
}

func (b *backends) ready(ctx context.Context) error {
	for _, check := range b.checks {
		if err := check(ctx); err != nil {
			return err
		}
	}
	return nil
}

func openBackends(ctx context.Context, cfg runtimeConfig, tracer search.QueryTracer) (*backends, error) {
	opened := &backends{}
	natsURL := cfg.NATS
	if cfg.Mode == "local" {
		opened.repo = search.NewMemoryRepo(10000)
		opened.limits = ratelimit.NewMemoryStore(time.Now)
		storeDir, err := os.MkdirTemp("", "foundry-jetstream-")
		if err != nil {
			return nil, err
		}
		embedded, err := events.StartEmbedded(storeDir)
		if err != nil {
			_ = os.RemoveAll(storeDir)
			return nil, err
		}
		opened.closers = append(opened.closers, func() {
			embedded.Shutdown()
			embedded.WaitForShutdown()
			_ = os.RemoveAll(storeDir)
		})
		natsURL = embedded.ClientURL()
	} else {
		if err := openShared(ctx, cfg, tracer, opened); err != nil {
			opened.close()
			return nil, err
		}
	}
	if err := openOrders(ctx, natsURL, opened); err != nil {
		opened.close()
		return nil, err
	}
	return opened, nil
}

func openShared(ctx context.Context, cfg runtimeConfig, tracer search.QueryTracer, opened *backends) error {
	poolConfig, err := pgxpool.ParseConfig(cfg.Postgres)
	if err != nil {
		return errors.New("FOUNDRY_POSTGRES_DSN is not a valid connection string")
	}
	poolConfig.MaxConns = 20
	poolConfig.ConnConfig.ConnectTimeout = 5 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return err
	}
	opened.closers = append(opened.closers, pool.Close)
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err = pool.Ping(pingCtx); err != nil {
		return errors.New("postgres is unreachable")
	}
	repo := search.NewPostgresRepo(pool, tracer)
	opened.pool = pool
	opened.repo = repo
	opened.checks = append(opened.checks, repo.Ping)
	redisStore := ratelimit.NewRedisStore(cfg.Redis, 50*time.Millisecond)
	opened.limits = redisStore
	return nil
}

func openOrders(ctx context.Context, url string, opened *backends) error {
	conn, err := nats.Connect(url, nats.Name("systemsfoundry-api"), nats.Timeout(5*time.Second), nats.MaxReconnects(-1))
	if err != nil {
		return errors.New("nats is unreachable")
	}
	opened.closers = append(opened.closers, func() { _ = conn.Drain() })
	js, err := jetstream.New(conn)
	if err != nil {
		return err
	}
	setupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	topology, err := events.EnsureTopology(setupCtx, js)
	if err != nil {
		return err
	}
	opened.js = js
	opened.orders = topology
	opened.checks = append(opened.checks, func(context.Context) error {
		if !conn.IsConnected() {
			return errors.New("nats disconnected")
		}
		return nil
	})
	return nil
}
