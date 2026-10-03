package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/client"
	temporallog "go.temporal.io/sdk/log"
	"go.temporal.io/sdk/worker"

	"systemsfoundry/auth"
	"systemsfoundry/workflows"
)

func main() {
	logger := slog.New(auth.NewRedactingHandler(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(logger); err != nil {
		logger.Error("onboarding-worker", "event", "exit", "message", err.Error())
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	address, dsn := os.Getenv("FOUNDRY_TEMPORAL_ADDR"), os.Getenv("FOUNDRY_POSTGRES_DSN")
	if address == "" || dsn == "" {
		return errors.New("FOUNDRY_TEMPORAL_ADDR and FOUNDRY_POSTGRES_DSN are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err = pool.Ping(ctx); err != nil {
		return errors.New("postgres is unreachable")
	}
	temporalClient, err := client.DialContext(ctx, client.Options{HostPort: address, Logger: temporallog.NewStructuredLogger(logger)})
	if err != nil {
		return err
	}
	defer temporalClient.Close()
	runner := worker.New(temporalClient, workflows.TaskQueue, worker.Options{})
	runner.RegisterWorkflow(workflows.Onboard)
	runner.RegisterActivity(&workflows.Activities{Dir: workflows.NewPostgresDirectory(pool)})
	logger.Info("onboarding-worker", "event", "polling", "task_queue", workflows.TaskQueue)
	return runner.Run(worker.InterruptCh())
}
