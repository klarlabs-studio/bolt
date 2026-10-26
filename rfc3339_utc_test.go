package bolt

import (
	"testing"
	"time"
)

// The encoder ends every timestamp in a literal 'Z'. A time in another zone must
// therefore be converted, not relabelled: 11:12 in Berlin is 10:12Z.
func TestAppendRFC3339IsUTC(t *testing.T) {
	berlin := time.FixedZone("CET", 3600)
	in := time.Date(2026, 10, 26, 11, 12, 41, 123456789, berlin)
	got := string(appendRFC3339(nil, in))
	want := "2026-10-26T10:12:41.123456789Z"
	if got != want {
		t.Errorf("appendRFC3339(%s) = %q, want %q", in, got, want)
	}
}
