package bolt

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// TestSlogHandlerSatisfiesInterface is a compile-time and runtime guarantee
// that bolt's slog adapter is a conformant slog.Handler. The package-level
// assertion `var _ slog.Handler = (*SlogHandler)(nil)` in slog.go enforces this
// at build time; this test documents the contract.
func TestSlogHandlerSatisfiesInterface(t *testing.T) {
	var _ slog.Handler = (*SlogHandler)(nil)

	var buf bytes.Buffer
	var h slog.Handler = NewSlogHandler(&buf, nil)
	slog.New(h).Info("ok")
	if !strings.Contains(buf.String(), `"message":"ok"`) {
		t.Errorf("slog.Handler adapter produced no output: %q", buf.String())
	}
}

// TestNewSlogJSONHandler verifies the spec-named slog constructor: the
// documented, clean path for using bolt as a slog.Handler in production. This
// is the compat surface the spec's `slog.New(bolt.NewJSONHandler(...))` example
// refers to.
func TestNewSlogJSONHandler(t *testing.T) {
	var buf bytes.Buffer

	// NewSlogJSONHandler returns slog.Handler by its signature.
	h := NewSlogJSONHandler(&buf, nil)
	logger := slog.New(h)
	logger.Info("request handled", "method", "GET", "status", 200)

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON: %v\nbuf: %s", err, buf.String())
	}
	if m["level"] != "info" {
		t.Errorf("expected level info, got %v", m["level"])
	}
	if m["method"] != "GET" {
		t.Errorf("expected method GET, got %v", m["method"])
	}
	if m["status"] != float64(200) {
		t.Errorf("expected status 200, got %v", m["status"])
	}
}

// TestNewSlogJSONHandlerSetDefault exercises the full spec compat example end
// to end: construct a bolt-backed slog handler and install it as the default.
func TestNewSlogJSONHandlerSetDefault(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	slog.SetDefault(slog.New(NewSlogJSONHandler(&buf, nil)))
	slog.Info("via default", "k", "v")

	if !strings.Contains(buf.String(), `"message":"via default"`) {
		t.Errorf("default slog logger produced no bolt output: %q", buf.String())
	}
}
