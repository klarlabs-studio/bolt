package bolt

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"testing/slogtest"
)

// TestSlogConformance runs the standard library slog conformance suite against
// SlogHandler. The suite exercises Group nesting, time-zero handling, empty
// keys, ResolveAttr, and other parts of the slog.Handler contract that bespoke
// tests routinely miss. It runs with and without AddSource, since the source
// group is written at the top level alongside the fields the suite checks.
func TestSlogConformance(t *testing.T) {
	for name, opts := range map[string]*SlogHandlerOptions{
		"default":   nil,
		"AddSource": {AddSource: true},
	} {
		t.Run(name, func(t *testing.T) { testSlogConformance(t, opts) })
	}
}

func testSlogConformance(t *testing.T, opts *SlogHandlerOptions) {
	var buf bytes.Buffer
	h := NewSlogHandler(&buf, opts)

	results := func() []map[string]any {
		var ms []map[string]any
		for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
			if line == "" {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				t.Fatalf("invalid JSON record %q: %v", line, err)
			}
			// slogtest expects "msg" not "message".
			if v, ok := m["message"]; ok {
				m["msg"] = v
				delete(m, "message")
			}
			ms = append(ms, m)
		}
		return ms
	}

	if err := slogtest.TestHandler(h, results); err != nil {
		t.Errorf("slogtest.TestHandler: %v", err)
	}
}
