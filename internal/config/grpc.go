package config

import "fmt"

const (
	defaultGRPCReflectionMaxDescriptors     = 256
	defaultGRPCReflectionMaxBytes           = 4 << 20
	defaultGRPCReflectionMaxDependencyDepth = 64

	absoluteGRPCReflectionMaxDescriptors     = 4096
	absoluteGRPCReflectionMaxBytes           = 16 << 20
	absoluteGRPCReflectionMaxDependencyDepth = 256
)

// GRPCReflectionLimits contains the effective, non-zero descriptor processing
// limits used by the gRPC driver.
type GRPCReflectionLimits struct {
	MaxDescriptors     int
	MaxBytes           int
	MaxDependencyDepth int
}

// ResolveGRPCReflectionLimits applies secure defaults and rejects requested
// values outside the absolute ceilings. It is shared by file-loaded config,
// programmatic tasks, and replay preflight.
func ResolveGRPCReflectionLimits(cfg GRPCConfig) (GRPCReflectionLimits, error) {
	resolve := func(value, fallback, ceiling int, field string) (int, error) {
		if value < 0 || value > ceiling {
			return 0, fmt.Errorf("%s must be 0 or in [1, %d]", field, ceiling)
		}
		if value == 0 {
			return fallback, nil
		}
		return value, nil
	}

	maxDescriptors, err := resolve(cfg.ReflectionMaxDescriptors, defaultGRPCReflectionMaxDescriptors, absoluteGRPCReflectionMaxDescriptors, "reflection_max_descriptors")
	if err != nil {
		return GRPCReflectionLimits{}, err
	}
	maxBytes, err := resolve(cfg.ReflectionMaxBytes, defaultGRPCReflectionMaxBytes, absoluteGRPCReflectionMaxBytes, "reflection_max_bytes")
	if err != nil {
		return GRPCReflectionLimits{}, err
	}
	maxDepth, err := resolve(cfg.ReflectionMaxDependencyDepth, defaultGRPCReflectionMaxDependencyDepth, absoluteGRPCReflectionMaxDependencyDepth, "reflection_max_dependency_depth")
	if err != nil {
		return GRPCReflectionLimits{}, err
	}
	return GRPCReflectionLimits{
		MaxDescriptors:     maxDescriptors,
		MaxBytes:           maxBytes,
		MaxDependencyDepth: maxDepth,
	}, nil
}
