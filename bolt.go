// Package bolt provides a high-performance, zero-allocation structured logging library for Go.
//
// Bolt is designed for applications that demand exceptional performance without compromising
// on features. It delivers sub-100ns logging operations with zero memory allocations in hot paths.
//
// # Key Features
//
// - Zero allocations in hot paths
// - Sub-100ns latency for logging operations
// - Type-safe field methods
// - JSON and console output formats
// - OpenTelemetry tracing integration
// - Environment variable configuration
// - Production-ready reliability
//
// # Performance
//
// Bolt achieves industry-leading performance:
//   - 105.2ns/op with 0 allocations
//   - 64% faster than Zerolog
//   - 80% faster than Zap
//   - 2603% faster than Logrus
//
// # Quick Start
//
// Basic usage with JSON output:
//
//	package main
//
//	import (
//	    "os"
//	    "go.klarlabs.de/bolt"
//	)
//
//	func main() {
//	    logger := bolt.New(bolt.NewJSONHandler(os.Stdout))
//
//	    logger.Info().
//	        Str("service", "auth").
//	        Int("user_id", 12345).
//	        Msg("User authenticated")
//	}
//
// Console output with colors:
//
//	logger := bolt.New(bolt.NewConsoleHandler(os.Stdout))
//	logger.Info().Str("status", "ready").Msg("Server started")
//
// # Configuration
//
// Bolt has no global logger and reads no environment variables. Loggers are
// always constructed explicitly and injected via constructors. Configure the
// level programmatically:
//
//	logger := bolt.New(bolt.NewJSONHandler(os.Stdout)).SetLevel(bolt.DEBUG)
//
// # Zero Allocations
//
// Bolt uses object pooling and direct serialization to achieve zero allocations:
//
//	// This logging operation performs 0 allocations
//	logger.Info().
//	    Str("method", "GET").
//	    Str("path", "/api/users").
//	    Int("status", 200).
//	    Dur("duration", time.Since(start)).
//	    Msg("Request completed")
//
// # OpenTelemetry Integration
//
// Bolt automatically extracts trace information from context:
//
//	ctx := context.Background()
//	logger.Info().Ctx(ctx).Msg("Operation completed")
//
// # Thread Safety
//
// All Bolt operations are thread-safe and can be used concurrently across goroutines.
// The library uses atomic operations for level changes and sync.Pool for event management.
//
// # Security Features
//
// Bolt includes comprehensive security protections:
//   - Automatic JSON escaping prevents log injection attacks
//   - Input validation with configurable size limits (keys: 256 chars, values: 64KB)
//   - Control character filtering in keys
//   - Buffer size limits prevent resource exhaustion (max 1MB per entry)
//   - Thread-safe operations prevent race conditions
//   - Secure error handling prevents information disclosure
//
// # Performance Characteristics
//
// Bolt delivers industry-leading performance:
//   - Zero allocations in hot paths through intelligent event pooling
//   - Sub-100ns latency for most logging operations
//   - Custom serialization optimized for common data types
//   - Lock-free event management with atomic synchronization
package bolt

import (
	"context"
	"io"
	"os"
	"sync/atomic"

	oteltrace "go.opentelemetry.io/otel/trace"
)

// Constants for buffer sizes and configuration
const (
	// DefaultBufferSize is the initial buffer size for events - increased to reduce reallocations
	DefaultBufferSize = 2048
	// MaxBufferSize is the maximum allowed buffer size to prevent unbounded growth
	MaxBufferSize = 1024 * 1024 // 1MB
	// PoolBufferCap is the maximum buffer capacity that may be returned to the
	// event pool. Buffers larger than this are dropped so the pool cannot retain
	// rare oversized allocations indefinitely.
	PoolBufferCap = 8192 // 8KB
	// StackTraceBufferSize is the buffer size for stack traces
	StackTraceBufferSize = 64 * 1024 // 64KB
	// DefaultFilePermissions for log files
	DefaultFilePermissions = 0644
	// MaxKeyLength is the maximum allowed key length
	MaxKeyLength = 256
	// MaxValueLength is the maximum allowed value length
	MaxValueLength = 64 * 1024 // 64KB
)

// exitFunc is called by Fatal-level events to terminate the process.
// Overridable for tests. Defaults to os.Exit.
var exitFunc = os.Exit

// Level string constants
const (
	traceStr   = "trace"
	debugStr   = "debug"
	infoStr    = "info"
	warnStr    = "warn"
	errorStr   = "error"
	fatalStr   = "fatal"
	consoleStr = "console"
)

// Level defines the logging level.
type Level int8

// String returns the string representation of the level.
func (l Level) String() string {
	switch l {
	case TRACE:
		return traceStr
	case DEBUG:
		return debugStr
	case INFO:
		return infoStr
	case WARN:
		return warnStr
	case ERROR:
		return errorStr
	case FATAL:
		return fatalStr
	default:
		return ""
	}
}

// Log levels.
const (
	TRACE Level = iota
	DEBUG
	INFO
	WARN
	ERROR
	FATAL
)

// slog-style level aliases. Prefer these in new code — they match the naming
// used by the standard library's [log/slog] package and most of the Go
// ecosystem. The SCREAMING_CASE constants above are retained for backward
// compatibility and remain functionally identical.
const (
	LevelTrace = TRACE
	LevelDebug = DEBUG
	LevelInfo  = INFO
	LevelWarn  = WARN
	LevelError = ERROR
	LevelFatal = FATAL
)

// Handler processes a log event and writes it to an output.
type Handler interface {
	// Write handles the log event, writing it to its destination.
	// The handler is responsible for returning the event's buffer to the pool.
	Write(e *Event) error
}

// ErrorHandler is called when a handler write operation fails
type ErrorHandler func(err error)

// defaultErrorHandler is the default error handler that does nothing for backward compatibility
func defaultErrorHandler(err error) {
	// Silent by default for backward compatibility
}

// Hook defines an interface for log event interception.
// Hooks are called during Msg() before the event is written to the handler.
// Returning false from Run suppresses the log event entirely.
//
// Hook only sees the level and the message string; it cannot read fields
// already encoded into the event. For redaction, cost-accounting, or any
// other field-aware interception, implement [EventHook] instead.
type Hook interface {
	Run(level Level, msg string) bool
}

// EventHook is the field-aware hook interface introduced after Hook v1.
// EventHook implementations receive the [*Event] mid-build and can call
// the event's read-only accessors ([Event.Level], [Event.Buffer],
// [Event.WalkFields]) to inspect what has been encoded so far.
//
// Returning false from Run suppresses the event entirely (no handler
// write, the buffer is recycled). Returning true lets the event proceed
// to handlers.
//
// EventHook implementations MUST NOT mutate the buffer returned by
// [Event.Buffer]; the slice aliases the in-flight log record. Hooks that
// need to add fields can call the regular field methods (Str/Int/...) on
// the event.
//
// EventHooks run after every [Hook] in the legacy chain. If any legacy
// hook suppresses the event, EventHooks are not called.
type EventHook interface {
	Run(e *Event, msg string) bool
}

// SampleHook implements Hook to sample log events at a rate of 1 in every N.
// It uses atomic operations for thread-safe counting.
type SampleHook struct {
	n       uint32
	counter uint32
}

// NewSampleHook creates a SampleHook that passes 1 out of every n events.
// If n is 0 or 1, all events pass through.
func NewSampleHook(n uint32) *SampleHook {
	return &SampleHook{n: n}
}

// Run implements Hook. It returns true for every Nth event.
func (h *SampleHook) Run(_ Level, _ string) bool {
	if h.n <= 1 {
		return true
	}
	c := atomic.AddUint32(&h.counter, 1)
	return c%h.n == 0
}

// Logger is the main logging interface.
type Logger struct {
	handler      Handler
	level        int64  // Use int64 for atomic operations with Level
	context      []byte // Pre-formatted context fields for this logger instance.
	errorHandler ErrorHandler
	hooks        []Hook
	eventHooks   []EventHook
}

// New creates a new logger with the given handler.
func New(handler Handler) *Logger {
	return &Logger{handler: handler, errorHandler: defaultErrorHandler}
}

// SetErrorHandler sets a custom error handler for the logger
func (l *Logger) SetErrorHandler(eh ErrorHandler) *Logger {
	l.errorHandler = eh
	return l
}

// AddHook adds a hook to the logger. Hooks are called in order during Msg().
// AddHook is intended for setup-time configuration and is not safe to call
// concurrently with logging operations.
func (l *Logger) AddHook(hook Hook) *Logger {
	l.hooks = append(l.hooks, hook)
	return l
}

// AddEventHook adds an [EventHook] to the logger. EventHooks run during
// Msg() after every legacy [Hook] succeeds. EventHooks are intended for
// setup-time configuration and are not safe to call concurrently with
// logging operations.
func (l *Logger) AddEventHook(hook EventHook) *Logger {
	l.eventHooks = append(l.eventHooks, hook)
	return l
}

// With creates a new Event with the current logger's context.
func (l *Logger) With() *Event {
	levelValue := atomic.LoadInt64(&l.level)
	// Ensure level is within valid range (defensive programming)
	// Level is int8, so valid range is -128 to 127, but our levels are 0-5
	if levelValue < int64(TRACE) || levelValue > int64(FATAL) {
		levelValue = int64(INFO) // Default to INFO if somehow corrupted
	}
	// Safe conversion after bounds check
	level := Level(levelValue) // #nosec G115 - bounds already checked above
	return &Event{buf: append([]byte{}, l.context...), level: level, l: l}
}

// Logger returns a new Logger with the event's fields as context.

// Ctx automatically includes OpenTelemetry trace/span IDs if present.
func (l *Logger) Ctx(ctx context.Context) *Logger {
	logger := l // Start with the current logger

	span := oteltrace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		// Create a new logger with trace and span IDs as context
		logger = logger.With().Str("trace_id", span.SpanContext().TraceID().String()).Str("span_id", span.SpanContext().SpanID().String()).Logger()
	}
	return logger
}

func (l *Logger) log(level Level) *Event {
	// Use atomic load to safely read the current level
	levelValue := atomic.LoadInt64(&l.level)
	// Ensure level is within valid range (defensive programming)
	// Level is int8, so valid range is -128 to 127, but our levels are 0-5
	if levelValue < int64(TRACE) || levelValue > int64(FATAL) {
		levelValue = int64(INFO) // Default to INFO if somehow corrupted
	}
	// Safe conversion after bounds check
	currentLevel := Level(levelValue) // #nosec G115 - bounds already checked above
	if level < currentLevel {
		return nil
	}

	e := eventPool.Get().(*Event)
	e.level = level
	e.l = l
	e.buf = e.buf[:0] // Reset buffer length but keep capacity

	e.buf = append(e.buf, '{') // Always start with '{'

	// Add level
	e.buf = append(e.buf, `"level":"`...)
	e.buf = append(e.buf, level.String()...)
	e.buf = append(e.buf, '"')

	// Add logger context if present
	if len(l.context) > 0 {
		e.buf = append(e.buf, ',') // Add comma before context
		e.buf = append(e.buf, l.context...)
	}
	return e
}

// Info starts a new message with the INFO level.
func (l *Logger) Info() *Event {
	e := l.log(INFO)
	if e == nil {
		return &Event{} // Return a no-op Event
	}
	return e
}

// Error starts a new message with the ERROR level.
func (l *Logger) Error() *Event {
	e := l.log(ERROR)
	if e == nil {
		return &Event{} // Return a no-op Event
	}
	return e
}

// Debug starts a new message with the DEBUG level.
func (l *Logger) Debug() *Event {
	e := l.log(DEBUG)
	if e == nil {
		return &Event{} // Return a no-op Event
	}
	return e
}

// Warn starts a new message with the WARN level.
func (l *Logger) Warn() *Event {
	e := l.log(WARN)
	if e == nil {
		return &Event{} // Return a no-op Event
	}
	return e
}

// Trace starts a new message with the TRACE level.
func (l *Logger) Trace() *Event {
	e := l.log(TRACE)
	if e == nil {
		return &Event{} // Return a no-op Event
	}
	return e
}

// Fatal starts a new message with the FATAL level.
func (l *Logger) Fatal() *Event {
	e := l.log(FATAL)
	if e == nil {
		return &Event{} // Return a no-op Event
	}
	return e
}

// Str adds a string field to the event with proper JSON escaping and validation.

// ParseLevel converts a string to a Level.
func ParseLevel(levelStr string) Level {
	switch levelStr {
	case traceStr:
		return TRACE
	case debugStr:
		return DEBUG
	case infoStr:
		return INFO
	case warnStr:
		return WARN
	case errorStr:
		return ERROR
	case fatalStr:
		return FATAL
	default:
		return INFO // Default to INFO if the level is not recognized
	}
}

// SetLevel sets the logging level for the logger using atomic operations for thread safety.
// This method is safe to call concurrently from multiple goroutines without additional
// synchronization. The atomic operations prevent race conditions that could lead to
// inconsistent filtering behavior or security bypass scenarios.
//
// Invalid levels are clamped to INFO to ensure defensive behavior.
func (l *Logger) SetLevel(level Level) *Logger {
	// Validate level before storing to prevent corruption
	if level < TRACE || level > FATAL {
		level = INFO // Defensive: clamp to INFO for invalid values
	}
	atomic.StoreInt64(&l.level, int64(level))
	return l
}

// Additional utility methods and performance optimizations

// Hex adds a hexadecimal field to the event.

// levelWriter adapts a Logger to the io.Writer interface.
type levelWriter struct {
	logger *Logger
	level  Level
}

// NewLevelWriter returns an io.Writer that logs each Write call as a message
// at the given level. Trailing newlines are trimmed. This is useful for bridging
// libraries that expect an io.Writer (such as the standard log package) into Bolt.
//
// The string(p) conversion allocates, which is acceptable since this is a
// compatibility bridge rather than a hot-path logging method.
func NewLevelWriter(logger *Logger, level Level) io.Writer {
	return &levelWriter{logger: logger, level: level}
}

func (w *levelWriter) Write(p []byte) (int, error) {
	n := len(p)
	msg := string(p)
	if len(msg) > 0 && msg[len(msg)-1] == '\n' {
		msg = msg[:len(msg)-1]
	}
	e := w.logger.log(w.level)
	if e != nil {
		e.Msg(msg)
	}
	return n, nil
}
