package observability

import (
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type tracedWriter struct {
	http.ResponseWriter
	status int
}

func (t *tracedWriter) WriteHeader(code int) {
	t.status = code
	t.ResponseWriter.WriteHeader(code)
}

func (t *tracedWriter) Flush() {
	if flusher, ok := t.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (t *tracedWriter) Unwrap() http.ResponseWriter {
	return t.ResponseWriter
}

func (p *Pipeline) Middleware(next http.Handler) http.Handler {
	propagator := propagation.TraceContext{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, end := p.StartHTTP(ctx, r.Method, r.URL.Path)
		span := trace.SpanFromContext(ctx)
		if requestID := w.Header().Get("X-Request-ID"); requestID != "" {
			span.SetAttributes(attribute.String("http.request_id", requestID))
		}
		w.Header().Set("X-Trace-ID", span.SpanContext().TraceID().String())
		traced := &tracedWriter{ResponseWriter: w, status: http.StatusOK}
		defer func() { end(traced.status) }()
		next.ServeHTTP(traced, r.WithContext(ctx))
	})
}
