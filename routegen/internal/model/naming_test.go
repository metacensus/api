package model

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

// TestGoNamesAgreeWithProtogen feeds the real descriptors to compiler/protogen,
// the public package protoc-gen-go is built on, and checks goNames agrees
// with it on every message and field, over every contract package (so an
// entry in Packages naming the wrong GoImport is caught here too).
func TestGoNamesAgreeWithProtogen(t *testing.T) {
	req := &pluginpb.CodeGeneratorRequest{
		CompilerVersion: &pluginpb.Version{Major: proto.Int32(0), Minor: proto.Int32(0), Patch: proto.Int32(0)},
	}
	seen := map[string]bool{}
	var add func(fd protoreflect.FileDescriptor)
	add = func(fd protoreflect.FileDescriptor) {
		if seen[fd.Path()] {
			return
		}
		seen[fd.Path()] = true
		imports := fd.Imports()
		for i := 0; i < imports.Len(); i++ {
			add(imports.Get(i).FileDescriptor)
		}
		req.ProtoFile = append(req.ProtoFile, protodesc.ToFileDescriptorProto(fd))
	}
	// The package each generated file belongs to, for goNames below.
	pkgOf := map[string]Package{}
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		for _, pkg := range Packages {
			if string(fd.Package()) == pkg.Proto {
				add(fd)
				req.FileToGenerate = append(req.FileToGenerate, fd.Path())
				pkgOf[fd.Path()] = pkg
			}
		}
		return true
	})
	if len(req.FileToGenerate) == 0 {
		t.Fatal("no contract files registered")
	}
	// Per package, so one surface's files cannot vouch for another's absence.
	for _, pkg := range Packages {
		var any bool
		for _, p := range pkgOf {
			if p.Proto == pkg.Proto {
				any = true
				break
			}
		}
		if !any {
			t.Fatalf("no files registered for %s", pkg.Proto)
		}
	}

	plugin, err := protogen.Options{}.New(req)
	if err != nil {
		t.Fatal(err)
	}

	checked := 0
	for _, f := range plugin.Files {
		if !f.Generate {
			continue
		}
		pkg := pkgOf[f.Desc.Path()]
		var walk func(msgs []*protogen.Message)
		walk = func(msgs []*protogen.Message) {
			for _, m := range msgs {
				if m.Desc.IsMapEntry() {
					continue
				}
				got, _, err := goNames(pkg, m.Desc, m.Desc)
				if err != nil {
					t.Errorf("%s: %v", m.Desc.FullName(), err)
					continue
				}
				if got.typeName != m.GoIdent.GoName {
					t.Errorf("%s: tags say Go type %q, protogen says %q", m.Desc.FullName(), got.typeName, m.GoIdent.GoName)
				}
				for _, fld := range m.Fields {
					if fld.Oneof != nil {
						continue
					}
					if got.fields[fld.Desc.Name()] != fld.GoName {
						t.Errorf("%s: tags say Go field %q, protogen says %q", fld.Desc.FullName(), got.fields[fld.Desc.Name()], fld.GoName)
					}
					checked++
				}
				walk(m.Messages)
			}
		}
		walk(f.Messages)
	}
	if checked == 0 {
		t.Fatal("no fields compared")
	}
	t.Logf("%d fields agree", checked)
}

// TestDescriptorsCarryGoPackage checks each file's go_package option against
// what Packages says it should be: GoImport, then ";", then the proto
// package with its dots removed. A file that spells either differently is
// what makes model.Packages and the .proto sources disagree.
func TestDescriptorsCarryGoPackage(t *testing.T) {
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		for _, pkg := range Packages {
			if string(fd.Package()) != pkg.Proto {
				continue
			}
			want := pkg.GoImport + ";" + strings.ReplaceAll(pkg.Proto, ".", "")
			opts, _ := fd.Options().(*descriptorpb.FileOptions)
			if got := opts.GetGoPackage(); got != want {
				t.Errorf("%s: go_package %q, want %q", fd.Path(), got, want)
			}
		}
		return true
	})
}
