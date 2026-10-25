package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lewta/sendit/internal/policy"
)

const (
	maxCrawlDiscoveredURLs = 8192
	maxCrawlQueueEntries   = 4096
	maxCrawlSitemapEntries = 2048
	maxCrawlSitemapDepth   = 4
	maxCrawlRobotsEntries  = 1024
	maxCrawlRobotsBytes    = 256 << 10
	maxCrawlRedirects      = 10
	maxCrawlTotalBytes     = 64 << 20
	maxCrawlURLBytes       = 8192
)

type crawlResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type crawlRuntime struct {
	resolver    crawlResolver
	dialContext func(context.Context, string, string) (net.Conn, error)
}

func defaultCrawlRuntime() crawlRuntime {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return crawlRuntime{resolver: net.DefaultResolver, dialContext: dialer.DialContext}
}

func (r crawlRuntime) normalized() crawlRuntime {
	defaults := defaultCrawlRuntime()
	if r.resolver == nil {
		r.resolver = defaults.resolver
	}
	if r.dialContext == nil {
		r.dialContext = defaults.dialContext
	}
	return r
}

type crawlLimits struct {
	maxPages          int
	maxDiscovered     int
	maxQueued         int
	maxSitemapEntries int
	maxSitemapDepth   int
	maxRobotsEntries  int
	maxRobotsBytes    int64
	maxRedirects      int
	maxTotalBytes     int64
}

type crawlOptions struct {
	ctx     context.Context
	access  policy.Access
	runtime crawlRuntime
	limits  crawlLimits
}

func defaultCrawlOptions(maxPages int) crawlOptions {
	if maxPages < 1 {
		maxPages = 1
	}
	scaled := maxCrawlDiscoveredURLs
	if maxPages <= maxCrawlDiscoveredURLs/32 {
		scaled = maxPages * 32
	}
	if scaled < 64 {
		scaled = 64
	}
	return crawlOptions{
		ctx:     context.Background(),
		access:  policy.Trusted(),
		runtime: defaultCrawlRuntime(),
		limits: crawlLimits{
			maxPages:          maxPages,
			maxDiscovered:     min(scaled, maxCrawlDiscoveredURLs),
			maxQueued:         min(scaled, maxCrawlQueueEntries),
			maxSitemapEntries: maxCrawlSitemapEntries,
			maxSitemapDepth:   maxCrawlSitemapDepth,
			maxRobotsEntries:  maxCrawlRobotsEntries,
			maxRobotsBytes:    maxCrawlRobotsBytes,
			maxRedirects:      maxCrawlRedirects,
			maxTotalBytes:     maxCrawlTotalBytes,
		},
	}
}

type crawlPolicyError struct{ message string }

func (e *crawlPolicyError) Error() string { return e.message }

type crawlLimitError struct{ message string }

func (e *crawlLimitError) Error() string { return e.message }

func crawlFatalError(err error) bool {
	var policyErr *crawlPolicyError
	var limitErr *crawlLimitError
	return errors.As(err, &policyErr) || errors.As(err, &limitErr) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// dialCrawlContext resolves every new connection and rejects the entire DNS
// answer set before dialing when any answer is outside the operator policy.
// Dialing the selected literal address prevents a second resolver lookup.
func dialCrawlContext(ctx context.Context, network, address string, access policy.Access, runtime crawlRuntime) (net.Conn, error) {
	runtime = runtime.normalized()
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("crawler split address: %w", err)
	}

	var addresses []netip.Addr
	if literal, parseErr := netip.ParseAddr(host); parseErr == nil {
		addresses = []netip.Addr{literal}
	} else {
		addresses, err = runtime.resolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("crawler resolve %q: %w", host, err)
		}
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("crawler resolve %q: no addresses", host)
	}
	for _, candidate := range addresses {
		if !access.AllowsAddress(candidate) {
			return nil, &crawlPolicyError{message: fmt.Sprintf("crawler address denied: %s", candidate.Unmap())}
		}
	}

	var dialErr error
	for _, candidate := range addresses {
		literal := net.JoinHostPort(candidate.Unmap().String(), port)
		conn, err := runtime.dialContext(ctx, network, literal)
		if err == nil {
			return conn, nil
		}
		dialErr = errors.Join(dialErr, err)
	}
	return nil, dialErr
}

func newCrawlHTTPClientWithPolicy(base *url.URL, access policy.Access, runtime crawlRuntime, timeout time.Duration) *http.Client {
	return newCrawlHTTPClientWithLimits(base, access, runtime, timeout, maxCrawlRedirects)
}

func newCrawlHTTPClientWithLimits(base *url.URL, access policy.Access, runtime crawlRuntime, timeout time.Duration, redirects int) *http.Client {
	if redirects < 1 {
		redirects = 1
	}
	transport := &http.Transport{
		Proxy:             nil,
		DisableKeepAlives: true,
		ForceAttemptHTTP2: true,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialCrawlContext(ctx, network, address, access, runtime)
		},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: timeout,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > redirects {
				return &crawlLimitError{message: fmt.Sprintf("crawler redirect budget exceeded (maximum %d)", redirects)}
			}
			if !sameCrawlOrigin(req.URL, base) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

func sameCrawlOrigin(candidate, base *url.URL) bool {
	if candidate == nil || base == nil || candidate.User != nil || !strings.EqualFold(candidate.Scheme, base.Scheme) ||
		!strings.EqualFold(candidate.Hostname(), base.Hostname()) {
		return false
	}
	return effectiveCrawlPort(candidate) == effectiveCrawlPort(base)
}

func effectiveCrawlPort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	return "80"
}

func crawlAccess(privateNetworks []string) (policy.Access, error) {
	grants := policy.Grants{}
	if privateNetworks != nil {
		grants.PrivateNetworks = make([]netip.Prefix, 0, len(privateNetworks))
		for _, value := range privateNetworks {
			prefix, err := netip.ParsePrefix(value)
			if err != nil {
				return policy.Access{}, fmt.Errorf("--allow-private-network must be a CIDR: %s", strconv.Quote(value))
			}
			grants.PrivateNetworks = append(grants.PrivateNetworks, prefix)
		}
	}
	return policy.New(policy.ModeRestricted, grants)
}

type crawlBudget struct {
	limits         crawlLimits
	totalBytes     int64
	discovered     int
	queued         int
	sitemapEntries int
	robotsEntries  int
}

func (b *crawlBudget) addDiscovered() error {
	if b.discovered >= b.limits.maxDiscovered {
		return &crawlLimitError{message: fmt.Sprintf("crawler discovered URL budget exceeded (maximum %d)", b.limits.maxDiscovered)}
	}
	b.discovered++
	return nil
}

func (b *crawlBudget) addQueued() error {
	if b.queued >= b.limits.maxQueued {
		return &crawlLimitError{message: fmt.Sprintf("crawler queue budget exceeded (maximum %d)", b.limits.maxQueued)}
	}
	b.queued++
	return nil
}

func (b *crawlBudget) addSitemapEntry() error {
	if b.sitemapEntries >= b.limits.maxSitemapEntries {
		return &crawlLimitError{message: fmt.Sprintf("crawler sitemap entry budget exceeded (maximum %d)", b.limits.maxSitemapEntries)}
	}
	b.sitemapEntries++
	return nil
}

func (b *crawlBudget) addRobotsEntry() error {
	if b.robotsEntries >= b.limits.maxRobotsEntries {
		return &crawlLimitError{message: fmt.Sprintf("crawler robots metadata budget exceeded (maximum %d)", b.limits.maxRobotsEntries)}
	}
	b.robotsEntries++
	return nil
}

func (b *crawlBudget) remainingBytes() int64 {
	return b.limits.maxTotalBytes - b.totalBytes
}
