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

	// PropCitation has two uint32 fields; VoteSetRequest an enum and a string.
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
				m := &v1.VoteSetRequest{}
				return m, func() bool { return m.Position == v1.Vote_Against && m.Explanation == "why" }
			},
		},
		{
			name: "unknown key", query: "start=3&bogus=1", allowed: []string{"start", "end"},
			msg:     func() (any, func() bool) { return &v1.PropCitation{}, nil },
			wantErr: "query_unknown",
		},
		{
			name: "not allowed even if a field", query: "propId=x", allowed: []string{"position"},
			msg:     func() (any, func() bool) { return &v1.VoteSetRequest{}, nil },
			wantErr: "query_unknown",
		},
		{
			name: "bad number", query: "start=three", allowed: []string{"start"},
			msg:     func() (any, func() bool) { return &v1.PropCitation{}, nil },
			wantErr: "query_invalid",
		},
		{
			name: "bad enum", query: "position=Maybe", allowed: []string{"position"},
			msg:     func() (any, func() bool) { return &v1.VoteSetRequest{}, nil },
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
