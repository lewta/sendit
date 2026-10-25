package policy

import "context"

type accessContextKey struct{}

// WithAccess attaches immutable operator authority to one execution path.
func WithAccess(ctx context.Context, access Access) context.Context {
	return context.WithValue(ctx, accessContextKey{}, access)
}

// FromContext returns attached authority. Callers outside config-driven engine
// execution retain the historical trusted behavior.
func FromContext(ctx context.Context) Access {
	if access, ok := ctx.Value(accessContextKey{}).(Access); ok && (access.Mode() == ModeTrusted || access.Mode() == ModeRestricted) {
		return access
	}
	return Trusted()
}
