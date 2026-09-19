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

// canonicalize writes v as JCS (RFC 8785).
//
// v is what encoding/json produced from protojson's output with UseNumber, so
// it is only ever map[string]any, []any, string, json.Number, bool or nil.
// Anything else is a bug here rather than a malformed document, and says so.
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

// sortUTF16 orders keys by UTF-16 code unit, which is what RFC 8785 §3.2.3
// specifies and is not the same as ordering by byte: a character above the
// BMP sorts below U+E000 as UTF-16 and above it as UTF-8.
//
// Every key this package sees today is a protojson field name, which
// TestJSONNamesAreLowerCamelCase holds to ASCII, so the two orders agree and
// this is here for the document the contract does not have yet rather than
// for the one it does.
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

// writeNumber refuses anything that is not an integer JavaScript and Go
// would print identically.
//
// RFC 8785 serialises numbers as ECMAScript would, and reproducing
// ECMAScript's shortest-round-trip printing of an arbitrary double in Go is
// the trap that makes JCS hard to implement twice. The contract avoids it
// rather than solving it: TestNoWideNumbersCrossJCS refuses every 64-bit
// integer and every float and double in the schema, which leaves int32 and
// uint32, whose protojson output is an integer both languages print the same
// way. This is the runtime half of that test — a number reaching here that
// the schema should not have allowed fails loudly instead of producing a
// digest the other language would not agree with.
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
	// 2^53: above it an ECMAScript number is no longer exactly an integer, so
	// the two languages stop agreeing about what the digits are.
	const maxExact = 1 << 53
	if i >= maxExact || i <= -maxExact {
		return fmt.Errorf("signing: %s is outside the range an ECMAScript number represents exactly", s)
	}
	b.WriteString(strconv.FormatInt(i, 10))
	return nil
}

// writeString escapes per RFC 8785 §3.2.2.2: the two mandatory escapes, the
// six short forms for the control characters that have one, \u00xx with
// lowercase hex for every other control character, and every other code point
// literal as UTF-8.
//
// encoding/json cannot be used for this. Its encoder escapes <, > and & to
// < and friends by default — defensible in a browser, fatal here, since
// the other implementation does not — and SetEscapeHTML(false) still leaves
// U+2028 and U+2029 escaped.
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
