package signing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// canonicalize writes v as JCS (RFC 8785). v is always map[string]any,
// []any, string, json.Number, bool or nil — what encoding/json produces from
// protojson's output with UseNumber; anything else is a bug here.
func canonicalize(b *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(t))
	case string:
		return writeString(b, t)
	case json.Number:
		return writeNumber(b, t)
	case []any:
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := canonicalize(b, e); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sortUTF16(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeString(b, k); err != nil {
				return err
			}
			b.WriteByte(':')
			if err := canonicalize(b, t[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return fmt.Errorf("signing: %T cannot appear in a canonical document", v)
	}
	return nil
}

// sortUTF16 orders keys by UTF-16 code unit, per RFC 8785 §3.2.3 — not the
// same as ordering by byte (a character above the BMP sorts differently in
// each). Every key seen today is ASCII (TestJSONNamesAreLowerCamelCase), so
// this only matters for a future document.
func sortUTF16(keys []string) {
	sort.Slice(keys, func(i, j int) bool {
		a, b := utf16.Encode([]rune(keys[i])), utf16.Encode([]rune(keys[j]))
		for n := 0; n < len(a) && n < len(b); n++ {
			if a[n] != b[n] {
				return a[n] < b[n]
			}
		}
		return len(a) < len(b)
	})
}

// writeNumber refuses any number Go and ECMAScript would not print
// identically. The schema already excludes wide integers, floats and doubles
// (TestNoWideNumbersCrossJCS); this is the runtime backstop.
func writeNumber(b *bytes.Buffer, n json.Number) error {
	s := n.String()
	if strings.ContainsAny(s, ".eE") {
		return fmt.Errorf("signing: %s is not an integer; the contract carries no float, "+
			"double or 64-bit integer field (TestNoWideNumbersCrossJCS), because "+
			"ECMAScript number formatting is the one part of RFC 8785 that does not "+
			"reimplement cleanly", s)
	}
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("signing: %s is not an integer: %w", s, err)
	}
	// 2^53: above it an ECMAScript number is no longer exactly an integer.
	const maxExact = 1 << 53
	if i >= maxExact || i <= -maxExact {
		return fmt.Errorf("signing: %s is outside the range an ECMAScript number represents exactly", s)
	}
	b.WriteString(strconv.FormatInt(i, 10))
	return nil
}

// writeString escapes per RFC 8785 §3.2.2.2: the mandatory escapes, the short
// forms for control characters that have one, \u00xx for the rest, everything
// else literal UTF-8.
//
// encoding/json can't be reused: it escapes <, >, & by default, and even with
// SetEscapeHTML(false) still escapes U+2028/U+2029.
func writeString(b *bytes.Buffer, s string) error {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return nil
}
