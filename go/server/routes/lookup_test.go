package routes

import (
	"reflect"
	"testing"
)

// TestLookup: a declared route resolves by its "Service.RPC" name to the same
// value the table holds; an undeclared name resolves to nothing.
func TestLookup(t *testing.T) {
	got, ok := Lookup("AuthRoutes.Refresh")
	if !ok {
		t.Fatal("AuthRoutes.Refresh not found")
	}
	if got.Method != "POST" || got.Path != "/refresh" {
		t.Errorf("AuthRoutes.Refresh = %s %s, want POST /refresh", got.Method, got.Path)
	}

	if _, ok := Lookup("AuthRoutes.LogOut"); ok { // a typo: no such rpc
		t.Error("a name absent from the manifest resolved")
	}
}

// TestLookupCoversEveryRoute: every route in the table is reachable by name, so
// the index and the table never disagree.
func TestLookupCoversEveryRoute(t *testing.T) {
	for _, r := range Routes {
		got, ok := Lookup(r.Service + "." + r.RPC)
		if !ok {
			t.Errorf("%s.%s in Routes but not in the index", r.Service, r.RPC)
			continue
		}
		if !reflect.DeepEqual(got, r) {
			t.Errorf("%s.%s: index holds %+v, table holds %+v", r.Service, r.RPC, got, r)
		}
	}
}
