package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/KdotIV/NostalgiaCade/internal/catalogapi"
	"github.com/KdotIV/NostalgiaCade/internal/database"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	listen := flag.String("listen", "127.0.0.1:8090", "Listen address; non-loopback requires TLS")
	cert := flag.String("tls-cert", "", "TLS certificate PEM")
	key := flag.String("tls-key", "", "TLS private key PEM")
	flag.Parse()
	host, _, err := net.SplitHostPort(*listen)
	if err != nil {
		return fmt.Errorf("invalid listen address")
	}
	ip := net.ParseIP(host)
	if (*cert == "") != (*key == "") || (*cert == "" && (ip == nil || !ip.IsLoopback())) {
		return fmt.Errorf("non-loopback listeners require --tls-cert and --tls-key")
	}
	config := catalogapi.Config{AdminToken: os.Getenv("NCADE_ADMIN_TOKEN"), AdminSubject: os.Getenv("NCADE_ADMIN_SUBJECT")}
	// Validate authentication before attempting a database connection.
	if _, err = catalogapi.NewHandler(nil, config); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	connect, stop := context.WithTimeout(ctx, 15*time.Second)
	db, err := database.OpenPostgres(connect, os.Getenv("NCADE_POSTGRES_DSN"))
	stop()
	if err != nil {
		return err
	}
	defer db.Close()
	handler, err := catalogapi.NewHandler(db, config)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdown, stop := context.WithTimeout(context.Background(), 10*time.Second)
			defer stop()
			_ = server.Shutdown(shutdown)
		case <-done:
		}
	}()
	defer close(done)
	fmt.Fprintln(os.Stderr, "NCade catalog API listening on", *listen)
	if *cert != "" {
		err = server.ListenAndServeTLS(*cert, *key)
	} else {
		err = server.ListenAndServe()
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
