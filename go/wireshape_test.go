package contract_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	contract "github.com/metacensus/ui/contract/go"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// The goldens record what the wire looks like; these record what it must look
// like. A new message's golden is whatever the encoder produced — only these
// notice that it broke a rule.

var (
	lowerCamel = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)
	pascalCase = regexp.MustCompile(`^[A-Z][a-zA-Z0-9]*$`)
)

// TestJSONKeysAreLowerCamelCase checks every key at every depth. protojson does
// this automatically; a `json_name` override or an already-camelCased proto
// field name would break it.
func TestJSONKeysAreLowerCamelCase(t *testing.T) {
	forEachGoldenDocument(t, func(t *testing.T, name string, doc any) {
		walkJSON(doc, "", func(path string, key string, _ any) {
			if !lowerCamel.MatchString(key) {
				t.Errorf("%s: key %q at %s is not lowerCamelCase", name, key, path)
			}
		})
	})
}

// TestEnumsSerialiseAsPascalCaseStrings checks the enum convention on the wire.
// buf's lint config excepts the rule that would normally police value names, so
// this replaces it.
func TestEnumsSerialiseAsPascalCaseStrings(t *testing.T) {
	for _, f := range fixtures() {
		f := f
		t.Run(f.name, func(t *testing.T) {
			raw, err := contract.Marshal(f.msg)
			if err != nil {
				t.Fatal(err)
			}
			var doc any
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			checkEnums(t, f.msg.ProtoReflect().Descriptor(), doc, f.name)
		})
	}
}

func checkEnums(t *testing.T, md protoreflect.MessageDescriptor, doc any, path string) {
	t.Helper()

	obj, ok := doc.(map[string]any)
	if !ok {
		return
	}
	for key, value := range obj {
		fd := md.Fields().ByJSONName(key)
		if fd == nil {
			t.Errorf("%s: field %q is not declared on %s", path, key, md.FullName())
			continue
		}

		values := []any{value}
		if fd.IsList() {
			list, ok := value.([]any)
			if !ok {
				t.Errorf("%s.%s: repeated field did not serialise as an array", path, key)
				continue
			}
			values = list
		}

		for _, v := range values {
			switch fd.Kind() {
			case protoreflect.EnumKind:
				s, ok := v.(string)
				if !ok {
					t.Errorf("%s.%s: enum serialised as %T, want string", path, key, v)
					continue
				}
				if !pascalCase.MatchString(s) {
					t.Errorf("%s.%s: enum value %q is not PascalCase", path, key, s)
				}
				if fd.Enum().Values().ByName(protoreflect.Name(s)) == nil {
					t.Errorf("%s.%s: %q is not a value of %s", path, key, s, fd.Enum().FullName())
				}
			case protoreflect.MessageKind, protoreflect.GroupKind:
				if strings.HasPrefix(string(fd.Message().FullName()), "google.protobuf.") {
					continue
				}
				checkEnums(t, fd.Message(), v, path+"."+key)
			}
		}
	}
}

// TestEveryEnumZeroValueIsUnspecified holds even if buf.yaml is edited.
func TestEveryEnumZeroValueIsUnspecified(t *testing.T) {
	forEachContractMessage(t, func(md protoreflect.MessageDescriptor) {
		enums := md.Enums()
		for i := 0; i < enums.Len(); i++ {
			ed := enums.Get(i)
			if got := string(ed.Values().Get(0).Name()); got != "Unspecified" {
				t.Errorf("%s: zero value is %q, want \"Unspecified\"", ed.FullName(), got)
			}
		}
	})
}

// TestNoEnumsAtPackageScope keeps every enum nested in its resource. Value
// names are scoped to the enclosing message, so two package-scope enums could
// not both have an `Unspecified`.
func TestNoEnumsAtPackageScope(t *testing.T) {
	forEachContractFile(t, func(fd protoreflect.FileDescriptor) {
		if n := fd.Enums().Len(); n > 0 {
			for i := 0; i < n; i++ {
				t.Errorf("%s: enum %s is declared at package scope; nest it in the message that owns it",
					fd.Path(), fd.Enums().Get(i).Name())
			}
		}
	})
}

// TestListResponsesWrapItems enforces the list envelope: a list response is an
// object with a single repeated `items`, never a bare array.
//
// `ListMetadata` is defined but referenced by nothing — see
// TestListMetadataIsUnreferenced — so a list carries only its items today.
func TestListResponsesWrapItems(t *testing.T) {
	forEachContractMessage(t, func(md protoreflect.MessageDescriptor) {
		name := string(md.Name())
		if !strings.HasSuffix(name, "List") || strings.HasSuffix(name, "ListRequest") {
			return
		}

		fields := md.Fields()
		if fields.Len() != 1 {
			t.Errorf("%s: list response has %d fields, want only items", name, fields.Len())
			return
		}
		items := fields.ByName("items")
		if items == nil {
			t.Errorf("%s: no `items` field", name)
		} else if !items.IsList() {
			t.Errorf("%s: `items` is not repeated", name)
		}
	})
}

// TestListMetadataIsUnreferenced holds the line that pagination is defined and
// not yet adopted. `ListMetadata` carries the fields a paginated response will
// need, so that the shape is agreed in advance; wiring it into a response is a
// separate, deliberate decision per route.
func TestListMetadataIsUnreferenced(t *testing.T) {
	forEachContractMessage(t, func(md protoreflect.MessageDescriptor) {
		fields := md.Fields()
		for i := 0; i < fields.Len(); i++ {
			fd := fields.Get(i)
			if fd.Kind() == protoreflect.MessageKind &&
				fd.Message().FullName() == "metacensus.v1.ListMetadata" {
				t.Errorf("%s.%s references ListMetadata. No route paginates yet; "+
					"adopting it is a per-route decision, not a default.", md.FullName(), fd.Name())
			}
		}
	})
}

// TestNoTopLevelArrays checks no golden document is a bare array. An array
// cannot grow a sibling field without breaking every consumer.
func TestNoTopLevelArrays(t *testing.T) {
	forEachGoldenDocument(t, func(t *testing.T, name string, doc any) {
		if _, ok := doc.(map[string]any); !ok {
			t.Errorf("%s: document root is %T, want a JSON object", name, doc)
		}
	})
}

// TestIdsAreStrings requires every field named `id` or `*_id` to be a string.
// The two backends mint incompatible id formats; strings ratify neither.
func TestIdsAreStrings(t *testing.T) {
	forEachContractMessage(t, func(md protoreflect.MessageDescriptor) {
		fields := md.Fields()
		for i := 0; i < fields.Len(); i++ {
			fd := fields.Get(i)
			name := string(fd.Name())
			if name != "id" && !strings.HasSuffix(name, "_id") {
				continue
			}
			if fd.Kind() != protoreflect.StringKind {
				t.Errorf("%s.%s is %s, want string", md.FullName(), name, fd.Kind())
			}
		}
	})
}

// TestTimestampsAreRFC3339 also catches the `{"seconds":…,"nanos":…}` shape a
// timestamp takes under encoding/json rather than protojson.
func TestTimestampsAreRFC3339(t *testing.T) {
	for _, f := range fixtures() {
		f := f
		t.Run(f.name, func(t *testing.T) {
			raw, err := contract.Marshal(f.msg)
			if err != nil {
				t.Fatal(err)
			}
			var doc any
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			checkTimestamps(t, f.msg.ProtoReflect().Descriptor(), doc, f.name)
		})
	}
}

func checkTimestamps(t *testing.T, md protoreflect.MessageDescriptor, doc any, path string) {
	t.Helper()

	obj, ok := doc.(map[string]any)
	if !ok {
		return
	}
	for key, value := range obj {
		fd := md.Fields().ByJSONName(key)
		if fd == nil || (fd.Kind() != protoreflect.MessageKind && fd.Kind() != protoreflect.GroupKind) {
			continue
		}

		values := []any{value}
		if fd.IsList() {
			list, _ := value.([]any)
			values = list
		}

		for _, v := range values {
			switch fd.Message().FullName() {
			case "google.protobuf.Timestamp":
				s, ok := v.(string)
				if !ok {
					t.Errorf("%s.%s: timestamp serialised as %T, want an RFC 3339 string", path, key, v)
					continue
				}
				if _, err := time.Parse(time.RFC3339, s); err != nil {
					t.Errorf("%s.%s: %q is not RFC 3339: %v", path, key, s, err)
				}
			default:
				if !strings.HasPrefix(string(fd.Message().FullName()), "google.protobuf.") {
					checkTimestamps(t, fd.Message(), v, path+"."+key)
				}
			}
		}
	}
}

// TestPresenceIsExpressedOnlyByMessageFields keeps Go and TypeScript agreeing
// about which keys are always present. A proto3 `optional` scalar is a third
// case neither side's settings describe.
func TestPresenceIsExpressedOnlyByMessageFields(t *testing.T) {
	forEachContractMessage(t, func(md protoreflect.MessageDescriptor) {
		fields := md.Fields()
		for i := 0; i < fields.Len(); i++ {
			fd := fields.Get(i)
			if fd.HasOptionalKeyword() && fd.Kind() != protoreflect.MessageKind {
				t.Errorf("%s.%s: proto3 `optional` on a scalar. Use a wrapper message "+
					"(google.protobuf.Int32Value and friends) so Go and TypeScript agree "+
					"about whether the key is present.", md.FullName(), fd.Name())
			}
		}
	})
}

// --- helpers ---------------------------------------------------------------

func forEachGoldenDocument(t *testing.T, check func(t *testing.T, name string, doc any)) {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(goldenDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no golden files; run `go test ./... -update`")
	}

	for _, path := range paths {
		path := path
		name := strings.TrimSuffix(filepath.Base(path), ".json")
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var doc any
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatalf("%s is not valid JSON: %v", path, err)
			}
			check(t, name, doc)
		})
	}
}

func walkJSON(node any, path string, visit func(path, key string, value any)) {
	switch n := node.(type) {
	case map[string]any:
		for key, value := range n {
			visit(path, key, value)
			walkJSON(value, path+"."+key, visit)
		}
	case []any:
		for i, value := range n {
			walkJSON(value, path+"["+itoa(i)+"]", visit)
		}
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
