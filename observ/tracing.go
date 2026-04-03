package observ

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// TracingConfig controls built-in OpenTelemetry instrumentation.
type TracingConfig struct {
	ServiceName    string
	ServiceVersion string
	SampleRatio    float64
	MaxSpans       int
}

// Tracing manages the OpenTelemetry provider and in-memory exporter.
type Tracing struct {
	tp         *sdktrace.TracerProvider
	propagator propagation.TextMapPropagator
	exporter   *MemoryExporter
	name       string
}

// SpanSnapshot is a lightweight JSON-safe record of an exported span.
type SpanSnapshot struct {
	Name         string            `json:"name"`
	TraceID      string            `json:"trace_id"`
	SpanID       string            `json:"span_id"`
	ParentSpanID string            `json:"parent_span_id,omitempty"`
	Kind         string            `json:"kind"`
	Status       string            `json:"status"`
	Start        time.Time         `json:"start"`
	End          time.Time         `json:"end"`
	Attributes   map[string]string `json:"attributes,omitempty"`
}

// MemoryExporter stores recent spans so they can be inspected over HTTP.
type MemoryExporter struct {
	mu       sync.Mutex
	maxSpans int
	spans    []SpanSnapshot
}

func NewTracing(cfg TracingConfig) (*Tracing, error) {
	if cfg.ServiceName == "" {
		cfg.ServiceName = "mana"
	}
	if cfg.ServiceVersion == "" {
		cfg.ServiceVersion = "dev"
	}
	if cfg.SampleRatio <= 0 || cfg.SampleRatio > 1 {
		cfg.SampleRatio = 1
	}
	if cfg.MaxSpans <= 0 {
		cfg.MaxSpans = 512
	}

	exporter := &MemoryExporter{maxSpans: cfg.MaxSpans}
	res, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.SampleRatio)),
		sdktrace.WithResource(res),
		sdktrace.WithSyncer(exporter),
	)

	propagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagator)

	return &Tracing{
		tp:         tp,
		propagator: propagator,
		exporter:   exporter,
		name:       cfg.ServiceName,
	}, nil
}

func (t *Tracing) Shutdown(ctx context.Context) error {
	if t == nil || t.tp == nil {
		return nil
	}
	return t.tp.Shutdown(ctx)
}

func (t *Tracing) Tracer(name string) trace.Tracer {
	if t == nil {
		return otel.Tracer("mana")
	}
	if name == "" {
		name = t.name
	}
	return t.tp.Tracer(name)
}

func (t *Tracing) StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	tracer := t.Tracer(t.name)
	return tracer.Start(ctx, name, trace.WithAttributes(attrs...))
}

func (t *Tracing) HTTPMiddleware(next http.Handler) http.Handler {
	if t == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := t.propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := t.Tracer("http").Start(ctx, r.Method+" "+r.URL.Path,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.HTTPRequestMethodKey.String(r.Method),
				semconv.URLPath(r.URL.Path),
			),
		)
		defer span.End()

		r = r.WithContext(ctx)
		next.ServeHTTP(w, r)
	})
}

func (t *Tracing) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"service": t.name,
			"spans":   t.exporter.Spans(),
		})
	}
}

func (e *MemoryExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, span := range spans {
		snapshot := SpanSnapshot{
			Name:         span.Name(),
			TraceID:      span.SpanContext().TraceID().String(),
			SpanID:       span.SpanContext().SpanID().String(),
			ParentSpanID: span.Parent().SpanID().String(),
			Kind:         span.SpanKind().String(),
			Status:       span.Status().Code.String(),
			Start:        span.StartTime(),
			End:          span.EndTime(),
			Attributes:   make(map[string]string, len(span.Attributes())),
		}
		for _, attr := range span.Attributes() {
			snapshot.Attributes[string(attr.Key)] = attr.Value.Emit()
		}
		e.spans = append(e.spans, snapshot)
	}

	if len(e.spans) > e.maxSpans {
		e.spans = append([]SpanSnapshot(nil), e.spans[len(e.spans)-e.maxSpans:]...)
	}
	return nil
}

func (e *MemoryExporter) Shutdown(context.Context) error { return nil }

func (e *MemoryExporter) Spans() []SpanSnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]SpanSnapshot, len(e.spans))
	copy(out, e.spans)
	return out
}

func RecordError(span trace.Span, err error) {
	if span == nil || err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
