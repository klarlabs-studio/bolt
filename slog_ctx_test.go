package bolt

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
)

// SlogHandler.Handle used to discard its context, so slog's *Context methods
// never carried trace correlation. It must add the same trace_id and span_id
// that Event.Ctx writes, read from the same context.
func TestSlogHandlerContextCarriesTrace(t *testing.T) {
	ctx := spanCtx()

	var viaSlog, viaEvent bytes.Buffer
	slog.New(NewSlogHandler(&viaSlog, nil)).InfoContext(ctx, "hello", "k", "v")
	New(NewJSONHandler(&viaEvent)).Info().Ctx(ctx).Str("k", "v").Msg("hello")

	got := decodeLine(t, viaSlog.Bytes())
	want := decodeLine(t, viaEvent.Bytes())
	for _, key := range []string{"trace_id", "span_id"} {
		if got[key] == nil || got[key] != want[key] {
			t.Errorf("%s: slog handler wrote %v, Event.Ctx wrote %v\n  line: %s",
				key, got[key], want[key], viaSlog.String())
		}
	}
}

// Correlation describes the record, not a group inside it: the fields stay at
// the top level however deeply the handler is nested.
func TestSlogHandlerTraceFieldsStayTopLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewSlogHandler(&buf, nil)).WithGroup("req").With("id", 7)
	logger.InfoContext(spanCtx(), "hello", "k", "v")

	m := decodeLine(t, buf.Bytes())
	if m["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" || m["span_id"] != "00f067aa0ba902b7" {
		t.Errorf("trace fields missing from the top level: %s", buf.String())
	}
	req, ok := m["req"].(map[string]any)
	if !ok || req["id"] != float64(7) || req["k"] != "v" {
		t.Errorf("group content changed: %s", buf.String())
	}
	if _, leaked := req["trace_id"]; leaked {
		t.Errorf("trace_id nested inside the group: %s", buf.String())
	}
}

// A context without a valid span adds nothing — the same rule as Event.Ctx.
func TestSlogHandlerContextWithoutSpanAddsNothing(t *testing.T) {
	var buf bytes.Buffer
	slog.New(NewSlogHandler(&buf, nil)).InfoContext(context.Background(), "hello", "k", "v")

	if strings.Contains(buf.String(), "trace_id") || strings.Contains(buf.String(), "span_id") {
		t.Errorf("no span should add no correlation fields: %s", buf.String())
	}
}

// Correlating a slog record must cost what Event.Ctx costs: nothing.
func TestSlogHandlerContextZeroAllocs(t *testing.T) {
	logger := slog.New(NewSlogHandler(io.Discard, nil))
	for name, ctx := range map[string]context.Context{
		"with span": spanCtx(),
		"no span":   context.Background(),
	} {
		allocs := testing.AllocsPerRun(100, func() {
			logger.InfoContext(ctx, "hello", "k", "v")
		})
		if allocs > 0 {
			t.Errorf("%s: expected 0 allocations, got %v", name, allocs)
		}
	}
}
