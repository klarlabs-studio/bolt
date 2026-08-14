package bolt

import (
	"context"
	"io"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func benchCtxLogger() *Logger {
	return New(NewJSONHandler(io.Discard))
}

func spanCtx() context.Context {
	tid, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	sid, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled,
	})
	return trace.ContextWithSpanContext(context.Background(), sc)
}

func BenchmarkWithoutCtx(b *testing.B) {
	l := benchCtxLogger()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Info().Str("k", "v").Msg("hello")
	}
}

func BenchmarkCtxNoSpan(b *testing.B) {
	l := benchCtxLogger()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Ctx(ctx).Info().Str("k", "v").Msg("hello")
	}
}

func BenchmarkCtxWithSpan(b *testing.B) {
	l := benchCtxLogger()
	ctx := spanCtx()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Ctx(ctx).Info().Str("k", "v").Msg("hello")
	}
}

// The replacement path: correlation on the event, not a derived logger.
func BenchmarkEventCtxWithSpan(b *testing.B) {
	l := benchCtxLogger()
	ctx := spanCtx()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Info().Ctx(ctx).Str("k", "v").Msg("hello")
	}
}

func BenchmarkEventCtxNoSpan(b *testing.B) {
	l := benchCtxLogger()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Info().Ctx(ctx).Str("k", "v").Msg("hello")
	}
}
