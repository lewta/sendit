package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/lewta/sendit/internal/policy"
)

type sequenceCrawlResolver struct {
	mu      sync.Mutex
	answers [][]netip.Addr
	calls   int
}

func (r *sequenceCrawlResolver) LookupNetIP(_ context.Context, _, _ string) ([]netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.calls >= len(r.answers) {
		return nil, errors.New("unexpected resolver call")
	}
	answer := append([]netip.Addr(nil), r.answers[r.calls]...)
	r.calls++
	return answer, nil
}

func restrictedCrawlAccess(t *testing.T, prefixes ...string) policy.Access {
	t.Helper()
	grants := policy.Grants{}
	for _, raw := range prefixes {
		grants.PrivateNetworks = append(grants.PrivateNetworks, netip.MustParsePrefix(raw))
	}
	access, err := policy.New(policy.ModeRestricted, grants)
	if err != nil {
		t.Fatal(err)
	}
	return access
}

type crawlTestResponse struct {
	status      int
	contentType string
	header      http.Header
	body        string
}

func pipeCrawlRuntime(t *testing.T, respond func(*http.Request) crawlTestResponse) crawlRuntime {
	t.Helper()
	return crawlRuntime{dialContext: func(context.Context, string, string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			req, err := http.ReadRequest(bufio.NewReader(server))
			if err != nil {
				t.Errorf("reading in-memory crawl request: %v", err)
				return
			}
			response := respond(req)
			if response.status == 0 {
				response.status = http.StatusOK
			}
			header := response.header.Clone()
			if header == nil {
				header = make(http.Header)
			}
			if response.contentType != "" {
				header.Set("Content-Type", response.contentType)
			}
			resp := &http.Response{
				StatusCode:    response.status,
				Status:        fmt.Sprintf("%d %s", response.status, http.StatusText(response.status)),
				ProtoMajor:    1,
				ProtoMinor:    1,
				Header:        header,
				Body:          io.NopCloser(strings.NewReader(response.body)),
				ContentLength: int64(len(response.body)),
				Close:         true,
				Request:       req,
			}
			if err := resp.Write(server); err != nil {
				t.Errorf("writing in-memory crawl response: %v", err)
			}
		}()
		return client, nil
	}}
}

func TestCrawlDialRejectsLiteralPrivateAddressBeforeNetwork(t *testing.T) {
	var dials atomic.Int32
	runtime := crawlRuntime{dialContext: func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("unexpected dial")
	}}

	_, err := dialCrawlContext(t.Context(), "tcp", "127.0.0.1:80", restrictedCrawlAccess(t), runtime)
	if err == nil || !strings.Contains(err.Error(), "address denied") {
		t.Fatalf("dial error = %v", err)
	}
	if dials.Load() != 0 {
		t.Fatalf("denied literal caused %d network dial(s)", dials.Load())
	}
}

func TestCrawlDialRejectsMixedAndMappedAnswersBeforeNetwork(t *testing.T) {
	for name, answers := range map[string][]netip.Addr{
		"mixed":  {netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("127.0.0.1")},
		"mapped": {netip.MustParseAddr("::ffff:127.0.0.1")},
	} {
		t.Run(name, func(t *testing.T) {
			var dials atomic.Int32
			runtime := crawlRuntime{
				resolver: &sequenceCrawlResolver{answers: [][]netip.Addr{answers}},
				dialContext: func(context.Context, string, string) (net.Conn, error) {
					dials.Add(1)
					return nil, errors.New("unexpected dial")
				},
			}
			_, err := dialCrawlContext(t.Context(), "tcp", "example.test:80", restrictedCrawlAccess(t), runtime)
			if err == nil || !strings.Contains(err.Error(), "address denied") {
				t.Fatalf("dial error = %v", err)
			}
			if dials.Load() != 0 {
				t.Fatalf("denied DNS answer caused %d network dial(s)", dials.Load())
			}
		})
	}
}

func TestCrawlDialRechecksDNSAndHonorsExplicitPrivateGrant(t *testing.T) {
	sentinel := errors.New("dial attempted")
	resolver := &sequenceCrawlResolver{answers: [][]netip.Addr{
		{netip.MustParseAddr("10.1.2.3")},
		{netip.MustParseAddr("127.0.0.1")},
	}}
	var dials atomic.Int32
	runtime := crawlRuntime{
		resolver: resolver,
		dialContext: func(_ context.Context, _, address string) (net.Conn, error) {
			dials.Add(1)
			if address != "10.1.2.3:80" {
				t.Fatalf("dial address = %q", address)
			}
			return nil, sentinel
		},
	}
	access := restrictedCrawlAccess(t, "10.0.0.0/8")
	if _, err := dialCrawlContext(t.Context(), "tcp", "example.test:80", access, runtime); !errors.Is(err, sentinel) {
		t.Fatalf("first dial error = %v", err)
	}
	if _, err := dialCrawlContext(t.Context(), "tcp", "example.test:80", access, runtime); err == nil || !strings.Contains(err.Error(), "address denied") {
		t.Fatalf("second dial error = %v", err)
	}
	if got := dials.Load(); got != 1 {
		t.Fatalf("network dials = %d, want 1", got)
	}
}

func TestRestrictedCrawlDisablesAmbientProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	base, err := url.Parse("http://example.test")
	if err != nil {
		t.Fatal(err)
	}
	client := newCrawlHTTPClientWithPolicy(base, restrictedCrawlAccess(t), defaultCrawlRuntime(), 0)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("restricted crawler inherited an ambient proxy")
	}
}

func TestRestrictedCrawlPrivateSeedRequiresExplicitGrant(t *testing.T) {
	for _, tc := range []struct {
		name       string
		access     policy.Access
		wantDenied bool
	}{
		{name: "denied-by-default", access: restrictedCrawlAccess(t), wantDenied: true},
		{name: "explicit-cidr", access: restrictedCrawlAccess(t, "127.0.0.0/8")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			opts := defaultCrawlOptions(2)
			opts.access = tc.access
			opts.runtime = pipeCrawlRuntime(t, func(*http.Request) crawlTestResponse {
				requests.Add(1)
				return crawlTestResponse{contentType: "text/html", body: "<html></html>"}
			})
			targets, err := targetsFromCrawlWithOptions("http://127.0.0.1", 1, true, opts)
			if tc.wantDenied {
				if err == nil || !strings.Contains(err.Error(), "address denied") {
					t.Fatalf("crawl error = %v", err)
				}
				if requests.Load() != 0 {
					t.Fatalf("denied crawl sent %d request(s)", requests.Load())
				}
				return
			}
			if err != nil || len(targets) != 1 || requests.Load() != 1 {
				t.Fatalf("allowed crawl: targets=%v requests=%d err=%v", targets, requests.Load(), err)
			}
		})
	}
}

func TestCrawlBudgetsFailClearlyAndRemainBounded(t *testing.T) {
	t.Run("discovery", func(t *testing.T) {
		opts := defaultCrawlOptions(10)
		opts.access = policy.Trusted()
		opts.limits.maxDiscovered = 2
		opts.runtime = pipeCrawlRuntime(t, func(*http.Request) crawlTestResponse {
			return crawlTestResponse{contentType: "text/html", body: `<html><a href="/a">a</a><a href="/b">b</a><a href="/c">c</a></html>`}
		})
		_, err := targetsFromCrawlWithOptions("http://93.184.216.34", 1, true, opts)
		if err == nil || !strings.Contains(err.Error(), "discovered URL budget") {
			t.Fatalf("crawl error = %v", err)
		}
	})

	t.Run("queue", func(t *testing.T) {
		opts := defaultCrawlOptions(10)
		opts.access = policy.Trusted()
		opts.limits.maxQueued = 2
		opts.runtime = pipeCrawlRuntime(t, func(*http.Request) crawlTestResponse {
			return crawlTestResponse{contentType: "text/html", body: `<html><a href="/a">a</a><a href="/b">b</a></html>`}
		})
		_, err := targetsFromCrawlWithOptions("http://93.184.216.34", 1, true, opts)
		if err == nil || !strings.Contains(err.Error(), "queue budget") {
			t.Fatalf("crawl error = %v", err)
		}
	})

	t.Run("robots-bytes", func(t *testing.T) {
		opts := defaultCrawlOptions(10)
		opts.access = policy.Trusted()
		opts.limits.maxRobotsBytes = 8
		opts.runtime = pipeCrawlRuntime(t, func(r *http.Request) crawlTestResponse {
			if r.URL.Path == "/robots.txt" {
				return crawlTestResponse{contentType: "text/plain", body: "User-agent: *\nDisallow: /private\n"}
			}
			return crawlTestResponse{contentType: "text/html", body: "<html></html>"}
		})
		_, err := targetsFromCrawlWithOptions("http://93.184.216.34", 1, false, opts)
		if err == nil || !strings.Contains(err.Error(), "robots byte budget") {
			t.Fatalf("crawl error = %v", err)
		}
	})

	t.Run("total-bytes", func(t *testing.T) {
		opts := defaultCrawlOptions(10)
		opts.access = policy.Trusted()
		opts.limits.maxTotalBytes = 4
		opts.runtime = pipeCrawlRuntime(t, func(*http.Request) crawlTestResponse {
			return crawlTestResponse{contentType: "text/html", body: "<html>payload</html>"}
		})
		_, err := targetsFromCrawlWithOptions("http://93.184.216.34", 1, true, opts)
		if err == nil || !strings.Contains(err.Error(), "total byte budget") {
			t.Fatalf("crawl error = %v", err)
		}
	})

	t.Run("robots-metadata", func(t *testing.T) {
		opts := defaultCrawlOptions(10)
		opts.access = policy.Trusted()
		opts.limits.maxRobotsEntries = 1
		opts.runtime = pipeCrawlRuntime(t, func(r *http.Request) crawlTestResponse {
			if r.URL.Path == "/robots.txt" {
				return crawlTestResponse{contentType: "text/plain", body: "User-agent: *\nDisallow: /one\nDisallow: /two\n"}
			}
			return crawlTestResponse{contentType: "text/html", body: "<html></html>"}
		})
		_, err := targetsFromCrawlWithOptions("http://93.184.216.34", 1, false, opts)
		if err == nil || !strings.Contains(err.Error(), "robots metadata budget") {
			t.Fatalf("crawl error = %v", err)
		}
	})

	t.Run("sitemap-entries", func(t *testing.T) {
		opts := defaultCrawlOptions(10)
		opts.access = policy.Trusted()
		opts.limits.maxSitemapEntries = 1
		opts.runtime = pipeCrawlRuntime(t, func(r *http.Request) crawlTestResponse {
			switch r.URL.Path {
			case "/robots.txt":
				return crawlTestResponse{contentType: "text/plain", body: "User-agent: *\nSitemap: http://93.184.216.34/sitemap.xml\n"}
			case "/sitemap.xml":
				return crawlTestResponse{contentType: "application/xml", body: `<urlset><url><loc>http://93.184.216.34/a</loc></url><url><loc>http://93.184.216.34/b</loc></url></urlset>`}
			default:
				return crawlTestResponse{contentType: "text/html", body: "<html></html>"}
			}
		})
		_, err := targetsFromCrawlWithOptions("http://93.184.216.34", 1, false, opts)
		if err == nil || !strings.Contains(err.Error(), "sitemap entry budget") {
			t.Fatalf("crawl error = %v", err)
		}
	})

	t.Run("sitemap-depth", func(t *testing.T) {
		opts := defaultCrawlOptions(10)
		opts.access = policy.Trusted()
		opts.limits.maxSitemapDepth = 1
		opts.runtime = pipeCrawlRuntime(t, func(r *http.Request) crawlTestResponse {
			switch r.URL.Path {
			case "/robots.txt":
				return crawlTestResponse{contentType: "text/plain", body: "User-agent: *\nSitemap: http://93.184.216.34/sitemap.xml\n"}
			case "/sitemap.xml":
				return crawlTestResponse{contentType: "application/xml", body: `<sitemapindex><sitemap><loc>http://93.184.216.34/sitemap-two.xml</loc></sitemap></sitemapindex>`}
			default:
				return crawlTestResponse{contentType: "text/html", body: "<html></html>"}
			}
		})
		_, err := targetsFromCrawlWithOptions("http://93.184.216.34", 1, false, opts)
		if err == nil || !strings.Contains(err.Error(), "sitemap depth budget") {
			t.Fatalf("crawl error = %v", err)
		}
	})

	t.Run("redirect", func(t *testing.T) {
		opts := defaultCrawlOptions(10)
		opts.access = policy.Trusted()
		opts.limits.maxRedirects = 1
		opts.runtime = pipeCrawlRuntime(t, func(r *http.Request) crawlTestResponse {
			header := make(http.Header)
			header.Set("Location", "/next")
			return crawlTestResponse{status: http.StatusFound, header: header}
		})
		_, err := targetsFromCrawlWithOptions("http://93.184.216.34", 1, true, opts)
		if err == nil || !strings.Contains(err.Error(), "redirect budget") {
			t.Fatalf("crawl error = %v", err)
		}
	})
}

func TestCrawlCancellationIsNotSwallowed(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	opts := defaultCrawlOptions(1)
	opts.ctx = ctx
	opts.access = policy.Trusted()
	_, err := targetsFromCrawlWithOptions("http://127.0.0.1", 1, true, opts)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("crawl error = %v", err)
	}
}

func TestCrawlNormalizesDuplicatesAndSitemapCyclesWithinBudget(t *testing.T) {
	t.Run("duplicate-links", func(t *testing.T) {
		opts := defaultCrawlOptions(4)
		opts.access = policy.Trusted()
		opts.limits.maxDiscovered = 2
		opts.runtime = pipeCrawlRuntime(t, func(r *http.Request) crawlTestResponse {
			if r.URL.Path == "/" {
				return crawlTestResponse{contentType: "text/html", body: `<html><a href="/a?one">a</a><a href="/a?two">a</a><a href="/a#three">a</a></html>`}
			}
			return crawlTestResponse{contentType: "text/html", body: "<html></html>"}
		})
		targets, err := targetsFromCrawlWithOptions("http://93.184.216.34", 1, true, opts)
		if err != nil || len(targets) != 2 {
			t.Fatalf("normalized crawl: targets=%v err=%v", targets, err)
		}
	})

	t.Run("sitemap-cycle", func(t *testing.T) {
		opts := defaultCrawlOptions(4)
		opts.access = policy.Trusted()
		opts.limits.maxSitemapDepth = 2
		opts.runtime = pipeCrawlRuntime(t, func(r *http.Request) crawlTestResponse {
			switch r.URL.Path {
			case "/robots.txt":
				return crawlTestResponse{contentType: "text/plain", body: "User-agent: *\nSitemap: http://93.184.216.34/sitemap-one.xml\n"}
			case "/sitemap-one.xml":
				return crawlTestResponse{contentType: "application/xml", body: `<sitemapindex><sitemap><loc>http://93.184.216.34/sitemap-two.xml</loc></sitemap></sitemapindex>`}
			case "/sitemap-two.xml":
				return crawlTestResponse{contentType: "application/xml", body: `<sitemapindex><sitemap><loc>http://93.184.216.34/sitemap-one.xml</loc></sitemap></sitemapindex>`}
			default:
				return crawlTestResponse{contentType: "text/html", body: "<html></html>"}
			}
		})
		targets, err := targetsFromCrawlWithOptions("http://93.184.216.34", 1, false, opts)
		if err != nil || len(targets) != 1 {
			t.Fatalf("cyclic sitemap crawl: targets=%v err=%v", targets, err)
		}
	})
}

func TestGenerateCrawlRejectsInvalidOrImplicitPrivateGrant(t *testing.T) {
	t.Run("invalid-cidr", func(t *testing.T) {
		cmd := generateCmd()
		cmd.SetArgs([]string{"--url", "http://127.0.0.1", "--allow-private-network", "not-a-cidr", "--ignore-robots"})
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "must be a CIDR") {
			t.Fatalf("generate error = %v", err)
		}
	})

	t.Run("literal-private-is-not-intent", func(t *testing.T) {
		cmd := generateCmd()
		cmd.SetArgs([]string{"--url", "http://127.0.0.1", "--ignore-robots"})
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "address denied") {
			t.Fatalf("generate error = %v", err)
		}
	})
}

func TestGenerateOutputRejectsAliasesWithoutSideEffects(t *testing.T) {
	input := filepath.Join(t.TempDir(), "targets.txt")
	if err := os.WriteFile(input, []byte("https://example.com http\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, link := range map[string]func(string, string) error{"symlink": os.Symlink, "hardlink": os.Link} {
		t.Run(name, func(t *testing.T) {
			victim := filepath.Join(t.TempDir(), "victim")
			if err := os.WriteFile(victim, []byte("preserve"), 0o600); err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(t.TempDir(), "generated.yaml")
			if err := link(victim, alias); err != nil {
				t.Skipf("%s unavailable: %v", name, err)
			}
			cmd := generateCmd()
			cmd.SetArgs([]string{"--targets-file", input, "--output", alias})
			cmd.SetIn(strings.NewReader("y\n"))
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			if err := cmd.Execute(); err == nil {
				t.Fatal("output alias accepted")
			}
			got, err := os.ReadFile(victim)
			if err != nil || string(got) != "preserve" {
				t.Fatalf("denial modified victim: %q, %v", got, err)
			}
		})
	}
}
