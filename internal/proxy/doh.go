package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// DefaultEndpoints are DNS-over-HTTPS servers addressed by IP, so there is no host name for a filter to
// match. Cloudflare first, Google as a last resort. (The ISP's own DNS returns a block page and sees every name.)
var DefaultEndpoints = []string{
	"https://1.1.1.1/dns-query",
	"https://1.0.0.1/dns-query",
	"https://8.8.8.8/resolve",
}

type cacheEntry struct {
	ips     []string
	expires time.Time
}

// Resolver looks up IPv4 addresses over DNS-over-HTTPS (JSON API) with a small cache.
type Resolver struct {
	Endpoints []string
	Client    *http.Client
	// AllowBogons keeps private/loopback answers. Tests only.
	AllowBogons bool

	mu       sync.Mutex
	cache    map[string]cacheEntry
	inflight map[string]*lookupCall
}

// lookupCall lets concurrent lookups of the same name share one network query.
type lookupCall struct {
	done chan struct{}
	ips  []string
}

// NewResolver returns a resolver that never uses a proxy for its own lookups.
func NewResolver(endpoints []string) *Resolver {
	if len(endpoints) == 0 {
		endpoints = DefaultEndpoints
	}
	return &Resolver{
		Endpoints: endpoints,
		Client: &http.Client{
			Timeout:   5 * time.Second,
			Transport: &http.Transport{Proxy: nil, MaxIdleConnsPerHost: 4, IdleConnTimeout: 60 * time.Second},
		},
		cache:    map[string]cacheEntry{},
		inflight: map[string]*lookupCall{},
	}
}

// Lookup returns IPv4 addresses for host, or nil if no endpoint answered (the caller then falls back to
// the system resolver). Answers are cached for 5 minutes, "no answer" for 30 seconds.
func (r *Resolver) Lookup(ctx context.Context, host string) []string {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if ip := net.ParseIP(host); ip != nil {
		return []string{host}
	}
	r.mu.Lock()
	if e, ok := r.cache[host]; ok && time.Now().Before(e.expires) {
		r.mu.Unlock()
		return e.ips
	}
	if c, ok := r.inflight[host]; ok { // someone is already asking: share their answer
		r.mu.Unlock()
		select {
		case <-c.done:
			return c.ips
		case <-ctx.Done():
			return nil
		}
	}
	call := &lookupCall{done: make(chan struct{})}
	r.inflight[host] = call
	r.mu.Unlock()

	var ips []string
	for _, ep := range r.Endpoints {
		if ips = r.query(ctx, ep, host); len(ips) > 0 {
			break
		}
		if ctx.Err() != nil {
			break
		}
	}
	r.mu.Lock()
	if ctx.Err() == nil { // a cancelled lookup says nothing about whether the name exists
		ttl := 5 * time.Minute
		if len(ips) == 0 {
			ttl = 30 * time.Second
		}
		r.cache[host] = cacheEntry{ips: ips, expires: time.Now().Add(ttl)}
	}
	delete(r.inflight, host)
	call.ips = ips
	r.mu.Unlock()
	close(call.done)
	return ips
}

func (r *Resolver) query(ctx context.Context, endpoint, host string) []string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil
	}
	q := u.Query()
	q.Set("name", host)
	q.Set("type", "A")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/dns-json")
	resp, err := r.Client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var body struct {
		Answer []struct {
			Type int    `json:"type"`
			Data string `json:"data"`
		} `json:"Answer"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body) != nil {
		return nil
	}
	var ips []string
	for _, a := range body.Answer {
		if a.Type != 1 {
			continue
		}
		if ip := net.ParseIP(a.Data); ip != nil && ip.To4() != nil && (r.AllowBogons || !isBogon(ip)) {
			ips = append(ips, a.Data)
		}
	}
	return ips
}

// isBogon rejects answers a blocking resolver typically returns instead of the real address
// (private/loopback/unspecified ranges and the Turkish ISP block-page address).
func isBogon(ip net.IP) bool {
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
		return true
	}
	return ip.String() == "195.175.254.2"
}
