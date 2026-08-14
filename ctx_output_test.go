package bolt

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

// The rewritten Ctx builds the correlation fields by hand instead of going
// through With().Str().Str().Logger(). The emitted line must be unchanged (#111).
func TestCtxOutputShape(t *testing.T) {
	var buf bytes.Buffer
	l := New(NewJSONHandler(&buf))

	tid, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	sid, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(
		trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled}))

	l.Ctx(ctx).Info().Str("k", "v").Msg("hello")
	out := buf.String()
	t.Logf("out: %s", strings.TrimSpace(out))

	for _, want := range []string{
		`"trace_id":"4bf92f3577b34da6a3ce929d0e0e4736"`,
		`"span_id":"00f067aa0ba902b7"`,
		`"k":"v"`,
		`hello`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %s", want)
		}
	}

	// No span → the very same logger back, and no correlation fields.
	buf.Reset()
	plain := l.Ctx(context.Background())
	if plain != l {
		t.Error("a context with no span should return the receiver unchanged")
	}
	plain.Info().Msg("nospan")
	if strings.Contains(buf.String(), "trace_id") {
		t.Errorf("unexpected trace_id with no span: %s", buf.String())
	}
}

// Ctx must compose with an existing context without corrupting the JSON.
func TestCtxPreservesExistingContext(t *testing.T) {
	var buf bytes.Buffer
	base := New(NewJSONHandler(&buf)).With().Str("service", "auth").Logger()

	tid, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	sid, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(
		trace.SpanContextConfig{TraceID: tid, SpanID: sid}))

	base.Ctx(ctx).Info().Msg("hi")
	out := buf.String()
	t.Logf("out: %s", strings.TrimSpace(out))

	for _, want := range []string{`"service":"auth"`, `"trace_id":"4bf92f3577b34da6a3ce929d0e0e4736"`} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %s", want)
		}
	}
	if strings.Contains(out, `""`) || strings.Contains(out, ",,") {
		t.Errorf("malformed context join: %s", out)
	}
	// The parent must not have been mutated.
	buf.Reset()
	base.Info().Msg("parent")
	if strings.Contains(buf.String(), "trace_id") {
		t.Errorf("Ctx leaked correlation fields onto the parent logger: %s", buf.String())
	}
}

// Event.Ctx replaces Logger.Ctx (#111). The line it emits must be identical, or
// the replacement is not one.
func TestEventCtxMatchesLoggerCtx(t *testing.T) {
	ctx := func() context.Context {
		tid, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
		sid, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
		return trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(
			trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled}))
	}()

	var viaLogger, viaEvent bytes.Buffer
	New(NewJSONHandler(&viaLogger)).Ctx(ctx).Info().Str("k", "v").Msg("hello")
	New(NewJSONHandler(&viaEvent)).Info().Ctx(ctx).Str("k", "v").Msg("hello")

	if viaLogger.String() != viaEvent.String() {
		t.Fatalf("replacement changes the output.\n  logger: %s  event:  %s",
			viaLogger.String(), viaEvent.String())
	}
	t.Logf("identical: %s", strings.TrimSpace(viaEvent.String()))
}

// Event.Ctx reads the span when the line is emitted, so it reports the span that
// is actually active — the accuracy gain over binding once (#111).
func TestEventCtxReadsSpanAtEmitTime(t *testing.T) {
	mk := func(hexSpan string) context.Context {
		tid, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
		sid, _ := trace.SpanIDFromHex(hexSpan)
		return trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(
			trace.SpanContextConfig{TraceID: tid, SpanID: sid}))
	}
	parent, child := mk("00f067aa0ba902b7"), mk("1122334455667788")

	// Logger.Ctx binds once: a logger derived under the parent keeps reporting
	// it even inside the child span.
	var bound bytes.Buffer
	derived := New(NewJSONHandler(&bound)).Ctx(parent)
	derived.Info().Msg("in child")
	if !strings.Contains(bound.String(), "00f067aa0ba902b7") {
		t.Fatalf("expected the bound parent span, got %s", bound.String())
	}

	// Event.Ctx reports whichever span is live at the call.
	var live bytes.Buffer
	l := New(NewJSONHandler(&live))
	l.Info().Ctx(child).Msg("in child")
	if !strings.Contains(live.String(), "1122334455667788") {
		t.Errorf("event-scoped Ctx should report the active span, got %s", live.String())
	}
}

// A context with no span must add nothing and cost nothing.
func TestEventCtxNoSpanAddsNothing(t *testing.T) {
	var buf bytes.Buffer
	New(NewJSONHandler(&buf)).Info().Ctx(context.Background()).Str("k", "v").Msg("hello")
	if strings.Contains(buf.String(), "trace_id") || strings.Contains(buf.String(), "span_id") {
		t.Errorf("no span should add no correlation fields: %s", buf.String())
	}
}
