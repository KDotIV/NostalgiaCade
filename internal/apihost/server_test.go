package apihost

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testEnv() map[string]string {
	return map[string]string{"NCADE_ADMIN_TOKEN": strings.Repeat("a", 64), "NCADE_ADMIN_SUBJECT": "admin", "NCADE_POSTGRES_DSN": "postgres://test@127.0.0.1/test?sslmode=disable"}
}
func TestConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name string
		vars map[string]string
		args []string
		bad  bool
	}{
		{name: "local default"},
		{name: "explicit listen", args: []string{"--listen", "127.0.0.1:8199"}},
		{name: "reject plaintext remote", vars: map[string]string{"NCADE_API_LISTEN": ":8080"}, bad: true},
		{name: "reject insecure remote database", vars: map[string]string{"NCADE_API_LISTEN": ":8080", "NCADE_API_BEHIND_TLS_PROXY": "true"}, bad: true},
		{name: "azure ingress", vars: map[string]string{"NCADE_API_LISTEN": ":8080", "NCADE_API_BEHIND_TLS_PROXY": "true", "NCADE_POSTGRES_DSN": "postgres://test@example.org/test?sslmode=verify-full"}},
		{name: "invalid bool", vars: map[string]string{"NCADE_API_BEHIND_TLS_PROXY": "sometimes"}, bad: true},
		{name: "missing token", vars: map[string]string{"NCADE_ADMIN_TOKEN": ""}, bad: true},
		{name: "partial certificate", args: []string{"--tls-cert", "cert.pem"}, bad: true},
		{name: "invalid port", args: []string{"--listen", "127.0.0.1:70000"}, bad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := testEnv()
			for k, v := range tc.vars {
				env[k] = v
			}
			_, err := Load(tc.args, func(k string) string { return env[k] }, io.Discard)
			if (err != nil) != tc.bad {
				t.Fatal(err)
			}
		})
	}
}
func TestHealthProxyAndLogRedaction(t *testing.T) {
	var log bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&log, nil))
	var draining atomic.Bool
	databaseReady := false
	called := 0
	catalog := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++; w.WriteHeader(204) })
	h := newHandler(catalog, func(context.Context) error {
		if !databaseReady {
			return errors.New("secret database details")
		}
		return nil
	}, &draining, true, logger)
	send := func(path, proto string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("X-Forwarded-Proto", proto)
		r.Header.Set("Authorization", "Bearer secret-token")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := send("/health/live", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := send("/health/ready", ""); w.Code != 503 || strings.Contains(w.Body.String(), "secret") {
		t.Fatal(w.Code, w.Body.String())
	}
	databaseReady = true
	if w := send("/health/ready", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	draining.Store(true)
	if w := send("/health/ready", ""); w.Code != 503 {
		t.Fatal(w.Code)
	}
	if w := send("/v1/catalog/genres", ""); w.Code != 400 || called != 0 {
		t.Fatal("untrusted transport accepted")
	}
	if w := send("/v1/catalog/genres?token=secret-query", "https"); w.Code != 204 || w.Header().Get("X-Request-ID") == "" {
		t.Fatal(w.Code)
	}
	if strings.Contains(log.String(), "secret") {
		t.Fatal("sensitive input was logged")
	}
}
func TestShutdownWaitsForActiveRequests(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	entered := make(chan struct{})
	release := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; w.WriteHeader(204) })}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var draining atomic.Bool
	result := make(chan error, 1)
	go func() {
		result <- serve(ctx, server, listener, "", "", &draining, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	}()
	response := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		r, e := client.Get("http://" + listener.Addr().String())
		if e == nil {
			r.Body.Close()
			if r.StatusCode != 204 {
				e = errors.New("wrong response")
			}
		}
		response <- e
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not enter")
	}
	cancel()
	select {
	case err := <-result:
		t.Fatal("shutdown returned before request finished", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown timed out")
	}
	if err := <-response; err != nil {
		t.Fatal(err)
	}
	if !draining.Load() {
		t.Fatal("readiness was not disabled")
	}
}
