package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lewta/sendit/internal/config"
)

// --- cobra registration ---

func TestGenerateCmd_Registered(t *testing.T) {
	for _, sub := range rootCmd.Commands() {
		if sub.Name() == "generate" {
			return
		}
	}
	t.Fatal("generate command not registered in rootCmd; users will see 'unknown command \"generate\"'")
}

func TestGenerateCmd_OutputFlag(t *testing.T) {
	cmd := generateCmd()
	if f := cmd.Flags().Lookup("output"); f == nil {
		t.Fatal("--output flag not registered on generateCmd")
	}
}

func TestGenerateCmd_URLFlag(t *testing.T) {
	cmd := generateCmd()
	if f := cmd.Flags().Lookup("url"); f == nil {
		t.Fatal("--url flag not registered on generateCmd")
	}
}

func TestGenerateCmd_TargetsFileFlag(t *testing.T) {
	cmd := generateCmd()
	if f := cmd.Flags().Lookup("targets-file"); f == nil {
		t.Fatal("--targets-file flag not registered on generateCmd")
	}
}

func TestGenerateCmd_HistoryLimitFlag(t *testing.T) {
	cmd := generateCmd()
	if f := cmd.Flags().Lookup("history-limit"); f == nil {
		t.Fatal("--history-limit flag not registered on generateCmd")
	}
}

// --- error cases ---

func TestGenerateCmd_NoInput(t *testing.T) {
	cmd := generateCmd()
	cmd.SetArgs([]string{})
	cmd.SetOut(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when no source is specified, got nil")
	}
}

func TestGenerateCmd_CrawlRequiresURL(t *testing.T) {
	cmd := generateCmd()
	cmd.SetArgs([]string{"--crawl"})
	cmd.SetOut(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for --crawl without --url, got nil")
	}
}

func TestGenerateCmd_InvalidHistoryBrowser(t *testing.T) {
	cmd := generateCmd()
	cmd.SetArgs([]string{"--from-history", "edge"})
	cmd.SetOut(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for unsupported browser, got nil")
	}
}

func TestGenerateCmd_InvalidBookmarksBrowser(t *testing.T) {
	cmd := generateCmd()
	cmd.SetArgs([]string{"--from-bookmarks", "edge"})
	cmd.SetOut(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for unsupported browser, got nil")
	}
}

func TestGenerateCmd_SafariBookmarksUnsupported(t *testing.T) {
	cmd := generateCmd()
	cmd.SetArgs([]string{"--from-bookmarks", "safari"})
	cmd.SetOut(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for Safari bookmarks (binary plist), got nil")
	}
}

// --- targetsFromFile ---

func TestTargetsFromFile_HappyPath(t *testing.T) {
	content := "https://example.com http 5\nexample.com dns\n# comment\n\n"
	f := filepath.Join(t.TempDir(), "targets.txt")
	if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	targets, err := targetsFromFile(f)
	if err != nil {
		t.Fatalf("targetsFromFile: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}
	if targets[0].URL != "https://example.com" || targets[0].Type != "http" || targets[0].Weight != 5 {
		t.Errorf("unexpected first target: %+v", targets[0])
	}
	if targets[1].URL != "example.com" || targets[1].Type != "dns" || targets[1].Weight != 1 {
		t.Errorf("unexpected second target: %+v", targets[1])
	}
}

func TestTargetsFromFile_MissingFile(t *testing.T) {
	if _, err := targetsFromFile("/tmp/sendit-no-such-targets-file.txt"); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestTargetsFromFile_BadType(t *testing.T) {
	f := filepath.Join(t.TempDir(), "bad.txt")
	if err := os.WriteFile(f, []byte("https://example.com grpc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := targetsFromFile(f); err == nil {
		t.Fatal("expected error for unknown type, got nil")
	}
}

func TestTargetsFromFile_BadWeight(t *testing.T) {
	f := filepath.Join(t.TempDir(), "bad.txt")
	if err := os.WriteFile(f, []byte("https://example.com http 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := targetsFromFile(f); err == nil {
		t.Fatal("expected error for zero weight, got nil")
	}
}

// --- deduplicateTargets ---

func TestDeduplicateTargets_RemovesDuplicates(t *testing.T) {
	a := defaultTarget("https://a.com", "http", 2)
	b := defaultTarget("https://b.com", "http", 1)
	aDup := defaultTarget("https://a.com", "http", 3) // duplicate

	got := deduplicateTargets([]config.TargetConfig{a, b, aDup})
	if len(got) != 2 {
		t.Fatalf("expected 2 after dedup, got %d", len(got))
	}
	if got[0].URL != "https://a.com" || got[0].Weight != 2 {
		t.Errorf("first entry wrong: %+v", got[0])
	}
	if got[1].URL != "https://b.com" {
		t.Errorf("second entry wrong: %+v", got[1])
	}
}

func TestDeduplicateTargets_Empty(t *testing.T) {
	got := deduplicateTargets(nil)
	if len(got) != 0 {
		t.Errorf("expected 0 from nil input, got %d", len(got))
	}
}

// --- crawl ---

func TestTargetsFromCrawl_SimplePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body>
			<a href="/about">About</a>
			<a href="/contact">Contact</a>
			<a href="https://external.com/page">External</a>
		</body></html>`))
	}))
	defer srv.Close()

	targets, err := targetsFromCrawl(srv.URL, 1, 50, true)
	if err != nil {
		t.Fatalf("targetsFromCrawl: %v", err)
	}
	if len(targets) < 1 {
		t.Fatalf("expected at least 1 target, got 0")
	}
	for _, tgt := range targets {
		if !strings.HasPrefix(tgt.URL, srv.URL) {
			t.Errorf("unexpected out-of-domain target: %s", tgt.URL)
		}
		if tgt.Type != "http" {
			t.Errorf("expected type http, got %q for %s", tgt.Type, tgt.URL)
		}
	}
}

func TestTargetsFromCrawl_InvalidScheme(t *testing.T) {
	if _, err := targetsFromCrawl("ftp://example.com", 1, 50, true); err == nil {
		t.Fatal("expected error for non-http scheme, got nil")
	}
}

func TestTargetsFromCrawl_RobotsRespected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /private/\n"))
		case "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><body>
				<a href="/private/secret">Secret</a>
				<a href="/about">About</a>
			</body></html>`))
		default:
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><body></body></html>`))
		}
	}))
	defer srv.Close()

	targets, err := targetsFromCrawl(srv.URL, 2, 50, false)
	if err != nil {
		t.Fatalf("targetsFromCrawl: %v", err)
	}
	for _, tgt := range targets {
		if strings.Contains(tgt.URL, "/private/") {
			t.Errorf("robots.txt Disallow was not respected; crawled: %s", tgt.URL)
		}
	}
}

func TestTargetsFromCrawl_MaxPagesRespected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Each page links to 10 new pages to stress the max-pages cap.
		w.Header().Set("Content-Type", "text/html")
		var links strings.Builder
		for i := 0; i < 10; i++ {
			links.WriteString(`<a href="/page` + r.URL.Path[1:] + strings.Repeat("x", i) + `">p</a>`)
		}
		_, _ = w.Write([]byte("<html><body>" + links.String() + "</body></html>"))
	}))
	defer srv.Close()

	const maxPages = 5
	targets, err := targetsFromCrawl(srv.URL, 3, maxPages, true)
	if err != nil {
		t.Fatalf("targetsFromCrawl: %v", err)
	}
	if len(targets) > maxPages {
		t.Errorf("expected <= %d targets, got %d", maxPages, len(targets))
	}
}

// --- output ---

func TestGenerateCmd_OutputToFile(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "targets.txt")
	outPath := filepath.Join(dir, "generated.yaml")

	if err := os.WriteFile(inPath, []byte("https://example.com http\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := generateCmd()
	cmd.SetArgs([]string{"--targets-file", inPath, "--output", outPath})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("generateCmd returned error: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("output file not created: %v", err)
	}
	content := string(data)
	for _, want := range []string{"pacing:", "targets:", "https://example.com"} {
		if !strings.Contains(content, want) {
			t.Errorf("output file missing %q", want)
		}
	}
	if !strings.Contains(stdout.String(), "Wrote") {
		t.Error("expected confirmation message on stdout, got none")
	}
}

func TestGenerateCmd_OutputToStdout(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "targets.txt")
	if err := os.WriteFile(inPath, []byte("https://example.com http 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := generateCmd()
	cmd.SetArgs([]string{"--targets-file", inPath})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("generateCmd returned error: %v", err)
	}

	got := out.String()
	for _, want := range []string{"pacing:", "targets:", "https://example.com", "weight: 3"} {
		if !strings.Contains(got, want) {
			t.Errorf("stdout missing %q", want)
		}
	}
}

func TestGenerateCmd_OverwritePrompt_Abort(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "targets.txt")
	outPath := filepath.Join(dir, "out.yaml")

	if err := os.WriteFile(inPath, []byte("https://example.com http\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outPath, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := generateCmd()
	cmd.SetArgs([]string{"--targets-file", inPath, "--output", outPath})
	cmd.SetIn(strings.NewReader("n\n"))
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetOut(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when user aborts overwrite, got nil")
	}

	data, _ := os.ReadFile(outPath)
	if string(data) != "existing" {
		t.Error("file was overwritten despite user answering no")
	}
}

// --- helpers ---

func TestIsDisallowed(t *testing.T) {
	disallowed := []string{"/private/", "/admin"}
	cases := []struct {
		url  string
		want bool
	}{
		{"http://example.com/private/page", true},
		{"http://example.com/admin", true},
		{"http://example.com/about", false},
		{"http://example.com/", false},
	}
	for _, tc := range cases {
		if got := isDisallowed(tc.url, disallowed); got != tc.want {
			t.Errorf("isDisallowed(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

func TestFormatConfig_ContainsRequiredSections(t *testing.T) {
	var b bytes.Buffer
	formatConfig(&b, nil)
	got := b.String()
	for _, section := range []string{"pacing:", "limits:", "rate_limits:", "backoff:", "targets:", "metrics:", "daemon:"} {
		if !strings.Contains(got, section) {
			t.Errorf("formatConfig output missing section %q", section)
		}
	}
}

func TestFormatTarget_HTTP(t *testing.T) {
	var b bytes.Buffer
	tgt := defaultTarget("https://example.com", "http", 1)
	formatTarget(&b, tgt)
	got := b.String()
	for _, want := range []string{"url:", "weight:", "type: http", "method: GET", "timeout_s: 15"} {
		if !strings.Contains(got, want) {
			t.Errorf("formatTarget (http) missing %q; got:\n%s", want, got)
		}
	}
}

func TestFormatTarget_DNS(t *testing.T) {
	var b bytes.Buffer
	tgt := defaultTarget("example.com", "dns", 1)
	formatTarget(&b, tgt)
	got := b.String()
	for _, want := range []string{"type: dns", "resolver:", "record_type: A"} {
		if !strings.Contains(got, want) {
			t.Errorf("formatTarget (dns) missing %q; got:\n%s", want, got)
		}
	}
}
