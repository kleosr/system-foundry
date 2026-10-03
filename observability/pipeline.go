package observability

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"systemsfoundry/auth"
)

type Pipeline struct {
	tracer trace.Tracer
}

func NewPipeline(provider trace.TracerProvider) *Pipeline {
	return &Pipeline{tracer: provider.Tracer("systemsfoundry")}
}

func (p *Pipeline) StartHTTP(ctx context.Context, method, path string) (context.Context, func(int)) {
	ctx, span := p.tracer.Start(ctx, "http.server")
	span.SetAttributes(
		attribute.String("http.method", method),
		attribute.String("http.route", path),
	)
	return ctx, func(status int) {
		span.SetAttributes(attribute.Int("http.status_code", status))
		if status >= 500 {
			span.SetStatus(codes.Error, "http error")
		}
		span.End()
	}
}

func (p *Pipeline) StartLLM(ctx context.Context, model, prompt string) (context.Context, func(error)) {
	ctx, span := p.tracer.Start(ctx, "llm.chat")
	span.SetAttributes(
		attribute.String("llm.model", model),
		attribute.String("llm.prompt", auth.Redact(prompt)),
	)
	return ctx, func(err error) {
		if err != nil {
			span.SetStatus(codes.Error, "llm error")
		}
		span.End()
	}
}

func (p *Pipeline) StartDB(ctx context.Context, statement string) (context.Context, func(error)) {
	ctx, span := p.tracer.Start(ctx, "db.query")
	span.SetAttributes(attribute.String("db.statement", auth.Redact(statement)))
	return ctx, func(err error) {
		if err != nil {
			span.SetStatus(codes.Error, "db error")
		}
		span.End()
	}
}
