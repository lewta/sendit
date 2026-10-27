package config

import (
	"strings"
	"testing"
)

func TestGRPCReflectionLimitsDefaultsAndOverrides(t *testing.T) {
	cfg, err := Load(writeTemp(t, `
target_defaults:
  grpc:
    reflection_max_descriptors: 300
    reflection_max_bytes: 5000000
    reflection_max_dependency_depth: 80
targets:
  - url: grpc://example.com/pkg.Service/Method
    type: grpc
    weight: 1
    grpc:
      reflection_max_descriptors: 400
      reflection_max_bytes: 6000000
      reflection_max_dependency_depth: 90
`))
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := ResolveGRPCReflectionLimits(cfg.TargetDefaults.GRPC)
	if err != nil {
		t.Fatal(err)
	}
	if defaults != (GRPCReflectionLimits{MaxDescriptors: 300, MaxBytes: 5000000, MaxDependencyDepth: 80}) {
		t.Fatalf("default limits = %+v", defaults)
	}
	target, err := ResolveGRPCReflectionLimits(cfg.Targets[0].GRPC)
	if err != nil {
		t.Fatal(err)
	}
	if target != (GRPCReflectionLimits{MaxDescriptors: 400, MaxBytes: 6000000, MaxDependencyDepth: 90}) {
		t.Fatalf("target limits = %+v", target)
	}

	implicit, err := ResolveGRPCReflectionLimits(GRPCConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if implicit != (GRPCReflectionLimits{MaxDescriptors: 256, MaxBytes: 4 << 20, MaxDependencyDepth: 64}) {
		t.Fatalf("implicit limits = %+v", implicit)
	}
}

func TestGRPCReflectionLimitsRejectInvalidValues(t *testing.T) {
	if _, err := ResolveGRPCReflectionLimits(GRPCConfig{
		ReflectionMaxDescriptors:     4096,
		ReflectionMaxBytes:           16 << 20,
		ReflectionMaxDependencyDepth: 256,
	}); err != nil {
		t.Fatalf("absolute ceilings rejected: %v", err)
	}

	tests := []struct {
		field string
		value string
	}{
		{"reflection_max_descriptors", "-1"},
		{"reflection_max_descriptors", "4097"},
		{"reflection_max_bytes", "-1"},
		{"reflection_max_bytes", "16777217"},
		{"reflection_max_dependency_depth", "-1"},
		{"reflection_max_dependency_depth", "257"},
	}
	for _, test := range tests {
		t.Run(test.field+"="+test.value, func(t *testing.T) {
			_, err := Load(writeTemp(t, "targets:\n  - url: grpc://example.com/pkg.Service/Method\n    type: grpc\n    weight: 1\n    grpc: {"+test.field+": "+test.value+"}\n"))
			if err == nil || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("error = %v, want %s validation", err, test.field)
			}
		})
	}
}

func TestGRPCReflectionLimitsProgrammaticValidation(t *testing.T) {
	cfg, err := Load(writeTemp(t, "targets:\n  - url: grpc://example.com/pkg.Service/Method\n    type: grpc\n    weight: 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Targets[0].GRPC.ReflectionMaxDescriptors = 4097
	if err := validate(cfg); err == nil || !strings.Contains(err.Error(), "targets[0].grpc.reflection_max_descriptors") {
		t.Fatalf("error = %v, want programmatic limit validation", err)
	}
}
