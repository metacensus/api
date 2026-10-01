package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/metacensus/api/go/server"
	"github.com/metacensus/api/go/server/routes"
)

const shutdownGrace = 10 * time.Second

// Serve is a backend's whole HTTP process on port: the API under
// routes.Prefix, GET /healthz outside it, request logging, panic recovery and
// timeouts. Cancelling ctx drains in-flight requests for up to 10s.
func (h *Handlers) Serve(ctx context.Context, port int, logger *slog.Logger) error {
	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	logger.Info("listening", slog.String("event", "listening"),
		slog.Int("port", port), slog.String("prefix", routes.Prefix))
	return h.serve(ctx, listener, logger)
}

func (h *Handlers) serve(ctx context.Context, listener net.Listener, logger *slog.Logger) error {
	srv := &http.Server{
		Handler:           h.handler(logger),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	served := make(chan error, 1)
	go func() {
		err := srv.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		served <- err
	}()

	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down", slog.String("event", "shutting_down"),
		slog.Duration("grace", shutdownGrace))
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		_ = srv.Close()
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	return <-served
}

func (h *Handlers) handler(logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	h.Register(server.StdMux{ServeMux: mux}, &server.Runtime{Prefix: routes.Prefix})
	return logRequests(logger, recoverPanics(logger, mux))
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.wrote {
		r.status, r.wrote = status, true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.wrote = true
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func logRequests(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			logger.Info("request completed",
				slog.String("event", "request_completed"),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Int("bytes", rec.bytes),
				slog.Duration("elapsed", time.Since(started)),
			)
		}()
		next.ServeHTTP(rec, r)
	})
}

func recoverPanics(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			if recovered == http.ErrAbortHandler {
				panic(recovered)
			}
			logger.Error("request panicked",
				slog.String("event", "request_panicked"),
				slog.Any("panic", recovered),
				slog.String("stack", string(debug.Stack())),
			)
			server.WriteError(w, nil)
		}()
		next.ServeHTTP(w, r)
	})
}
