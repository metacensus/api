package contract_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/metacensus/api/go/internal/protoscan"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// These are the conventions in contract/README.md, checked against the compiled
// descriptors so they are properties of the schema rather than of a paragraph.
//
// Agreement between the JSON Go emits and the TypeScript generated from the same
// .proto is not checked here: it needs documents, and inventing them proved
// worse than waiting for real ones. https://github.com/metacensus/ui/issues/49.

// Every proto package these invariants govern. TestEveryPackageIsGoverned
// makes a package missing from this list a failure rather than a silence.
//
// The invariants are about how a schema meets JSON, so neither surface gets to
// opt out of them, however much else the two differ.
var contractPackages = []string{
	"metacensus.v1",
	"metacensus.public.v1",
}

// protoDir is relative to this package's directory, where `go test` runs.
const protoDir = "../proto"

func isContractPackage(pkg string) bool {
	return slices.Contains(contractPackages, pkg)
}

// sharedFileOf is the one file in a package whose messages any file in that
// package may be typed from. Shared within a surface: nothing makes
// metacensus.v1's common.proto shared with the public one.
func sharedFileOf(pkg string) string {
	return strings.ReplaceAll(pkg, ".", "/") + "/common.proto"
}

var (
	lowerCamel = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)
	pascalCase = regexp.MustCompile(`^[A-Z][a-zA-Z0-9]*$`)
)

// protojson derives JSON names from snake_case proto names, so this catches an
// explicit json_name override or an already-camelCased field.
func TestJSONNamesAreLowerCamelCase(t *testing.T) {
	forEachContractMessage(t, func(md protoreflect.MessageDescriptor) {
		fields := md.Fields()
		for i := 0; i < fields.Len(); i++ {
			fd := fields.Get(i)
			if !lowerCamel.MatchString(fd.JSONName()) {
				t.Errorf("%s: JSON name %q is not lowerCamelCase", fd.FullName(), fd.JSONName())
			}
		}
	})
}

// protojson serialises an enum as its value name, so the value names are the
// JSON vocabulary. buf's lint config excepts the rule that would police them.
func TestEnumValuesArePascalCase(t *testing.T) {
	forEachContractEnum(t, func(ed protoreflect.EnumDescriptor) {
		values := ed.Values()
		for i := 0; i < values.Len(); i++ {
			if name := string(values.Get(i).Name()); !pascalCase.MatchString(name) {
				t.Errorf("%s: value %q is not PascalCase", ed.FullName(), name)
			}
		}
	})
}

// Holds even if buf.yaml's enum_zero_value_suffix is edited.
func TestEveryEnumZeroValueIsUnspecified(t *testing.T) {
	forEachContractEnum(t, func(ed protoreflect.EnumDescriptor) {
		if got := string(ed.Values().Get(0).Name()); got != "Unspecified" {
			t.Errorf("%s: zero value is %q, want \"Unspecified\"", ed.FullName(), got)
		}
	})
}

// Enum value names are scoped to the enclosing message, so two package-scope
// enums could not both have an Unspecified.
func TestNoEnumsAtPackageScope(t *testing.T) {
	forEachContractFile(t, func(fd protoreflect.FileDescriptor) {
		enums := fd.Enums()
		for i := 0; i < enums.Len(); i++ {
			t.Errorf("%s: enum %s is declared at package scope; nest it in the message that owns it",
				fd.Path(), enums.Get(i).Name())
		}
	})
}

// A list response is an object with a single repeated items, never a bare array
// and never a second field.
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

// ListMetadata fixes the shape a paginated response will take. Wiring it into
// one is a per-route decision, not a default.
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

// A proto3 optional scalar is a third presence case neither side's settings
// describe, so Go and TypeScript would disagree about whether the key exists.
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

// A message may be typed only from its own file or its own package's
// common.proto, so one resource's shape cannot be bent by another's needs — and
// never from another contract package, because the two surfaces owe different
// compatibility and a shared field would put one policy in charge of the
// other's wire shape.
//
// Fields, not imports: an rpc naming another resource as its return type adds no
// field and shapes no message.
func TestNoMessageFieldCrossesResourceFiles(t *testing.T) {
	forEachContractMessage(t, func(md protoreflect.MessageDescriptor) {
		home := md.ParentFile().Path()
		homePkg := string(md.ParentFile().Package())
		shared := sharedFileOf(homePkg)

		fields := md.Fields()
		for i := 0; i < fields.Len(); i++ {
			fd := fields.Get(i)

			var target protoreflect.FileDescriptor
			switch fd.Kind() {
			case protoreflect.MessageKind, protoreflect.GroupKind:
				target = fd.Message().ParentFile()
			case protoreflect.EnumKind:
				target = fd.Enum().ParentFile()
			default:
				continue
			}

			targetPkg := string(target.Package())

			// Well-known types belong to everyone.
			if !isContractPackage(targetPkg) {
				continue
			}
			if targetPkg != homePkg {
				t.Errorf("%s.%s is typed from %s, in package %s. The two surfaces owe "+
					"different compatibility to different callers; a field spanning them "+
					"would put one surface's policy in charge of the other's wire shape.",
					md.FullName(), fd.Name(), target.Path(), targetPkg)
				continue
			}
			if target.Path() == home || target.Path() == shared {
				continue
			}
			t.Errorf("%s.%s is typed from %s. A resource's messages may only be "+
				"typed from their own file or %s.", md.FullName(), fd.Name(), target.Path(), shared)
		}
	})
}

// TestEveryPackageIsGoverned is the check that makes every other check in this
// file honest: each one iterates contractPackages, so a package absent from it
// is not exempt but invisible, and the suite stays green over a schema smaller
// than the one that ships.
//
// It reads the .proto tree rather than protoregistry, which holds only what
// this binary imported — a package nobody blank-imported in registered_test.go
// would be missing from both the registry and the check.
func TestEveryPackageIsGoverned(t *testing.T) {
	onDisk, err := protoscan.Packages(protoDir)
	if err != nil {
		t.Fatalf("scanning %s: %v", protoDir, err)
	}
	if len(onDisk) == 0 {
		t.Fatalf("no .proto files under %s", protoDir)
	}

	for _, pkg := range onDisk {
		if !isContractPackage(pkg) {
			t.Errorf("%s is not in contractPackages, so none of the schema invariants in "+
				"this file are checked against it. Add it — or, if it genuinely should be "+
				"exempt, record here which invariant it cannot satisfy and why.", pkg)
		}
	}
}

// A package whose shared file is not named where sharedFileOf looks would
// silently lose the exemption above, and the resulting failures would point at
// the wrong thing.
func TestSharedFileNamingHolds(t *testing.T) {
	present := map[string]bool{}
	forEachContractFile(t, func(fd protoreflect.FileDescriptor) {
		present[fd.Path()] = true
	})

	for _, pkg := range contractPackages {
		if shared := sharedFileOf(pkg); !present[shared] {
			t.Errorf("package %s has no %s. The cross-file rule exempts that path by "+
				"name; without it, a genuinely shared message has nowhere to live.", pkg, shared)
		}
	}
}

// --- helpers ---------------------------------------------------------------

func forEachContractFile(t *testing.T, visit func(protoreflect.FileDescriptor)) {
	t.Helper()

	seen := map[string]int{}
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		pkg := string(fd.Package())
		if isContractPackage(pkg) {
			seen[pkg]++
			visit(fd)
		}
		return true
	})

	// Per package, not in total: one package's files must not vouch for
	// another's absence.
	for _, pkg := range contractPackages {
		if seen[pkg] == 0 {
			t.Fatalf("no files registered for package %s. Either the name is wrong, or "+
				"its generated Go is not imported by this test binary — see the blank "+
				"imports in registered_test.go. Every check here would otherwise pass "+
				"without reading it.", pkg)
		}
	}
}

func forEachContractMessage(t *testing.T, visit func(protoreflect.MessageDescriptor)) {
	t.Helper()

	var walk func(protoreflect.MessageDescriptors)
	walk = func(mds protoreflect.MessageDescriptors) {
		for i := 0; i < mds.Len(); i++ {
			md := mds.Get(i)
			// Map entries are synthetic and never appear on the wire.
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

// forEachContractEnum visits every enum nested in a contract message.
// TestNoEnumsAtPackageScope holds that there are no others.
func forEachContractEnum(t *testing.T, visit func(protoreflect.EnumDescriptor)) {
	t.Helper()

	forEachContractMessage(t, func(md protoreflect.MessageDescriptor) {
		enums := md.Enums()
		for i := 0; i < enums.Len(); i++ {
			visit(enums.Get(i))
		}
	})
}
