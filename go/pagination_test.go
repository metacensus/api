package contract_test

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// No route paginates. demo does, so re-deriving any of its request messages
// would reintroduce the parameters one endpoint at a time: a test, not a
// one-off audit.
func TestNoPaginationFields(t *testing.T) {
	paginationNames := map[string]bool{
		"page": true, "limit": true, "offset": true, "cursor": true,
		"page_size": true, "per_page": true, "page_token": true,
		"skip": true, "take": true, "count": true, "total": true,
		"start": true, "end": true,
	}

	// The legitimate uses of those words on this surface.
	allowed := map[string]bool{
		// Referenced by no response, so defining these commits nothing.
		"metacensus.v1.ListMetadata.page":  true,
		"metacensus.v1.ListMetadata.limit": true,
		"metacensus.v1.ListMetadata.total": true,
		// Where in a PDF a reviewer highlighted an extracted value.
		"metacensus.v1.SourceLocationPage.page": true,
		// A character range in a prop's description.
		"metacensus.v1.PropCitation.start": true,
		"metacensus.v1.PropCitation.end":   true,
	}

	forEachContractMessage(t, func(md protoreflect.MessageDescriptor) {
		fields := md.Fields()
		for i := 0; i < fields.Len(); i++ {
			fd := fields.Get(i)
			name := string(fd.Name())
			if !paginationNames[name] || allowed[string(fd.FullName())] {
				continue
			}
			t.Errorf("%s: %q looks like a pagination field. There is no pagination in "+
				"this contract yet — every route serves everything. If this route "+
				"genuinely needs to page, that is a decision to take deliberately and on "+
				"its own; if the name means something else here, allow-list it in this "+
				"test with a comment saying what it means.", md.FullName(), name)
		}
	})
}
