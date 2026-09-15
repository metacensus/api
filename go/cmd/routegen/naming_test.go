package main

import (
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

// goNames reads Go names off the generated structs' tags. protoc-gen-go
// computes them in compiler/protogen, a public package. Feed protogen the same
// descriptors and the two must agree on every message and field; if they ever
// do not, the tag format or the naming has changed and the generated server
// would bind the wrong fields.
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
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if string(fd.Package()) == protoPkg {
			add(fd)
			req.FileToGenerate = append(req.FileToGenerate, fd.Path())
		}
		return true
	})
	if len(req.FileToGenerate) == 0 {
		t.Fatal("no contract files registered")
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
		var walk func(msgs []*protogen.Message)
		walk = func(msgs []*protogen.Message) {
			for _, m := range msgs {
				if m.Desc.IsMapEntry() {
					continue
				}
				got, _, err := goNames(m.Desc, m.Desc)
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

// The Go names the server rendering uses must be the ones protoc-gen-go emitted
// for the same protoc-gen-go version, so this pins which version that is: the
// runtime and the generator are the same module here.
func TestDescriptorsCarryGoPackage(t *testing.T) {
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if string(fd.Package()) != protoPkg {
			return true
		}
		opts, _ := fd.Options().(*descriptorpb.FileOptions)
		if got := opts.GetGoPackage(); got != v1Import+";metacensusv1" {
			t.Errorf("%s: go_package %q, want %q", fd.Path(), got, v1Import+";metacensusv1")
		}
		return true
	})
}
