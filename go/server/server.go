// Package server wires the OpenAPI-generated server to the MetaCensus API
// contract.
//
// The generation is a three-stage pipeline, all third-party:
// protoc-gen-openapiv2 emits a Swagger 2.0 document from the same
// google.api.http annotations the route manifest comes from, swagger2openapi
// converts it to OpenAPI 3, and oapi-codegen turns that into
// StrictServerInterface, chi routing and request binding in server.gen.go.
//
// This file is the configuration those need. It generates nothing.
//
// # What this costs, stated here because it is not visible at a call site
//
// oapi-codegen derives its own Go types from the document rather than reusing
// the protobuf ones: V1Topic, not metacensusv1.Topic. There are 117 of them,
// one per contract message, and they are a second representation of the same
// definition. Two consequences:
//
//   - Every field is a pointer with omitempty, so an empty string is omitted.
//     The contract marshals with EmitDefaultValues, and the TypeScript
//     generated from the same .proto declares `description: string` as always
//     present. The two generated sides disagree about presence.
//   - server.gen.go encodes with json.NewEncoder, hard-coded, in generated
//     code. protojson cannot be substituted; there is no seam for it.
//
// protoc-gen-openapiv2 can emit x-go-type extensions naming the protobuf
// types, but in its own shape -- {"import": {...}, "type": ...} -- rather than
// the x-go-type plus x-go-type-import pair oapi-codegen reads. Bridging them
// needs a transform of the document between the two generators.
package server

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/metacensus/api/go/routes"
)

// Authenticator decides whether a request may proceed. Returning an error
// stops it, and the error travels the same path as one from a handler.
type Authenticator interface {
	Authenticate(*http.Request) (*http.Request, error)
}

// AuthenticatorFunc adapts a function to Authenticator.
type AuthenticatorFunc func(*http.Request) (*http.Request, error)

func (f AuthenticatorFunc) Authenticate(r *http.Request) (*http.Request, error) { return f(r) }

// Operation is the generated method name for a route, which is what
// oapi-codegen passes to strict middleware as the operation id: the manifest's
// Service and RPC concatenated. It is how Options.Public names a route.
func Operation(r routes.Route) string { return r.Service + r.RPC }

// Options configures the server.
type Options struct {
	// BaseRouter is the chi router to register on. Nil means a fresh one.
	//
	// chi is not a choice this package makes. oapi-codegen's chi-server
	// generator names it in server.gen.go, so it is a requirement of the
	// published module rather than a decision a caller can make.
	BaseRouter chi.Router

	// Log receives every error a handler returns, including the ones the
	// client never sees. May be nil.
	Log func(*http.Request, error)

	// Auth authenticates every operation not named in Public. Nil serves
	// everything unauthenticated.
	Auth Authenticator

	// Public names the operations an anonymous caller may reach, by
	// Operation() -- "AuthRoutesLogin". Ignored when Auth is nil. New fails on
	// a name the manifest does not declare.
	//
	// Unlike a path-matching guard this works for parameterised routes too:
	// oapi-codegen hands its strict middleware the operation id, so the
	// middleware knows which route it is on.
	Public map[string]bool
}

// New returns the http.Handler serving every route in h, rooted at
// routes.Prefix.
func New(h StrictServerInterface, opts Options) (http.Handler, error) {
	router := opts.BaseRouter
	if router == nil {
		router = chi.NewRouter()
	}

	var middlewares []StrictMiddlewareFunc
	if opts.Auth != nil {
		mw, err := guard(opts.Auth, opts.Public)
		if err != nil {
			return nil, err
		}
		middlewares = append(middlewares, mw)
	}

	onError := errorHandler(opts.Log)

	si := NewStrictHandlerWithOptions(h, middlewares, StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  onError,
		ResponseErrorHandlerFunc: onError,
	})

	return HandlerWithOptions(si, ChiServerOptions{
		// The document carries the prefix as basePath and oapi-codegen strips
		// it, so it has to be put back here. routes.Prefix is the manifest's,
		// which is the same string proto/openapi.yaml configures.
		BaseURL:          routes.Prefix,
		BaseRouter:       router,
		ErrorHandlerFunc: onError,
	}), nil
}

// guard authenticates every operation except those named public.
func guard(a Authenticator, public map[string]bool) (StrictMiddlewareFunc, error) {
	declared := map[string]bool{}
	for _, r := range routes.Routes {
		declared[Operation(r)] = true
	}
	for name := range public {
		if !declared[name] {
			return nil, fmt.Errorf("server: %q is named public but the manifest declares no such operation", name)
		}
	}

	return func(f StrictHandlerFunc, operationID string) StrictHandlerFunc {
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
			if public[operationID] {
				return f(ctx, w, r, request)
			}
			authed, err := a.Authenticate(r)
			if err != nil {
				return nil, err
			}
			return f(authed.Context(), w, authed, request)
		}
	}, nil
}
