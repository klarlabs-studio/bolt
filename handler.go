package bolt

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"time"
)

// JSONHandler formats logs as JSON. Safe for concurrent use by multiple
// goroutines: writes to the underlying io.Writer are serialized so log records
// never interleave (io.Writer.Write is only guaranteed atomic up to PIPE_BUF).
type JSONHandler struct {
	mu  sync.Mutex
	out io.Writer
}

// NewJSONHandler creates a new JSON handler.
func NewJSONHandler(out io.Writer) *JSONHandler {
	return &JSONHandler{out: out}
}

// Write handles the log event.
func (h *JSONHandler) Write(e *Event) error {
	h.mu.Lock()
	_, err := h.out.Write(e.buf)
	h.mu.Unlock()
	return err
}

// ConsoleHandler formats logs for human-readable console output. Safe for
// concurrent use by multiple goroutines: each event's worth of output is
// written under a single mutex so colorized records never interleave.
type ConsoleHandler struct {
	mu  sync.Mutex
	out io.Writer
}

// NewConsoleHandler creates a new ConsoleHandler.
func NewConsoleHandler(out io.Writer) *ConsoleHandler {
	return &ConsoleHandler{out: out}
}

// NewTextHandler creates a handler that writes human-readable, colorized text
// for development. It is the spec-named development handler and is an alias for
// [NewConsoleHandler] — both return the same [*ConsoleHandler].
//
//	logger := bolt.New(bolt.NewTextHandler(os.Stderr))
func NewTextHandler(out io.Writer) *ConsoleHandler {
	return NewConsoleHandler(out)
}

// Write handles the log event with zero allocations by streaming JSON parsing.
func (h *ConsoleHandler) Write(e *Event) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Extract level and message without unmarshaling (zero-allocation)
	level := extractJSONField(e.buf, "level")
	message := extractJSONField(e.buf, "message")

	// Get color for the level
	color := getColorForLevel(string(level))

	// Format timestamp (reuse buffer for efficiency)
	var timeBuf [25]byte // RFC3339 is max 25 chars
	timestamp := appendRFC3339(timeBuf[:0], time.Now())

	// Write level with color
	if _, err := h.out.Write([]byte(color)); err != nil {
		return fmt.Errorf("failed to write color: %w", err)
	}
	if _, err := h.out.Write(level); err != nil {
		return fmt.Errorf("failed to write level: %w", err)
	}

	// Reset color and write timestamp
	if _, err := h.out.Write([]byte("\x1b[0m[")); err != nil {
		return fmt.Errorf("failed to write reset: %w", err)
	}
	if _, err := h.out.Write(timestamp); err != nil {
		return fmt.Errorf("failed to write timestamp: %w", err)
	}

	// Write message
	if _, err := h.out.Write([]byte("] ")); err != nil {
		return fmt.Errorf("failed to write separator: %w", err)
	}
	if _, err := h.out.Write(message); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	// Write remaining fields by streaming through JSON
	if err := writeFieldsStreaming(h.out, e.buf, level, message); err != nil {
		return fmt.Errorf("failed to write fields: %w", err)
	}

	// Write newline
	if _, err := h.out.Write([]byte("\n")); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	return nil
}

// multiHandler is a Handler that writes to multiple handlers.
type multiHandler struct {
	handlers []Handler
}

// MultiHandler returns a Handler that writes to all provided handlers.
// The handlers slice is copied at construction, so the original slice can be
// safely modified afterward. Write returns the first error encountered.
func MultiHandler(handlers ...Handler) Handler {
	h := make([]Handler, len(handlers))
	copy(h, handlers)
	return &multiHandler{handlers: h}
}

// NewMultiHandler returns a Handler that writes to all provided handlers. It is
// the spec-named constructor and is an alias for [MultiHandler].
//
//	logger := bolt.New(bolt.NewMultiHandler(
//	    bolt.NewJSONHandler(logFile),
//	    bolt.NewTextHandler(os.Stdout),
//	))
func NewMultiHandler(handlers ...Handler) Handler {
	return MultiHandler(handlers...)
}

// Write sends the event to all handlers, returning the first error encountered.
func (m *multiHandler) Write(e *Event) error {
	var firstErr error
	for _, h := range m.handlers {
		if err := h.Write(e); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// findJSONFieldStart locates the start position of a JSON field value
// Returns -1 if field not found
func findJSONFieldStart(buf []byte, key string) int {
	searchPattern := []byte(`"` + key + `":`)
	idx := bytes.Index(buf, searchPattern)
	if idx == -1 {
		return -1
	}

	start := idx + len(searchPattern)
	// Skip whitespace after colon
	for start < len(buf) && (buf[start] == ' ' || buf[start] == '\t') {
		start++
	}

	if start >= len(buf) {
		return -1
	}

	return start
}

// extractStringValue extracts a quoted string value from position start
// Returns the unquoted value or nil if invalid
func extractStringValue(buf []byte, start int) []byte {
	if start >= len(buf) || buf[start] != '"' {
		return nil
	}

	start++ // Skip opening quote
	end := start

	// Find closing quote (handle escaped quotes)
	for end < len(buf) {
		if buf[end] == '"' && (end == start || buf[end-1] != '\\') {
			return buf[start:end]
		}
		end++
	}

	return nil
}

// extractNonStringValue extracts a non-string value (number, boolean, null)
// Returns the value with trailing whitespace trimmed
func extractNonStringValue(buf []byte, start int) []byte {
	end := start
	for end < len(buf) && buf[end] != ',' && buf[end] != '}' {
		end++
	}

	// Trim trailing whitespace
	for end > start && (buf[end-1] == ' ' || buf[end-1] == '\t') {
		end--
	}

	return buf[start:end]
}

// extractJSONField extracts a field value from JSON without unmarshaling.
// Returns the value as a byte slice (no allocation).
func extractJSONField(buf []byte, key string) []byte {
	start := findJSONFieldStart(buf, key)
	if start == -1 {
		return nil
	}

	// Check if value is a string (starts with ")
	if buf[start] == '"' {
		return extractStringValue(buf, start)
	}

	// Non-string value (number, boolean, null)
	return extractNonStringValue(buf, start)
}

// skipWhitespace advances index past whitespace characters
func skipWhitespace(buf []byte, i int) int {
	for i < len(buf) && (buf[i] == ' ' || buf[i] == '\t' || buf[i] == '\n') {
		i++
	}
	return i
}

// extractJSONKey extracts a JSON key starting at position i (should point to opening ")
// Returns key bytes and new position after closing "
func extractJSONKey(buf []byte, i int) ([]byte, int) {
	if i >= len(buf) || buf[i] != '"' {
		return nil, i
	}

	keyStart := i + 1
	keyEnd := keyStart
	for keyEnd < len(buf) && buf[keyEnd] != '"' {
		keyEnd++
	}

	if keyEnd >= len(buf) {
		return nil, len(buf)
	}

	return buf[keyStart:keyEnd], keyEnd + 1
}

// extractJSONValue extracts a JSON value starting at position i
// Returns value bytes and new position after value
func extractJSONValue(buf []byte, i int) ([]byte, int) {
	if i >= len(buf) {
		return nil, i
	}

	if buf[i] == '"' {
		// String value
		valueStart := i + 1
		valueEnd := valueStart
		for valueEnd < len(buf) && (buf[valueEnd] != '"' || (valueEnd != valueStart && buf[valueEnd-1] == '\\')) {
			valueEnd++
		}
		return buf[valueStart:valueEnd], valueEnd + 1
	}

	// Non-string value
	valueStart := i
	valueEnd := i
	for valueEnd < len(buf) && buf[valueEnd] != ',' && buf[valueEnd] != '}' {
		valueEnd++
	}

	// Trim trailing whitespace
	for valueEnd > valueStart && (buf[valueEnd-1] == ' ' || buf[valueEnd-1] == '\t') {
		valueEnd--
	}

	return buf[valueStart:valueEnd], valueEnd
}

// writeKeyValue writes a key=value pair to the writer
func writeKeyValue(w io.Writer, key, value []byte) error {
	if _, err := w.Write([]byte(" ")); err != nil {
		return err
	}
	if _, err := w.Write(key); err != nil {
		return err
	}
	if _, err := w.Write([]byte("=")); err != nil {
		return err
	}
	if _, err := w.Write(value); err != nil {
		return err
	}
	return nil
}

// skipCommaIfPresent advances past comma if found
func skipCommaIfPresent(buf []byte, i int) int {
	if i < len(buf) && buf[i] == ',' {
		return i + 1
	}
	return i
}

// isReservedField checks if field should be skipped (already written)
func isReservedField(key []byte) bool {
	return bytes.Equal(key, []byte("level")) || bytes.Equal(key, []byte("message"))
}

// writeFieldsStreaming writes additional fields by parsing JSON without allocations
func writeFieldsStreaming(w io.Writer, buf []byte, _ []byte, _ []byte) error {
	i := 1 // Skip opening {

	for i < len(buf) {
		i = skipWhitespace(buf, i)

		if i >= len(buf) || buf[i] == '}' {
			break
		}

		// Extract key
		key, newPos := extractJSONKey(buf, i)
		if key == nil {
			i++
			continue
		}
		i = newPos

		// Skip to value (colon and whitespace)
		i = skipWhitespace(buf, i)
		if i < len(buf) && buf[i] == ':' {
			i++
		}
		i = skipWhitespace(buf, i)

		if i >= len(buf) {
			break
		}

		// Extract value
		value, newPos := extractJSONValue(buf, i)
		i = newPos

		// Skip reserved fields (already written)
		if isReservedField(key) {
			i = skipCommaIfPresent(buf, i)
			continue
		}

		// Write field
		if err := writeKeyValue(w, key, value); err != nil {
			return err
		}

		i = skipCommaIfPresent(buf, i)
	}

	return nil
}

func getColorForLevel(level string) string {
	switch level {
	case infoStr:
		return "\x1b[34m" // Blue
	case warnStr:
		return "\x1b[33m" // Yellow
	case errorStr, fatalStr:
		return "\x1b[31m" // Red
	case debugStr, traceStr:
		return "\x1b[90m" // Bright Black (Gray)
	default:
		return "\x1b[0m" // Reset
	}
}
