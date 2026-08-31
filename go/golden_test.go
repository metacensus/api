package contract_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	contract "github.com/metacensus/ui/contract/go"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

var update = flag.Bool("update", false, "rewrite the golden files and the TypeScript cross-check")

const (
	goldenDir  = "testdata/golden"
	tsGoldenFn = "../ts/test/golden.ts"
	protoPkg   = "metacensus.v1"
)

// marshalGolden renders one fixture the way the contract says to, then
// re-indents it.
//
// The re-indent is not cosmetic. protojson deliberately randomises the
// whitespace between JSON members — the same message marshalled twice can
// differ byte for byte — precisely so that nobody builds a byte comparison on
// top of it. Running the output back through encoding/json normalises the
// framing while preserving field order, which protojson emits in field-number
// order. That is what makes a committed golden file possible at all.
func marshalGolden(t *testing.T, f fixture) []byte {
	t.Helper()

	raw, err := contract.Marshal(f.msg)
	if err != nil {
		t.Fatalf("%s: marshal: %v", f.name, err)
	}

	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		t.Fatalf("%s: indent: %v", f.name, err)
	}
	buf.WriteByte('\n')
	return buf.Bytes()
}

// TestGolden is the check that makes the contract's conventions real.
//
// Everything the .proto files and buf config assert about the wire — that
// fields are lowerCamelCase, that enums are PascalCase strings, that timestamps
// are RFC 3339, that ids are strings, that a list is `{items, metadata}` — is
// only true if the encoder actually does it. These files are what the encoder
// actually does.
func TestGolden(t *testing.T) {
	if *update {
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatal(err)
		}
		// Remove stale goldens so a renamed or deleted message does not leave
		// an orphan behind that the freshness check would never notice.
		existing, err := filepath.Glob(filepath.Join(goldenDir, "*.json"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range existing {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}
	}

	for _, f := range fixtures() {
		t.Run(f.name, func(t *testing.T) {
			got := marshalGolden(t, f)
			path := filepath.Join(goldenDir, f.name+".json")

			if *update {
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("missing golden (run `go test ./... -update`): %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("wire shape changed.\n--- want (%s)\n%s\n--- got\n%s", path, want, got)
			}
		})
	}

	if *update {
		writeTypeScriptGolden(t)
	}
}

// TestRoundTrip proves the golden files are not just what the encoder emits but
// what the decoder accepts: every golden parses back into its message and
// re-marshals identically.
//
// This is where a golden that was hand-edited into something protojson cannot
// read gets caught — an enum spelled the JSON way rather than the proto way, a
// timestamp that is not RFC 3339, a 64-bit integer left unquoted.
func TestRoundTrip(t *testing.T) {
	for _, f := range fixtures() {
		t.Run(f.name, func(t *testing.T) {
			path := filepath.Join(goldenDir, f.name+".json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Skipf("no golden yet: %v", err)
			}

			clone := f.msg.ProtoReflect().New().Interface()
			if err := contract.Unmarshal(data, clone); err != nil {
				t.Fatalf("unmarshal golden: %v", err)
			}

			reMarshalled := marshalGolden(t, fixture{name: f.name, msg: clone})
			if !bytes.Equal(reMarshalled, data) {
				t.Errorf("round trip changed the document.\n--- golden\n%s\n--- round-tripped\n%s", data, reMarshalled)
			}
		})
	}
}

// TestEveryMessageHasAFixture keeps the golden set honest: a message added to
// the contract without a fixture has no proof that its wire shape is what
// anyone intended.
func TestEveryMessageHasAFixture(t *testing.T) {
	covered := map[string]bool{}
	for _, f := range fixtures() {
		covered[string(f.msg.ProtoReflect().Descriptor().FullName())] = true
	}

	var missing []string
	forEachContractMessage(t, func(md protoreflect.MessageDescriptor) {
		if !covered[string(md.FullName())] {
			missing = append(missing, string(md.FullName()))
		}
	})

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("messages with no fixture in fixtures_test.go:\n  %s", strings.Join(missing, "\n  "))
	}
}

// forEachContractMessage visits every message declared in package
// metacensus.v1, nested messages included.
func forEachContractMessage(t *testing.T, visit func(protoreflect.MessageDescriptor)) {
	t.Helper()

	var walk func(protoreflect.MessageDescriptors)
	walk = func(mds protoreflect.MessageDescriptors) {
		for i := 0; i < mds.Len(); i++ {
			md := mds.Get(i)
			// Skip synthetic map entry messages: they are an implementation
			// detail of `map<k, v>` and never appear on the wire as objects of
			// their own.
			if md.IsMapEntry() {
				continue
			}
			visit(md)
			walk(md.Messages())
		}
	}

	forEachContractFile(t, func(fd protoreflect.FileDescriptor) {
		walk(fd.Messages())
	})
}

// forEachContractFile visits every .proto file in package metacensus.v1.
//
// The generated packages register themselves on import, so the registry is the
// complete contract as long as this test binary imports the generated package —
// which it does, via the fixtures.
func forEachContractFile(t *testing.T, visit func(protoreflect.FileDescriptor)) {
	t.Helper()

	seen := 0
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if string(fd.Package()) == protoPkg {
			seen++
			visit(fd)
		}
		return true
	})
	if seen == 0 {
		t.Fatalf("no files registered for package %s", protoPkg)
	}
}

// --- TypeScript cross-check generation ------------------------------------
//
// The Go tests prove the JSON is what the contract says. They cannot prove the
// generated TypeScript agrees with it — that the field the encoder emits is the
// field the interface declares, that a field the encoder omits is one the
// interface marks optional, that the enum string is a member of the generated
// enum. Only tsc can prove that, and only against a typed literal.
//
// So the same test run that writes the golden JSON also writes that literal,
// transcribed from the identical bytes. Hand-writing it would let the two drift;
// generating it means they cannot. CI regenerates both and fails on any diff.

func writeTypeScriptGolden(t *testing.T) {
	t.Helper()

	typeImports := map[string]map[string]bool{} // module -> named type imports
	valueImports := map[string]map[string]bool{}
	enumAsserts := map[string]string{} // "Prop_Type.Statement" -> "Statement"

	var body strings.Builder
	for _, f := range fixtures() {
		md := f.msg.ProtoReflect().Descriptor()
		raw, err := contract.Marshal(f.msg)
		if err != nil {
			t.Fatal(err)
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, raw); err != nil {
			t.Fatal(err)
		}

		addImport(typeImports, tsModule(md), tsName(md))
		lit := tsMessageLiteral(t, md, compact.Bytes(), 1, valueImports, enumAsserts)
		// `golden` prefix so the constant never collides with the type it is
		// annotated with, or with a global (`Error` is both a message here and
		// a TypeScript built-in).
		fmt.Fprintf(&body, "export const golden%s: %s = %s;\n\n", f.name, tsName(md), lit)
	}

	var out strings.Builder
	out.WriteString(`// Code generated by contract/go's golden test. DO NOT EDIT.
//
// Regenerate with:  cd contract && make golden
//
// Every literal below is a transcription of the matching file in
// contract/go/testdata/golden, produced from the same protojson output in the
// same run. Type-checking this file is the cross-language half of the contract's
// verification: it proves the JSON the Go side emits is assignable to the
// TypeScript interfaces the same .proto files generate — same field names, same
// optionality, same enum vocabulary.
//
// If tsc fails here, the Go and TypeScript views of the wire have diverged.
// That is the check working.

/* eslint-disable */
`)

	for _, mod := range sortedKeys(typeImports) {
		fmt.Fprintf(&out, "import type { %s } from %q;\n", strings.Join(sortedSet(typeImports[mod]), ", "), mod)
	}
	for _, mod := range sortedKeys(valueImports) {
		fmt.Fprintf(&out, "import { %s } from %q;\n", strings.Join(sortedSet(valueImports[mod]), ", "), mod)
	}

	out.WriteString("\n")
	out.WriteString(body.String())

	// Prove each enum member's *value* is the exact string the golden JSON
	// carries. The literal above proves the member is accepted where the
	// interface wants the enum; this proves the member serialises back to the
	// same characters protojson wrote.
	// `${Enum.Member}` is a template literal type, which resolves a string enum
	// member down to its literal value. Annotating the property with it and
	// assigning the characters protojson actually wrote makes tsc compare the
	// two: rename a proto enum value on one side only and this stops compiling.
	// A plain `Record<string, string>` would prove nothing — every enum member
	// is a string.
	out.WriteString(`// Every enum member used in the literals above, pinned to the exact characters
// protojson emitted for it. This is what proves the two languages share one
// enum vocabulary and not merely one set of field names.
export const enumWireValues: {
`)
	keys := sortedKeys2(enumAsserts)
	for _, k := range keys {
		fmt.Fprintf(&out, "  %q: `${%s}`;\n", k, k)
	}
	out.WriteString("} = {\n")
	for _, k := range keys {
		fmt.Fprintf(&out, "  %q: %q,\n", k, enumAsserts[k])
	}
	out.WriteString("};\n")

	if err := os.MkdirAll(filepath.Dir(tsGoldenFn), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tsGoldenFn, []byte(out.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// tsMessageLiteral transcribes one JSON object into a TypeScript object
// literal, walking the message descriptor alongside it so enum-valued fields
// come out as enum members rather than bare strings.
func tsMessageLiteral(
	t *testing.T,
	md protoreflect.MessageDescriptor,
	raw []byte,
	depth int,
	valueImports map[string]map[string]bool,
	enumAsserts map[string]string,
) string {
	t.Helper()

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	if _, err := dec.Token(); err != nil { // consume '{'
		t.Fatalf("%s: %v", md.FullName(), err)
	}

	pad := strings.Repeat("  ", depth)
	closePad := strings.Repeat("  ", depth-1)

	var fields []string
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			t.Fatalf("%s: %v", md.FullName(), err)
		}
		key := keyTok.(string)

		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			t.Fatalf("%s.%s: %v", md.FullName(), key, err)
		}

		fd := md.Fields().ByJSONName(key)
		if fd == nil {
			t.Fatalf("%s: no field with JSON name %q", md.FullName(), key)
		}
		fields = append(fields, fmt.Sprintf("%s%s: %s,", pad, key,
			tsValue(t, fd, value, depth+1, valueImports, enumAsserts)))
	}

	if len(fields) == 0 {
		return "{}"
	}
	return "{\n" + strings.Join(fields, "\n") + "\n" + closePad + "}"
}

func tsValue(
	t *testing.T,
	fd protoreflect.FieldDescriptor,
	raw json.RawMessage,
	depth int,
	valueImports map[string]map[string]bool,
	enumAsserts map[string]string,
) string {
	t.Helper()

	if fd.IsList() {
		var elems []json.RawMessage
		if err := json.Unmarshal(raw, &elems); err != nil {
			t.Fatalf("%s: %v", fd.FullName(), err)
		}
		if len(elems) == 0 {
			return "[]"
		}
		pad := strings.Repeat("  ", depth)
		closePad := strings.Repeat("  ", depth-1)
		parts := make([]string, 0, len(elems))
		for _, e := range elems {
			parts = append(parts, pad+tsScalarOrMessage(t, fd, e, depth+1, valueImports, enumAsserts)+",")
		}
		return "[\n" + strings.Join(parts, "\n") + "\n" + closePad + "]"
	}
	return tsScalarOrMessage(t, fd, raw, depth, valueImports, enumAsserts)
}

func tsScalarOrMessage(
	t *testing.T,
	fd protoreflect.FieldDescriptor,
	raw json.RawMessage,
	depth int,
	valueImports map[string]map[string]bool,
	enumAsserts map[string]string,
) string {
	t.Helper()

	switch fd.Kind() {
	case protoreflect.EnumKind:
		var name string
		if err := json.Unmarshal(raw, &name); err != nil {
			t.Fatalf("%s: enum did not serialise as a string: %v", fd.FullName(), err)
		}
		ed := fd.Enum()
		ref := tsName(ed) + "." + name
		addImport(valueImports, tsModule(ed), tsName(ed))
		enumAsserts[ref] = name
		return ref

	case protoreflect.MessageKind, protoreflect.GroupKind:
		md := fd.Message()
		// Well-known types serialise as scalars, not objects: a Timestamp is an
		// RFC 3339 string, an Int32Value is a number. ts-proto unwraps them the
		// same way, so the raw JSON is already the right literal.
		if strings.HasPrefix(string(md.FullName()), "google.protobuf.") {
			return string(raw)
		}
		return tsMessageLiteral(t, md, raw, depth, valueImports, enumAsserts)

	default:
		// Strings, numbers and booleans are valid TypeScript literals as JSON
		// wrote them, 64-bit integers included: protojson quotes those and
		// ts-proto's forceLong=string types them as strings.
		return string(raw)
	}
}

func tsName(d protoreflect.Descriptor) string {
	return strings.ReplaceAll(strings.TrimPrefix(string(d.FullName()), protoPkg+"."), ".", "_")
}

// tsModule is the specifier the cross-check imports a type from. The `.js`
// suffix matches ts-proto's `importSuffix=.js`, which NodeNext resolution
// requires on relative imports.
func tsModule(d protoreflect.Descriptor) string {
	return "../src/" + strings.TrimSuffix(d.ParentFile().Path(), ".proto") + ".js"
}

func addImport(into map[string]map[string]bool, module, name string) {
	if into[module] == nil {
		into[module] = map[string]bool{}
	}
	into[module][name] = true
}

func sortedKeys(m map[string]map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeys2(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
