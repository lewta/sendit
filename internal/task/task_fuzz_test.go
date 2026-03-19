package task

import (
	"testing"

	"github.com/lewta/sendit/internal/config"
)

// FuzzSelector fuzzes the Vose alias selector with arbitrary weight slices.
// Each input byte is treated as a target weight so the fuzzer can explore
// variable-length slices with any weight distribution, including edge cases
// like empty slices and all-zero weights.
func FuzzSelector(f *testing.F) {
	f.Add([]byte{1})           // single target
	f.Add([]byte{1, 2, 3})     // three equal-ish targets
	f.Add([]byte{255, 1, 128}) // skewed weights
	f.Add([]byte{0})           // all-zero → error expected
	f.Add([]byte{0, 0, 1})     // mixed zero and non-zero
	f.Add([]byte{})            // empty → error expected

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		targets := make([]config.TargetConfig, len(data))
		for i, b := range data {
			targets[i] = config.TargetConfig{
				URL:    "http://example.com",
				Type:   "http",
				Weight: int(b),
			}
		}
		sel, err := NewSelector(targets)
		if err != nil {
			// Expected for all-zero weight slices.
			return
		}
		_ = sel.Pick()
	})
}
