package bolt

import (
	"bytes"
	"strings"
	"testing"
)

// TestNewTextHandler verifies the spec-named development handler. The spec's
// Core API lists bolt.NewTextHandler as the human-readable dev handler
// (alongside NewJSONHandler for production).
func TestNewTextHandler(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewTextHandler(&buf))
	logger.Info().Str("service", "api").Msg("Server starting")

	out := buf.String()
	if !strings.Contains(out, "Server starting") {
		t.Errorf("NewTextHandler output missing message: %q", out)
	}
	if !strings.Contains(out, "service=api") {
		t.Errorf("NewTextHandler output missing field: %q", out)
	}
}

// TestNewTextHandlerIsConsoleHandler verifies NewTextHandler is the spec name
// for the existing human-readable console handler.
func TestNewTextHandlerIsConsoleHandler(t *testing.T) {
	var buf bytes.Buffer
	if _, ok := any(NewTextHandler(&buf)).(*ConsoleHandler); !ok {
		t.Errorf("NewTextHandler should return *ConsoleHandler, got %T", NewTextHandler(&buf))
	}
}

// TestNewMultiHandler verifies the spec-named multi-output constructor. The
// spec's Core API lists bolt.NewMultiHandler(...).
func TestNewMultiHandler(t *testing.T) {
	var a, b bytes.Buffer
	logger := New(NewMultiHandler(
		NewJSONHandler(&a),
		NewJSONHandler(&b),
	))
	logger.Info().Str("k", "v").Msg("hello")

	for name, buf := range map[string]*bytes.Buffer{"a": &a, "b": &b} {
		if !strings.Contains(buf.String(), "hello") {
			t.Errorf("NewMultiHandler output %s missing message: %q", name, buf.String())
		}
	}
}

// TestNewMultiHandlerMixedFormats verifies the spec's JSON+Text composition.
func TestNewMultiHandlerMixedFormats(t *testing.T) {
	var jsonOut, textOut bytes.Buffer
	logger := New(NewMultiHandler(
		NewJSONHandler(&jsonOut),
		NewTextHandler(&textOut),
	))
	logger.Info().Str("region", "eu-west").Msg("ready")

	if !strings.Contains(jsonOut.String(), `"region":"eu-west"`) {
		t.Errorf("JSON output wrong: %q", jsonOut.String())
	}
	if !strings.Contains(textOut.String(), "region=eu-west") {
		t.Errorf("Text output wrong: %q", textOut.String())
	}
}
