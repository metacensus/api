package main

import (
	"reflect"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// goCamelCase reimplements naming that lives in an internal package upstream,
// so "it agrees with protoc-gen-go" is an assertion until something checks it.
// A disagreement is a compile error in generated code, but only for a field
// shape the contract already has — this covers every field in the package
// rather than the handful the routes happen to bind, so a new one that names
// itself differently fails here rather than in whatever generates against it
// next.
func TestGoCamelCaseMatchesProtocGenGo(t *testing.T) {
	checked := 0

	eachMessage(t, func(md protoreflect.MessageDescriptor) {
		mt, err := protoregistry.GlobalTypes.FindMessageByName(md.FullName())
		if err != nil {
			t.Errorf("%s: not in the type registry: %v", md.FullName(), err)
			return
		}
		st := reflect.TypeOf(mt.New().Interface()).Elem()

		fields := md.Fields()
		for i := 0; i < fields.Len(); i++ {
			fd := fields.Get(i)
			want := goCamelCase(string(fd.Name()))
			if _, ok := st.FieldByName(want); !ok {
				t.Errorf("%s.%s: goCamelCase gives %q, which %s has no field for",
					md.FullName(), fd.Name(), want, st)
			}
			checked++
		}
	})

	if checked == 0 {
		t.Fatal("no fields checked; the registry walk found nothing")
	}
	t.Logf("checked %d fields", checked)
}

// eachMessage visits every message in the package, nested ones included.
func eachMessage(t *testing.T, fn func(protoreflect.MessageDescriptor)) {
	t.Helper()

	var walk func(protoreflect.MessageDescriptors)
	walk = func(mds protoreflect.MessageDescriptors) {
		for i := 0; i < mds.Len(); i++ {
			md := mds.Get(i)
			if md.IsMapEntry() {
				continue
			}
			fn(md)
			walk(md.Messages())
		}
	}

	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if string(fd.Package()) == protoPkg {
			walk(fd.Messages())
		}
		return true
	})
}
