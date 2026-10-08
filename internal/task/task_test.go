package task

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lewta/sendit/internal/config"
)

func makeTarget(url string, weight int, typ string) config.TargetConfig {
	return config.TargetConfig{URL: url, Weight: weight, Type: typ}
}

// TestNewSelector_Empty ensures an empty slice is rejected.
func TestNewSelector_Empty(t *testing.T) {
	_, err := NewSelector(nil)
	if err == nil {
		t.Fatal("expected error for nil targets, got nil")
	}
	_, err = NewSelector([]config.TargetConfig{})
	if err == nil {
		t.Fatal("expected error for empty targets, got nil")
	}
}

// TestNewSelector_ZeroWeight ensures zero total weight is rejected.
func TestNewSelector_ZeroWeight(t *testing.T) {
	targets := []config.TargetConfig{
		makeTarget("https://a.com", 0, "http"),
	}
	_, err := NewSelector(targets)
	if err == nil {
		t.Fatal("expected error for zero total weight, got nil")
	}
}

// TestNewSelector_Single ensures a single target is always selected.
func TestNewSelector_Single(t *testing.T) {
	targets := []config.TargetConfig{
		makeTarget("https://only.com", 5, "http"),
	}
	sel, err := NewSelector(targets)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := 0; i < 100; i++ {
		tk := sel.Pick()
		if tk.URL != "https://only.com" {
			t.Errorf("pick %d: got URL %q, want https://only.com", i, tk.URL)
		}
	}
}

// TestPick_FieldMapping ensures Pick propagates all TargetConfig fields.
func TestPick_FieldMapping(t *testing.T) {
	targets := []config.TargetConfig{
		{
			URL:    "https://mapped.com",
			Weight: 1,
			Type:   "browser",
			Browser: config.BrowserConfig{
				Scroll:   true,
				TimeoutS: 30,
			},
		},
	}
	sel, err := NewSelector(targets)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tk := sel.Pick()
	if tk.URL != "https://mapped.com" {
		t.Errorf("URL = %q, want https://mapped.com", tk.URL)
	}
	if tk.Type != "browser" {
		t.Errorf("Type = %q, want browser", tk.Type)
	}
	if !tk.Config.Browser.Scroll {
		t.Error("Scroll should be true")
	}
}

// TestPick_WeightedDistribution uses chi-square test to verify the
// Vose alias method produces the correct distribution.
func TestPick_WeightedDistribution(t *testing.T) {
	targets := []config.TargetConfig{
		makeTarget("https://a.com", 1, "http"), // 10%
		makeTarget("https://b.com", 3, "http"), // 30%
		makeTarget("https://c.com", 6, "http"), // 60%
	}
	sel, err := NewSelector(targets)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	const iterations = 10_000
	counts := make(map[string]int, 3)
	for i := 0; i < iterations; i++ {
		tk := sel.Pick()
		counts[tk.URL]++
	}

	expected := map[string]float64{
		"https://a.com": 0.10,
		"https://b.com": 0.30,
		"https://c.com": 0.60,
	}

	// Allow ±5% absolute tolerance for statistical noise at N=10,000.
	const tol = 0.05
	for url, want := range expected {
		got := float64(counts[url]) / float64(iterations)
		if math.Abs(got-want) > tol {
			t.Errorf("URL %s: frequency = %.3f, want %.3f ± %.3f", url, got, want, tol)
		}
	}
}

// TestPick_EqualWeights ensures equal weights give roughly equal frequencies.
func TestPick_EqualWeights(t *testing.T) {
	n := 4
	targets := make([]config.TargetConfig, n)
	for i := 0; i < n; i++ {
		targets[i] = makeTarget("https://"+string(rune('a'+i))+".com", 1, "http")
	}
	sel, err := NewSelector(targets)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	const iterations = 8_000
	counts := make(map[string]int, n)
	for i := 0; i < iterations; i++ {
		tk := sel.Pick()
		counts[tk.URL]++
	}

	want := 1.0 / float64(n)
	const tol = 0.05
	for url, c := range counts {
		got := float64(c) / float64(iterations)
		if math.Abs(got-want) > tol {
			t.Errorf("URL %s: frequency = %.3f, want %.3f ± %.3f", url, got, want, tol)
		}
	}
}

// TestPick_ConcurrentSafety runs many concurrent goroutines to surface data races.
func TestPick_ConcurrentSafety(t *testing.T) {
	targets := []config.TargetConfig{
		makeTarget("https://a.com/{{seq}}", 1, "http"),
	}
	sel, err := NewSelector(targets)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	values := make(chan uint64, 10_000)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				value, err := strconv.ParseUint(strings.TrimPrefix(sel.Pick().URL, "https://a.com/"), 10, 64)
				if err != nil {
					t.Errorf("parsing sequence: %v", err)
					return
				}
				values <- value
			}
		}()
	}
	wg.Wait()
	close(values)
	seen := make(map[uint64]bool, 10_000)
	for value := range values {
		if seen[value] {
			t.Fatalf("duplicate sequence %d", value)
		}
		seen[value] = true
	}
	if len(seen) != 10_000 {
		t.Fatalf("got %d sequences, want 10000", len(seen))
	}
}

func TestPickExpandsOneValueAcrossRequest(t *testing.T) {
	before := time.Now().Unix()
	target := config.TargetConfig{
		URL:    "https://example.com/{{user}}/{{seq}}/{{uuid}}/{{timestamp}}",
		Type:   "http",
		Weight: 1,
		Vars:   map[string][]string{"user": {"alice"}},
		HTTP: config.HTTPConfig{
			Body: `{"user":"{{user}}","seq":{{seq}}}`,
		},
		GRPC: config.GRPCConfig{
			Body: `{"user":"{{user}}"}`,
		},
		WebSocket: config.WebSocketConfig{
			SendMessages: []string{"{{user}}-{{seq}}"},
		},
	}
	sel, err := NewSelector([]config.TargetConfig{target})
	if err != nil {
		t.Fatal(err)
	}

	got := sel.Pick()
	parts := strings.Split(strings.TrimPrefix(got.URL, "https://example.com/"), "/")
	if len(parts) != 4 {
		t.Fatalf("expanded URL = %q", got.URL)
	}
	if parts[0] != "alice" || parts[1] != "1" {
		t.Fatalf("expanded URL = %q", got.URL)
	}
	if matched := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(parts[2]); !matched {
		t.Fatalf("UUID = %q, want UUIDv4", parts[2])
	}
	timestamp, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil || timestamp < before || timestamp > time.Now().Unix() {
		t.Fatalf("timestamp = %q", parts[3])
	}
	if got.Config.HTTP.Body != `{"user":"alice","seq":1}` {
		t.Fatalf("HTTP body = %q", got.Config.HTTP.Body)
	}
	if got.Config.GRPC.Body != `{"user":"alice"}` {
		t.Fatalf("gRPC body = %q", got.Config.GRPC.Body)
	}
	if got.Config.WebSocket.SendMessages[0] != "alice-1" {
		t.Fatalf("WebSocket message = %q", got.Config.WebSocket.SendMessages[0])
	}
	if target.URL != "https://example.com/{{user}}/{{seq}}/{{uuid}}/{{timestamp}}" || target.WebSocket.SendMessages[0] != "{{user}}-{{seq}}" {
		t.Fatal("Pick mutated source target")
	}
}

func TestPickExpandsCandidatesAndSequence(t *testing.T) {
	target := config.TargetConfig{
		URL:    "https://example.com/{{user}}/{{seq}}",
		Type:   "http",
		Weight: 1,
		Vars:   map[string][]string{"user": {"alice", "bob"}},
	}
	sel, err := NewSelector([]config.TargetConfig{target})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		parts := strings.Split(strings.TrimPrefix(sel.Pick().URL, "https://example.com/"), "/")
		if len(parts) != 2 || parts[0] != "alice" && parts[0] != "bob" || parts[1] != strconv.Itoa(i) {
			t.Fatalf("pick %d URL parts = %v", i, parts)
		}
	}
}

func TestTemplateExpansionDoesNotRecurse(t *testing.T) {
	target := config.TargetConfig{
		URL:    "https://example.com/{{first}}/{{second}}",
		Type:   "http",
		Weight: 1,
		Vars: map[string][]string{
			"first":  {`a$\\{{second}}`},
			"second": {"世界"},
		},
	}
	sel, err := NewSelector([]config.TargetConfig{target})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := sel.Pick().URL, `https://example.com/a$\\{{second}}/世界`; got != want {
		t.Fatalf("URL = %q, want %q", got, want)
	}
}

func TestExamplesExpandEveryTargetIndependently(t *testing.T) {
	targets := []config.TargetConfig{
		makeTarget("https://a.com/{{seq}}", 1, "http"),
		makeTarget("https://b.com/{{seq}}", 1, "http"),
	}
	sel, err := NewSelector(targets)
	if err != nil {
		t.Fatal(err)
	}
	first := sel.Examples()
	second := sel.Examples()
	if first[0].URL != "https://a.com/1" || first[1].URL != "https://b.com/1" {
		t.Fatalf("first examples = %q, %q", first[0].URL, first[1].URL)
	}
	if second[0].URL != "https://a.com/2" || second[1].URL != "https://b.com/2" {
		t.Fatalf("second examples = %q, %q", second[0].URL, second[1].URL)
	}
}

func TestPickLeavesUntemplatedTargetUnchanged(t *testing.T) {
	target := makeTarget("https://example.com", 1, "http")
	sel, err := NewSelector([]config.TargetConfig{target})
	if err != nil {
		t.Fatal(err)
	}
	if got := sel.Pick(); got.URL != target.URL || got.Config.URL != target.URL {
		t.Fatalf("Pick() = %#v", got)
	}
}

func TestNewSelectorRejectsInvalidTemplate(t *testing.T) {
	tests := []config.TargetConfig{
		{URL: "https://example.com/{{missing}}", Type: "http", Weight: 1},
		{URL: "https://example.com/{{name", Type: "http", Weight: 1, Vars: map[string][]string{"name": {"alice"}}},
	}
	for _, target := range tests {
		if _, err := NewSelector([]config.TargetConfig{target}); err == nil {
			t.Fatalf("NewSelector(%q) returned nil error", target.URL)
		}
	}
}
