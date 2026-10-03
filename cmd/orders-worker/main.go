package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"systemsfoundry/auth"
	"systemsfoundry/events"
)

func main() {
	logger := slog.New(auth.NewRedactingHandler(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(logger); err != nil {
		logger.Error("orders-worker", "event", "exit", "message", err.Error())
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	url := os.Getenv("FOUNDRY_NATS_URL")
	if url == "" {
		return errors.New("FOUNDRY_NATS_URL is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	conn, err := nats.Connect(url, nats.Name("systemsfoundry-orders-worker"), nats.Timeout(5*time.Second), nats.MaxReconnects(-1))
	if err != nil {
		return err
	}
	defer func() { _ = conn.Drain() }()
	js, err := jetstream.New(conn)
	if err != nil {
		return err
	}
	setupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	topology, err := events.EnsureTopology(setupCtx, js)
	cancel()
	if err != nil {
		return err
	}
	worker := events.NewWorker(js, topology.Views, logger)
	logger.Info("orders-worker", "event", "consuming", "stream", events.StreamOrders, "consumer", events.ConsumerOrders)
	err = worker.Run(ctx, topology.Consumer)
	logger.Info("orders-worker", "event", "drained", "applied", worker.Applied.Load(), "dead", worker.Dead.Load())
	return err
}
