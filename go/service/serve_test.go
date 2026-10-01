package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/metacensus/api/go/server/routes"
	"github.com/metacensus/api/go/store"
)

type panickingStore struct{ store.Store }

type blockingStore struct {
	store.Store
	started, release chan struct{}
}

func (b blockingStore) Credential(context.Context, string) (string, string, error) {
	close(b.started)
	<-b.release
	return "", "", errors.New("no such email")
}

func TestHandler(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantBody   string
		wantLog    string
	}{
		{"healthz is outside the prefix", "GET", "/healthz", http.StatusOK, `{"ok":true}`, `"event":"request_completed"`},
		{"healthz is GET only", "POST", "/healthz", http.StatusMethodNotAllowed, "", ""},
		{"healthz does not move under the prefix", "GET", routes.Prefix + "/healthz", http.StatusNotFound, "", ""},
		{"the API is routed under the prefix", "GET", routes.Prefix + "/healthcheck", http.StatusOK, "healthy", ""},
		{"the API is not routed outside the prefix", "GET", "/healthcheck", http.StatusNotFound, "", ""},
		{"authenticated routes keep their middleware", "GET", routes.Prefix + "/self", http.StatusUnauthorized, "", ""},
		{"a panic is a 500 that leaks nothing", "POST", routes.Prefix + "/login", http.StatusInternalServerError, `"internal"`, `"event":"request_panicked"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			h := New(Config{Store: panickingStore{}, BcryptCost: 4})
			resp := httptest.NewRecorder()
			h.handler(slog.New(slog.NewJSONHandler(&logs, nil))).
				ServeHTTP(resp, httptest.NewRequest(tt.method, tt.path, strings.NewReader(`{}`)))
			body := resp.Body.String()

			if resp.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (body %q)", resp.Code, tt.wantStatus, body)
			}
			if !strings.Contains(body, tt.wantBody) {
				t.Errorf("body = %q, want it to contain %q", body, tt.wantBody)
			}
			if strings.Contains(body, "nil pointer") {
				t.Errorf("body leaked the panic: %q", body)
			}
			if !strings.Contains(logs.String(), tt.wantLog) {
				t.Errorf("log = %q, want it to contain %q", logs.String(), tt.wantLog)
			}
		})
	}
}

func TestServeDrains(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	st := blockingStore{started: make(chan struct{}), release: make(chan struct{})}
	h := New(Config{Store: st, BcryptCost: 4})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.serve(ctx, l, slog.New(slog.DiscardHandler)) }()

	base := "http://" + l.Addr().String()
	status := make(chan int, 1)
	go func() {
		resp, err := http.Post(base+routes.Prefix+"/login", "application/json", strings.NewReader(`{}`))
		if err != nil {
			status <- 0
			return
		}
		resp.Body.Close()
		status <- resp.StatusCode
	}()
	<-st.started

	cancel()
	select {
	case err := <-done:
		t.Fatalf("Serve returned %v while a request was in flight", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(st.release)

	if got := <-status; got != http.StatusUnauthorized {
		t.Errorf("in-flight login = %d, want 401", got)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after the request finished")
	}
}
