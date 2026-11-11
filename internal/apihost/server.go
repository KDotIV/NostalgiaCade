package apihost

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/KdotIV/NostalgiaCade/internal/catalogapi"
	"github.com/KdotIV/NostalgiaCade/internal/postgres"
	"github.com/google/uuid"
)

func Run(ctx context.Context, c Config, logger *slog.Logger) error {
	if err := c.Validate(); err != nil {
		return err
	}
	connect, cancel := context.WithTimeout(ctx, 15*time.Second)
	db, err := postgres.Open(connect, c.PostgresDSN)
	cancel()
	if err != nil {
		return err
	}
	defer db.Close()
	catalog, err := catalogapi.NewHandler(db, c.Admin)
	if err != nil {
		return err
	}
	var draining atomic.Bool
	handler := newHandler(catalog, func(ctx context.Context) error { return postgres.CheckCatalog(ctx, db) }, &draining, c.BehindTLSProxy, logger)
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024, ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError)}
	listener, err := net.Listen("tcp", c.Listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	logger.Info("api started", "address", listener.Addr().String(), "tls_proxy", c.BehindTLSProxy)
	return serve(ctx, server, listener, c.TLSCert, c.TLSKey, &draining, logger)
}

func serve(ctx context.Context, server *http.Server, listener net.Listener, cert, key string, draining *atomic.Bool, logger *slog.Logger) error {
	stopped := make(chan error, 1)
	go func() {
		if cert != "" {
			stopped <- server.ServeTLS(listener, cert, key)
		} else {
			stopped <- server.Serve(listener)
		}
	}()
	select {
	case err := <-stopped:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		draining.Store(true)
		logger.Info("api shutting down")
		shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		// Wait for in-flight handlers before closing the database or returning
		// from main. Serve stops accepting connections before Shutdown finishes.
		err := server.Shutdown(shutdown)
		if err != nil {
			_ = server.Close()
		}
		serveErr := <-stopped
		if err != nil {
			return err
		}
		if !errors.Is(serveErr, http.ErrServerClosed) {
			return serveErr
		}
		logger.Info("api stopped")
		return nil
	}
}

func newHandler(catalog http.Handler, ready func(context.Context) error, draining *atomic.Bool, behindProxy bool, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) { writeStatus(w, 200, "alive") })
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if draining.Load() {
			writeStatus(w, 503, "unavailable")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if ready(ctx) != nil {
			writeStatus(w, 503, "unavailable")
			return
		}
		writeStatus(w, 200, "ready")
	})
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if behindProxy && !strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			writeStatus(w, 400, "https_required")
			return
		}
		catalog.ServeHTTP(w, r)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := uuid.NewString()
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		rw := &responseStatus{ResponseWriter: w}
		defer func() {
			if recover() != nil {
				logger.Error("request panicked", "request_id", requestID)
				if rw.code == 0 {
					writeStatus(rw, 500, "internal_error")
				}
			}
			code := rw.code
			if code == 0 {
				code = 200
			}
			// Do not log raw URLs, query strings, bodies, headers, tokens or SQL.
			logger.Info("http request", "request_id", requestID, "method", r.Method, "route", r.Pattern, "status", code, "duration_ms", time.Since(start).Milliseconds())
		}()
		mux.ServeHTTP(rw, r)
	})
}

type responseStatus struct {
	http.ResponseWriter
	code int
}

func (w *responseStatus) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
		w.ResponseWriter.WriteHeader(code)
	}
}
func (w *responseStatus) Write(p []byte) (int, error) {
	if w.code == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(p)
}
func (w *responseStatus) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func writeStatus(w http.ResponseWriter, code int, status string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
}
