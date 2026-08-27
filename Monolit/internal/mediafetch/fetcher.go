package mediafetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"
)

const defaultMaxBytes int64 = 500 << 20

type Fetcher struct {
	client   *http.Client
	maxBytes int64
}

func New(maxBytes int64) *Fetcher {
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			if len(ips) == 0 {
				return nil, models.ErrRecordingURLForbidden
			}
			for _, ip := range ips {
				if forbiddenIP(ip) {
					return nil, models.ErrRecordingURLForbidden
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		DisableCompression:    true,
	}
	f := &Fetcher{maxBytes: maxBytes}
	f.client = &http.Client{Transport: transport, Timeout: 45 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many redirects")
		}
		return ValidateURL(req.URL.String())
	}}
	return f
}

// NewWebhookClient applies the same DNS-rebinding/SSRF protections as media
// fetch and never follows redirects to a different destination.
func NewWebhookClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	base := New(1)
	return &http.Client{Transport: base.client.Transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirects disabled") }}
}

func ValidateURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return models.ErrRecordingURLForbidden
	}
	if net.ParseIP(u.Hostname()) != nil && forbiddenIP(net.ParseIP(u.Hostname())) {
		return models.ErrRecordingURLForbidden
	}
	return nil
}

func forbiddenIP(ip net.IP) bool {
	return ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast()
}

func (f *Fetcher) Fetch(ctx context.Context, raw string) (io.ReadCloser, int64, string, error) {
	if err := ValidateURL(raw); err != nil {
		return nil, 0, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, 0, "", err
	}
	req.Header.Set("Accept", "audio/*,video/*,application/octet-stream")
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, 0, "", fmt.Errorf("fetch recording: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, 0, "", fmt.Errorf("fetch recording status %d", resp.StatusCode)
	}
	if resp.ContentLength > f.maxBytes {
		_ = resp.Body.Close()
		return nil, 0, "", fmt.Errorf("recording too large")
	}
	return &limitedReadCloser{Reader: io.LimitReader(resp.Body, f.maxBytes+1), closer: resp.Body, max: f.maxBytes}, resp.ContentLength, resp.Header.Get("Content-Type"), nil
}

type limitedReadCloser struct {
	io.Reader
	closer io.Closer
	max    int64
}

func (r *limitedReadCloser) Close() error { return r.closer.Close() }
