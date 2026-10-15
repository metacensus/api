package contract_test

import (
	"maps"
	"testing"

	"github.com/metacensus/api/go/server/routes"
)

func TestNonConformingRoutes_Methods(t *testing.T) {
	tests := []struct {
		name, service, rpc, method, path, want string
	}{
		{"error - write over GET", "TopicRoutes", "CreateTopic", "GET", "/topic", "write over GET"},
		{"error - set over GET", "PropRoutes", "SetVote", "GET", "/vote", "write over GET"},
		{"error - auth over GET", "AuthRoutes", "Login", "GET", "/login", "write over GET"},
		{"error - read over POST", "TopicRoutes", "GetTopic", "POST", "/topic", "read over POST"},
		{"error - list over POST", "TopicRoutes", "ListTopics", "POST", "/topic", "read over POST"},
		{"error - lookup over POST", "TopicRoutes", "LookupTopic", "POST", "/topic", "read over POST"},
		{"error - PUT", "TopicRoutes", "CreateTopic", "PUT", "/topic", "unsupported method: PUT"},
		{"error - PATCH", "TopicRoutes", "CreateTopic", "PATCH", "/topic", "unsupported method: PATCH"},
		{"error - DELETE", "TopicRoutes", "CreateTopic", "DELETE", "/topic", "unsupported method: DELETE"},
		{"error - HEAD", "TopicRoutes", "CreateTopic", "HEAD", "/topic", "unsupported method: HEAD"},
		{"error - OPTIONS", "TopicRoutes", "CreateTopic", "OPTIONS", "/topic", "unsupported method: OPTIONS"},
		{"error - unknown method", "TopicRoutes", "CreateTopic", "BREW", "/topic", "unsupported method: BREW"},
		{"error - empty method", "TopicRoutes", "CreateTopic", "", "/topic", "unsupported method: "},
		{"error - lowercase method", "TopicRoutes", "CreateTopic", "post", "/topic", "unsupported method: post"},
		{"error - read over HEAD", "TopicRoutes", "GetTopic", "HEAD", "/topic", "unsupported method: HEAD; read over HEAD"},
		{"error - method read and path", "TopicRoutes", "GetTopic", "PUT", "/topic/update", "unsupported method: PUT; read over PUT; verb in path: update"},
		{"error - write and path", "TopicRoutes", "CreateTopic", "GET", "/topic/create", "write over GET; verb in path: create"},
		{"error - path alone", "TopicRoutes", "CreateTopic", "POST", "/topic/create", "verb in path: create"},
		{"error - health over POST", "HealthRoutes", "Healthcheck", "POST", "/healthcheck", "read over POST"},
		{"error - health over OPTIONS", "HealthRoutes", "Healthcheck", "OPTIONS", "/healthcheck", "unsupported method: OPTIONS; read over OPTIONS"},
		{"error - health name on another service", "TopicRoutes", "Healthcheck", "GET", "/healthcheck", "write over GET"},
		{"error - health name prefix", "HealthRoutes", "HealthcheckReset", "GET", "/healthcheck", "write over GET"},
		{"success - get", "TopicRoutes", "GetTopic", "GET", "/topic/{topicId}", ""},
		{"success - list", "TopicRoutes", "ListTopics", "GET", "/topic", ""},
		{"success - lookup", "TopicRoutes", "LookupTopic", "GET", "/topic", ""},
		{"success - create", "TopicRoutes", "CreateTopic", "POST", "/topic", ""},
		{"success - set", "PropRoutes", "SetVote", "POST", "/vote", ""},
		{"success - auth", "AuthRoutes", "Login", "POST", "/login", ""},
		{"success - health", "HealthRoutes", "Healthcheck", "GET", "/healthcheck", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := routes.Route{Prefix: routes.Prefix, Service: tt.service, RPC: tt.rpc, Method: tt.method, Path: tt.path}
			want := map[string]string{}
			if tt.want != "" {
				want[r.Method+" "+fullPath(r)] = tt.want
			}
			if got := nonConformingRoutes([]routes.Route{r}); !maps.Equal(got, want) {
				t.Errorf("non-conforming routes = %v, want %v", got, want)
			}
		})
	}
}

func TestNonConformingRoutes_AccumulatesByFullPath(t *testing.T) {
	tests := []struct {
		name   string
		routes []routes.Route
		want   map[string]string
	}{
		{
			name: "error - shared route keeps both reasons",
			routes: []routes.Route{
				{Prefix: routes.Prefix, RPC: "GetTopic", Method: "POST", Path: "/topic/create"},
				{Prefix: routes.Prefix, RPC: "CreateTopic", Method: "POST", Path: "/topic/create"},
			},
			want: map[string]string{
				"POST " + routes.Prefix + "/topic/create": "read over POST; verb in path: create; verb in path: create",
			},
		},
		{
			name: "error - surfaces keep separate reasons",
			routes: []routes.Route{
				{Prefix: routes.Prefix, RPC: "GetTopic", Method: "POST", Path: "/topic/create"},
				{Prefix: routes.PublicPrefix, RPC: "CreateTopic", Method: "POST", Path: "/topic/create"},
			},
			want: map[string]string{
				"POST " + routes.Prefix + "/topic/create":       "read over POST; verb in path: create",
				"POST " + routes.PublicPrefix + "/topic/create": "verb in path: create",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nonConformingRoutes(tt.routes); !maps.Equal(got, tt.want) {
				t.Errorf("non-conforming routes = %v, want %v", got, tt.want)
			}
		})
	}
}
