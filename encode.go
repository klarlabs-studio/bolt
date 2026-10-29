package bolt

import (
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Fast number conversion helpers to avoid allocations
var digits = "0123456789"
var hexDigits = "0123456789ABCDEF"

// appendInt appends an integer to the buffer without allocations
func appendInt(buf []byte, i int) []byte {
	if i == 0 {
		return append(buf, '0')
	}

	// Handle math.MinInt64 special case to prevent overflow
	// MinInt64 = -9223372036854775808, which cannot be negated without overflow
	if i == -9223372036854775808 {
		return append(buf, "-9223372036854775808"...)
	}

	// Handle negative numbers
	if i < 0 {
		buf = append(buf, '-')
		i = -i
	}

	// Fast path for small numbers (0-99) - most common case
	if i < 100 {
		if i < 10 {
			return append(buf, digits[i])
		}
		return append(buf, digits[i/10], digits[i%10])
	}

	// For larger numbers, build from the end
	start := len(buf)
	for i > 0 {
		buf = append(buf, digits[i%10])
		i /= 10
	}

	// Reverse the digits we just added
	end := len(buf) - 1
	for start < end {
		buf[start], buf[end] = buf[end], buf[start]
		start++
		end--
	}

	return buf
}

// appendUint appends an unsigned integer to the buffer without allocations
func appendUint(buf []byte, i uint64) []byte {
	if i == 0 {
		return append(buf, '0')
	}

	// Fast path for small numbers (0-99) - most common case
	if i < 100 {
		if i < 10 {
			return append(buf, digits[i])
		}
		return append(buf, digits[i/10], digits[i%10])
	}

	// For larger numbers, build from the end
	start := len(buf)
	for i > 0 {
		buf = append(buf, digits[i%10])
		i /= 10
	}

	// Reverse the digits we just added
	end := len(buf) - 1
	for start < end {
		buf[start], buf[end] = buf[end], buf[start]
		start++
		end--
	}

	return buf
}

// appendBool appends a boolean to the buffer without allocations
func appendBool(buf []byte, b bool) []byte {
	if b {
		return append(buf, "true"...)
	}
	return append(buf, "false"...)
}

// appendJSONString appends a JSON-escaped string to the buffer without allocations.
// This is a critical security function that prevents JSON injection attacks by properly
// escaping all special characters according to RFC 7159. It handles:
//   - Quote characters that could break JSON structure
//   - Control characters that could corrupt log format
//   - Backslashes that could create escape sequences
//   - Unicode control characters (U+0000 to U+001F)
func appendJSONString(buf []byte, s string) []byte {
	// Fast path: iterate once, handling both UTF-8 validation and JSON escaping
	for i := 0; i < len(s); {
		c := s[i]

		// Fast path for ASCII characters (most common case)
		if c < utf8.RuneSelf {
			switch c {
			case '"':
				buf = append(buf, '\\', '"')
			case '\\':
				buf = append(buf, '\\', '\\')
			case '\b':
				buf = append(buf, '\\', 'b')
			case '\f':
				buf = append(buf, '\\', 'f')
			case '\n':
				buf = append(buf, '\\', 'n')
			case '\r':
				buf = append(buf, '\\', 'r')
			case '\t':
				buf = append(buf, '\\', 't')
			default:
				if c < 0x20 {
					// Escape control characters as \u00XX
					buf = append(buf, '\\', 'u', '0', '0')
					buf = append(buf, hexDigits[c>>4], hexDigits[c&0xF])
				} else {
					buf = append(buf, c)
				}
			}
			i++
			continue
		}

		// Multi-byte UTF-8 character - validate and copy
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			// Invalid UTF-8 - replace with replacement character (U+FFFD = �)
			buf = append(buf, 0xEF, 0xBF, 0xBD) // UTF-8 encoding of U+FFFD
		} else {
			// Valid UTF-8 - copy bytes directly
			buf = append(buf, s[i:i+size]...)
		}
		i += size
	}
	return buf
}

// appendIP appends an IP address to the buffer without allocations.
// IPv4 addresses use dotted-decimal notation, IPv6 uses colon-hex notation.
func appendIP(buf []byte, ip net.IP) []byte {
	if p4 := ip.To4(); p4 != nil {
		buf = appendUint(buf, uint64(p4[0]))
		buf = append(buf, '.')
		buf = appendUint(buf, uint64(p4[1]))
		buf = append(buf, '.')
		buf = appendUint(buf, uint64(p4[2]))
		buf = append(buf, '.')
		buf = appendUint(buf, uint64(p4[3]))
		return buf
	}
	if len(ip) == net.IPv6len {
		for i := 0; i < net.IPv6len; i += 2 {
			if i > 0 {
				buf = append(buf, ':')
			}
			buf = appendHex16(buf, uint16(ip[i])<<8|uint16(ip[i+1]))
		}
		return buf
	}
	return buf
}

// appendHex16 appends a 16-bit value as 4 lowercase hex digits.
func appendHex16(buf []byte, v uint16) []byte {
	const lowerHex = "0123456789abcdef"
	buf = append(buf, lowerHex[(v>>12)&0xf])
	buf = append(buf, lowerHex[(v>>8)&0xf])
	buf = append(buf, lowerHex[(v>>4)&0xf])
	buf = append(buf, lowerHex[v&0xf])
	return buf
}

// validateKey validates a key parameter for safety and length to prevent security vulnerabilities.
// This function protects against:
//   - Control character injection (prevents log format corruption)
//   - Resource exhaustion attacks (enforces 256 character limit)
//   - Empty key exploitation (requires non-empty keys)
func validateKey(key string) error {
	// Trim whitespace and check for empty
	trimmed := strings.TrimSpace(key)
	if len(trimmed) == 0 {
		return fmt.Errorf("key cannot be empty or whitespace-only")
	}
	if len(key) > MaxKeyLength {
		return fmt.Errorf("key length exceeds maximum of %d characters", MaxKeyLength)
	}
	// Check for control characters in key
	for i := 0; i < len(key); i++ {
		if key[i] < 0x20 || key[i] == 0x7F {
			return fmt.Errorf("key contains invalid control character")
		}
	}
	return nil
}

// validateValue validates a value parameter for safety and length
func validateValue(value string) error {
	if len(value) > MaxValueLength {
		return fmt.Errorf("value length exceeds maximum of %d characters", MaxValueLength)
	}
	return nil
}

// checkBufferSize checks if buffer size is within limits and prevents unbounded growth
func checkBufferSize(buf []byte) error {
	if len(buf) > MaxBufferSize {
		return fmt.Errorf("buffer size exceeds maximum of %d bytes", MaxBufferSize)
	}
	return nil
}

// RFC3339 timestamp formatting without allocations
func appendRFC3339(buf []byte, t time.Time) []byte {
	// The suffix below is a literal 'Z', so the clock fields must be UTC: a
	// local time would be relabelled, not converted.
	t = t.UTC()
	year, month, day := t.Date()
	hour, minute, sec := t.Clock()
	nano := t.Nanosecond()

	buf = appendDate(buf, year, int(month), day)
	buf = append(buf, 'T')
	buf = appendTime(buf, hour, minute, sec)
	buf = appendNanoseconds(buf, nano)
	buf = append(buf, 'Z')
	return buf
}

// appendDate appends date in YYYY-MM-DD format
func appendDate(buf []byte, year, month, day int) []byte {
	buf = appendInt(buf, year)
	buf = append(buf, '-')
	buf = appendTwoDigits(buf, month)
	buf = append(buf, '-')
	buf = appendTwoDigits(buf, day)
	return buf
}

// appendTime appends time in HH:MM:SS format
func appendTime(buf []byte, hour, minute, sec int) []byte {
	buf = appendTwoDigits(buf, hour)
	buf = append(buf, ':')
	buf = appendTwoDigits(buf, minute)
	buf = append(buf, ':')
	buf = appendTwoDigits(buf, sec)
	return buf
}

// appendTwoDigits appends a number with leading zero if needed
func appendTwoDigits(buf []byte, value int) []byte {
	if value < 10 {
		buf = append(buf, '0')
	}
	return appendInt(buf, value)
}

// appendNanoseconds appends nanoseconds if non-zero using zero-allocation formatting
func appendNanoseconds(buf []byte, nano int) []byte {
	if nano == 0 {
		return buf
	}
	buf = append(buf, '.')
	// Format nanoseconds to 9 digits without allocations
	buf = appendNanoDigits(buf, nano)
	// Trim trailing zeros
	for len(buf) > 0 && buf[len(buf)-1] == '0' {
		buf = buf[:len(buf)-1]
	}
	return buf
}

// appendNanoDigits appends nanoseconds as 9 digits without allocations
func appendNanoDigits(buf []byte, nano int) []byte {
	// Convert nanoseconds to 9-digit string without allocations
	digitBuf := [9]byte{}
	for i := 8; i >= 0; i-- {
		digitBuf[i] = byte(nano%10) + '0'
		nano /= 10
	}
	return append(buf, digitBuf[:]...)
}

// appendFloat64 appends a float64 to the buffer without allocations.
//
// Encoding rules:
//   - NaN, +Inf, -Inf are emitted as JSON strings ("NaN"/"+Inf"/"-Inf")
//     because RFC 8259 has no representation for them.
//   - All other values are encoded via strconv.AppendFloat with the
//     shortest-round-trip ('g', -1) verb, matching encoding/json. This
//     preserves the full IEEE-754 precision of the input — there is no
//     6-decimal truncation. Callers that previously logged values like
//     transaction amounts or scientific measurements will now see the
//     correct digits in their JSON output.
//
// strconv.AppendFloat writes into a small stack buffer before copying
// into buf, so this remains 0 allocs/op on the hot path.
func appendFloat64(buf []byte, f float64) []byte {
	switch {
	case math.IsNaN(f):
		return append(buf, `"NaN"`...)
	case math.IsInf(f, 1):
		return append(buf, `"+Inf"`...)
	case math.IsInf(f, -1):
		return append(buf, `"-Inf"`...)
	}
	return strconv.AppendFloat(buf, f, 'g', -1, 64)
}
