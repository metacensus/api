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

// The golden files record what the wire looks like. These tests record what it
// is required to look like — the constraints the contract was written under,
// asserted directly rather than inferred by reading the goldens.
//
// The distinction matters when someone adds a message: the golden for it will
// be whatever the encoder produced, and only these tests will notice that what
// it produced breaks a rule.

var (
	lowerCamel = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)
	pascalCase = regexp.MustCompile(`^[A-Z][a-zA-Z0-9]*$`)
)

// TestJSONKeysAreLowerCamelCase walks every golden document and checks every
// key at every depth.
//
// This is the constraint that costs nothing and is therefore easiest to break:
// protojson derives lowerCamelCase from proto's snake_case automatically, so it
// holds as long as nobody adds a `json_name` override or a field name that is
// already camel-cased in the .proto (`sortOrder` instead of `sort_order`, which
// protoc accepts and passes through unchanged).
func TestJSONKeysAreLowerCamelCase(t *testing.T) {
	forEachGoldenDocument(t, func(t *testing.T, name string, doc any) {
		walkJSON(doc, "", func(path string, key string, _ any) {
			if !lowerCamel.MatchString(key) {
				t.Errorf("%s: key %q at %s is not lowerCamelCase", name, key, path)
			}
		})
	})
}

// TestEnumsSerialiseAsPascalCaseStrings checks the enum convention on the wire
// rather than in the .proto: every enum-valued field must be a JSON string, and
// that string must be PascalCase.
//
// The reason to assert it here and not just trust buf's lint config is that the
// lint config *excepts* the rule that would normally police enum value names.
// Something has to police them instead.
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

// TestEveryEnumZeroValueIsUnspecified backs up the buf lint setting with a
// check that runs even if someone edits buf.yaml.
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

// TestNoEnumsAtPackageScope checks the nesting rule.
//
// Enum value names are scoped to the enclosing message, C++ style, not to the
// enum. Two package-scope enums could not both have an `Unspecified` value, and
// a package-scope enum's Go constants would be named for the enum rather than
// for anything that owns them. Nesting each enum inside its resource keeps the
// vocabulary attached to the thing it describes and makes collisions
// structurally impossible.
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

// TestListResponsesUseItemsAndMetadata enforces the list envelope.
//
// Any message whose name ends in `List` is a list response and must have
// exactly two fields: a repeated `items` and a `metadata`. The k8s shape,
// picked because it leaves room to add to a list response without breaking it —
// which a bare array does not.
func TestListResponsesUseItemsAndMetadata(t *testing.T) {
	forEachContractMessage(t, func(md protoreflect.MessageDescriptor) {
		name := string(md.Name())
		if !strings.HasSuffix(name, "List") || strings.HasSuffix(name, "ListRequest") {
			return
		}

		fields := md.Fields()
		if fields.Len() != 2 {
			t.Errorf("%s: list response has %d fields, want exactly items and metadata", name, fields.Len())
			return
		}

		items := fields.ByName("items")
		if items == nil {
			t.Errorf("%s: no `items` field", name)
		} else if !items.IsList() {
			t.Errorf("%s: `items` is not repeated", name)
		}

		metadata := fields.ByName("metadata")
		if metadata == nil {
			t.Errorf("%s: no `metadata` field", name)
		} else if metadata.Kind() != protoreflect.MessageKind ||
			metadata.Message().FullName() != "metacensus.v1.ListMetadata" {
			t.Errorf("%s: `metadata` is not a ListMetadata", name)
		}
	})
}

// TestNoTopLevelArrays checks that no golden document is a bare array.
//
// Every current backend returns bare arrays from its list endpoints; this is
// the rule that says they may not. A top-level array cannot grow a sibling
// field, so pagination or a warning can never be added to one without breaking
// every consumer.
func TestNoTopLevelArrays(t *testing.T) {
	forEachGoldenDocument(t, func(t *testing.T, name string, doc any) {
		if _, ok := doc.(map[string]any); !ok {
			t.Errorf("%s: document root is %T, want a JSON object", name, doc)
		}
	})
}

// TestIdsAreStrings checks constraint 6 on the wire.
//
// demo mints integer serials and infra mints prefixed UUIDs, so any numeric id
// in a payload would be ratifying one backend's format. Every field whose name
// is or ends in `id` must be declared as a string and must serialise as one.
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

// TestTimestampsAreRFC3339 checks that every timestamp in every golden parses
// as RFC 3339.
//
// It also catches the shape a `time.Time` would take under encoding/json rather
// than protojson: `{"seconds":…,"nanos":…}` is an object, not a string, and
// fails here.
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

// TestPresenceIsExpressedOnlyByMessageFields locks in the other half of the
// EmitDefaultValues decision.
//
// Under EmitDefaultValues a scalar is always emitted and a message field is
// emitted only when set, which is exactly what ts-proto's
// `useOptionals=messages` describes. A proto3 `optional` scalar would be a
// third case — omitted by the encoder but not marked optional by ts-proto under
// that setting — and the two languages would disagree about whether the key is
// always there. Wrapper types (`Int32Value`) express the same optionality
// through a message field, which both sides already agree on.
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
