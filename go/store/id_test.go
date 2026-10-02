package store

import (
	"strings"
	"testing"
)

func TestParseID(t *testing.T) {
	const v7 = "0192a642-817d-7a3e-a282-d7a282ebd482"
	for _, tt := range []struct {
		name    string
		kind    IDKind
		id      string
		wantErr bool
	}{
		{name: "success - minted", kind: TopicID, id: NewID(TopicID)},
		{name: "success - infra's", kind: TopicID, id: "topc:" + v7},
		{name: "error - another kind", kind: UserID, id: "topc:" + v7, wantErr: true},
		{name: "error - no kind", kind: TopicID, id: v7, wantErr: true},
		{name: "error - empty", kind: TopicID, id: "", wantErr: true},
		{name: "error - uppercase", kind: TopicID, id: "topc:" + strings.ToUpper(v7), wantErr: true},
		{name: "error - no dashes", kind: TopicID, id: "topc:" + strings.ReplaceAll(v7, "-", ""), wantErr: true},
		{name: "error - urn form", kind: TopicID, id: "topc:urn:uuid:" + v7, wantErr: true},
		{name: "error - UUIDv4", kind: TopicID, id: "topc:0192a642-817d-4a3e-a282-d7a282ebd482", wantErr: true},
		{name: "error - not RFC 9562 variant", kind: TopicID, id: "topc:0192a642-817d-7a3e-c282-d7a282ebd482", wantErr: true},
		{name: "error - the old base64url minter", kind: TopicID, id: "topc:q83vEjRWeJq83vEjRWeJqw", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			u, err := ParseID(tt.kind, tt.id)
			if tt.wantErr {
				if KindOf(err) != InvalidContent {
					t.Fatalf("want InvalidContent, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if want := tt.id[len(tt.kind)+1:]; u.String() != want {
				t.Errorf("suffix %s, want %s", u, want)
			}
		})
	}
}

// Ids minted in one process sort in the order they were minted.
func TestNewIDSortsByMint(t *testing.T) {
	prev := NewID(PropID)
	for range 1000 {
		next := NewID(PropID)
		if next <= prev {
			t.Fatalf("%s minted after %s sorts before it", next, prev)
		}
		prev = next
	}
}
