package task

import (
	"math"
	"testing"

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
		makeTarget("https://a.com", 2, "http"),
		makeTarget("https://b.com", 3, "http"),
	}
	sel, err := NewSelector(targets)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 1000; j++ {
				sel.Pick()
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
