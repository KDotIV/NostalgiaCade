// Package apihost composes the HTTP service for local processes and containers.
package apihost

import (
	"flag"
	"fmt"
	"io"
	"net"
	"strconv"

	"github.com/KdotIV/NostalgiaCade/internal/catalogapi"
	"github.com/jackc/pgx/v5"
)

type Config struct {
	Listen, TLSCert, TLSKey string
	BehindTLSProxy          bool
	PostgresDSN             string
	Admin                   catalogapi.Config
}

// Load supports the same environment contract on Windows, Linux and Azure.
// Secrets have no command-line flags so they never enter a process command line.
func Load(args []string, getenv func(string) string, output io.Writer) (Config, error) {
	c := Config{Listen: getenv("NCADE_API_LISTEN"), TLSCert: getenv("NCADE_API_TLS_CERT"), TLSKey: getenv("NCADE_API_TLS_KEY"), PostgresDSN: getenv("NCADE_POSTGRES_DSN"), Admin: catalogapi.Config{AdminToken: getenv("NCADE_ADMIN_TOKEN"), AdminSubject: getenv("NCADE_ADMIN_SUBJECT")}}
	if c.Listen == "" {
		c.Listen = "127.0.0.1:8090"
	}
	if value := getenv("NCADE_API_BEHIND_TLS_PROXY"); value != "" {
		var err error
		c.BehindTLSProxy, err = strconv.ParseBool(value)
		if err != nil {
			return c, fmt.Errorf("NCADE_API_BEHIND_TLS_PROXY must be true or false")
		}
	}
	flags := flag.NewFlagSet("ncade-api", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&c.Listen, "listen", c.Listen, "HTTP listen address")
	flags.StringVar(&c.TLSCert, "tls-cert", c.TLSCert, "TLS certificate PEM")
	flags.StringVar(&c.TLSKey, "tls-key", c.TLSKey, "TLS private key PEM")
	flags.BoolVar(&c.BehindTLSProxy, "behind-tls-proxy", c.BehindTLSProxy, "Trust HTTPS termination at a controlled ingress; keep the container port private")
	if err := flags.Parse(args); err != nil {
		return c, err
	}
	if flags.NArg() != 0 {
		return c, fmt.Errorf("unexpected positional arguments")
	}
	return c, c.Validate()
}
func (c Config) Validate() error {
	if err := c.Admin.Validate(); err != nil {
		return err
	}
	host, port, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return fmt.Errorf("invalid listen address")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("invalid listen port")
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return fmt.Errorf("both TLS certificate and key are required")
	}
	if c.BehindTLSProxy && c.TLSCert != "" {
		return fmt.Errorf("choose direct TLS or a TLS-terminating proxy")
	}
	ip := net.ParseIP(host)
	loopback := ip != nil && ip.IsLoopback()
	if !loopback && c.TLSCert == "" && !c.BehindTLSProxy {
		return fmt.Errorf("non-loopback listeners require TLS or an explicitly trusted TLS ingress")
	}
	if c.PostgresDSN == "" {
		return fmt.Errorf("NCADE_POSTGRES_DSN is required")
	}
	dbConfig, err := pgx.ParseConfig(c.PostgresDSN)
	if err != nil {
		return fmt.Errorf("invalid PostgreSQL configuration")
	}
	// A remotely hosted service must verify the database certificate and cannot
	// fall back to an unencrypted PostgreSQL connection.
	if !loopback || c.BehindTLSProxy {
		if dbConfig.TLSConfig == nil || dbConfig.TLSConfig.InsecureSkipVerify || len(dbConfig.Fallbacks) != 0 {
			return fmt.Errorf("remote hosting requires PostgreSQL sslmode=verify-full")
		}
	}
	return nil
}
