// Package server wires grpc-gateway to the MetaCensus API contract.
//
// Nothing here is generated. protoc-gen-go-grpc emits one interface per
// resource — TopicRoutesServer and its siblings, in go/metacensus/v1 — and
// protoc-gen-grpc-gateway emits the HTTP transcoding for them from the same
// google.api.http annotations the route manifest comes from. This package is
// only the configuration those two need to serve the contract rather than
// something adjacent to it.
//
// No gRPC server runs and nothing is dialled. RegisterXHandlerServer is
// grpc-gateway's in-process mode: the generated handler calls the
// implementation directly. That mode has one consequence worth knowing before
// depending on it — gRPC interceptors do not run, so the interceptor chain is
// not available as a place to put cross-cutting concerns. See auth.go.
package server

import (
	"context"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	contract "github.com/metacensus/api/go"
	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/routes"
)

// Registrar registers one service's routes on a gateway mux. The generated
// RegisterXHandlerServer functions have this shape, so a caller lists them
// rather than passing implementations one at a time.
type Registrar func(context.Context, *runtime.ServeMux) error

// Handlers is every service the contract declares. A nil field is a service
// this process does not serve; its routes are simply not registered, and the
// gateway answers them 404.
//
// This is where the compile-time guarantee lives. Each field is a generated
// interface, so an implementation missing a route, or carrying the wrong
// request or response type, does not build.
type Handlers struct {
	Auth       v1.AuthRoutesServer
	Health     v1.HealthRoutesServer
	Extraction v1.ExtractionRoutesServer
	Prop       v1.PropRoutesServer
	Protocol   v1.ProtocolRoutesServer
	Topic      v1.TopicRoutesServer
	User       v1.UserRoutesServer
}

// registrars pairs each supplied handler with its generated registration.
func (h Handlers) registrars() []Registrar {
	var out []Registrar
	if h.Auth != nil {
		out = append(out, func(ctx context.Context, m *runtime.ServeMux) error {
			return v1.RegisterAuthRoutesHandlerServer(ctx, m, h.Auth)
		})
	}
	if h.Health != nil {
		out = append(out, func(ctx context.Context, m *runtime.ServeMux) error {
			return v1.RegisterHealthRoutesHandlerServer(ctx, m, h.Health)
		})
	}
	if h.Extraction != nil {
		out = append(out, func(ctx context.Context, m *runtime.ServeMux) error {
			return v1.RegisterExtractionRoutesHandlerServer(ctx, m, h.Extraction)
		})
	}
	if h.Prop != nil {
		out = append(out, func(ctx context.Context, m *runtime.ServeMux) error {
			return v1.RegisterPropRoutesHandlerServer(ctx, m, h.Prop)
		})
	}
	if h.Protocol != nil {
		out = append(out, func(ctx context.Context, m *runtime.ServeMux) error {
			return v1.RegisterProtocolRoutesHandlerServer(ctx, m, h.Protocol)
		})
	}
	if h.Topic != nil {
		out = append(out, func(ctx context.Context, m *runtime.ServeMux) error {
			return v1.RegisterTopicRoutesHandlerServer(ctx, m, h.Topic)
		})
	}
	if h.User != nil {
		out = append(out, func(ctx context.Context, m *runtime.ServeMux) error {
			return v1.RegisterUserRoutesHandlerServer(ctx, m, h.User)
		})
	}
	return out
}

// Options configures the gateway.
type Options struct {
	// Log receives every error a handler returns, including the ones the
	// client never sees. May be nil.
	Log func(*http.Request, error)

	// DiscardUnknownFields relaxes body decoding to ignore fields the contract
	// does not declare, rather than rejecting the request.
	//
	// The contract's decoder rejects them, which is right for policing
	// conformance and wrong while a sender that is ahead of the contract
	// migrates onto it. Set it deliberately, per side.
	DiscardUnknownFields bool

	// Auth authenticates every route not named in Public. Nil serves
	// everything unauthenticated.
	Auth Authenticator

	// Public names the routes an anonymous caller may reach, as
	// "METHOD /path" against the manifest — "POST /login". Ignored when Auth
	// is nil. New fails if a name is not a declared route, or carries path
	// parameters; see guard.
	Public map[string]bool
}

// defaultMarshaler is the contract's encoder, for the paths that write a
// response without one to hand — the guard, which runs before the mux has
// chosen a marshaler for the request.
func defaultMarshaler() runtime.Marshaler {
	return &runtime.JSONPb{MarshalOptions: contract.MarshalOptions}
}

// New returns the http.Handler serving every route in h, rooted at
// routes.Prefix.
//
// The prefix is stripped before the gateway sees a request because
// google.api.http annotations carry a path each and no prefix; the manifest
// holds the prefix, so this is the one place the two are joined.
func New(ctx context.Context, h Handlers, opts Options) (http.Handler, error) {
	marshaler := &runtime.JSONPb{
		MarshalOptions:   contract.MarshalOptions,
		UnmarshalOptions: contract.UnmarshalOptions,
	}
	marshaler.UnmarshalOptions.DiscardUnknown = opts.DiscardUnknownFields

	muxOpts := []runtime.ServeMuxOption{
		// Without this the gateway uses its own protojson settings, which are
		// not the contract's: the wire rules in go/wire.go would describe the
		// encoder nothing was using.
		runtime.WithMarshalerOption(runtime.MIMEWildcard, marshaler),
		runtime.WithErrorHandler(errorHandler(opts.Log)),
	}

	if opts.Auth != nil {
		mw, err := guard(opts.Auth, opts.Public, opts.Log)
		if err != nil {
			return nil, err
		}
		muxOpts = append(muxOpts, runtime.WithMiddlewares(mw))
	}

	mux := runtime.NewServeMux(muxOpts...)

	for _, register := range h.registrars() {
		if err := register(ctx, mux); err != nil {
			return nil, err
		}
	}

	return http.StripPrefix(routes.Prefix, mux), nil
}
