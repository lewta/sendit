package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/policy"
	"github.com/spf13/cobra"
	"golang.org/x/net/html"
	"howett.net/plist"
	_ "modernc.org/sqlite" // registers the "sqlite" driver for database/sql
)

const generateUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36"

const (
	maxCrawlHTMLBytes    = 5 << 20
	maxCrawlSitemapBytes = 1 << 20
)

// generateCmd returns the cobra command for 'sendit generate'.
func generateCmd() *cobra.Command {
	var (
		targetsFile   string
		seedURL       string
		crawl         bool
		depth         int
		maxPages      int
		ignoreRobots  bool
		fromHistory   string
		fromBookmarks string
		historyLimit  int
		output        string
		privateCIDRs  []string
	)

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a config.yaml from a targets file, URL crawl, or browser data",
		Long: `Generate a ready-to-use config.yaml from one or more input sources.

Sources (can be combined; duplicates are removed automatically):
  --targets-file   parse an existing targets file (url type [weight] per line)
  --url            seed URL for crawl-based generation (implies --crawl)
  --from-history   harvest visited URLs from local browser history
  --from-bookmarks harvest bookmarked URLs from local browser bookmarks

Output is written to stdout by default; use --output to write to a file.
The generated config uses sensible defaults for pacing, limits, and backoff
that can be tuned before running. Validate with 'sendit validate --config'.

URL crawls allow ordinary public destinations by default, disable ambient
proxies, and check every resolved address before dialing. A literal private
seed is not implicit permission; grant intentional lab/private destinations
with repeatable --allow-private-network CIDR flags. Crawl discovery, redirects,
robots/sitemap metadata, response sizes, and total bytes have hard ceilings.

Examples:
  sendit generate --targets-file config/targets.txt
  sendit generate --url https://example.com --depth 2 --output config/generated.yaml
  sendit generate --url http://10.20.0.4 --allow-private-network 10.20.0.0/16
  sendit generate --from-history chrome --history-limit 50 --output config/generated.yaml
  sendit generate --from-bookmarks firefox --output config/generated.yaml
  sendit generate --url https://example.com --from-history chrome --output config/gen.yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if seedURL != "" {
				crawl = true
			}
			if crawl && seedURL == "" {
				return fmt.Errorf("--crawl requires --url <seed-url>")
			}
			if depth < 0 {
				return fmt.Errorf("--depth must be non-negative")
			}
			if maxPages < 1 {
				return fmt.Errorf("--max-pages must be positive")
			}

			var targets []config.TargetConfig

			if targetsFile != "" {
				ts, err := targetsFromFile(targetsFile)
				if err != nil {
					return fmt.Errorf("--targets-file: %w", err)
				}
				targets = append(targets, ts...)
			}

			if crawl {
				access, err := crawlAccess(privateCIDRs)
				if err != nil {
					return err
				}
				opts := defaultCrawlOptions(maxPages)
				opts.ctx = cmd.Context()
				opts.access = access
				ts, err := targetsFromCrawlWithOptions(seedURL, depth, ignoreRobots, opts)
				if err != nil {
					return fmt.Errorf("--url crawl: %w", err)
				}
				targets = append(targets, ts...)
			}

			if fromHistory != "" {
				ts, err := targetsFromHistory(fromHistory, historyLimit)
				if err != nil {
					return fmt.Errorf("--from-history: %w", err)
				}
				targets = append(targets, ts...)
			}

			if fromBookmarks != "" {
				ts, err := targetsFromBookmarks(fromBookmarks)
				if err != nil {
					return fmt.Errorf("--from-bookmarks: %w", err)
				}
				targets = append(targets, ts...)
			}

			if len(targets) == 0 {
				return fmt.Errorf("no input source specified; use --targets-file, --url, --from-history, or --from-bookmarks")
			}

			targets = deduplicateTargets(targets)

			return emitGeneratedConfig(cmd, targets, output)
		},
	}

	cmd.Flags().StringVar(&targetsFile, "targets-file", "", "Generate from an existing targets file (url type [weight] per line)")
	cmd.Flags().StringVar(&seedURL, "url", "", "Seed URL for crawl-based generation (implies --crawl)")
	cmd.Flags().BoolVar(&crawl, "crawl", false, "Enable in-domain page discovery for HTTP targets (used with --url)")
	cmd.Flags().IntVar(&depth, "depth", 2, "Maximum crawl depth (used with --url)")
	cmd.Flags().IntVar(&maxPages, "max-pages", 50, "Maximum number of pages to discover (used with --url)")
	cmd.Flags().BoolVar(&ignoreRobots, "ignore-robots", false, "Skip robots.txt enforcement during crawl")
	cmd.Flags().StringArrayVar(&privateCIDRs, "allow-private-network", nil, "Allow crawler connections to a nonpublic or special-use CIDR (repeatable)")
	cmd.Flags().StringVar(&fromHistory, "from-history", "", "Harvest visited URLs from local browser history (chrome|firefox|safari)")
	cmd.Flags().StringVar(&fromBookmarks, "from-bookmarks", "", "Harvest bookmarked URLs from local browser bookmarks (chrome|firefox|safari)")
	cmd.Flags().IntVar(&historyLimit, "history-limit", 100, "Cap the number of URLs imported from history (ordered by visit count)")
	cmd.Flags().StringVar(&output, "output", "", "Write config to a file instead of stdout")

	return cmd
}

// --- source: targets file ---

// targetsFromFile reads a plain-text targets file (url type [weight] per line)
// and returns a slice of TargetConfig with sensible driver defaults applied.
func targetsFromFile(path string) ([]config.TargetConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening %q: %w", path, err)
	}
	defer f.Close() //nolint:errcheck

	validTypes := map[string]bool{"http": true, "browser": true, "dns": true, "websocket": true}

	var targets []config.TargetConfig
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("line %d: expected \"<url> <type> [weight]\", got %q", lineNum, line)
		}
		u := fields[0]
		typ := strings.ToLower(fields[1])
		if !validTypes[typ] {
			return nil, fmt.Errorf("line %d: unknown type %q (must be http|browser|dns|websocket)", lineNum, typ)
		}
		weight := 1
		if len(fields) >= 3 {
			w, err := strconv.Atoi(fields[2])
			if err != nil || w <= 0 {
				return nil, fmt.Errorf("line %d: invalid weight %q (must be a positive integer)", lineNum, fields[2])
			}
			weight = w
		}
		targets = append(targets, defaultTarget(u, typ, weight))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading %q: %w", path, err)
	}
	return targets, nil
}

// --- source: HTTP crawl ---

// targetsFromCrawl fetches the seed URL, discovers in-domain links up to
// maxDepth levels deep (BFS), and returns each unique HTML URL as an http
// target. Non-HTML file extensions (.pdf, .css, .js, images, etc.) are
// excluded. Sitemap XML files found via robots.txt or as crawled links are
// parsed for additional URLs rather than included as targets themselves.
// Respects robots.txt unless ignoreRobots is true.
func targetsFromCrawl(seedURL string, maxDepth, maxPages int, ignoreRobots bool) ([]config.TargetConfig, error) {
	opts := defaultCrawlOptions(maxPages)
	return targetsFromCrawlWithOptions(seedURL, maxDepth, ignoreRobots, opts)
}

func targetsFromCrawlWithOptions(seedURL string, maxDepth int, ignoreRobots bool, opts crawlOptions) ([]config.TargetConfig, error) {
	if maxDepth < 0 {
		return nil, fmt.Errorf("crawl depth must be non-negative")
	}
	base, err := url.Parse(seedURL)
	if err != nil {
		return nil, fmt.Errorf("invalid seed URL %q: %w", seedURL, err)
	}
	if (base.Scheme != "http" && base.Scheme != "https") || base.Hostname() == "" || base.User != nil {
		return nil, fmt.Errorf("seed URL must be an http:// or https:// origin without user information")
	}
	normalizeCrawlURL(base)
	if len(base.String()) > maxCrawlURLBytes {
		return nil, &crawlLimitError{message: fmt.Sprintf("crawler URL exceeds %d bytes", maxCrawlURLBytes)}
	}

	if opts.ctx == nil {
		opts.ctx = context.Background()
	}
	if opts.access.Mode() != policy.ModeTrusted && opts.access.Mode() != policy.ModeRestricted {
		return nil, fmt.Errorf("crawler policy is uninitialized")
	}
	opts.runtime = opts.runtime.normalized()
	ctx, cancel := context.WithTimeout(opts.ctx, 60*time.Second)
	defer cancel()

	client := newCrawlHTTPClientWithLimits(base, opts.access, opts.runtime, 10*time.Second, opts.limits.maxRedirects)
	robotsClient := newCrawlHTTPClientWithLimits(base, opts.access, opts.runtime, 5*time.Second, opts.limits.maxRedirects)
	budget := &crawlBudget{limits: opts.limits}

	var disallowed, sitemaps []string
	if !ignoreRobots {
		disallowed, sitemaps, err = fetchRobotsInfoWithBudget(ctx, robotsClient, base, budget)
		if err != nil && crawlFatalError(err) {
			return nil, err
		}
	}

	type queueItem struct {
		u            string
		depth        int
		sitemap      bool // true: parse as sitemap XML, not an HTML crawl target
		sitemapDepth int
	}

	visited := make(map[string]bool)
	queue := make([]queueItem, 0, min(opts.limits.maxQueued, 64))
	enqueue := func(raw string, depth int, sitemap bool, sitemapDepth int) error {
		if len(raw) > maxCrawlURLBytes {
			return &crawlLimitError{message: fmt.Sprintf("crawler URL exceeds %d bytes", maxCrawlURLBytes)}
		}
		u, parseErr := url.Parse(raw)
		if parseErr != nil || u.User != nil || !sameCrawlOrigin(u, base) {
			return nil
		}
		normalizeCrawlURL(u)
		normalized := u.String()
		if visited[normalized] {
			return nil
		}
		if sitemap && sitemapDepth > opts.limits.maxSitemapDepth {
			return &crawlLimitError{message: fmt.Sprintf("crawler sitemap depth budget exceeded (maximum %d)", opts.limits.maxSitemapDepth)}
		}
		if err := budget.addDiscovered(); err != nil {
			return err
		}
		if err := budget.addQueued(); err != nil {
			return err
		}
		visited[normalized] = true
		queue = append(queue, queueItem{u: normalized, depth: depth, sitemap: sitemap, sitemapDepth: sitemapDepth})
		return nil
	}
	if err := enqueue(base.String(), 0, false, 0); err != nil {
		return nil, err
	}

	// Seed sitemaps discovered from robots.txt.
	for _, s := range sitemaps {
		if err := enqueue(s, 0, true, 1); err != nil {
			return nil, err
		}
	}

	var targets []config.TargetConfig
	for len(queue) > 0 && len(targets) < opts.limits.maxPages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		item := queue[0]
		queue = queue[1:]

		if isDisallowed(item.u, disallowed) {
			continue
		}

		if item.sitemap {
			// Parse sitemap XML and feed discovered in-domain URLs into the queue.
			locs, err := fetchSitemapLocsWithBudget(ctx, client, item.u, budget)
			if err != nil {
				if crawlFatalError(err) {
					return nil, err
				}
				continue // non-fatal
			}
			for _, loc := range locs {
				u, err := url.Parse(loc)
				if err != nil || u.User != nil || !strings.EqualFold(u.Host, base.Host) {
					continue
				}
				// Normalise scheme to match the seed URL — sitemaps often list
				// http:// entries even when the site now serves https://.
				u.Scheme = base.Scheme
				normalizeCrawlURL(u)
				s := u.String()
				nextSitemap := isSitemapURL(s)
				nextSitemapDepth := 0
				if nextSitemap {
					nextSitemapDepth = item.sitemapDepth + 1
				}
				if err := enqueue(s, 0, nextSitemap, nextSitemapDepth); err != nil {
					return nil, err
				}
			}
			continue
		}

		// Skip non-HTML URLs (images, fonts, stylesheets, feeds, etc.).
		if !isHTMLURL(item.u) {
			continue
		}

		targets = append(targets, defaultTarget(item.u, "http", 1))

		if item.depth >= maxDepth {
			continue
		}

		remaining := opts.limits.maxDiscovered - budget.discovered
		links, err := crawlPageWithBudget(ctx, client, item.u, base, budget, remaining, visited)
		if err != nil {
			if crawlFatalError(err) {
				return nil, err
			}
			continue // non-fatal; skip this page
		}
		for _, link := range links {
			if err := enqueue(link, item.depth+1, isSitemapURL(link), 1); err != nil {
				return nil, err
			}
		}
	}
	return targets, nil
}

func newCrawlHTTPClient(base *url.URL, timeout time.Duration) *http.Client {
	return newCrawlHTTPClientWithPolicy(base, policy.Trusted(), defaultCrawlRuntime(), timeout)
}

// crawlPage fetches rawURL and returns in-domain links found on the page.
func crawlPage(ctx context.Context, client *http.Client, rawURL string, base *url.URL) ([]string, error) {
	budget := &crawlBudget{limits: defaultCrawlOptions(maxCrawlDiscoveredURLs).limits}
	return crawlPageWithBudget(ctx, client, rawURL, base, budget, maxCrawlDiscoveredURLs, nil)
}

func crawlPageWithBudget(ctx context.Context, client *http.Client, rawURL string, base *url.URL, budget *crawlBudget, maxLinks int, known map[string]bool) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", generateUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		return nil, nil // skip non-HTML responses
	}

	body, err := readCrawlResponseBody(resp.Body, maxCrawlHTMLBytes, "HTML", budget)
	if err != nil {
		return nil, err
	}

	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	pageURL, _ := url.Parse(rawURL)
	return extractLinksLimited(doc, pageURL, base, maxLinks, known)
}

// extractLinks walks an HTML node tree and returns absolute in-domain URLs.
func extractLinks(n *html.Node, pageURL, base *url.URL) []string {
	links, _ := extractLinksLimited(n, pageURL, base, maxCrawlDiscoveredURLs, nil)
	return links
}

func extractLinksLimited(n *html.Node, pageURL, base *url.URL, maxLinks int, known map[string]bool) ([]string, error) {
	var links []string
	seen := make(map[string]bool)
	var walk func(*html.Node) error
	walk = func(n *html.Node) error {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					if len(attr.Val) > maxCrawlURLBytes {
						return &crawlLimitError{message: fmt.Sprintf("crawler URL exceeds %d bytes", maxCrawlURLBytes)}
					}
					ref, err := url.Parse(attr.Val)
					if err != nil {
						break
					}
					abs := pageURL.ResolveReference(ref)
					normalizeCrawlURL(abs)
					if sameCrawlOrigin(abs, base) {
						normalized := abs.String()
						if seen[normalized] || known[normalized] {
							break
						}
						if len(links) >= maxLinks {
							return &crawlLimitError{message: fmt.Sprintf("crawler discovered URL budget exceeded (maximum %d)", maxLinks)}
						}
						seen[normalized] = true
						links = append(links, normalized)
					}
					break
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if err := walk(c); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(n); err != nil {
		return nil, err
	}
	return links, nil
}

// normalizeCrawlURL strips fragment, query string, and trailing slashes from
// non-root paths so that https://example.com and https://example.com/ are
// treated as the same URL during crawl deduplication.
func normalizeCrawlURL(u *url.URL) {
	u.Fragment = ""
	u.RawQuery = ""
	if u.Path == "" {
		u.Path = "/"
	} else if u.Path != "/" {
		u.Path = strings.TrimRight(u.Path, "/")
	}
}

// fetchRobotsInfo downloads /robots.txt and returns the Disallow path prefixes
// for the wildcard user-agent and any Sitemap: URLs declared in the file.
// The compatibility wrapper ignores errors; the command path uses the budgeted
// variant below and propagates policy, cancellation, and resource-limit errors.
func fetchRobotsInfo(ctx context.Context, client *http.Client, base *url.URL) (disallowed, sitemaps []string) {
	budget := &crawlBudget{limits: defaultCrawlOptions(50).limits}
	disallowed, sitemaps, _ = fetchRobotsInfoWithBudget(ctx, client, base, budget)
	return disallowed, sitemaps
}

func fetchRobotsInfoWithBudget(ctx context.Context, client *http.Client, base *url.URL, budget *crawlBudget) (disallowed, sitemaps []string, retErr error) {
	robotsURL := &url.URL{Scheme: base.Scheme, Host: base.Host, Path: "/robots.txt"}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", generateUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil, nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, nil, nil
	}
	body, err := readCrawlResponseBody(resp.Body, budget.limits.maxRobotsBytes, "robots", budget)
	if err != nil {
		return nil, nil, err
	}

	inWildcard := false
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 64<<10), int(budget.limits.maxRobotsBytes))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			inWildcard = false
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch strings.ToLower(key) {
		case "user-agent":
			inWildcard = val == "*"
		case "disallow":
			if inWildcard && val != "" {
				if err := budget.addRobotsEntry(); err != nil {
					return nil, nil, err
				}
				disallowed = append(disallowed, val)
			}
		case "sitemap":
			if val != "" {
				if err := budget.addRobotsEntry(); err != nil {
					return nil, nil, err
				}
				sitemaps = append(sitemaps, val)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	return disallowed, sitemaps, nil
}

// fetchSitemapLocs downloads a sitemap XML file and returns all <loc> URLs,
// handling both urlset (regular sitemaps) and sitemapindex (sitemap indexes).
// The compatibility wrapper keeps the historical error-returning signature;
// the command path supplies its shared crawl budget to the variant below.
func fetchSitemapLocs(ctx context.Context, client *http.Client, sitemapURL string) ([]string, error) {
	budget := &crawlBudget{limits: defaultCrawlOptions(50).limits}
	return fetchSitemapLocsWithBudget(ctx, client, sitemapURL, budget)
}

func fetchSitemapLocsWithBudget(ctx context.Context, client *http.Client, sitemapURL string, budget *crawlBudget) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sitemapURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", generateUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	body, err := readCrawlResponseBody(resp.Body, maxCrawlSitemapBytes, "sitemap", budget)
	if err != nil {
		return nil, err
	}

	var locs []string
	decoder := xml.NewDecoder(bytes.NewReader(body))
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || strings.ToLower(start.Name.Local) != "loc" {
			continue
		}
		var loc string
		if err := decoder.DecodeElement(&loc, &start); err != nil {
			return nil, err
		}
		loc = strings.TrimSpace(loc)
		if loc == "" {
			continue
		}
		if len(loc) > maxCrawlURLBytes {
			return nil, &crawlLimitError{message: fmt.Sprintf("crawler URL exceeds %d bytes", maxCrawlURLBytes)}
		}
		if err := budget.addSitemapEntry(); err != nil {
			return nil, err
		}
		locs = append(locs, loc)
	}
	return locs, nil
}

func readLimitedResponseBody(r io.Reader, maxBytes int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("response body exceeds %d bytes", maxBytes)
	}
	return body, nil
}

func readCrawlResponseBody(r io.Reader, maxBytes int64, kind string, budget *crawlBudget) ([]byte, error) {
	if budget == nil {
		return readLimitedResponseBody(r, maxBytes)
	}
	remaining := budget.remainingBytes()
	if remaining < 0 {
		remaining = 0
	}
	readLimit := min(maxBytes, remaining)
	body, err := io.ReadAll(io.LimitReader(r, readLimit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, &crawlLimitError{message: fmt.Sprintf("crawler %s byte budget exceeded (maximum %d)", kind, maxBytes)}
	}
	if int64(len(body)) > remaining {
		return nil, &crawlLimitError{message: fmt.Sprintf("crawler total byte budget exceeded (maximum %d)", budget.limits.maxTotalBytes)}
	}
	budget.totalBytes += int64(len(body))
	return body, nil
}

// isHTMLURL returns true for URLs that are likely HTML pages. URLs with no
// recognised non-HTML extension are treated as HTML.
func isHTMLURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	ext := strings.ToLower(filepath.Ext(u.Path))
	switch ext {
	case ".xml", ".json", ".atom", ".rss",
		".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
		".css", ".js", ".ts", ".map",
		".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".ico", ".avif",
		".woff", ".woff2", ".ttf", ".eot", ".otf",
		".mp4", ".mp3", ".webm", ".ogg", ".wav",
		".zip", ".gz", ".tar", ".br":
		return false
	}
	return true
}

// isSitemapURL returns true for URLs whose filename contains "sitemap" and
// ends in ".xml" (e.g. /sitemap.xml, /sitemap-index.xml, /sitemaps/news.xml).
func isSitemapURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	name := strings.ToLower(filepath.Base(u.Path))
	return strings.Contains(name, "sitemap") && strings.HasSuffix(name, ".xml")
}

// isDisallowed returns true if rawURL's path starts with any disallowed prefix.
func isDisallowed(rawURL string, disallowed []string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	for _, d := range disallowed {
		if strings.HasPrefix(u.Path, d) {
			return true
		}
	}
	return false
}

// --- source: browser history ---

// targetsFromHistory reads the most-visited HTTP/HTTPS URLs from the local
// browser history and returns them as weighted http targets. Weight is derived
// from visit count (capped at 10 to prevent one URL from dominating).
func targetsFromHistory(browser string, limit int) ([]config.TargetConfig, error) {
	browser = strings.ToLower(browser)
	dbPath, query, err := historyDBInfo(browser)
	if err != nil {
		return nil, err
	}
	return historyFromSQLite(dbPath, query, limit)
}

// historyDBInfo returns the SQLite file path and SELECT query for the given browser.
func historyDBInfo(browser string) (string, string, error) {
	switch browser {
	case "chrome", "chromium":
		path, err := chromePath("History")
		if err != nil {
			return "", "", fmt.Errorf("finding Chrome history: %w", err)
		}
		const q = `SELECT url, visit_count FROM urls WHERE url LIKE 'http%' ORDER BY visit_count DESC LIMIT ?`
		return path, q, nil
	case "firefox":
		path, err := firefoxPath("places.sqlite")
		if err != nil {
			return "", "", fmt.Errorf("finding Firefox history: %w", err)
		}
		const q = `SELECT url, visit_count FROM moz_places WHERE visit_count > 0 AND url LIKE 'http%' ORDER BY visit_count DESC LIMIT ?`
		return path, q, nil
	case "safari":
		if runtime.GOOS != "darwin" {
			return "", "", fmt.Errorf("safari is only available on macOS")
		}
		home, _ := os.UserHomeDir()
		path := filepath.Join(home, "Library", "Safari", "History.db")
		const q = `SELECT url, visit_count FROM history_items WHERE url LIKE 'http%' ORDER BY visit_count DESC LIMIT ?`
		return path, q, nil
	default:
		return "", "", fmt.Errorf("unknown browser %q: must be chrome, firefox, or safari", browser)
	}
}

// historyFromSQLite queries a SQLite history DB and returns http targets.
// The query must SELECT (url TEXT, visit_count INT) with one LIMIT ? parameter.
func historyFromSQLite(dbPath, query string, limit int) ([]config.TargetConfig, error) {
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("database not found at %q (is the browser installed and has been opened at least once?): %w", dbPath, err)
	}

	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&immutable=1")
	if err != nil {
		return nil, fmt.Errorf("opening %q: %w", dbPath, err)
	}
	defer db.Close() //nolint:errcheck

	rows, err := db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("querying history: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var targets []config.TargetConfig
	for rows.Next() {
		var u string
		var visits int
		if err := rows.Scan(&u, &visits); err != nil {
			continue
		}
		weight := min(visits, 10) // cap weight; avoid one URL dominating the distribution
		if weight < 1 {
			weight = 1
		}
		targets = append(targets, defaultTarget(u, "http", weight))
	}
	return targets, rows.Err()
}

// --- source: browser bookmarks ---

// targetsFromBookmarks reads browser bookmarks and returns them as http targets.
func targetsFromBookmarks(browser string) ([]config.TargetConfig, error) {
	switch strings.ToLower(browser) {
	case "chrome", "chromium":
		path, err := chromePath("Bookmarks")
		if err != nil {
			return nil, fmt.Errorf("finding Chrome bookmarks: %w", err)
		}
		return chromeBookmarks(path)
	case "firefox":
		path, err := firefoxPath("places.sqlite")
		if err != nil {
			return nil, fmt.Errorf("finding Firefox bookmarks: %w", err)
		}
		return firefoxBookmarks(path)
	case "safari":
		if runtime.GOOS != "darwin" {
			return nil, fmt.Errorf("safari is only available on macOS")
		}
		home, _ := os.UserHomeDir()
		path := filepath.Join(home, "Library", "Safari", "Bookmarks.plist")
		return safariBookmarks(path)
	default:
		return nil, fmt.Errorf("unknown browser %q: must be chrome, firefox, or safari", browser)
	}
}

// chromeBookmarks reads Chrome's Bookmarks JSON file and returns all HTTP/HTTPS URLs.
func chromeBookmarks(path string) ([]config.TargetConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %q: %w", path, err)
	}
	var root struct {
		Roots map[string]json.RawMessage `json:"roots"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parsing bookmarks JSON: %w", err)
	}
	var targets []config.TargetConfig
	for _, v := range root.Roots {
		targets = append(targets, walkChromeNode(v)...)
	}
	return targets, nil
}

// walkChromeNode recursively extracts URL entries from a Chrome bookmark node.
func walkChromeNode(raw json.RawMessage) []config.TargetConfig {
	var node struct {
		Type     string            `json:"type"`
		URL      string            `json:"url"`
		Children []json.RawMessage `json:"children"`
	}
	if err := json.Unmarshal(raw, &node); err != nil {
		return nil
	}
	if node.Type == "url" {
		if strings.HasPrefix(node.URL, "http://") || strings.HasPrefix(node.URL, "https://") {
			return []config.TargetConfig{defaultTarget(node.URL, "http", 1)}
		}
		return nil
	}
	var targets []config.TargetConfig
	for _, child := range node.Children {
		targets = append(targets, walkChromeNode(child)...)
	}
	return targets
}

// firefoxBookmarks reads bookmarks from Firefox's places.sqlite.
func firefoxBookmarks(dbPath string) ([]config.TargetConfig, error) {
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("database not found at %q: %w", dbPath, err)
	}

	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&immutable=1")
	if err != nil {
		return nil, fmt.Errorf("opening %q: %w", dbPath, err)
	}
	defer db.Close() //nolint:errcheck

	const q = `SELECT p.url FROM moz_places p
		JOIN moz_bookmarks b ON b.fk = p.id
		WHERE b.type = 1 AND p.url LIKE 'http%'`
	rows, err := db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("querying bookmarks: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var targets []config.TargetConfig
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			continue
		}
		targets = append(targets, defaultTarget(u, "http", 1))
	}
	return targets, rows.Err()
}

// safariBookmarkNode mirrors the nested structure inside Safari's Bookmarks.plist.
// The plist stores both leaf URLs (WebBookmarkTypeLeaf) and folders
// (WebBookmarkTypeList) under the same node type.
type safariBookmarkNode struct {
	Type      string               `plist:"WebBookmarkType"`
	URLString string               `plist:"URLString"`
	Children  []safariBookmarkNode `plist:"Children"`
}

// safariBookmarks reads Safari's Bookmarks.plist (binary or XML plist) and
// returns all HTTP/HTTPS bookmark URLs as equally-weighted http targets.
func safariBookmarks(path string) ([]config.TargetConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reading %q (is Safari installed and has been opened at least once?): %w", path, err)
	}
	defer f.Close() //nolint:errcheck

	var root safariBookmarkNode
	if err := plist.NewDecoder(f).Decode(&root); err != nil {
		return nil, fmt.Errorf("parsing Safari bookmarks: %w", err)
	}
	return walkSafariNodes(root.Children), nil
}

// walkSafariNodes recursively extracts HTTP/HTTPS URL entries from a Safari
// bookmark tree, descending into folders (WebBookmarkTypeList nodes).
func walkSafariNodes(nodes []safariBookmarkNode) []config.TargetConfig {
	var targets []config.TargetConfig
	for _, n := range nodes {
		switch n.Type {
		case "WebBookmarkTypeLeaf":
			if strings.HasPrefix(n.URLString, "http://") || strings.HasPrefix(n.URLString, "https://") {
				targets = append(targets, defaultTarget(n.URLString, "http", 1))
			}
		default:
			targets = append(targets, walkSafariNodes(n.Children)...)
		}
	}
	return targets
}

// --- browser path helpers ---

// chromePath returns the path to a file in the Chrome Default profile directory.
func chromePath(filename string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	var base string
	switch runtime.GOOS {
	case "linux":
		base = filepath.Join(home, ".config", "google-chrome", "Default")
	case "darwin":
		base = filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "Default")
	default:
		return "", fmt.Errorf("chrome history/bookmarks are not supported on %s", runtime.GOOS)
	}
	return filepath.Join(base, filename), nil
}

// firefoxPath returns the path to a file in the default Firefox profile directory.
func firefoxPath(filename string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	var configDir string
	switch runtime.GOOS {
	case "linux":
		configDir = filepath.Join(home, ".mozilla", "firefox")
	case "darwin":
		configDir = filepath.Join(home, "Library", "Application Support", "Firefox", "Profiles")
	default:
		return "", fmt.Errorf("firefox history/bookmarks are not supported on %s", runtime.GOOS)
	}
	profile, err := firefoxDefaultProfile(configDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(profile, filename), nil
}

// firefoxDefaultProfile finds the default Firefox profile directory by reading
// profiles.ini. Falls back to the first *.default* directory on failure.
func firefoxDefaultProfile(configDir string) (string, error) {
	iniPath := filepath.Join(configDir, "profiles.ini")
	if runtime.GOOS == "darwin" {
		// On macOS the Profiles dir is one level deeper; profiles.ini is in the parent.
		iniPath = filepath.Join(filepath.Dir(configDir), "profiles.ini")
	}

	if f, err := os.Open(iniPath); err == nil {
		defer f.Close() //nolint:errcheck
		var (
			inWildcard bool
			path       string
		)
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			switch {
			case strings.HasPrefix(line, "[Profile"):
				inWildcard = false
				path = ""
			case strings.HasPrefix(line, "Default=1"):
				inWildcard = true
			case strings.HasPrefix(line, "Path="):
				path = strings.TrimPrefix(line, "Path=")
			}
			if inWildcard && path != "" {
				if filepath.IsAbs(path) {
					return path, nil
				}
				return filepath.Join(configDir, path), nil
			}
		}
	}
	return firefoxFallbackProfile(configDir)
}

// firefoxFallbackProfile picks the first *.default* sub-directory of configDir.
func firefoxFallbackProfile(configDir string) (string, error) {
	entries, err := os.ReadDir(configDir)
	if err != nil {
		return "", fmt.Errorf("no Firefox profile found in %q: %w", configDir, err)
	}
	for _, e := range entries {
		if e.IsDir() && strings.Contains(e.Name(), "default") {
			return filepath.Join(configDir, e.Name()), nil
		}
	}
	return "", fmt.Errorf("no default Firefox profile found in %q", configDir)
}

// --- output ---

// emitGeneratedConfig writes the generated config YAML to stdout or a file.
// If outPath names an existing file the user is prompted before overwriting.
func emitGeneratedConfig(cmd *cobra.Command, targets []config.TargetConfig, outPath string) error {
	w := cmd.OutOrStdout()

	if outPath != "" {
		if _, err := os.Stat(outPath); err == nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "File %q already exists. Overwrite? [y/N] ", outPath)
			var answer string
			fmt.Fscan(cmd.InOrStdin(), &answer) //nolint:errcheck,gosec
			if !strings.EqualFold(strings.TrimSpace(answer), "y") {
				return fmt.Errorf("aborted")
			}
		}
		prepared, err := policy.PrepareExplicitOutput(outPath, false)
		if err != nil {
			return fmt.Errorf("creating %q: %w", outPath, err)
		}
		f, err := prepared.Commit()
		if err != nil {
			return fmt.Errorf("creating %q: %w", outPath, err)
		}
		defer f.Close() //nolint:errcheck
		w = f
		formatConfig(w, targets)
		fmt.Fprintf(cmd.OutOrStdout(), "Wrote %d target(s) to %q\n", len(targets), outPath)
		return nil
	}

	formatConfig(w, targets)
	return nil
}

// formatConfig writes a complete sendit config YAML to w.
func formatConfig(w io.Writer, targets []config.TargetConfig) {
	fmt.Fprintf(w, "# Generated by sendit generate on %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintln(w, "# Edit pacing, limits, and rate_limits sections to tune behaviour.")
	fmt.Fprintln(w, "# Run 'sendit validate --config <file>' to check before running.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "pacing:")
	fmt.Fprintln(w, "  mode: human")
	fmt.Fprintln(w, "  min_delay_ms: 800")
	fmt.Fprintln(w, "  max_delay_ms: 8000")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "limits:")
	fmt.Fprintln(w, "  max_workers: 4")
	fmt.Fprintln(w, "  max_browser_workers: 1")
	fmt.Fprintln(w, "  cpu_threshold_pct: 60.0")
	fmt.Fprintln(w, "  memory_threshold_mb: 512")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "rate_limits:")
	fmt.Fprintln(w, "  default_rps: 0.5")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "backoff:")
	fmt.Fprintln(w, "  initial_ms: 1000")
	fmt.Fprintln(w, "  max_ms: 120000")
	fmt.Fprintln(w, "  multiplier: 2.0")
	fmt.Fprintln(w, "  max_attempts: 3")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "targets:")
	for _, t := range targets {
		formatTarget(w, t)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "metrics:")
	fmt.Fprintln(w, "  enabled: false")
	fmt.Fprintln(w, "  bind_address: 127.0.0.1")
	fmt.Fprintln(w, "  prometheus_port: 9090")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "daemon:")
	fmt.Fprintln(w, "  # pid_file: custom trusted-config path; omit for the restricted-compatible per-user default")
	fmt.Fprintln(w, "  log_level: info")
	fmt.Fprintln(w, "  log_format: text")
}

// formatTarget writes a single YAML target block (indented, preceded by blank line).
func formatTarget(w io.Writer, t config.TargetConfig) {
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  - url: %q\n", t.URL)
	fmt.Fprintf(w, "    weight: %d\n", t.Weight)
	fmt.Fprintf(w, "    type: %s\n", t.Type)
	switch t.Type {
	case "http", "browser":
		fmt.Fprintln(w, "    http:")
		if t.Type == "http" {
			fmt.Fprintf(w, "      http_version: %d\n", t.HTTP.HTTPVersion)
		}
		fmt.Fprintln(w, "      method: GET")
		fmt.Fprintf(w, "      headers:\n        User-Agent: %q\n", generateUserAgent)
		fmt.Fprintln(w, "      timeout_s: 15")
	case "dns":
		fmt.Fprintln(w, "    dns:")
		resolver := t.DNS.Resolver
		if resolver == "" {
			resolver = "8.8.8.8:53"
		}
		fmt.Fprintf(w, "      resolver: %q\n", resolver)
		recordType := t.DNS.RecordType
		if recordType == "" {
			recordType = "A"
		}
		fmt.Fprintf(w, "      record_type: %s\n", recordType)
	case "websocket":
		fmt.Fprintln(w, "    websocket:")
		dur := t.WebSocket.DurationS
		if dur == 0 {
			dur = 30
		}
		fmt.Fprintf(w, "      duration_s: %d\n", dur)
	}
}

// --- helpers ---

// defaultTarget constructs a TargetConfig with sensible driver defaults.
func defaultTarget(u, typ string, weight int) config.TargetConfig {
	return config.TargetConfig{
		URL:    u,
		Weight: weight,
		Type:   typ,
		HTTP: config.HTTPConfig{
			Method:   "GET",
			TimeoutS: 15,
		},
		DNS: config.DNSConfig{
			Resolver:   "8.8.8.8:53",
			RecordType: "A",
		},
		WebSocket: config.WebSocketConfig{
			DurationS: 30,
		},
	}
}

// deduplicateTargets removes duplicate URLs, keeping the first occurrence.
func deduplicateTargets(targets []config.TargetConfig) []config.TargetConfig {
	seen := make(map[string]bool, len(targets))
	out := make([]config.TargetConfig, 0, len(targets))
	for _, t := range targets {
		if !seen[t.URL] {
			seen[t.URL] = true
			out = append(out, t)
		}
	}
	return out
}
