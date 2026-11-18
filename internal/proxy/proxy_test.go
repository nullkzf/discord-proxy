package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---- helpers -----------------------------------------------------------------------------------

func selfSigned(t *testing.T, host string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: host},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		DNSNames: []string{host}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	pool := x509.NewCertPool()
	c, _ := x509.ParseCertificate(der)
	pool.AddCert(c)
	return cert, pool
}

// fakeDoH answers every A query with 127.0.0.1 (the test servers all live there).
func fakeDoH(t *testing.T, hits *int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			atomic.AddInt32(hits, 1)
		}
		w.Header().Set("Content-Type", "application/dns-json")
		fmt.Fprintf(w, `{"Status":0,"Answer":[{"name":%q,"type":1,"TTL":60,"data":"127.0.0.1"}]}`, r.URL.Query().Get("name"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

type testProxy struct {
	srv  *Server
	addr string
	logs *syncBuf
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func startProxy(t *testing.T, fragment bool, doh *httptest.Server) *testProxy {
	t.Helper()
	res := NewResolver([]string{doh.URL + "/dns-query"})
	res.AllowBogons = true
	logs := &syncBuf{}
	srv := New(Config{Fragment: fragment, Resolver: res, AllowLoopback: true,
		Logf: func(f string, a ...any) { fmt.Fprintf(logs, f+"\n", a...) }})
	addr, err := srv.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return &testProxy{srv: srv, addr: addr, logs: logs}
}

func (p *testProxy) client(pool *x509.CertPool) *http.Client {
	pu, _ := url.Parse("http://" + p.addr)
	return &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{
		Proxy: http.ProxyURL(pu), TLSClientConfig: &tls.Config{RootCAs: pool}, DisableKeepAlives: true}}
}

// tlsServer serves "hello" over TLS for host "fake.test". If picky, it hangs up on any ClientHello that is
// split across TLS records (to exercise the proxy's "retry without splitting" path).
func tlsServer(t *testing.T, picky bool) (port string, pool *x509.CertPool, splitSeen *int32) {
	t.Helper()
	cert, pool := selfSigned(t, "fake.test")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var seen int32
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				br := bufio.NewReader(c)
				if picky {
					h, err := br.Peek(9)
					if err != nil {
						return
					}
					recPayload := int(h[3])<<8 | int(h[4])
					hsLen := int(h[6])<<16 | int(h[7])<<8 | int(h[8])
					if recPayload < hsLen+4 { // handshake message continues in another record
						atomic.AddInt32(&seen, 1)
						return // hang up
					}
				}
				conn := tls.Server(&bufConn{Conn: c, r: br}, cfg)
				if conn.Handshake() != nil {
					return
				}
				io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 5\r\nConnection: close\r\n\r\nhello")
				conn.Close()
			}()
		}
	}()
	_, port, _ = net.SplitHostPort(ln.Addr().String())
	return port, pool, &seen
}

type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufConn) Read(p []byte) (int, error) { return b.r.Read(p) }

// ---- tests -------------------------------------------------------------------------------------

func realClientHello(t *testing.T, name string) []byte {
	t.Helper()
	a, b := net.Pipe()
	go func() {
		c := tls.Client(a, &tls.Config{ServerName: name, InsecureSkipVerify: true})
		_ = c.Handshake()
	}()
	buf := make([]byte, 16*1024)
	n, err := b.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	b.Close()
	a.Close()
	return buf[:n]
}

func TestFragmentHidesTheNameAndReassembles(t *testing.T) {
	rec := realClientHello(t, "discord.com")
	if rec[0] != 0x16 {
		t.Fatalf("not a handshake record: %x", rec[0])
	}
	payload := rec[5:]
	off, n, ok := sniRange(payload)
	if !ok || string(payload[off:off+n]) != "discord.com" {
		t.Fatalf("SNI not found correctly: ok=%v %q", ok, payload[off:off+n])
	}
	pieces := Fragment(rec)
	if len(pieces) != 3 {
		t.Fatalf("want 3 records, got %d", len(pieces))
	}
	var rebuilt []byte
	for i, p := range pieces {
		if p[0] != 0x16 || p[1] != rec[1] || p[2] != rec[2] {
			t.Errorf("record %d has a bad header: %x", i, p[:3])
		}
		if int(p[3])<<8|int(p[4]) != len(p)-5 {
			t.Errorf("record %d length field is wrong", i)
		}
		if bytes.Contains(p, []byte("discord.com")) {
			t.Errorf("record %d still contains the whole host name", i)
		}
		rebuilt = append(rebuilt, p[5:]...)
	}
	if !bytes.Equal(rebuilt, payload) {
		t.Error("pieces do not reassemble to the original handshake")
	}
}

func TestFragmentRejectsNonHandshake(t *testing.T) {
	for _, in := range [][]byte{nil, {0x17, 3, 3, 0, 1, 0}, {0x16, 3, 1, 0, 0}} {
		if Fragment(in) != nil {
			t.Errorf("Fragment(%x) should be nil", in)
		}
	}
}

func TestSNIRangeSurvivesGarbage(t *testing.T) {
	rec := realClientHello(t, "example.org")[5:]
	for i := 0; i < len(rec); i++ { // every truncation must be handled without panicking
		sniRange(rec[:i])
	}
	sniRange([]byte{0x01, 0xff, 0xff, 0xff})
}

func TestProxySplitsHelloAndTLSStillWorks(t *testing.T) {
	port, pool, _ := tlsServer(t, false)
	p := startProxy(t, true, fakeDoH(t, nil))
	resp, err := p.client(pool).Get("https://fake.test:" + port + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "hello" {
		t.Fatalf("body %q", body)
	}
	if !strings.Contains(p.logs.String(), "split ClientHello for fake.test into 3 records") {
		t.Errorf("hello was not split; log:\n%s", p.logs.String())
	}
}

func TestProxyWithoutFragmentDoesNotSplit(t *testing.T) {
	port, pool, _ := tlsServer(t, false)
	p := startProxy(t, false, fakeDoH(t, nil))
	resp, err := p.client(pool).Get("https://fake.test:" + port + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if strings.Contains(p.logs.String(), "split ClientHello") {
		t.Error("split although fragmentation is off")
	}
}

func TestProxyRetriesPlainWhenServerRejectsSplit(t *testing.T) {
	port, pool, rejected := tlsServer(t, true)
	p := startProxy(t, true, fakeDoH(t, nil))
	resp, err := p.client(pool).Get("https://fake.test:" + port + "/")
	if err != nil {
		t.Fatalf("request failed even though a plain retry should have worked: %v\nlog:\n%s", err, p.logs.String())
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "hello" {
		t.Fatalf("body %q", body)
	}
	if atomic.LoadInt32(rejected) < 1 {
		t.Error("the picky server never saw a split hello, so the retry path was not exercised")
	}
	if !strings.Contains(p.logs.String(), "retrying plain") {
		t.Errorf("expected a plain retry; log:\n%s", p.logs.String())
	}
}

func TestServerFirstProtocolIsRelayed(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		io.WriteString(c, "220 hello first\r\n") // the server speaks before the client does
		io.Copy(io.Discard, c)
	}()
	p := startProxy(t, true, fakeDoH(t, nil))
	c, err := net.Dial("tcp", p.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	fmt.Fprintf(c, "CONNECT 127.0.0.1:%s HTTP/1.1\r\nHost: 127.0.0.1:%s\r\n\r\n", port, port)
	br := bufio.NewReader(c)
	if line, _ := br.ReadString('\n'); !strings.HasPrefix(line, "HTTP/1.1 200") {
		t.Fatalf("CONNECT not accepted: %q", line)
	}
	br.ReadString('\n') // blank line
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if line, err := br.ReadString('\n'); err != nil || line != "220 hello first\r\n" {
		t.Fatalf("server-first bytes lost: %q %v", line, err)
	}
}

func TestLoopbackDestinationsAreRefusedByDefault(t *testing.T) {
	srv := New(Config{})
	addr, err := srv.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	c, _ := net.Dial("tcp", addr)
	defer c.Close()
	io.WriteString(c, "CONNECT 127.0.0.1:80 HTTP/1.1\r\nHost: 127.0.0.1:80\r\n\r\n")
	line, _ := bufio.NewReader(c).ReadString('\n')
	if !strings.HasPrefix(line, "HTTP/1.1 403") {
		t.Fatalf("want 403, got %q", line)
	}
}

func TestRefusesToListenOffLoopback(t *testing.T) {
	for _, l := range []string{"0.0.0.0:0", "192.168.1.5:0", ":0", "example.com:0"} {
		if _, err := New(Config{Listen: l}).Start(); err == nil {
			t.Errorf("Start(%q) should have failed", l)
		}
	}
}

func TestPlainHTTPIsForwarded(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" || r.Header.Get("Proxy-Connection") != "" {
			http.Error(w, "proxy headers leaked", 500)
			return
		}
		fmt.Fprint(w, "plain ok ", r.URL.Path)
	}))
	defer origin.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(origin.URL, "http://"))
	p := startProxy(t, true, fakeDoH(t, nil))
	pu, _ := url.Parse("http://" + p.addr)
	cl := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(pu)}}
	resp, err := cl.Get("http://fake.test:" + port + "/x")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "plain ok /x" {
		t.Fatalf("got %q", body)
	}
}

func TestBypassApplies(t *testing.T) {
	yes := []string{"discord.com", "cdn.discordapp.com", "8.8.8.8", "gateway.discord.gg"}
	no := []string{"localhost", "printer.local", "nas.lan", "192.168.1.1", "10.0.0.7", "172.16.4.4", "127.0.0.1", "169.254.1.1"}
	for _, h := range yes {
		if !BypassApplies(h) {
			t.Errorf("%s should be bypassed", h)
		}
	}
	for _, h := range no {
		if BypassApplies(h) {
			t.Errorf("%s should NOT be bypassed", h)
		}
	}
}

func TestResolverParsesFiltersCachesAndFailsOver(t *testing.T) {
	var hits int32
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", 500) }))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.URL.Query().Get("type") != "A" || r.Header.Get("Accept") != "application/dns-json" {
			http.Error(w, "bad query", 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"Status": 0, "Answer": []map[string]any{
			{"type": 5, "data": "alias.example."},
			{"type": 1, "data": "195.175.254.2"}, // the ISP block page: must be dropped
			{"type": 1, "data": "10.1.2.3"},      // private: must be dropped
			{"type": 1, "data": "162.159.137.232"},
			{"type": 28, "data": "2606:4700::1"},
		}})
	}))
	defer good.Close()
	r := NewResolver([]string{bad.URL + "/q", good.URL + "/q"})
	ips := r.Lookup(context.Background(), "Discord.com")
	if len(ips) != 1 || ips[0] != "162.159.137.232" {
		t.Fatalf("got %v", ips)
	}
	r.Lookup(context.Background(), "discord.com.") // same name, differently written: must hit the cache
	if h := atomic.LoadInt32(&hits); h != 1 {
		t.Errorf("expected 1 upstream query, got %d", h)
	}
	if got := r.Lookup(context.Background(), "1.2.3.4"); len(got) != 1 || got[0] != "1.2.3.4" {
		t.Errorf("IP literal should pass through, got %v", got)
	}
}

func TestResolverReturnsNilWhenEveryEndpointFails(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", 500) }))
	defer bad.Close()
	r := NewResolver([]string{bad.URL})
	if ips := r.Lookup(context.Background(), "x.example"); ips != nil {
		t.Errorf("want nil, got %v", ips)
	}
}

func TestResolverSharesOneQueryAmongConcurrentLookups(t *testing.T) {
	var hits int32
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		time.Sleep(300 * time.Millisecond)
		fmt.Fprint(w, `{"Status":0,"Answer":[{"type":1,"data":"162.159.137.232"}]}`)
	}))
	defer slow.Close()
	r := NewResolver([]string{slow.URL})
	var wg sync.WaitGroup
	results := make([][]string, 12)
	for i := range results {
		wg.Add(1)
		go func(i int) { defer wg.Done(); results[i] = r.Lookup(context.Background(), "gateway.discord.gg") }(i)
	}
	wg.Wait()
	for i, got := range results {
		if len(got) != 1 || got[0] != "162.159.137.232" {
			t.Errorf("lookup %d got %v", i, got)
		}
	}
	if h := atomic.LoadInt32(&hits); h != 1 {
		t.Errorf("12 concurrent lookups caused %d upstream queries, want 1", h)
	}
}

func TestCancelledLookupIsNotCachedAsMissing(t *testing.T) {
	gate := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, `{"Status":0,"Answer":[{"type":1,"data":"162.159.137.232"}]}`)
	}))
	defer slow.Close()
	r := NewResolver([]string{slow.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if ips := r.Lookup(ctx, "late.example"); ips != nil {
		t.Fatalf("expected nothing, got %v", ips)
	}
	close(gate) // now the server answers; the earlier cancellation must not have poisoned the cache
	if ips := r.Lookup(context.Background(), "late.example"); len(ips) != 1 {
		t.Errorf("a cancelled lookup was cached as 'no answer': %v", ips)
	}
}
