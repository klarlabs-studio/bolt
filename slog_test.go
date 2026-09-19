package bolt

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestSlogHandler_Basic(t *testing.T) {
	var buf bytes.Buffer
	h := NewSlogHandler(&buf, nil)
	logger := slog.New(h)

	logger.Info("hello world")

	var m map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON output: %v\nbuf: %s", err, buf.String())
	}

	if m["level"] != "info" {
		t.Errorf("expected level info, got %v", m["level"])
	}
	if m["message"] != "hello world" {
		t.Errorf("expected message 'hello world', got %v", m["message"])
	}
	if _, ok := m["time"]; !ok {
		t.Error("expected time field")
	}
}

func TestSlogHandler_Fields(t *testing.T) {
	var buf bytes.Buffer
	h := NewSlogHandler(&buf, nil)
	logger := slog.New(h)

	logger.Info("request",
		"method", "GET",
		"status", 200,
		"path", "/api/users",
		"duration", time.Second,
		"authenticated", true,
		"latency", 0.123,
	)

	var m map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON: %v\nbuf: %s", err, buf.String())
	}

	if m["method"] != "GET" {
		t.Errorf("expected method GET, got %v", m["method"])
	}
	if m["status"] != float64(200) {
		t.Errorf("expected status 200, got %v", m["status"])
	}
	if m["path"] != "/api/users" {
		t.Errorf("expected path /api/users, got %v", m["path"])
	}
	if m["authenticated"] != true {
		t.Errorf("expected authenticated true, got %v", m["authenticated"])
	}
}

func TestSlogHandler_Levels(t *testing.T) {
	tests := []struct {
		logFunc func(*slog.Logger, string, ...any)
		want    string
	}{
		{(*slog.Logger).Debug, "debug"},
		{(*slog.Logger).Info, "info"},
		{(*slog.Logger).Warn, "warn"},
		{(*slog.Logger).Error, "error"},
	}

	for _, tt := range tests {
		var buf bytes.Buffer
		h := NewSlogHandler(&buf, &SlogHandlerOptions{
			Level: slog.LevelDebug,
		})
		logger := slog.New(h)

		tt.logFunc(logger, "test")

		var m map[string]interface{}
		if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
			t.Fatalf("invalid JSON for level %s: %v", tt.want, err)
		}
		if m["level"] != tt.want {
			t.Errorf("expected level %s, got %v", tt.want, m["level"])
		}
	}
}

func TestSlogHandler_LevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	h := NewSlogHandler(&buf, &SlogHandlerOptions{
		Level: slog.LevelWarn,
	})
	logger := slog.New(h)

	logger.Info("should be filtered")
	if buf.Len() > 0 {
		t.Error("info message should have been filtered at warn level")
	}

	logger.Warn("should appear")
	if buf.Len() == 0 {
		t.Error("warn message should have appeared")
	}
}

func TestSlogHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer
	h := NewSlogHandler(&buf, nil)
	logger := slog.New(h).With("service", "api", "version", "v3")

	logger.Info("started")

	var m map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if m["service"] != "api" {
		t.Errorf("expected service api, got %v", m["service"])
	}
	if m["version"] != "v3" {
		t.Errorf("expected version v3, got %v", m["version"])
	}
}

func TestSlogHandler_WithGroup(t *testing.T) {
	var buf bytes.Buffer
	h := NewSlogHandler(&buf, nil)
	logger := slog.New(h).WithGroup("request")

	logger.Info("handled", "method", "POST", "status", 201)

	var m map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON: %v\nbuf: %s", err, buf.String())
	}

	req, ok := m["request"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected request to be nested object, got %T: %v", m["request"], m["request"])
	}
	if req["method"] != "POST" {
		t.Errorf("expected request.method POST, got %v", req["method"])
	}
	if req["status"] != float64(201) {
		t.Errorf("expected request.status 201, got %v", req["status"])
	}
}

func TestSlogHandler_Enabled(t *testing.T) {
	h := NewSlogHandler(nil, &SlogHandlerOptions{Level: slog.LevelWarn})

	if h.Enabled(context.TODO(), slog.LevelDebug) {
		t.Error("debug should not be enabled at warn level")
	}
	if h.Enabled(context.TODO(), slog.LevelInfo) {
		t.Error("info should not be enabled at warn level")
	}
	if !h.Enabled(context.TODO(), slog.LevelWarn) {
		t.Error("warn should be enabled at warn level")
	}
	if !h.Enabled(context.TODO(), slog.LevelError) {
		t.Error("error should be enabled at warn level")
	}
}

func TestSlogHandler_JSONEscaping(t *testing.T) {
	var buf bytes.Buffer
	h := NewSlogHandler(&buf, nil)
	logger := slog.New(h)

	logger.Info("test", "input", "value with \"quotes\" and \nnewline")

	// Should be valid JSON
	var m map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON with special chars: %v\nbuf: %s", err, buf.String())
	}

	if !strings.Contains(m["input"].(string), "quotes") {
		t.Error("expected escaped quotes in output")
	}
}

func TestSlogHandler_EmptyMessage(t *testing.T) {
	var buf bytes.Buffer
	h := NewSlogHandler(&buf, nil)
	logger := slog.New(h)

	logger.Info("", "key", "value")

	var m map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	// Empty message should not be in output
	if _, ok := m["message"]; ok {
		t.Error("empty message should not produce a message field")
	}
}

func TestSlogHandler_NilOptions(t *testing.T) {
	var buf bytes.Buffer
	h := NewSlogHandler(&buf, nil)

	// Should default to info level
	if h.Enabled(context.TODO(), slog.LevelDebug) {
		t.Error("debug should not be enabled with nil options (default info)")
	}
	if !h.Enabled(context.TODO(), slog.LevelInfo) {
		t.Error("info should be enabled with nil options")
	}
}

func BenchmarkSlogHandler(b *testing.B) {
	var buf bytes.Buffer
	h := NewSlogHandler(&buf, nil)
	logger := slog.New(h)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		logger.Info("request handled",
			"method", "GET",
			"status", 200,
			"path", "/api/users",
		)
	}
}

func BenchmarkSlogHandler_Disabled(b *testing.B) {
	var buf bytes.Buffer
	h := NewSlogHandler(&buf, &SlogHandlerOptions{Level: slog.LevelError})
	logger := slog.New(h)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		logger.Info("filtered out", "key", "value")
	}
}

// AddSource resolves the call site on every record, which is where the
// handler's allocations go; without it the path allocates nothing.
func BenchmarkSlogHandler_AddSource(b *testing.B) {
	var buf bytes.Buffer
	h := NewSlogHandler(&buf, &SlogHandlerOptions{AddSource: true})
	logger := slog.New(h)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		logger.Info("request handled",
			"method", "GET",
			"status", 200,
			"path", "/api/users",
		)
	}
}
