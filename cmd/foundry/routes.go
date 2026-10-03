package main

import (
	"net/http"

	"systemsfoundry/auth"
	"systemsfoundry/chaos"
	"systemsfoundry/finops"
	"systemsfoundry/observability"
	ratelimit "systemsfoundry/rate-limiting"
	"systemsfoundry/reconciliation"
	"systemsfoundry/streaming"
	"systemsfoundry/workflows"
)

const apiAudience = "foundry-api"

type routeDeps struct {
	cfg        runtimeConfig
	provider   *auth.Provider
	api        *api
	limiter    *ratelimit.Limiter
	hub        *streaming.Hub
	board      *chaos.Board
	injector   *chaos.Injector
	ledger     *finops.Ledger
	webhooks   *reconciliation.Processor
	onboarding *workflows.Onboarding
	tracing    *observability.Pipeline
}

func buildRoutes(d routeDeps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", d.provider.ServeDiscovery)
	mux.HandleFunc("GET /jwks.json", d.provider.ServeJWKS)
	mux.HandleFunc("POST /login", d.provider.ServeLogin)
	mux.HandleFunc("POST /logout", d.provider.ServeLogout)
	mux.HandleFunc("GET /authorize", d.provider.ServeAuthorize)
	mux.HandleFunc("POST /token", d.provider.ServeToken)
	mux.HandleFunc("GET /dashboard", streaming.Dashboard)
	mux.Handle("GET /stream", d.provider.AuthenticateSession(d.hub))
	mux.Handle("GET /slo", d.board)
	mux.Handle("GET /slo/view", d.board)

	bearer := func(next http.HandlerFunc) http.Handler {
		return d.provider.Authenticate(apiAudience, d.limiter.Guard(next))
	}
	mux.Handle("POST /v1/complete", bearer(d.api.complete))
	mux.Handle("POST /v1/documents", bearer(d.api.indexDocument))
	mux.Handle("GET /v1/search", bearer(d.api.searchDocuments))
	mux.Handle("POST /v1/orders/{order}/events", bearer(d.api.appendOrderEvent))
	mux.Handle("GET /v1/orders/{order}", bearer(d.api.readOrder))
	mux.Handle("GET /v1/costs", bearer(d.ledger.ServeCosts))

	operator := func(next http.Handler) http.Handler {
		return d.provider.Authenticate(apiAudience, auth.RequireOperator(d.cfg.OpsTenant, next))
	}
	mux.Handle("POST /ops/users", operator(http.HandlerFunc(d.provider.ServeCreateUser)))
	if d.webhooks != nil {
		mux.HandleFunc("POST /webhooks/{provider}", d.webhooks.ServeWebhook)
		mux.Handle("GET /ops/webhooks/dead", operator(http.HandlerFunc(d.webhooks.ServeDead)))
		mux.Handle("POST /ops/webhooks/{provider}/{id}/replay", operator(http.HandlerFunc(d.webhooks.ServeReplay)))
	}
	if d.onboarding != nil {
		mux.Handle("POST /ops/onboarding", operator(http.HandlerFunc(d.onboarding.ServeStart)))
		mux.Handle("POST /ops/onboarding/{tenant}/approve", operator(http.HandlerFunc(d.onboarding.ServeApprove)))
	}
	var app http.Handler = mux
	if d.cfg.ChaosEnabled {
		root := http.NewServeMux()
		root.Handle("/ops/chaos", operator(d.injector))
		root.Handle("/", d.injector.Middleware(mux))
		app = root
	}
	return d.tracing.Middleware(d.board.Observe(app))
}
