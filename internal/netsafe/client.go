// Package netsafe provides an HTTP client protected against SSRF: it refuses
// to connect to private, loopback, link-local and other non-public
// addresses. The check runs on the resolved IP at dial time, so it also
// covers redirects and DNS rebinding.
package netsafe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

const UserAgent = "Mozilla/5.0 (compatible; EbiFurai/1.0; RSS reader)"

var ErrBlocked = errors.New("address not allowed")

var blockedPrefixes = mustPrefixes(
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
	"172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16",
	"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
	"::/128", "::1/128", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001:db8::/32",
	"fc00::/7", "fe80::/10", "ff00::/8",
)

func mustPrefixes(list ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(list))
	for _, s := range list {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}

// IsPublicIP reports whether ip is a globally routable unicast address.
func IsPublicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

func control(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	if !IsPublicIP(ip) {
		return fmt.Errorf("%w: %s", ErrBlocked, ip)
	}
	return nil
}

// NewClient returns an SSRF-safe client with the given overall timeout.
func NewClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   control,
	}
	transport := &http.Transport{
		Proxy:                 nil, // never route through an env proxy: it would bypass the IP check
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          50,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("redirect to a scheme not allowed")
			}
			// net/http keeps Authorization on a redirect to the same host even
			// from https to http, where Basic credentials travel in clear.
			if req.URL.Scheme == "http" && req.Header.Get("Authorization") != "" {
				req.Header.Del("Authorization")
			}
			return nil
		},
	}
}

// ValidateURL checks that raw is an absolute http(s) URL with a host.
func ValidateURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, errors.New("invalid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("only http and https URLs are allowed")
	}
	if u.Hostname() == "" {
		return nil, errors.New("URL without host")
	}
	if u.User != nil {
		return nil, errors.New("URL with credentials not allowed")
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !IsPublicIP(ip) {
		return nil, ErrBlocked
	}
	u.Fragment = ""
	return u, nil
}

// Get performs a GET with the default user agent. Extra headers are optional.
func Get(ctx context.Context, c *http.Client, rawURL string, headers map[string]string) (*http.Response, error) {
	u, err := ValidateURL(rawURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	return c.Do(req)
}

var ErrTooLarge = errors.New("response too large")

// ReadLimited reads at most max bytes from r; it fails if the body is larger.
func ReadLimited(r io.Reader, max int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, ErrTooLarge
	}
	return b, nil
}
