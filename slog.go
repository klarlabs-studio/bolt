package bolt

import (
	"context"
	"io"
	"log/slog"
	"runtime"
	"sync"
)

// SlogHandler implements [slog.Handler] backed by Bolt's zero-allocation engine.
// This allows use of the standard [slog] API with Bolt's high-performance output.
//
// The handler is conformant with the standard library's slogtest.TestHandler
// suite: WithGroup produces nested JSON objects; attrs added via WithAttrs are
// scoped to whichever group was active at the time of the call; empty groups
// are omitted from the output; empty-key attrs are ignored; and slog.LogValuer
// values are resolved before encoding.
//
// Usage:
//
//	handler := bolt.NewSlogHandler(os.Stdout, nil)
//	logger := slog.New(handler)
//	logger.Info("request handled", "method", "GET", "status", 200)
type SlogHandler struct {
	out   io.Writer
	level slog.Level
	mu    *sync.Mutex

	// ctxs is a non-empty stack of group contexts. The first frame is the
	// root (Name == ""); subsequent frames correspond to WithGroup calls.
	// Each frame stores the attrs added via WithAttrs while that frame was
	// the current scope.
	ctxs []groupContext
}

type groupContext struct {
	Name  string // "" for root
	Attrs []slog.Attr
}

// SlogHandlerOptions configures a [SlogHandler].
type SlogHandlerOptions struct {
	// Level sets the minimum log level. Defaults to slog.LevelInfo.
	Level slog.Leveler

	// AddSource adds source file information to log entries.
	AddSource bool
}

// Compile-time guarantee that the slog adapter is a conformant slog.Handler.
// bolt exposes two surfaces: the zero-allocation fluent Event API
// (logger.Info().Str(...).Msg(...)) and this conformant slog.Handler adapter.
var _ slog.Handler = (*SlogHandler)(nil)

// NewSlogJSONHandler returns a conformant [slog.Handler] that writes JSON using
// Bolt's serialization. It is the documented, spec-named entry point for using
// bolt as a drop-in slog handler in production:
//
//	logger := slog.New(bolt.NewSlogJSONHandler(os.Stdout, nil))
//	slog.SetDefault(logger)
//
// It is an alias for [NewSlogHandler]. For the zero-allocation fluent API, use
// [New] with [NewJSONHandler] instead.
func NewSlogJSONHandler(out io.Writer, opts *SlogHandlerOptions) slog.Handler {
	return NewSlogHandler(out, opts)
}

// NewSlogHandler creates a new [slog.Handler] that writes JSON logs using
// Bolt's zero-allocation serialization. If opts is nil, defaults are used.
func NewSlogHandler(out io.Writer, opts *SlogHandlerOptions) *SlogHandler {
	h := &SlogHandler{
		out:  out,
		mu:   &sync.Mutex{},
		ctxs: []groupContext{{}},
	}
	if opts != nil {
		if opts.Level != nil {
			h.level = opts.Level.Level()
		}
	}
	return h
}

// Enabled reports whether the handler handles records at the given level.
func (h *SlogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

// Handle writes the [slog.Record] as JSON to the output writer.
func (h *SlogHandler) Handle(_ context.Context, r slog.Record) error {
	buf := bufPool.Get().(*[]byte)
	b := (*buf)[:0]

	b = append(b, `{"level":"`...)
	b = appendSlogLevel(b, r.Level)
	b = append(b, '"')

	if !r.Time.IsZero() {
		b = append(b, `,"time":"`...)
		b = appendRFC3339(b, r.Time)
		b = append(b, '"')
	}

	if r.PC != 0 {
		frame, _ := runtime.CallersFrames([]uintptr{r.PC}).Next()
		if frame.File != "" {
			b = append(b, `,"source":{"function":"`...)
			b = appendJSONString(b, frame.Function)
			b = append(b, `","file":"`...)
			b = appendJSONString(b, frame.File)
			b = append(b, `","line":`...)
			b = appendInt(b, frame.Line)
			b = append(b, '}')
		}
	}

	emitRecordAttrs := func(dst []byte) []byte {
		r.Attrs(func(a slog.Attr) bool {
			dst = appendSlogAttr(dst, a)
			return true
		})
		return dst
	}
	b = encodeContexts(b, h.ctxs, emitRecordAttrs)

	if r.Message != "" {
		b = append(b, `,"message":"`...)
		b = appendJSONString(b, r.Message)
		b = append(b, '"')
	}

	b = append(b, '}', '\n')

	h.mu.Lock()
	_, err := h.out.Write(b)
	h.mu.Unlock()

	*buf = b
	bufPool.Put(buf)

	return err
}

// encodeContexts walks the group-context stack, opening one nested JSON
// object per non-root frame. Each frame contributes its WithAttrs-stored
// attrs at its scope; the deepest frame additionally receives the per-record
// attrs via emitRecord. Empty frames (no attrs after recursion) are omitted.
func encodeContexts(b []byte, ctxs []groupContext, emitRecord func([]byte) []byte) []byte {
	if len(ctxs) == 0 {
		return emitRecord(b)
	}
	cur := ctxs[0]
	rest := ctxs[1:]

	emit := func(dst []byte) []byte {
		for _, a := range cur.Attrs {
			dst = appendSlogAttr(dst, a)
		}
		if len(rest) == 0 {
			return emitRecord(dst)
		}
		return encodeContexts(dst, rest, emitRecord)
	}

	if cur.Name == "" {
		return emit(b)
	}

	startLen := len(b)
	b = append(b, ',', '"')
	b = appendJSONString(b, cur.Name)
	b = append(b, `":{`...)
	innerStart := len(b)
	b = emit(b)
	if len(b) == innerStart {
		return b[:startLen]
	}
	if b[innerStart] == ',' {
		copy(b[innerStart:], b[innerStart+1:])
		b = b[:len(b)-1]
	}
	b = append(b, '}')
	return b
}

// WithAttrs returns a new handler with the given attributes pre-set, scoped
// to whichever group is currently active.
func (h *SlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	h2 := h.clone()
	last := len(h2.ctxs) - 1
	merged := make([]slog.Attr, 0, len(h2.ctxs[last].Attrs)+len(attrs))
	merged = append(merged, h2.ctxs[last].Attrs...)
	merged = append(merged, attrs...)
	h2.ctxs[last].Attrs = merged
	return h2
}

// WithGroup returns a new handler that nests subsequent attributes under the
// given key.
func (h *SlogHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	h2 := h.clone()
	h2.ctxs = append(h2.ctxs, groupContext{Name: name})
	return h2
}

func (h *SlogHandler) clone() *SlogHandler {
	ctxs := make([]groupContext, len(h.ctxs))
	for i, c := range h.ctxs {
		ctxs[i] = groupContext{
			Name:  c.Name,
			Attrs: append([]slog.Attr(nil), c.Attrs...),
		}
	}
	return &SlogHandler{
		out:   h.out,
		level: h.level,
		mu:    h.mu,
		ctxs:  ctxs,
	}
}

// bufPool is a pool for slog handler buffers.
var bufPool = &sync.Pool{
	New: func() interface{} {
		b := make([]byte, 0, 1024)
		return &b
	},
}

func appendSlogLevel(b []byte, l slog.Level) []byte {
	switch {
	case l < slog.LevelInfo:
		return append(b, debugStr...)
	case l < slog.LevelWarn:
		return append(b, infoStr...)
	case l < slog.LevelError:
		return append(b, warnStr...)
	default:
		return append(b, errorStr...)
	}
}

// appendSlogAttr encodes one slog.Attr as a JSON object member, preceded by a
// comma. Groups become nested objects (or expand inline when keyless). Attrs
// with an empty key are ignored (slog contract).
func appendSlogAttr(b []byte, a slog.Attr) []byte {
	a.Value = a.Value.Resolve()

	if a.Equal(slog.Attr{}) {
		return b
	}

	if a.Value.Kind() == slog.KindGroup {
		attrs := a.Value.Group()
		if len(attrs) == 0 {
			return b
		}
		// Inline group (empty key) expands at the parent level.
		if a.Key == "" {
			for _, ga := range attrs {
				b = appendSlogAttr(b, ga)
			}
			return b
		}
		startLen := len(b)
		b = append(b, ',', '"')
		b = appendJSONString(b, a.Key)
		b = append(b, `":{`...)
		innerStart := len(b)
		for _, ga := range attrs {
			b = appendSlogAttr(b, ga)
		}
		if len(b) == innerStart {
			return b[:startLen]
		}
		if b[innerStart] == ',' {
			copy(b[innerStart:], b[innerStart+1:])
			b = b[:len(b)-1]
		}
		b = append(b, '}')
		return b
	}

	if a.Key == "" {
		return b
	}

	b = append(b, ',', '"')
	b = appendJSONString(b, a.Key)
	b = append(b, `":`...)
	return appendSlogValue(b, a.Value)
}

// appendSlogValue appends a slog.Value as JSON.
func appendSlogValue(b []byte, v slog.Value) []byte {
	switch v.Kind() {
	case slog.KindString:
		b = append(b, '"')
		b = appendJSONString(b, v.String())
		b = append(b, '"')
	case slog.KindInt64:
		b = appendInt(b, int(v.Int64()))
	case slog.KindUint64:
		b = appendUint(b, v.Uint64())
	case slog.KindFloat64:
		b = appendFloat64(b, v.Float64())
	case slog.KindBool:
		b = appendBool(b, v.Bool())
	case slog.KindDuration:
		b = append(b, '"')
		b = appendJSONString(b, v.Duration().String())
		b = append(b, '"')
	case slog.KindTime:
		b = append(b, '"')
		b = appendRFC3339(b, v.Time())
		b = append(b, '"')
	default:
		b = append(b, '"')
		b = appendJSONString(b, v.String())
		b = append(b, '"')
	}
	return b
}
