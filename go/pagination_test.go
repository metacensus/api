package contract_test

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// TestNoPaginationFields locks in a ruling about the whole surface: there is no
// pagination anywhere in this contract. Every route serves everything, all
// together. A route grows pagination when it needs it — at which point the
// `{items, metadata}` envelope on every list response lets it do so without
// breaking consumers, which is the only reason that envelope exists today.
//
// This is a test rather than a one-time audit because the failure mode is
// gradual. demo genuinely paginates today — `GET /topic` and `GET /user` take
// `page`/`limit` in the query string, `POST /paper`, `POST /extraction-review`
// and `POST /protocol-template` take them as body fields — so re-deriving any
// one of those request messages from demo would quietly reintroduce the
// parameters, one endpoint at a time, with no single change large enough to
// argue about.
//
// It also guards the shared `ListMetadata`, which is where the same pressure
// would land from the response side. See the warning on that message: it is
// shared by infra-derived lists and demo-only lists alike, so the first field
// added to it is added to all of them on behalf of whichever route asked.
func TestNoPaginationFields(t *testing.T) {
	paginationNames := map[string]bool{
		"page": true, "limit": true, "offset": true, "cursor": true,
		"page_size": true, "per_page": true, "page_token": true,
		"skip": true, "take": true, "count": true, "total": true,
		"start": true, "end": true,
	}

	// The legitimate uses of those words on this surface. Both are offsets into
	// a document rather than into a result set.
	allowed := map[string]bool{
		// Where in a PDF a reviewer highlighted an extracted value.
		"metacensus.v1.SourceLocationPage.page": true,
		// A character range in a prop's description, highlighted by a voter.
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
