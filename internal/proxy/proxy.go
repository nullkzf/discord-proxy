package proxy

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Config configures a Server.
type Config struct {
	Listen   string    // e.g. "127.0.0.1:0"; must be a loopback address
	Fragment bool      // split the TLS ClientHello (turn off only to test)
	Resolver *Resolver // secure DNS; nil = NewResolver(DefaultEndpoints)
	Logf     func(format string, args ...any)

	// AllowLoopback lets CONNECT/forward reach 127.0.0.1. Tests only; real use must leave it false.
	AllowLoopback bool
}

// Server is a loopback-only HTTP CONNECT proxy.
type Server struct {
	cfg    Config
	ln     net.Listener
	wg     sync.WaitGroup
	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	closed bool
}

// New creates a server. Call Start to listen.
func New(cfg Config) *Server {
	if cfg.Resolver == nil {
		cfg.Resolver = NewResolver(nil)
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:0"
	}
	return &Server{cfg: cfg, conns: map[net.Conn]struct{}{}}
}

// Start listens and serves in the background. It returns the "127.0.0.1:port" address.
func (s *Server) Start() (string, error) {
	host, _, err := net.SplitHostPort(s.cfg.Listen)
	if err != nil {
		return "", err
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return "", errors.New("proxy: refusing to listen on a non-loopback address")
	}
	ln, err := net.Listen("tcp", s.cfg.Listen)
	if err != nil {
		return "", err
	}
	s.ln = ln
	s.wg.Add(1)
	go s.serve()
	return ln.Addr().String(), nil
}

// Close stops listening and drops every open connection.
func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	if s.ln != nil {
		s.ln.Close()
	}
	for c := range s.conns {
		c.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *Server) track(c net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.conns[c] = struct{}{}
	return true
}

func (s *Server) untrack(c net.Conn) {
	s.mu.Lock()
	delete(s.conns, c)
	s.mu.Unlock()
}

func (s *Server) serve() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		// Defence in depth: the listener is bound to loopback, but also refuse anything not from loopback.
		if ta, ok := c.RemoteAddr().(*net.TCPAddr); !ok || !ta.IP.IsLoopback() || !s.track(c) {
			c.Close()
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer s.untrack(c)
			defer c.Close()
			s.handle(c)
		}()
	}
}

func (s *Server) handle(c net.Conn) {
	br := bufio.NewReaderSize(c, 32*1024)
	_ = c.SetReadDeadline(time.Now().Add(15 * time.Second))
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}
	_ = c.SetReadDeadline(time.Time{})
	if req.Method == http.MethodConnect {
		s.connect(c, br, req)
	} else {
		s.forward(c, req)
	}
}

// ---- host policy -------------------------------------------------------------------------------

// BypassApplies reports whether the DoH + split-handshake treatment applies to host. It does for every public
// name; local-network and loopback names/addresses are left alone.
func BypassApplies(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if h == "localhost" || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".lan") || strings.HasSuffix(h, ".home.arpa") {
		return false
	}
	if ip := net.ParseIP(h); ip != nil {
		return !(ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified())
	}
	return true
}

// refused destinations: loopback (also stops the proxy being pointed at itself).
func (s *Server) refused(host string) bool {
	if s.cfg.AllowLoopback {
		return false
	}
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

func (s *Server) targets(ctx context.Context, host string) []string {
	if net.ParseIP(host) != nil || !BypassApplies(host) {
		return []string{host}
	}
	ips := s.cfg.Resolver.Lookup(ctx, host)
	if len(ips) == 0 {
		return []string{host} // DoH had nothing (e.g. IPv6-only site): let the system resolver try
	}
	if len(ips) > 4 {
		ips = ips[:4]
	}
	return ips
}

// dialFrom tries targets[start:] in order and returns the connection and the index it used.
func dialFrom(targets []string, start int, port string) (net.Conn, int, error) {
	var last error
	for i := start; i < len(targets); i++ {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(targets[i], port), 6*time.Second)
		if err == nil {
			return c, i, nil
		}
		last = err
	}
	if last == nil {
		last = errors.New("no address to try")
	}
	return nil, 0, last
}

// ---- CONNECT -----------------------------------------------------------------------------------

func (s *Server) connect(c net.Conn, br *bufio.Reader, req *http.Request) {
	host, port, err := net.SplitHostPort(req.Host)
	if err != nil {
		host, port = req.Host, "443"
	}
	if s.refused(host) {
		writeStatus(c, 403, "Forbidden")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	targets := s.targets(ctx, host)
	cancel()
	up, idx, err := dialFrom(targets, 0, port)
	if err != nil {
		s.cfg.Logf("connect %s:%s failed: %v", host, port, err)
		writeStatus(c, 502, "Bad Gateway")
		return
	}
	defer func() { up.Close() }()
	if _, err := io.WriteString(c, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}

	// Look at the client's first bytes: a TLS handshake gets split, anything else is just relayed.
	_ = c.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
	b, err := br.Peek(1)
	_ = c.SetReadDeadline(time.Time{})
	if err != nil || b[0] != 0x16 || !s.cfg.Fragment || !BypassApplies(host) {
		relay(c, br, up)
		return
	}
	hdr, err := br.Peek(5)
	if err != nil {
		relay(c, br, up)
		return
	}
	recLen := int(hdr[3])<<8 | int(hdr[4])
	if recLen == 0 || 5+recLen > br.Size() {
		relay(c, br, up)
		return
	}
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	rec := make([]byte, 5+recLen)
	_, err = io.ReadFull(br, rec)
	_ = c.SetReadDeadline(time.Time{})
	if err != nil {
		return
	}
	pieces := Fragment(rec)
	if pieces == nil {
		if _, err := up.Write(rec); err != nil {
			return
		}
		relay(c, br, up)
		return
	}
	s.cfg.Logf("split ClientHello for %s into %d records", host, len(pieces))
	if writePieces(up, pieces) != nil {
		return
	}
	// Wait for the server's first bytes. If it hangs up instead, retry once without splitting.
	_ = up.SetReadDeadline(time.Now().Add(10 * time.Second))
	first := make([]byte, 16*1024)
	n, rerr := up.Read(first)
	_ = up.SetReadDeadline(time.Time{})
	if n == 0 {
		s.cfg.Logf("no answer to the split hello for %s (%v); retrying plain", host, rerr)
		up.Close()
		up2, _, err := dialFrom(targets, idx, port)
		if err != nil {
			return
		}
		up = up2
		if _, err := up.Write(rec); err != nil {
			return
		}
		relay(c, br, up)
		return
	}
	if _, err := c.Write(first[:n]); err != nil {
		return
	}
	relay(c, br, up)
}

// writePieces sends each piece as its own write (TCP_NODELAY is Go's default) so they leave as separate packets.
func writePieces(w net.Conn, pieces [][]byte) error {
	for _, p := range pieces {
		if _, err := w.Write(p); err != nil {
			return err
		}
	}
	return nil
}

func writeStatus(c net.Conn, code int, text string) {
	_, _ = io.WriteString(c, "HTTP/1.1 "+itoa(code)+" "+text+"\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// relay copies both directions, half-closing as each side finishes, and bounds the lingering half.
func relay(client net.Conn, clientReader io.Reader, up net.Conn) {
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(up, clientReader); closeWrite(up); done <- struct{}{} }()
	go func() { _, _ = io.Copy(client, up); closeWrite(client); done <- struct{}{} }()
	<-done
	d := time.Now().Add(60 * time.Second)
	_ = client.SetDeadline(d)
	_ = up.SetDeadline(d)
	<-done
}

func closeWrite(c net.Conn) {
	if tc, ok := c.(*net.TCPConn); ok {
		_ = tc.CloseWrite()
	}
}

// ---- plain http:// -----------------------------------------------------------------------------

func (s *Server) forward(c net.Conn, req *http.Request) {
	if req.URL == nil || req.URL.Host == "" {
		writeStatus(c, 400, "Bad Request")
		return
	}
	host, port, err := net.SplitHostPort(req.URL.Host)
	if err != nil {
		host, port = req.URL.Host, "80"
	}
	if s.refused(host) {
		writeStatus(c, 403, "Forbidden")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	targets := s.targets(ctx, host)
	cancel()
	up, _, err := dialFrom(targets, 0, port)
	if err != nil {
		writeStatus(c, 502, "Bad Gateway")
		return
	}
	defer up.Close()
	req.Header.Del("Proxy-Connection")
	req.Header.Del("Proxy-Authorization")
	req.RequestURI = ""
	req.Close = true
	if err := req.Write(up); err != nil {
		return
	}
	_, _ = io.Copy(c, up)
}
