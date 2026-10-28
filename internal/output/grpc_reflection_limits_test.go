package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lewta/sendit/internal/config"
)

func TestGRPCReflectionLimitsReplayRoundTrip(t *testing.T) {
	r := capturedResult("grpc", "grpc://example.com/pkg.Service/Method")
	r.Task.Config.GRPC = config.GRPCConfig{
		TimeoutS:                     5,
		ReflectionMaxDescriptors:     300,
		ReflectionMaxBytes:           5_000_000,
		ReflectionMaxDependencyDepth: 80,
	}
	var encoded bytes.Buffer
	if err := EncodeJSONL(json.NewEncoder(&encoded), r); err != nil {
		t.Fatal(err)
	}
	records, err := ReadReplay(&encoded)
	if err != nil {
		t.Fatal(err)
	}
	got := records[0].Envelope.Request.Task().Config.GRPC
	if got != r.Task.Config.GRPC {
		t.Fatalf("gRPC replay config = %+v, want %+v", got, r.Task.Config.GRPC)
	}
}

func TestGRPCReflectionLimitsReplayCompatibilityAndValidation(t *testing.T) {
	r := capturedResult("grpc", "grpc://example.com/pkg.Service/Method")
	var encoded bytes.Buffer
	if err := EncodeJSONL(json.NewEncoder(&encoded), r); err != nil {
		t.Fatal(err)
	}
	line := encoded.String()
	for _, field := range []string{"reflection_max_descriptors", "reflection_max_bytes", "reflection_max_dependency_depth"} {
		start := `,"` + field + `":`
		index := strings.Index(line, start)
		if index < 0 {
			t.Fatalf("encoded replay is missing %s: %s", field, line)
		}
		end := index + len(start)
		for end < len(line) && line[end] >= '0' && line[end] <= '9' {
			end++
		}
		line = line[:index] + line[end:]
	}
	if _, err := DecodeReplayRecord([]byte(line)); err != nil {
		t.Fatalf("legacy v2 gRPC replay rejected: %v", err)
	}

	invalid := strings.Replace(encoded.String(), `"reflection_max_descriptors":256`, `"reflection_max_descriptors":4097`, 1)
	if _, err := DecodeReplayRecord([]byte(invalid)); err == nil {
		t.Fatal("over-ceiling replay limit accepted")
	}
	legacyWithNewFields := strings.Replace(encoded.String(), `"version":2`, `"version":1`, 1)
	if _, err := DecodeReplayRecord([]byte(legacyWithNewFields)); err == nil {
		t.Fatal("v1 replay accepted v2 reflection limit fields")
	}
}
