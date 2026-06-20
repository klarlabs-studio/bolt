package bolt

import "testing"

// TestParseLevel covers the public ParseLevel helper. ParseLevel is a pure
// string-to-Level mapping with no global state — bolt has no global logger and
// reads no environment variables.
func TestParseLevel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Level
	}{
		{name: "trace", input: "trace", expected: TRACE},
		{name: "debug", input: "debug", expected: DEBUG},
		{name: "info", input: "info", expected: INFO},
		{name: "warn", input: "warn", expected: WARN},
		{name: "error", input: "error", expected: ERROR},
		{name: "fatal", input: "fatal", expected: FATAL},
		{name: "unknown defaults to info", input: "foo", expected: INFO},
		{name: "empty defaults to info", input: "", expected: INFO},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseLevel(tt.input); got != tt.expected {
				t.Errorf("ParseLevel(%q) = %s, want %s", tt.input, got, tt.expected)
			}
		})
	}
}
