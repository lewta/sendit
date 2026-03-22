package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/lewta/sendit/internal/config"
	_ "modernc.org/sqlite"
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

func TestGenerateCmd_SafariBookmarksNonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("skipping non-darwin guard test on macOS")
	}
	cmd := generateCmd()
	cmd.SetArgs([]string{"--from-bookmarks", "safari"})
	cmd.SetOut(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for Safari bookmarks on non-macOS, got nil")
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

// --- browser history SQLite fixtures ---

// makeHistoryDB creates a minimal SQLite database at path with a Chrome-style
// urls table and the given rows, then closes the connection.
func makeHistoryDB(t *testing.T, path string, rows []struct {
	url    string
	visits int
}) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open history db: %v", err)
	}
	defer db.Close() //nolint:errcheck
	if _, err := db.Exec(`CREATE TABLE urls (url TEXT, visit_count INTEGER)`); err != nil {
		t.Fatalf("create urls table: %v", err)
	}
	for _, r := range rows {
		if _, err := db.Exec(`INSERT INTO urls VALUES (?, ?)`, r.url, r.visits); err != nil {
			t.Fatalf("insert row: %v", err)
		}
	}
}

func TestHistoryFromSQLite_Chrome(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "History")

	makeHistoryDB(t, dbPath, []struct {
		url    string
		visits int
	}{
		{"https://example.com", 10},
		{"https://go.dev/doc", 5},
		{"ftp://oldproto.net", 3}, // non-http, must be excluded
	})

	const q = `SELECT url, visit_count FROM urls WHERE url LIKE 'http%' ORDER BY visit_count DESC LIMIT ?`
	targets, err := historyFromSQLite(dbPath, q, 100)
	if err != nil {
		t.Fatalf("historyFromSQLite: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 http targets, got %d", len(targets))
	}
	if targets[0].URL != "https://example.com" {
		t.Errorf("expected example.com first, got %q", targets[0].URL)
	}
	if targets[0].Weight != 10 {
		t.Errorf("expected weight 10 (capped), got %d", targets[0].Weight)
	}
	if targets[1].URL != "https://go.dev/doc" {
		t.Errorf("expected go.dev second, got %q", targets[1].URL)
	}
}

func TestHistoryFromSQLite_LimitRespected(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "History")

	rows := make([]struct {
		url    string
		visits int
	}, 20)
	for i := range rows {
		rows[i] = struct {
			url    string
			visits int
		}{
			url:    "https://example.com/page" + strings.Repeat("x", i),
			visits: i + 1,
		}
	}
	makeHistoryDB(t, dbPath, rows)

	const q = `SELECT url, visit_count FROM urls WHERE url LIKE 'http%' ORDER BY visit_count DESC LIMIT ?`
	targets, err := historyFromSQLite(dbPath, q, 5)
	if err != nil {
		t.Fatalf("historyFromSQLite: %v", err)
	}
	if len(targets) != 5 {
		t.Errorf("expected 5 targets (limit), got %d", len(targets))
	}
}

func TestHistoryFromSQLite_WeightCappedAt10(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "History")

	makeHistoryDB(t, dbPath, []struct {
		url    string
		visits int
	}{
		{"https://example.com", 999},
	})

	const q = `SELECT url, visit_count FROM urls WHERE url LIKE 'http%' ORDER BY visit_count DESC LIMIT ?`
	targets, err := historyFromSQLite(dbPath, q, 100)
	if err != nil {
		t.Fatalf("historyFromSQLite: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].Weight != 10 {
		t.Errorf("expected weight capped at 10, got %d", targets[0].Weight)
	}
}

func TestHistoryFromSQLite_MissingFile(t *testing.T) {
	_, err := historyFromSQLite("/tmp/sendit-no-such-history.db",
		`SELECT url, visit_count FROM urls WHERE url LIKE 'http%' ORDER BY visit_count DESC LIMIT ?`, 10)
	if err == nil {
		t.Fatal("expected error for missing database file, got nil")
	}
}

// --- browser bookmarks SQLite fixtures ---

// makeFirefoxPlacesDB creates a minimal places.sqlite at path with moz_places
// and moz_bookmarks tables and the given bookmark URLs.
func makeFirefoxPlacesDB(t *testing.T, path string, urls []string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open places db: %v", err)
	}
	defer db.Close() //nolint:errcheck

	if _, err := db.Exec(`CREATE TABLE moz_places (id INTEGER PRIMARY KEY, url TEXT, visit_count INTEGER)`); err != nil {
		t.Fatalf("create moz_places: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE moz_bookmarks (id INTEGER PRIMARY KEY, fk INTEGER, type INTEGER)`); err != nil {
		t.Fatalf("create moz_bookmarks: %v", err)
	}
	for i, u := range urls {
		id := i + 1
		if _, err := db.Exec(`INSERT INTO moz_places VALUES (?, ?, 1)`, id, u); err != nil {
			t.Fatalf("insert moz_places row: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO moz_bookmarks VALUES (?, ?, 1)`, id, id); err != nil {
			t.Fatalf("insert moz_bookmarks row: %v", err)
		}
	}
}

func TestFirefoxBookmarks_HappyPath(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "places.sqlite")

	makeFirefoxPlacesDB(t, dbPath, []string{
		"https://example.com",
		"https://go.dev",
		"ftp://oldproto.net", // non-http, must be excluded
	})

	targets, err := firefoxBookmarks(dbPath)
	if err != nil {
		t.Fatalf("firefoxBookmarks: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 http targets, got %d: %v", len(targets), targets)
	}
	urls := map[string]bool{}
	for _, tgt := range targets {
		urls[tgt.URL] = true
	}
	if !urls["https://example.com"] || !urls["https://go.dev"] {
		t.Errorf("unexpected targets: %v", targets)
	}
}

func TestFirefoxBookmarks_MissingFile(t *testing.T) {
	_, err := firefoxBookmarks("/tmp/sendit-no-such-places.sqlite")
	if err == nil {
		t.Fatal("expected error for missing database, got nil")
	}
}

// --- Safari bookmarks plist fixture ---

// safariPlistXML is a minimal XML plist with the same structure Safari writes.
const safariPlistXML = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>WebBookmarkType</key>
	<string>WebBookmarkTypeList</string>
	<key>Children</key>
	<array>
		<dict>
			<key>WebBookmarkType</key>
			<string>WebBookmarkTypeLeaf</string>
			<key>URLString</key>
			<string>https://example.com</string>
		</dict>
		<dict>
			<key>WebBookmarkType</key>
			<string>WebBookmarkTypeList</string>
			<key>Children</key>
			<array>
				<dict>
					<key>WebBookmarkType</key>
					<string>WebBookmarkTypeLeaf</string>
					<key>URLString</key>
					<string>https://go.dev/doc</string>
				</dict>
				<dict>
					<key>WebBookmarkType</key>
					<string>WebBookmarkTypeLeaf</string>
					<key>URLString</key>
					<string>reading-list://ignored</string>
				</dict>
			</array>
		</dict>
	</array>
</dict>
</plist>`

func TestSafariBookmarks_HappyPath(t *testing.T) {
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "Bookmarks.plist")
	if err := os.WriteFile(plistPath, []byte(safariPlistXML), 0o600); err != nil {
		t.Fatal(err)
	}

	targets, err := safariBookmarks(plistPath)
	if err != nil {
		t.Fatalf("safariBookmarks: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 http targets, got %d", len(targets))
	}
	urls := map[string]bool{}
	for _, tgt := range targets {
		urls[tgt.URL] = true
		if tgt.Type != "http" {
			t.Errorf("expected type http, got %q for %s", tgt.Type, tgt.URL)
		}
		if tgt.Weight != 1 {
			t.Errorf("expected weight 1, got %d for %s", tgt.Weight, tgt.URL)
		}
	}
	if !urls["https://example.com"] {
		t.Error("missing https://example.com")
	}
	if !urls["https://go.dev/doc"] {
		t.Error("missing https://go.dev/doc")
	}
	if urls["reading-list://ignored"] {
		t.Error("non-http URL should have been excluded")
	}
}

func TestSafariBookmarks_MissingFile(t *testing.T) {
	_, err := safariBookmarks("/tmp/sendit-no-such-Bookmarks.plist")
	if err == nil {
		t.Fatal("expected error for missing plist file, got nil")
	}
}

func TestWalkSafariNodes_Empty(t *testing.T) {
	if got := walkSafariNodes(nil); got != nil {
		t.Errorf("expected nil for empty nodes, got %v", got)
	}
}

// --- chromeBookmarks + walkChromeNode ---

const chromeBookmarksJSON = `{
	"roots": {
		"bookmark_bar": {
			"type": "folder",
			"children": [
				{"type": "url", "url": "https://example.com"},
				{"type": "url", "url": "https://go.dev"},
				{"type": "url", "url": "ftp://oldproto.net"},
				{
					"type": "folder",
					"children": [
						{"type": "url", "url": "https://nested.example.com"}
					]
				}
			]
		}
	}
}`

func TestChromeBookmarks_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Bookmarks")
	if err := os.WriteFile(path, []byte(chromeBookmarksJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	targets, err := chromeBookmarks(path)
	if err != nil {
		t.Fatalf("chromeBookmarks: %v", err)
	}

	urls := map[string]bool{}
	for _, tgt := range targets {
		urls[tgt.URL] = true
	}
	if !urls["https://example.com"] {
		t.Error("missing https://example.com")
	}
	if !urls["https://go.dev"] {
		t.Error("missing https://go.dev")
	}
	if !urls["https://nested.example.com"] {
		t.Error("missing https://nested.example.com")
	}
	if urls["ftp://oldproto.net"] {
		t.Error("ftp:// URL should be excluded")
	}
}

func TestChromeBookmarks_MissingFile(t *testing.T) {
	_, err := chromeBookmarks("/tmp/sendit-no-such-Bookmarks")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestChromeBookmarks_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Bookmarks")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := chromeBookmarks(path); err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestWalkChromeNode_URLNode(t *testing.T) {
	raw := json.RawMessage(`{"type":"url","url":"https://example.com"}`)
	targets := walkChromeNode(raw)
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].URL != "https://example.com" {
		t.Errorf("expected https://example.com, got %q", targets[0].URL)
	}
}

func TestWalkChromeNode_NonHTTPURL(t *testing.T) {
	raw := json.RawMessage(`{"type":"url","url":"ftp://example.com"}`)
	targets := walkChromeNode(raw)
	if len(targets) != 0 {
		t.Errorf("expected 0 targets for ftp:// URL, got %d", len(targets))
	}
}

func TestWalkChromeNode_FolderNode(t *testing.T) {
	raw := json.RawMessage(`{"type":"folder","children":[{"type":"url","url":"https://child.com"}]}`)
	targets := walkChromeNode(raw)
	if len(targets) != 1 {
		t.Fatalf("expected 1 target from folder, got %d", len(targets))
	}
	if targets[0].URL != "https://child.com" {
		t.Errorf("expected https://child.com, got %q", targets[0].URL)
	}
}

func TestWalkChromeNode_InvalidJSON(t *testing.T) {
	raw := json.RawMessage(`not json`)
	targets := walkChromeNode(raw)
	if targets != nil {
		t.Errorf("expected nil for invalid JSON, got %v", targets)
	}
}

// --- firefoxFallbackProfile ---

func TestFirefoxFallbackProfile_HappyPath(t *testing.T) {
	dir := t.TempDir()
	profDir := filepath.Join(dir, "abc123.default-release")
	if err := os.Mkdir(profDir, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := firefoxFallbackProfile(dir)
	if err != nil {
		t.Fatalf("firefoxFallbackProfile: %v", err)
	}
	if got != profDir {
		t.Errorf("expected %q, got %q", profDir, got)
	}
}

func TestFirefoxFallbackProfile_NoMatchingDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "somedir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := firefoxFallbackProfile(dir); err == nil {
		t.Fatal("expected error when no default profile dir exists, got nil")
	}
}

func TestFirefoxFallbackProfile_MissingDir(t *testing.T) {
	if _, err := firefoxFallbackProfile("/tmp/sendit-no-such-firefox-dir-" + strings.Repeat("x", 8)); err == nil {
		t.Fatal("expected error for missing directory, got nil")
	}
}

// --- firefoxDefaultProfile ---

func TestFirefoxDefaultProfile_WithValidINI(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("INI path differs on macOS — test targets linux layout")
	}

	dir := t.TempDir()
	profDir := filepath.Join(dir, "profile.default-release")
	if err := os.Mkdir(profDir, 0o755); err != nil {
		t.Fatal(err)
	}

	ini := "[Profile0]\nDefault=1\nPath=profile.default-release\n"
	if err := os.WriteFile(filepath.Join(dir, "profiles.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := firefoxDefaultProfile(dir)
	if err != nil {
		t.Fatalf("firefoxDefaultProfile: %v", err)
	}
	if got != profDir {
		t.Errorf("expected %q, got %q", profDir, got)
	}
}

func TestFirefoxDefaultProfile_FallsBackWithNoINI(t *testing.T) {
	dir := t.TempDir()
	profDir := filepath.Join(dir, "xyz.default")
	if err := os.Mkdir(profDir, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := firefoxDefaultProfile(dir)
	if err != nil {
		t.Fatalf("firefoxDefaultProfile fallback: %v", err)
	}
	if got != profDir {
		t.Errorf("expected %q, got %q", profDir, got)
	}
}

// TestHistoryDBInfo_Chrome verifies Chrome returns a valid path and query.
// chromePath constructs the path without checking that Chrome is installed,
// so this test works on any linux/darwin machine.
func TestHistoryDBInfo_Chrome(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("chrome path not supported on this OS")
	}
	path, query, err := historyDBInfo("chrome")
	if err != nil {
		t.Fatalf("historyDBInfo(chrome): %v", err)
	}
	if path == "" {
		t.Error("expected non-empty path")
	}
	if query == "" {
		t.Error("expected non-empty query")
	}
}

func TestHistoryDBInfo_UnsupportedBrowser(t *testing.T) {
	_, _, err := historyDBInfo("edge")
	if err == nil {
		t.Fatal("expected error for unsupported browser, got nil")
	}
}
