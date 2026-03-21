package ratelimit

import (
	"context"
	"errors"
	"testing"
)

var statusCodes = []int{200, 204, 400, 403, 404, 429, 500, 502, 503, 504}

func BenchmarkClassifyStatusCode(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = ClassifyStatusCode(statusCodes[i%len(statusCodes)])
	}
}

var errMsgs = []error{
	errors.New("connection refused"),
	errors.New("context canceled"),
	errors.New("timeout"),
	errors.New("EOF"),
	errors.New("no such host"),
}

func BenchmarkClassifyError(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = ClassifyError(errMsgs[i%len(errMsgs)])
	}
}

// BenchmarkRegistryWait measures token-bucket acquire overhead at a rate high
// enough that Wait never blocks (throughput path only, no queuing).
func BenchmarkRegistryWait(b *testing.B) {
	r := NewRegistry(1e9, nil)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Wait(ctx, "example.com")
	}
}
