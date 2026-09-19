package bolt

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
	"runtime"
	"testing"
	"time"
)

// SlogHandlerOptions.AddSource was declared but never read: the handler wrote
// "source" on every record that had a PC, which through slog.Logger is every
// record. It must follow log/slog — source only when asked for.
func TestSlogHandlerOmitsSourceByDefault(t *testing.T) {
	for name, opts := range map[string]*SlogHandlerOptions{
		"nil options":     nil,
		"AddSource false": {AddSource: false},
	} {
		var buf bytes.Buffer
		slog.New(NewSlogHandler(&buf, opts)).Info("hello")

		if _, ok := decodeLine(t, buf.Bytes())[slog.SourceKey]; ok {
			t.Errorf("%s: source written without AddSource: %s", name, buf.String())
		}
	}
}

// With AddSource, the source is the log call site, encoded as the group
// slog.JSONHandler writes: {"function":…,"file":…,"line":…}.
func TestSlogHandlerAddSourceReportsCallSite(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewSlogHandler(&buf, &SlogHandlerOptions{AddSource: true}))

	_, file, line, _ := runtime.Caller(0)
	logger.Info("hello") // must stay on the line after runtime.Caller

	src, ok := decodeLine(t, buf.Bytes())[slog.SourceKey].(map[string]any)
	if !ok {
		t.Fatalf("expected a source object, got: %s", buf.String())
	}
	want := map[string]any{
		"function": "go.klarlabs.de/bolt.TestSlogHandlerAddSourceReportsCallSite",
		"file":     file,
		"line":     float64(line + 1),
	}
	if !reflect.DeepEqual(src, want) {
		t.Errorf("source = %v, want %v", src, want)
	}
}

// The source group must be exactly what slog.JSONHandler writes for the same
// record, and — like the standard handler — stay at the top level rather than
// inside any open group.
func TestSlogHandlerAddSourceMatchesStdlib(t *testing.T) {
	var pcs [1]uintptr
	runtime.Callers(1, pcs[:])
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "hello", pcs[0])

	var bolt, std bytes.Buffer
	bh := NewSlogHandler(&bolt, &SlogHandlerOptions{AddSource: true}).WithGroup("g")
	sh := slog.NewJSONHandler(&std, &slog.HandlerOptions{AddSource: true}).WithGroup("g")
	if err := bh.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if err := sh.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}

	got := decodeLine(t, bolt.Bytes())[slog.SourceKey]
	want := decodeLine(t, std.Bytes())[slog.SourceKey]
	if got == nil || !reflect.DeepEqual(got, want) {
		t.Errorf("source differs from slog.JSONHandler\n  bolt:   %s  stdlib: %s",
			bolt.String(), std.String())
	}
}

// A record without a PC has no source to report; slog omits the key.
func TestSlogHandlerAddSourceSkipsZeroPC(t *testing.T) {
	var buf bytes.Buffer
	h := NewSlogHandler(&buf, &SlogHandlerOptions{AddSource: true})
	if err := h.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "hello", 0)); err != nil {
		t.Fatal(err)
	}

	if _, ok := decodeLine(t, buf.Bytes())[slog.SourceKey]; ok {
		t.Errorf("source written for a record with no PC: %s", buf.String())
	}
}

func decodeLine(t *testing.T, line []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		t.Fatalf("invalid JSON %q: %v", line, err)
	}
	return m
}
