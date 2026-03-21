package task

import (
	"fmt"
	"testing"

	"github.com/lewta/sendit/internal/config"
)

func makeTargets(n int) []config.TargetConfig {
	targets := make([]config.TargetConfig, n)
	for i := range targets {
		targets[i] = config.TargetConfig{
			URL:    fmt.Sprintf("http://example%d.com", i),
			Type:   "http",
			Weight: i + 1,
		}
	}
	return targets
}

// BenchmarkSelectorPick verifies O(1) behaviour across different fleet sizes.
func BenchmarkSelectorPick(b *testing.B) {
	for _, n := range []int{1, 10, 100} {
		sel, err := NewSelector(makeTargets(n))
		if err != nil {
			b.Fatalf("NewSelector(%d): %v", n, err)
		}
		b.Run(fmt.Sprintf("targets=%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = sel.Pick()
			}
		})
	}
}
