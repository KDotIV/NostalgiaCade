package library

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func validHost(h string) bool {
	if h == "" || strings.ContainsAny(h, "/:*\\ @\t\r\n") || net.ParseIP(h) != nil || !strings.Contains(h, ".") {
		return false
	}
	for _, c := range h {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '-') {
			return false
		}
	}
	return true
}
func allowedURL(raw string, hosts []string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || u.Port() != "" {
		return fail("source_denied", "downloads require HTTPS, a default TLS port, and no URL credentials")
	}
	for _, h := range hosts {
		if strings.EqualFold(u.Hostname(), h) {
			return nil
		}
	}
	return fail("source_denied", "download host is not approved in setup")
}
func secureHTTP(hosts []string) *http.Client {
	transport := &http.Transport{ResponseHeaderTimeout: 15 * time.Second, TLSHandshakeTimeout: 10 * time.Second, DisableCompression: true}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("download DNS lookup failed")
		}
		for _, ip := range ips {
			if !ip.IP.IsGlobalUnicast() || ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() {
				return nil, fail("source_denied", "download host resolves to a non-public address")
			}
		}
		dial := net.Dialer{Timeout: 15 * time.Second}
		for _, ip := range ips {
			conn, e := dial.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if e == nil {
				return conn, nil
			}
		}
		return nil, fmt.Errorf("download connection failed")
	}
	return &http.Client{Transport: transport, Timeout: 30 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fail("source_denied", "too many download redirects")
		}
		return allowedURL(req.URL.String(), hosts)
	}}
}
func supportedExtension(filename string) (string, error) {
	if filepath.Base(filename) != filename || strings.ContainsAny(filename, "\\/:\x00") {
		return "", fail("invalid_file", "filename must be a plain basename")
	}
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".iso", ".gcm", ".rvz", ".wbfs", ".ciso", ".gcz", ".dol", ".elf":
		return ext, nil
	}
	return "", fail("unsupported_format", "Dolphin supports raw disc/homebrew files here; archives and installed WAD titles are not supported yet")
}
func artifactName(e Entry) string { return e.SHA256 + strings.ToLower(filepath.Ext(e.Filename)) }
func contentName(e Entry) string  { return "content/" + artifactName(e) }
func hashStream(ctx context.Context, r io.Reader, w io.Writer, limit int64, spaceCheck func() error) (string, int64, error) {
	h := sha256.New()
	buf := make([]byte, 256*1024)
	var total, checked int64
	for {
		if err := ctx.Err(); err != nil {
			return "", total, err
		}
		n, err := r.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > limit {
				return "", total, fail("size_mismatch", "content exceeds declared size")
			}
			if spaceCheck != nil && (checked == 0 || total-checked >= 16*1024*1024) {
				if e := spaceCheck(); e != nil {
					return "", total, e
				}
				checked = total
			}
			h.Write(buf[:n])
			if w != nil {
				written, e := w.Write(buf[:n])
				if e != nil {
					return "", total, e
				}
				if written != n {
					return "", total, io.ErrShortWrite
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", total, err
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil)), total, nil
}
func verifyFile(ctx context.Context, path, hash string, size int64) error {
	if err := noLinks(path); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != size {
		return fail("size_mismatch", "file is not a regular file of the expected size")
	}
	got, n, err := hashStream(ctx, f, nil, size, nil)
	if err != nil {
		return err
	}
	if n != size || got != hash {
		return fail("hash_mismatch", "file content does not match its SHA-256")
	}
	return nil
}
func (m *Manager) acquire(ctx context.Context, setup Setup, root *os.Root, e Entry, operation string) error {
	name := contentName(e)
	if info, err := root.Lstat(name); err == nil {
		if !info.Mode().IsRegular() {
			return fail("unsafe_path", "managed content is not a regular file")
		}
		return verifyFile(ctx, filepath.Join(setup.Paths.ROMs, name), e.SHA256, e.Bytes)
	}
	if err := root.MkdirAll("content", 0700); err != nil {
		return err
	}
	partial := "content/.partial-" + operation
	// Only this journaled operation's temporary file may be discarded.
	if err := root.Remove(partial); err != nil && !os.IsNotExist(err) {
		return err
	}
	for attempt := 0; attempt < 3; attempt++ {
		err := m.acquireOnce(ctx, setup, root, e, partial, name)
		if err == nil {
			return nil
		}
		_ = root.Remove(partial)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e.Source.Kind != "download" || attempt == 2 {
			return err
		}
		// Retry transport/server errors only; integrity and policy failures stop.
		if _, policy := err.(*Fault); policy {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * time.Second):
		}
	}
	return nil
}
func (m *Manager) acquireOnce(ctx context.Context, s Setup, root *os.Root, e Entry, partial, name string) error {
	if err := m.checkSpace(s.Paths.ROMs, e.Bytes); err != nil {
		return err
	}
	var input io.ReadCloser
	if e.Source.Kind == "local" {
		f, err := os.Open(e.Source.Path)
		if err != nil {
			return err
		}
		input = f
	} else {
		if err := allowedURL(e.Source.URL, s.DownloadHosts); err != nil {
			return err
		}
		client := m.httpClient
		if client == nil {
			client = secureHTTP(s.DownloadHosts)
			defer client.CloseIdleConnections()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.Source.URL, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("download request failed; inspect host availability and TLS")
		}
		input = resp.Body
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			if resp.StatusCode >= 500 {
				return fmt.Errorf("download server returned %d", resp.StatusCode)
			}
			return fail("download_rejected", "download server returned %d", resp.StatusCode)
		}
		if resp.ContentLength >= 0 && resp.ContentLength != e.Bytes {
			resp.Body.Close()
			return fail("size_mismatch", "download length differs from manifest")
		}
	}
	defer input.Close()
	out, err := root.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	hash, n, err := hashStream(ctx, input, out, e.Bytes, func() error { return m.checkSpace(s.Paths.ROMs, 256*1024) })
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if hash != e.SHA256 || n != e.Bytes {
		return fail("hash_mismatch", "download/import does not match expected size and SHA-256")
	}
	return root.Rename(partial, name)
}
func (m *Manager) checkSpace(path string, needed int64) error {
	free, err := m.freeBytes(path)
	if err != nil {
		return err
	}
	if needed < 0 || free < m.reserveBytes || needed > free-m.reserveBytes {
		return fail("disk_full", "insufficient free space after the cabinet reserve")
	}
	return nil
}
