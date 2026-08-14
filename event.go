package bolt

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	oteltrace "go.opentelemetry.io/otel/trace"
)

type Event struct {
	buf   []byte // The raw buffer for building the log line.
	level Level
	l     *Logger
}

// Global pool for event objects.
var eventPool = &sync.Pool{
	New: func() interface{} {
		return &Event{
			buf: make([]byte, 0, DefaultBufferSize), // Start with larger buffer
		}
	},
}

// Logger returns a new Logger with the event's fields as context.
func (e *Event) Logger() *Logger {
	// Remove the leading comma if present
	contextBuf := e.buf
	if len(contextBuf) > 0 && contextBuf[0] == ',' {
		contextBuf = contextBuf[1:]
	}
	// Create new logger with atomic level
	newLogger := &Logger{handler: e.l.handler, context: contextBuf, errorHandler: e.l.errorHandler, hooks: e.l.hooks, eventHooks: e.l.eventHooks}
	atomic.StoreInt64(&newLogger.level, atomic.LoadInt64(&e.l.level))
	return newLogger
}

func (e *Event) Str(key, value string) *Event {
	if e.l == nil {
		return e
	}

	// Validate inputs for security
	if err := validateKey(key); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("invalid key in Str(): %w", err))
		}
		return e
	}
	if err := validateValue(value); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("invalid value in Str(): %w", err))
		}
		return e
	}

	// Check buffer size before adding content
	if err := checkBufferSize(e.buf); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("buffer size limit exceeded in Str(): %w", err))
		}
		return e
	}

	e.buf = append(e.buf, ',')
	e.buf = append(e.buf, '"')
	e.buf = appendJSONString(e.buf, key)
	e.buf = append(e.buf, `":"`...)
	e.buf = appendJSONString(e.buf, value)
	e.buf = append(e.buf, '"')
	return e
}

// Stringer adds a field whose value is obtained by calling the String method of
// a [fmt.Stringer]. If val is nil, the field value is JSON null.
func (e *Event) Stringer(key string, val fmt.Stringer) *Event {
	if e.l == nil {
		return e
	}
	if val == nil {
		if err := validateKey(key); err != nil {
			if e.l.errorHandler != nil {
				e.l.errorHandler(fmt.Errorf("invalid key in Stringer(): %w", err))
			}
			return e
		}
		e.buf = append(e.buf, ',')
		e.buf = append(e.buf, '"')
		e.buf = appendJSONString(e.buf, key)
		e.buf = append(e.buf, `":null`...)
		return e
	}
	return e.Str(key, val.String())
}

// Int adds an integer field to the event using fast conversion.
func (e *Event) Int(key string, value int) *Event {
	if e.l == nil {
		return e
	}

	// Validate key for security
	if err := validateKey(key); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("invalid key in Int(): %w", err))
		}
		return e
	}

	e.buf = append(e.buf, ',')
	e.buf = append(e.buf, '"')
	e.buf = appendJSONString(e.buf, key)
	e.buf = append(e.buf, `":`...)
	e.buf = appendInt(e.buf, value)
	return e
}

// Bool adds a boolean field to the event using fast conversion.
func (e *Event) Bool(key string, value bool) *Event {
	if e.l == nil {
		return e
	}

	// Validate key for security
	if err := validateKey(key); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("invalid key in Bool(): %w", err))
		}
		return e
	}

	e.buf = append(e.buf, ',')
	e.buf = append(e.buf, '"')
	e.buf = appendJSONString(e.buf, key)
	e.buf = append(e.buf, `":`...)
	e.buf = appendBool(e.buf, value)
	return e
}

// Float64 adds a float64 field with 6 decimal precision (zero-allocation).
//
// IMPORTANT: This method uses a custom formatter that limits precision to 6 decimal
// places to achieve zero allocations. For values requiring full precision, use Any()
// which delegates to encoding/json (allocates but preserves full precision).
//
// Precision examples:
//   - 99.99      → "99.989999" (6 decimals, minor rounding)
//   - 3.14159265 → "3.141592"  (6 decimals, truncated)
//   - 1.23e100   → "1.23e+100" (scientific notation for very large/small)
//
// Special values:
//   - NaN        → "NaN"
//   - +Infinity  → "+Inf" (quoted)
//   - -Infinity  → "-Inf" (quoted)
//   - -0.0       → 0 (negative zero not preserved)
//
// For financial/scientific applications requiring exact precision:
//
//	logger.Any("precise_value", 99.99)  // Full precision, allocates
//
// For performance-critical logging where 6 decimals suffice:
//
//	logger.Float64("fast_value", 99.99) // Zero allocation, 6 decimals
func (e *Event) Float64(key string, value float64) *Event {
	if e.l == nil {
		return e
	}

	// Validate key for security
	if err := validateKey(key); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("invalid key in Float64(): %w", err))
		}
		return e
	}

	e.buf = append(e.buf, ',')
	e.buf = append(e.buf, '"')
	e.buf = appendJSONString(e.buf, key)
	e.buf = append(e.buf, `":`...)
	e.buf = appendFloat64(e.buf, value)
	return e
}

func (e *Event) Time(key string, value time.Time) *Event {
	if e.l == nil {
		return e
	}

	// Validate key for security
	if err := validateKey(key); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("invalid key in Time(): %w", err))
		}
		return e
	}

	e.buf = append(e.buf, ',')
	e.buf = append(e.buf, '"')
	e.buf = appendJSONString(e.buf, key)
	e.buf = append(e.buf, `":"`...)
	e.buf = appendRFC3339(e.buf, value)
	e.buf = append(e.buf, '"')
	return e
}

func (e *Event) Dur(key string, value time.Duration) *Event {
	if e.l == nil {
		return e
	}

	// Validate key for security
	if err := validateKey(key); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("invalid key in Dur(): %w", err))
		}
		return e
	}

	e.buf = append(e.buf, ',')
	e.buf = append(e.buf, '"')
	e.buf = appendJSONString(e.buf, key)
	e.buf = append(e.buf, `":`...)
	e.buf = appendInt(e.buf, int(value.Nanoseconds()))
	return e
}

func (e *Event) Uint(key string, value uint) *Event {
	if e.l == nil {
		return e
	}

	// Validate key for security
	if err := validateKey(key); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("invalid key in Uint(): %w", err))
		}
		return e
	}

	e.buf = append(e.buf, ',')
	e.buf = append(e.buf, '"')
	e.buf = appendJSONString(e.buf, key)
	e.buf = append(e.buf, `":`...)
	e.buf = appendUint(e.buf, uint64(value))
	return e
}

func (e *Event) Any(key string, value interface{}) *Event {
	if e.l == nil {
		return e
	}

	// Validate key for security
	if err := validateKey(key); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("invalid key in Any(): %w", err))
		}
		return e
	}

	e.buf = append(e.buf, ',')
	e.buf = append(e.buf, '"')
	e.buf = appendJSONString(e.buf, key)
	e.buf = append(e.buf, `":`...)
	marshaledValue, err := json.Marshal(value)
	if err != nil {
		// Handle error with proper JSON escaping
		errorMsg := fmt.Sprintf("!ERROR: %v!", err)
		e.buf = append(e.buf, '"')
		e.buf = appendJSONString(e.buf, errorMsg)
		e.buf = append(e.buf, '"')
	} else {
		e.buf = append(e.buf, marshaledValue...)
	}
	return e
}

func (e *Event) Err(err error) *Event {
	if e.l == nil || err == nil {
		return e
	}
	return e.Str("error", err.Error())
}

// Msg sends the event to the handler for processing.
// This is always the final method in the chain.
func (e *Event) Msg(message string) {
	if e.l == nil {
		return // No-op for disabled events
	}

	// Validate message length
	if err := validateValue(message); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("invalid message in Msg(): %w", err))
		}
		return
	}

	// Run legacy hooks first; if any returns false, suppress the event.
	for _, hook := range e.l.hooks {
		if !hook.Run(e.level, message) {
			e.buf = e.buf[:0]
			e.l = nil
			eventPool.Put(e)
			return
		}
	}

	// Run field-aware hooks. Same suppression semantics as legacy hooks.
	// EventHooks may inspect the in-flight buffer via e.Buffer() / e.WalkFields()
	// and may add fields by calling Str/Int/etc on the event.
	for _, hook := range e.l.eventHooks {
		if !hook.Run(e, message) {
			e.buf = e.buf[:0]
			e.l = nil
			eventPool.Put(e)
			return
		}
	}

	// Check buffer size before finalizing
	if err := checkBufferSize(e.buf); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("buffer size limit exceeded in Msg(): %w", err))
		}
		return
	}

	// Add message with proper JSON escaping
	e.buf = append(e.buf, `,"message":"`...)
	e.buf = appendJSONString(e.buf, message)
	e.buf = append(e.buf, '"')

	// Finalize JSON and add newline
	e.buf = append(e.buf, '}')
	e.buf = append(e.buf, '\n')

	// Pass the event to the handler with proper error handling
	if err := e.l.handler.Write(e); err != nil && e.l.errorHandler != nil {
		e.l.errorHandler(fmt.Errorf("handler write failed: %w", err))
	}

	// Capture FATAL before recycling so we can exit after the buffer is freed.
	fatal := e.level == FATAL

	// Reset the buffer and put the event back into the pool. Drop oversized
	// buffers so the pool cannot retain rare 1MB allocations forever.
	if cap(e.buf) > PoolBufferCap {
		e.buf = nil
	} else {
		e.buf = e.buf[:0]
	}
	e.l = nil // Clear logger reference
	eventPool.Put(e)

	if fatal {
		exitFunc(1)
	}
}

func (e *Event) Hex(key string, value []byte) *Event {
	if e.l == nil {
		return e
	}

	// Validate key for security
	if err := validateKey(key); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("invalid key in Hex(): %w", err))
		}
		return e
	}

	e.buf = append(e.buf, ',')
	e.buf = append(e.buf, '"')
	e.buf = appendJSONString(e.buf, key)
	e.buf = append(e.buf, `":"`...)
	e.buf = append(e.buf, hex.EncodeToString(value)...)
	e.buf = append(e.buf, '"')
	return e
}

// Base64 adds a base64-encoded field to the event.
func (e *Event) Base64(key string, value []byte) *Event {
	if e.l == nil {
		return e
	}

	// Validate key for security
	if err := validateKey(key); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("invalid key in Base64(): %w", err))
		}
		return e
	}

	e.buf = append(e.buf, ',')
	e.buf = append(e.buf, '"')
	e.buf = appendJSONString(e.buf, key)
	e.buf = append(e.buf, `":"`...)
	e.buf = append(e.buf, base64.StdEncoding.EncodeToString(value)...)
	e.buf = append(e.buf, '"')
	return e
}

// IPAddr adds a net.IP address field to the event. IPv4 addresses are formatted
// as dotted-decimal (e.g. "192.168.1.1"), IPv6 as colon-hex notation.
// If ip is nil, the field value is JSON null. This method is zero-allocation.
func (e *Event) IPAddr(key string, ip net.IP) *Event {
	if e.l == nil {
		return e
	}
	if err := validateKey(key); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("invalid key in IPAddr(): %w", err))
		}
		return e
	}
	if ip == nil {
		e.buf = append(e.buf, ',')
		e.buf = append(e.buf, '"')
		e.buf = appendJSONString(e.buf, key)
		e.buf = append(e.buf, `":null`...)
		return e
	}
	e.buf = append(e.buf, ',')
	e.buf = append(e.buf, '"')
	e.buf = appendJSONString(e.buf, key)
	e.buf = append(e.buf, `":"`...)
	e.buf = appendIP(e.buf, ip)
	e.buf = append(e.buf, '"')
	return e
}

// Bytes adds a byte array field as a string to the event.
func (e *Event) Bytes(key string, value []byte) *Event {
	if e.l == nil {
		return e
	}
	return e.Str(key, string(value))
}

// Stack adds a stack trace field to the event.
func (e *Event) Stack() *Event {
	if e.l == nil {
		return e
	}
	buf := make([]byte, StackTraceBufferSize)
	n := runtime.Stack(buf, false)
	return e.Str("stack", string(buf[:n]))
}

// Caller adds caller information (file:line) to the event.
func (e *Event) Caller() *Event {
	if e.l == nil {
		return e
	}
	_, file, line, ok := runtime.Caller(1)
	if !ok {
		return e.Str("caller", "unknown")
	}
	// Extract just the filename
	if idx := strings.LastIndex(file, "/"); idx >= 0 {
		file = file[idx+1:]
	}
	return e.Str("caller", fmt.Sprintf("%s:%d", file, line))
}

// CallerSkip adds caller information (file:line) to the event, skipping the
// specified number of additional stack frames. This is useful when Bolt is
// wrapped in helper functions and you need the caller of the wrapper.
func (e *Event) CallerSkip(skip int) *Event {
	if e.l == nil {
		return e
	}
	_, file, line, ok := runtime.Caller(1 + skip)
	if !ok {
		return e.Str("caller", "unknown")
	}
	if idx := strings.LastIndex(file, "/"); idx >= 0 {
		file = file[idx+1:]
	}
	return e.Str("caller", fmt.Sprintf("%s:%d", file, line))
}

// RandID adds a random ID field to the event for request tracing.
func (e *Event) RandID(key string) *Event {
	if e.l == nil {
		return e
	}
	// Generate a random 8-byte ID
	id := make([]byte, 8)
	_, _ = rand.Read(id) // crypto/rand.Read never fails
	return e.Hex(key, id)
}

// Fields allows adding multiple fields at once from a map.
func (e *Event) Fields(fields map[string]interface{}) *Event {
	if e.l == nil {
		return e
	}
	for k, v := range fields {
		e.Any(k, v)
	}
	return e
}

// Ints adds an integer slice field to the event as a JSON array.
// This method is zero-allocation.
func (e *Event) Ints(key string, values []int) *Event {
	if e.l == nil {
		return e
	}
	if err := validateKey(key); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("invalid key in Ints(): %w", err))
		}
		return e
	}
	e.buf = append(e.buf, ',')
	e.buf = append(e.buf, '"')
	e.buf = appendJSONString(e.buf, key)
	e.buf = append(e.buf, `":[`...)
	for i, v := range values {
		if i > 0 {
			e.buf = append(e.buf, ',')
		}
		e.buf = appendInt(e.buf, v)
	}
	e.buf = append(e.buf, ']')
	return e
}

// Strs adds a string slice field to the event as a JSON array.
// This method is zero-allocation.
func (e *Event) Strs(key string, values []string) *Event {
	if e.l == nil {
		return e
	}
	if err := validateKey(key); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("invalid key in Strs(): %w", err))
		}
		return e
	}
	e.buf = append(e.buf, ',')
	e.buf = append(e.buf, '"')
	e.buf = appendJSONString(e.buf, key)
	e.buf = append(e.buf, `":[`...)
	for i, v := range values {
		if i > 0 {
			e.buf = append(e.buf, ',')
		}
		e.buf = append(e.buf, '"')
		e.buf = appendJSONString(e.buf, v)
		e.buf = append(e.buf, '"')
	}
	e.buf = append(e.buf, ']')
	return e
}

// Dict adds a sub-object field to the event. The provided function is called
// with a temporary Event that collects the sub-object's fields. The fields
// are then embedded as a JSON object under the given key.
func (e *Event) Dict(key string, fn func(d *Event)) *Event {
	if e.l == nil {
		return e
	}
	if err := validateKey(key); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("invalid key in Dict(): %w", err))
		}
		return e
	}
	sub := eventPool.Get().(*Event)
	sub.buf = sub.buf[:0]
	sub.level = e.level
	sub.l = e.l
	fn(sub)
	e.buf = append(e.buf, ',')
	e.buf = append(e.buf, '"')
	e.buf = appendJSONString(e.buf, key)
	e.buf = append(e.buf, `":{`...)
	subBuf := sub.buf
	if len(subBuf) > 0 && subBuf[0] == ',' {
		subBuf = subBuf[1:]
	}
	e.buf = append(e.buf, subBuf...)
	e.buf = append(e.buf, '}')
	sub.buf = sub.buf[:0]
	sub.l = nil
	eventPool.Put(sub)
	return e
}

// Int64 adds a 64-bit integer field to the event.
func (e *Event) Int64(key string, value int64) *Event {
	if e.l == nil {
		return e
	}

	// Validate key for security
	if err := validateKey(key); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("invalid key in Int64(): %w", err))
		}
		return e
	}

	e.buf = append(e.buf, ',')
	e.buf = append(e.buf, '"')
	e.buf = appendJSONString(e.buf, key)
	e.buf = append(e.buf, `":`...)
	e.buf = appendInt(e.buf, int(value))
	return e
}

// Int32 adds a 32-bit integer field to the event.
func (e *Event) Int32(key string, value int32) *Event {
	if e.l == nil {
		return e
	}

	// Validate key for security
	if err := validateKey(key); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("invalid key in Int32(): %w", err))
		}
		return e
	}

	e.buf = append(e.buf, ',')
	e.buf = append(e.buf, '"')
	e.buf = appendJSONString(e.buf, key)
	e.buf = append(e.buf, `":`...)
	e.buf = appendInt(e.buf, int(value))
	return e
}

// Int16 adds a 16-bit integer field to the event.
func (e *Event) Int16(key string, value int16) *Event {
	if e.l == nil {
		return e
	}

	// Validate key for security
	if err := validateKey(key); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("invalid key in Int16(): %w", err))
		}
		return e
	}

	e.buf = append(e.buf, ',')
	e.buf = append(e.buf, '"')
	e.buf = appendJSONString(e.buf, key)
	e.buf = append(e.buf, `":`...)
	e.buf = appendInt(e.buf, int(value))
	return e
}

// Int8 adds an 8-bit integer field to the event.
func (e *Event) Int8(key string, value int8) *Event {
	if e.l == nil {
		return e
	}

	// Validate key for security
	if err := validateKey(key); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("invalid key in Int8(): %w", err))
		}
		return e
	}

	e.buf = append(e.buf, ',')
	e.buf = append(e.buf, '"')
	e.buf = appendJSONString(e.buf, key)
	e.buf = append(e.buf, `":`...)
	e.buf = appendInt(e.buf, int(value))
	return e
}

// Uint64 adds a 64-bit unsigned integer field to the event.
func (e *Event) Uint64(key string, value uint64) *Event {
	if e.l == nil {
		return e
	}

	// Validate key for security
	if err := validateKey(key); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("invalid key in Uint64(): %w", err))
		}
		return e
	}

	e.buf = append(e.buf, ',')
	e.buf = append(e.buf, '"')
	e.buf = appendJSONString(e.buf, key)
	e.buf = append(e.buf, `":`...)
	e.buf = appendUint(e.buf, value)
	return e
}

// Uint32 adds a 32-bit unsigned integer field to the event.
func (e *Event) Uint32(key string, value uint32) *Event {
	if e.l == nil {
		return e
	}
	if err := validateKey(key); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("invalid key in Uint32(): %w", err))
		}
		return e
	}
	e.buf = append(e.buf, ',')
	e.buf = append(e.buf, '"')
	e.buf = appendJSONString(e.buf, key)
	e.buf = append(e.buf, `":`...)
	e.buf = appendUint(e.buf, uint64(value))
	return e
}

// Uint16 adds a 16-bit unsigned integer field to the event.
func (e *Event) Uint16(key string, value uint16) *Event {
	if e.l == nil {
		return e
	}
	if err := validateKey(key); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("invalid key in Uint16(): %w", err))
		}
		return e
	}
	e.buf = append(e.buf, ',')
	e.buf = append(e.buf, '"')
	e.buf = appendJSONString(e.buf, key)
	e.buf = append(e.buf, `":`...)
	e.buf = appendUint(e.buf, uint64(value))
	return e
}

// Uint8 adds an 8-bit unsigned integer field to the event.
func (e *Event) Uint8(key string, value uint8) *Event {
	if e.l == nil {
		return e
	}
	if err := validateKey(key); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("invalid key in Uint8(): %w", err))
		}
		return e
	}
	e.buf = append(e.buf, ',')
	e.buf = append(e.buf, '"')
	e.buf = appendJSONString(e.buf, key)
	e.buf = append(e.buf, `":`...)
	e.buf = appendUint(e.buf, uint64(value))
	return e
}

// Counter adds an atomic counter value to the event.
func (e *Event) Counter(key string, counter *int64) *Event {
	if e.l == nil {
		return e
	}

	// Validate key for security
	if err := validateKey(key); err != nil {
		if e.l.errorHandler != nil {
			e.l.errorHandler(fmt.Errorf("invalid key in Counter(): %w", err))
		}
		return e
	}

	value := atomic.LoadInt64(counter)
	return e.Int64(key, value)
}

// Timestamp adds the current timestamp to the event.
func (e *Event) Timestamp() *Event {
	if e.l == nil {
		return e
	}
	return e.Time("timestamp", time.Now())
}

// Interface adds an interface{} field to the event (alias for Any).
func (e *Event) Interface(key string, value interface{}) *Event {
	return e.Any(key, value)
}

// Printf adds a formatted message to the event.
func (e *Event) Printf(format string, args ...interface{}) {
	if e.l == nil {
		return
	}
	e.Msg(fmt.Sprintf(format, args...))
}

// Send is an alias for Msg for consistency with other logging libraries.
func (e *Event) Send() {
	e.Msg("")
}

// Level returns the event's log level. Intended for [EventHook]
// implementations to inspect events mid-build.
func (e *Event) Level() Level {
	return e.level
}

// Buffer returns the in-flight JSON buffer. Intended for [EventHook]
// implementations that need to inspect the encoded record before it
// is written.
//
// The returned slice ALIASES the event's internal buffer; callers MUST
// NOT mutate it. The buffer is partially-formed JSON of the shape
// `{"level":"info","key":"value"...` (no closing brace, no message
// field yet — those are appended by Msg after hooks run). Use
// [Event.WalkFields] for structured field iteration.
func (e *Event) Buffer() []byte {
	return e.buf
}

// WalkFields invokes fn for each (key, value) pair already encoded into
// the event. Iteration stops early if fn returns false. Returns the
// number of fields visited.
//
// The key and value byte slices passed to fn alias the event's internal
// buffer and are valid only for the duration of the call; copy them
// before retaining. String values are presented WITHOUT the surrounding
// quotes; non-string values (numbers, booleans, nested objects) are
// returned as the raw JSON literal.
//
// Intended for [EventHook] implementations doing redaction screening,
// cost accounting, sensitivity tagging, etc. Walking is O(buffer size),
// so prefer using it from sampling/redaction hooks rather than from a
// per-event metric counter.
func (e *Event) WalkFields(fn func(key, value []byte) bool) int {
	if len(e.buf) == 0 || e.buf[0] != '{' {
		return 0
	}
	count := 0
	i := 1 // skip opening '{'
	for i < len(e.buf) {
		i = skipWhitespace(e.buf, i)
		if i >= len(e.buf) || e.buf[i] == '}' {
			break
		}
		key, ni := extractJSONKey(e.buf, i)
		if key == nil {
			i++
			continue
		}
		i = ni
		i = skipWhitespace(e.buf, i)
		if i < len(e.buf) && e.buf[i] == ':' {
			i++
		}
		i = skipWhitespace(e.buf, i)
		if i >= len(e.buf) {
			break
		}
		value, ni := extractJSONValue(e.buf, i)
		i = ni
		count++
		if !fn(key, value) {
			return count
		}
		i = skipCommaIfPresent(e.buf, i)
	}
	return count
}

// Ctx attaches the active trace and span IDs from ctx to this event.
//
// This is the zero-allocation way to correlate a log line with a trace, and the
// one to reach for:
//
//	log.Info().Ctx(ctx).Str("order", id).Msg("processing")
//
// It writes into the event's pooled buffer — the same append path Str uses — so
// correlation costs nothing beyond the bytes. Logger.Ctx, which this replaces,
// had to build a whole derived Logger to carry the same two fields, and paid for
// it on every line (#111).
//
// Reading the context at emit time is also more accurate than binding it once:
// a logger derived by Logger.Ctx keeps the span it was created with, so a
// request-scoped logger reused inside a child span reports the parent's span ID.
// This reports the span that is actually active.
//
// A context with no valid span adds nothing. Call it before the event's other
// fields to keep trace_id and span_id leading, as Logger.Ctx placed them.
func (e *Event) Ctx(ctx context.Context) *Event {
	if e.l == nil {
		return e
	}
	sc := oteltrace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return e
	}
	// Both IDs are fixed-length hex from OTel: no JSON escaping, and none of the
	// key/value validation Str performs, because there is no input here that
	// could be invalid.
	tid, sid := sc.TraceID(), sc.SpanID()
	e.buf = append(e.buf, `,"trace_id":"`...)
	e.buf = hex.AppendEncode(e.buf, tid[:])
	e.buf = append(e.buf, `","span_id":"`...)
	e.buf = hex.AppendEncode(e.buf, sid[:])
	e.buf = append(e.buf, '"')
	return e
}
