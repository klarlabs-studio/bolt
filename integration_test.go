// Package bolt integration tests
//
// This file contains end-to-end integration tests that verify Bolt works correctly
// in real-world scenarios with actual output validation and error scenarios.
package bolt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"
)

// TestEndToEndJSONLogging tests complete JSON logging workflow
func TestEndToEndJSONLogging(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewJSONHandler(&buf))

	t.Run("Complete Application Flow", func(t *testing.T) {
		buf.Reset()

		// Simulate application startup
		logger.Info().
			Str("event", "startup").
			Str("version", "v1.2.1").
			Str("environment", "production").
			Msg("Application starting")

		// Simulate request processing
		start := time.Now()
		logger.Info().
			Str("event", "request").
			Str("method", "GET").
			Str("path", "/api/users").
			Int("user_id", 12345).
			Msg("Processing request")

		// Simulate error
		err := errors.New("database connection failed")
		logger.Error().
			Err(err).
			Str("component", "database").
			Int("retry_attempt", 3).
			Msg("Failed to connect")

		// Simulate successful completion
		duration := time.Since(start)
		logger.Info().
			Str("event", "response").
			Int("status_code", 200).
			Dur("duration", duration).
			Float64("duration_ms", float64(duration.Nanoseconds())/1_000_000).
			Msg("Request completed")

		// Verify all logs are valid JSON
		output := buf.String()
		lines := strings.Split(strings.TrimSpace(output), "\n")

		if len(lines) != 4 {
			t.Errorf("Expected 4 log lines, got %d", len(lines))
		}

		for i, line := range lines {
			var data map[string]interface{}
			if err := json.Unmarshal([]byte(line), &data); err != nil {
				t.Errorf("Line %d is not valid JSON: %v\nLine: %s", i+1, err, line)
			}

			// Verify required fields
			if _, ok := data["level"]; !ok {
				t.Errorf("Line %d missing 'level' field", i+1)
			}
			if _, ok := data["message"]; !ok {
				t.Errorf("Line %d missing 'message' field", i+1)
			}
		}
	})

	t.Run("Error Handler Integration", func(t *testing.T) {
		buf.Reset()
		var handlerErrors []error

		logger := New(NewJSONHandler(&buf)).SetErrorHandler(func(err error) {
			handlerErrors = append(handlerErrors, err)
		})

		// Trigger validation error
		logger.Info().Str("", "value").Msg("test")

		if len(handlerErrors) != 1 {
			t.Errorf("Expected 1 handler error, got %d", len(handlerErrors))
		}

		if !strings.Contains(handlerErrors[0].Error(), "key cannot be empty") {
			t.Errorf("Expected 'key cannot be empty' error, got: %v", handlerErrors[0])
		}
	})
}

// TestEndToEndConsoleLogging tests console handler in real scenarios
func TestEndToEndConsoleLogging(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewConsoleHandler(&buf))

	t.Run("Colorized Output", func(t *testing.T) {
		buf.Reset()

		logger.Info().Str("component", "server").Msg("Server started")
		logger.Warn().Str("component", "cache").Msg("Cache miss")
		logger.Error().Str("component", "database").Msg("Connection failed")

		output := buf.String()
		lines := strings.Split(strings.TrimSpace(output), "\n")

		if len(lines) != 3 {
			t.Errorf("Expected 3 log lines, got %d", len(lines))
		}

		// Verify ANSI color codes are present
		if !strings.Contains(output, "\x1b[") {
			t.Error("Expected ANSI color codes in output")
		}

		// Verify each line has level and message
		for i, line := range lines {
			if !strings.Contains(line, "[") || !strings.Contains(line, "]") {
				t.Errorf("Line %d missing timestamp brackets: %s", i+1, line)
			}
		}
	})
}

// TestEndToEndOpenTelemetry tests OpenTelemetry integration
func TestEndToEndOpenTelemetry(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewJSONHandler(&buf))

	t.Run("Trace Context Propagation", func(t *testing.T) {
		buf.Reset()

		// Create mock trace context
		traceID := trace.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
		spanID := trace.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
		scc := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    traceID,
			SpanID:     spanID,
			TraceFlags: trace.FlagsSampled,
		})
		ctx := trace.ContextWithSpanContext(context.Background(), scc)

		// Log with trace context
		logger.Ctx(ctx).Info().
			Str("operation", "database_query").
			Msg("Executing query")

		// Verify trace IDs are in output
		output := buf.String()
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(output), &data); err != nil {
			t.Fatalf("Failed to parse JSON: %v", err)
		}

		if traceIDStr, ok := data["trace_id"].(string); !ok {
			t.Error("Missing trace_id in output")
		} else if traceIDStr != traceID.String() {
			t.Errorf("Expected trace_id %s, got %s", traceID.String(), traceIDStr)
		}

		if spanIDStr, ok := data["span_id"].(string); !ok {
			t.Error("Missing span_id in output")
		} else if spanIDStr != spanID.String() {
			t.Errorf("Expected span_id %s, got %s", spanID.String(), spanIDStr)
		}
	})
}

// TestEndToEndLevelFiltering tests log level filtering
func TestEndToEndLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewJSONHandler(&buf))

	t.Run("Level Filtering", func(t *testing.T) {
		// Set level to WARN
		logger.SetLevel(WARN)

		buf.Reset()
		logger.Trace().Msg("trace message") // Should be filtered
		logger.Debug().Msg("debug message") // Should be filtered
		logger.Info().Msg("info message")   // Should be filtered
		logger.Warn().Msg("warn message")   // Should appear
		logger.Error().Msg("error message") // Should appear
		logger.Fatal().Msg("fatal message") // Should appear

		lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		if len(lines) != 3 {
			t.Errorf("Expected 3 log lines (warn, error, fatal), got %d", len(lines))
		}
	})

	t.Run("Level Change During Operation", func(t *testing.T) {
		logger.SetLevel(ERROR)
		buf.Reset()

		logger.Info().Msg("should be filtered")
		logger.Error().Msg("should appear 1")

		// Change level mid-operation
		logger.SetLevel(DEBUG)

		logger.Debug().Msg("should appear 2")
		logger.Info().Msg("should appear 3")

		lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		if len(lines) != 3 {
			t.Errorf("Expected 3 log lines after level change, got %d", len(lines))
		}
	})
}

// TestEndToEndContextLogger tests context-aware logging
func TestEndToEndContextLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewJSONHandler(&buf))

	t.Run("Persistent Context Fields", func(t *testing.T) {
		buf.Reset()

		// Create logger with persistent context
		requestLogger := logger.With().
			Str("request_id", "abc-123").
			Str("user_id", "user-456").
			Logger()

		// All logs should include context
		requestLogger.Info().Msg("processing started")
		requestLogger.Info().Str("step", "validation").Msg("validating input")
		requestLogger.Info().Str("step", "execution").Msg("executing operation")

		lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		for i, line := range lines {
			var data map[string]interface{}
			if err := json.Unmarshal([]byte(line), &data); err != nil {
				t.Fatalf("Line %d: Failed to parse JSON: %v", i+1, err)
			}

			if requestID, ok := data["request_id"].(string); !ok || requestID != "abc-123" {
				t.Errorf("Line %d: Missing or incorrect request_id", i+1)
			}

			if userID, ok := data["user_id"].(string); !ok || userID != "user-456" {
				t.Errorf("Line %d: Missing or incorrect user_id", i+1)
			}
		}
	})
}

// TestEndToEndLevelConfiguration verifies that a logger constructed with a
// parsed level filters records correctly. bolt has no global logger and reads
// no environment variables; callers parse and inject levels explicitly.
func TestEndToEndLevelConfiguration(t *testing.T) {
	t.Run("WARN level filtering", func(t *testing.T) {
		var buf bytes.Buffer
		testLogger := New(NewJSONHandler(&buf)).SetLevel(ParseLevel("warn"))

		buf.Reset()
		testLogger.Info().Msg("should be filtered")
		testLogger.Warn().Msg("should appear")

		lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		if len(lines) != 1 {
			t.Errorf("Expected 1 log line with BOLT_LEVEL=warn, got %d", len(lines))
		}
	})
}

// TestEndToEndHighThroughput verifies that a long run of logging operations
// completes successfully and writes the expected number of records. The
// previous version of this test asserted a wall-clock latency budget
// (2.5 μs/op) which produced false positives on shared CI; latency drift
// is now measured by the benchstat workflow with statistical significance.
func TestEndToEndHighThroughput(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping high-throughput test in short mode")
	}

	var buf bytes.Buffer
	logger := New(NewJSONHandler(&buf))

	const iterations = 10000
	start := time.Now()
	for i := 0; i < iterations; i++ {
		logger.Info().
			Str("request_id", "req-12345").
			Int("iteration", i).
			Int64("timestamp", time.Now().Unix()).
			Bool("success", true).
			Msg("request processed")
	}
	duration := time.Since(start)

	t.Logf("High throughput: %d iterations in %v (avg: %d ns/op)",
		iterations, duration, duration.Nanoseconds()/int64(iterations))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != iterations {
		t.Errorf("Expected %d log lines, got %d", iterations, len(lines))
	}
}

// TestEndToEndAllFieldTypes tests all field types in realistic scenario
func TestEndToEndAllFieldTypes(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewJSONHandler(&buf))

	t.Run("All Field Types Combined", func(t *testing.T) {
		buf.Reset()

		logger.Info().
			Str("string_field", "value").
			Int("int_field", 42).
			Int8("int8_field", 8).
			Int16("int16_field", 16).
			Int32("int32_field", 32).
			Int64("int64_field", 64).
			Uint("uint_field", 42).
			Uint64("uint64_field", 64).
			Bool("bool_field", true).
			Float64("float_field", 3.14159).
			Time("time_field", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)).
			Dur("dur_field", time.Second).
			Hex("hex_field", []byte{0xDE, 0xAD, 0xBE, 0xEF}).
			Base64("base64_field", []byte("test")).
			Msg("all fields test")

		// Verify valid JSON
		var data map[string]interface{}
		if err := json.Unmarshal(buf.Bytes(), &data); err != nil {
			t.Fatalf("Failed to parse JSON: %v", err)
		}

		// Verify all fields present
		requiredFields := []string{
			"level", "message", "string_field", "int_field", "int8_field",
			"int16_field", "int32_field", "int64_field", "uint_field",
			"uint64_field", "bool_field", "float_field", "time_field",
			"dur_field", "hex_field", "base64_field",
		}

		for _, field := range requiredFields {
			if _, ok := data[field]; !ok {
				t.Errorf("Missing field: %s", field)
			}
		}

		t.Logf("Successfully logged all field types with %d total fields", len(data))
	})
}
