package routes

// Lookup returns the manifest's route for a "Service.RPC" name — e.g.
// Lookup("AuthRoutes.Refresh") — and whether the manifest declares it. It is
// the manifest's lookup face for code that needs one route by name rather than
// the whole table; the index is built once from Routes, so the two never drift.
//
// This file is hand-written (Routes is generated); a new route joins the index
// for free.
func Lookup(name string) (Route, bool) {
	r, ok := byName[name]
	return r, ok
}

var byName = func() map[string]Route {
	m := make(map[string]Route, len(Routes))
	for _, r := range Routes {
		m[r.Service+"."+r.RPC] = r
	}
	return m
}()
