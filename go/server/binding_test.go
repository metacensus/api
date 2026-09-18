package server

import (
	"net/http/httptest"
	"testing"

	v1 "github.com/metacensus/api/go/metacensus/v1"
	"google.golang.org/protobuf/proto"
)

// No route carries query parameters today, so the generated code never calls
// bindQuery. This exercises it against a contract message anyway, so the path
// is not dead code the first time a GET grows a filter.
func TestBindQuery(t *testing.T) {
	rt := &Runtime{}

	// PropCitation has two uint32 fields; VoteContent an enum and a string.
	cases := []struct {
		name    string
		query   string
		allowed []string
		msg     func() (any, func() bool)
		wantErr string
	}{
		{
			name: "uint32 fields", query: "start=3&end=9", allowed: []string{"start", "end"},
			msg: func() (any, func() bool) {
				m := &v1.PropCitation{}
				return m, func() bool { return m.Start == 3 && m.End == 9 }
			},
		},
		{
			name: "enum by name and string", query: "position=Against&explanation=why", allowed: []string{"position", "explanation"},
			msg: func() (any, func() bool) {
				m := &v1.VoteContent{}
				return m, func() bool { return m.Position == v1.VoteContent_Against && m.Explanation == "why" }
			},
		},
		{
			name: "unknown key", query: "start=3&bogus=1", allowed: []string{"start", "end"},
			msg:     func() (any, func() bool) { return &v1.PropCitation{}, nil },
			wantErr: "query_unknown",
		},
		{
			name: "not allowed even if a field", query: "propId=x", allowed: []string{"position"},
			msg:     func() (any, func() bool) { return &v1.VoteContent{}, nil },
			wantErr: "query_unknown",
		},
		{
			name: "bad number", query: "start=three", allowed: []string{"start"},
			msg:     func() (any, func() bool) { return &v1.PropCitation{}, nil },
			wantErr: "query_invalid",
		},
		{
			name: "bad enum", query: "position=Maybe", allowed: []string{"position"},
			msg:     func() (any, func() bool) { return &v1.VoteContent{}, nil },
			wantErr: "query_invalid",
		},
		{
			name: "scalar repeated", query: "start=1&start=2", allowed: []string{"start"},
			msg:     func() (any, func() bool) { return &v1.PropCitation{}, nil },
			wantErr: "query_repeated",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, check := tc.msg()
			r := httptest.NewRequest("GET", "/x?"+tc.query, nil)
			err := rt.bindQuery(r, m.(proto.Message), tc.allowed)
			if tc.wantErr != "" {
				e, ok := err.(*Error)
				if !ok || e.Code != tc.wantErr {
					t.Fatalf("err %v, want code %s", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !check() {
				t.Errorf("bound %v", m)
			}
		})
	}
}

// A request message is one message however its fields travel, so the query
// string accepts the spelling protojson accepts in a body. Accepting
// {"topic_id": …} and rejecting ?topic_id= would make the wire format depend
// on which half of the request a field landed in.
func TestBindQueryAcceptsEitherSpelling(t *testing.T) {
	rt := &Runtime{}

	for _, q := range []string{"explanation=why", "explanation=why&position=Against"} {
		m := &v1.VoteContent{}
		r := httptest.NewRequest("GET", "/x?"+q, nil)
		if err := rt.bindQuery(r, m, []string{"position", "explanation"}); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		if m.Explanation != "why" {
			t.Errorf("%s: bound %v", q, m)
		}
	}

	// propId's proto name is prop_id; both spellings name the same field.
	for _, q := range []string{"propId=p1", "prop_id=p1"} {
		m := &v1.VoteContent{}
		r := httptest.NewRequest("GET", "/x?"+q, nil)
		if err := rt.bindQuery(r, m, []string{"propId"}); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		if m.PropId != "p1" {
			t.Errorf("%s: bound %q", q, m.PropId)
		}
	}

	// Both spellings at once: one field, two names, and nothing to say which
	// wins — so neither does.
	m := &v1.VoteContent{}
	r := httptest.NewRequest("GET", "/x?propId=a&prop_id=b", nil)
	err := rt.bindQuery(r, m, []string{"propId"})
	e, ok := err.(*Error)
	if !ok || e.Code != "query_repeated" {
		t.Errorf("err %v, want query_repeated", err)
	}

	// A field that exists but is not declared by this route is still unknown,
	// under either spelling.
	for _, q := range []string{"propId=p1", "prop_id=p1"} {
		r := httptest.NewRequest("GET", "/x?"+q, nil)
		err := rt.bindQuery(r, &v1.VoteContent{}, []string{"position"})
		if e, ok := err.(*Error); !ok || e.Code != "query_unknown" {
			t.Errorf("%s: err %v, want query_unknown", q, err)
		}
	}
}
