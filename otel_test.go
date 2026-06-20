package bolt

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// TestOpenTelemetryIntegration tests OpenTelemetry trace/span ID injection
func TestOpenTelemetryIntegration(t *testing.T) {
	t.Run("Valid Trace and Span IDs", func(t *testing.T) {
		var buf bytes.Buffer
		logger := New(NewJSONHandler(&buf))

		// Create mock trace context
		traceID := trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
		spanID := trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}

		spanContext := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: traceID,
			SpanID:  spanID,
		})

		ctx := trace.ContextWithSpanContext(context.Background(), spanContext)

		ctxLogger := logger.Ctx(ctx)
		ctxLogger.Info().Msg("test")

		output := buf.String()
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("Invalid JSON: %v", err)
		}

		// Check trace_id
		if traceIDStr, ok := result["trace_id"].(string); !ok || traceIDStr == "" {
			t.Error("Expected trace_id in output")
		}

		// Check span_id
		if spanIDStr, ok := result["span_id"].(string); !ok || spanIDStr == "" {
			t.Error("Expected span_id in output")
		}
	})

	t.Run("No Active Span", func(t *testing.T) {
		var buf bytes.Buffer
		logger := New(NewJSONHandler(&buf))

		// Context without span
		ctx := context.Background()
		ctxLogger := logger.Ctx(ctx)
		ctxLogger.Info().Msg("test")

		output := buf.String()
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("Invalid JSON: %v", err)
		}

		// Should not have trace/span IDs when no active span
		if _, ok := result["trace_id"]; ok {
			t.Error("Should not have trace_id without active span")
		}
		if _, ok := result["span_id"]; ok {
			t.Error("Should not have span_id without active span")
		}
	})

	t.Run("Nil Context", func(t *testing.T) {
		var buf bytes.Buffer
		logger := New(NewJSONHandler(&buf))

		// Should handle nil context gracefully (use context.TODO for testing)
		ctxLogger := logger.Ctx(context.TODO())
		ctxLogger.Info().Msg("test")

		output := buf.String()
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("Invalid JSON: %v", err)
		}

		// Should log successfully without trace IDs
		if msg, ok := result["message"].(string); !ok || msg != "test" {
			t.Error("Expected message to be logged")
		}
	})

	t.Run("Invalid Span Context", func(t *testing.T) {
		var buf bytes.Buffer
		logger := New(NewJSONHandler(&buf))

		// Invalid span context (all zeros)
		invalidSpanContext := trace.NewSpanContext(trace.SpanContextConfig{})
		ctx := trace.ContextWithSpanContext(context.Background(), invalidSpanContext)

		ctxLogger := logger.Ctx(ctx)
		ctxLogger.Info().Msg("test")

		output := buf.String()
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("Invalid JSON: %v", err)
		}

		// Invalid span contexts should not add trace/span IDs
		if _, ok := result["trace_id"]; ok {
			t.Error("Should not have trace_id for invalid span context")
		}
	})

	t.Run("Trace ID Format", func(t *testing.T) {
		var buf bytes.Buffer
		logger := New(NewJSONHandler(&buf))

		// Specific trace ID to verify format
		traceID := trace.TraceID{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
			0xfe, 0xdc, 0xba, 0x98, 0x76, 0x54, 0x32, 0x10}
		spanID := trace.SpanID{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0}

		spanContext := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: traceID,
			SpanID:  spanID,
		})

		ctx := trace.ContextWithSpanContext(context.Background(), spanContext)
		ctxLogger := logger.Ctx(ctx)
		ctxLogger.Info().Msg("test")

		output := buf.String()
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("Invalid JSON: %v", err)
		}

		// Verify trace_id is hex encoded
		traceIDStr, ok := result["trace_id"].(string)
		if !ok {
			t.Fatal("trace_id not a string")
		}

		expectedTraceID := "0123456789abcdeffedcba9876543210"
		if traceIDStr != expectedTraceID {
			t.Errorf("Expected trace_id %s, got %s", expectedTraceID, traceIDStr)
		}

		// Verify span_id is hex encoded
		spanIDStr, ok := result["span_id"].(string)
		if !ok {
			t.Fatal("span_id not a string")
		}

		expectedSpanID := "123456789abcdef0"
		if spanIDStr != expectedSpanID {
			t.Errorf("Expected span_id %s, got %s", expectedSpanID, spanIDStr)
		}
	})

	t.Run("Context Cancellation", func(t *testing.T) {
		var buf bytes.Buffer
		logger := New(NewJSONHandler(&buf))

		// Cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		// Should still log successfully even with cancelled context
		ctxLogger := logger.Ctx(ctx)
		ctxLogger.Info().Msg("test")

		output := buf.String()
		if len(output) == 0 {
			t.Error("Expected log output even with cancelled context")
		}
	})

	t.Run("Multiple Context Calls", func(t *testing.T) {
		var buf bytes.Buffer
		logger := New(NewJSONHandler(&buf))

		traceID := trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
		spanID := trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}
		spanContext := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: traceID,
			SpanID:  spanID,
		})
		ctx := trace.ContextWithSpanContext(context.Background(), spanContext)

		// Multiple Ctx() calls - last one should win
		ctxLogger := logger.Ctx(context.Background()).Ctx(ctx)
		ctxLogger.Info().Msg("test")

		output := buf.String()
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("Invalid JSON: %v", err)
		}

		// Should have trace_id from second context
		if _, ok := result["trace_id"]; !ok {
			t.Error("Expected trace_id from second Ctx() call")
		}
	})

	t.Run("Concurrent Context Usage", func(t *testing.T) {
		buf := &ThreadSafeBuffer{}
		logger := New(NewJSONHandler(buf))

		var wg sync.WaitGroup
		wg.Add(10)

		for i := 0; i < 10; i++ {
			go func(id int) {
				defer wg.Done()
				traceID := trace.TraceID{byte(id), 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
				spanID := trace.SpanID{byte(id), 2, 3, 4, 5, 6, 7, 8}
				spanContext := trace.NewSpanContext(trace.SpanContextConfig{
					TraceID: traceID,
					SpanID:  spanID,
				})
				ctx := trace.ContextWithSpanContext(context.Background(), spanContext)

				ctxLogger := logger.Ctx(ctx)
				ctxLogger.Info().Int("id", id).Msg("concurrent")
			}(i)
		}

		wg.Wait()

		// Verify all logs were written
		output := string(buf.Bytes())
		lines := strings.Split(strings.TrimSpace(output), "\n")
		if len(lines) != 10 {
			t.Errorf("Expected 10 log lines, got %d: %s", len(lines), output)
		}

		// Each line should be valid JSON with trace_id
		for i, line := range lines {
			if line == "" {
				continue
			}
			var result map[string]interface{}
			if err := json.Unmarshal([]byte(line), &result); err != nil {
				t.Errorf("Line %d invalid JSON: %v", i, err)
				continue
			}
			if _, ok := result["trace_id"]; !ok {
				t.Errorf("Line %d missing trace_id", i)
			}
		}
	})
}

// TestOpenTelemetryEdgeCases tests edge cases in OpenTelemetry integration
func TestOpenTelemetryEdgeCases(t *testing.T) {
	t.Run("Zero Trace ID", func(t *testing.T) {
		var buf bytes.Buffer
		logger := New(NewJSONHandler(&buf))

		// All-zero trace ID (invalid)
		zeroTraceID := trace.TraceID{}
		zeroSpanID := trace.SpanID{}

		spanContext := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: zeroTraceID,
			SpanID:  zeroSpanID,
		})

		ctx := trace.ContextWithSpanContext(context.Background(), spanContext)
		ctxLogger := logger.Ctx(ctx)
		ctxLogger.Info().Msg("test")

		output := buf.String()
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("Invalid JSON: %v", err)
		}

		// Zero IDs should be treated as invalid and not included
		if _, ok := result["trace_id"]; ok {
			t.Error("Zero trace_id should not be included")
		}
	})

	t.Run("Max Value Trace and Span IDs", func(t *testing.T) {
		var buf bytes.Buffer
		logger := New(NewJSONHandler(&buf))

		// Max values
		maxTraceID := trace.TraceID{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
			0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
		maxSpanID := trace.SpanID{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}

		spanContext := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: maxTraceID,
			SpanID:  maxSpanID,
		})

		ctx := trace.ContextWithSpanContext(context.Background(), spanContext)
		ctxLogger := logger.Ctx(ctx)
		ctxLogger.Info().Msg("test")

		output := buf.String()
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("Invalid JSON: %v", err)
		}

		traceIDStr := result["trace_id"].(string)
		if traceIDStr != "ffffffffffffffffffffffffffffffff" {
			t.Errorf("Expected max trace_id, got %s", traceIDStr)
		}

		spanIDStr := result["span_id"].(string)
		if spanIDStr != "ffffffffffffffff" {
			t.Errorf("Expected max span_id, got %s", spanIDStr)
		}
	})

	t.Run("Context With Fields", func(t *testing.T) {
		var buf bytes.Buffer
		logger := New(NewJSONHandler(&buf))

		traceID := trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
		spanID := trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}
		spanContext := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: traceID,
			SpanID:  spanID,
		})
		ctx := trace.ContextWithSpanContext(context.Background(), spanContext)

		// Context logger with additional fields
		ctxLogger := logger.Ctx(ctx)
		ctxLogger.Info().
			Str("before", "ctx").
			Str("after", "ctx").
			Msg("test")

		output := buf.String()
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("Invalid JSON: %v", err)
		}

		// Should have trace_id from context
		if _, ok := result["trace_id"]; !ok {
			t.Error("Expected trace_id in output")
		}

		// Should have both fields
		if _, ok := result["before"]; !ok {
			t.Error("Expected 'before' field")
		}
		if _, ok := result["after"]; !ok {
			t.Error("Expected 'after' field")
		}
	})

	t.Run("Tracer Provider", func(t *testing.T) {
		// Verify Ctx works with a TracerProvider-derived span. We use the
		// trace-API noop provider rather than otel.GetTracerProvider() so the
		// core module stays within the otel/trace dependency boundary.
		var buf bytes.Buffer
		logger := New(NewJSONHandler(&buf))

		// Use a trace-API tracer provider
		tp := noop.NewTracerProvider()
		tracer := tp.Tracer("test-tracer")

		ctx, span := tracer.Start(context.Background(), "test-span")
		defer span.End()

		ctxLogger := logger.Ctx(ctx)
		ctxLogger.Info().Msg("with global tracer")

		output := buf.String()

		// Should work with global tracer provider
		if len(output) == 0 {
			t.Error("Expected log output with global tracer")
		}
	})

	t.Run("Nested Spans", func(t *testing.T) {
		var buf bytes.Buffer
		logger := New(NewJSONHandler(&buf))

		// Parent span
		parentTraceID := trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
		parentSpanID := trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}
		parentContext := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: parentTraceID,
			SpanID:  parentSpanID,
		})
		parentCtx := trace.ContextWithSpanContext(context.Background(), parentContext)

		// Child span (same trace ID, different span ID)
		childSpanID := trace.SpanID{9, 10, 11, 12, 13, 14, 15, 16}
		childContext := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: parentTraceID, // Same trace
			SpanID:  childSpanID,   // Different span
		})
		childCtx := trace.ContextWithSpanContext(parentCtx, childContext)

		ctxLogger := logger.Ctx(childCtx)
		ctxLogger.Info().Msg("child span")

		output := buf.String()
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("Invalid JSON: %v", err)
		}

		// Should have child span ID
		spanIDStr := result["span_id"].(string)
		expectedChildSpanID := "090a0b0c0d0e0f10"
		if spanIDStr != expectedChildSpanID {
			t.Errorf("Expected child span_id %s, got %s", expectedChildSpanID, spanIDStr)
		}

		// Should have same trace ID
		traceIDStr := result["trace_id"].(string)
		expectedTraceID := "0102030405060708090a0b0c0d0e0f10"
		if traceIDStr != expectedTraceID {
			t.Errorf("Expected trace_id %s, got %s", expectedTraceID, traceIDStr)
		}
	})
}
