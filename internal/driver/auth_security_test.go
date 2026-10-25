package driver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/policy"
	"github.com/lewta/sendit/internal/task"
)

func TestHTTPBasicPasswordResolutionErrorIsSafeAndSendsNoRequest(t *testing.T) {
	const (
		envName = "SENDIT_PASSWORD_ENV_IDENTIFIER"
		secret  = "password-value-marker"
	)

	denied, err := policy.New(policy.ModeRestricted, policy.Grants{})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name  string
		ctx   context.Context
		value string
	}{
		{
			name:  "restricted policy denies populated environment variable",
			ctx:   policy.WithAccess(t.Context(), denied),
			value: secret,
		},
		{
			name:  "trusted policy sees missing environment variable",
			ctx:   policy.WithAccess(t.Context(), policy.Trusted()),
			value: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(envName, tc.value)
			transport := new(authBoundaryTransport)
			driver := NewHTTPDriver()
			driver.client.Transport = transport
			result := driver.Execute(tc.ctx, task.Task{
				URL:  "http://example.test",
				Type: "http",
				Config: config.TargetConfig{
					HTTP: config.HTTPConfig{TimeoutS: 2},
					Auth: config.AuthConfig{
						Type:        "basic",
						Username:    "operator",
						PasswordEnv: envName,
					},
				},
			})

			if result.Error == nil || result.Error.Error() != "auth: password resolution failed" {
				t.Fatalf("error = %v, want constant password-resolution error", result.Error)
			}
			for current := result.Error; current != nil; current = errors.Unwrap(current) {
				for _, sensitive := range []string{envName, secret} {
					if strings.Contains(current.Error(), sensitive) {
						t.Errorf("error layer %T exposed %q: %v", current, sensitive, current)
					}
				}
			}
			logged := fmt.Sprintf("ERR %v", result.Error)
			for _, sensitive := range []string{envName, secret} {
				if strings.Contains(logged, sensitive) {
					t.Errorf("log representation exposed %q: %s", sensitive, logged)
				}
			}
			if transport.calls != 0 {
				t.Errorf("password resolution failure sent %d requests, want 0", transport.calls)
			}
		})
	}
}

func TestApplyAuthPreservesNonPasswordEnvironmentDiagnostics(t *testing.T) {
	const envName = "SENDIT_MISSING_BEARER_TOKEN"
	t.Setenv(envName, "")
	req, err := http.NewRequest(http.MethodGet, "http://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = applyAuth(policy.WithAccess(t.Context(), policy.Trusted()), req, config.AuthConfig{
		Type:     "bearer",
		TokenEnv: envName,
	})
	if err == nil || !strings.Contains(err.Error(), envName) {
		t.Fatalf("error = %v, want non-password environment identifier", err)
	}
}

func TestAuthHeadersBasicPasswordResolutionErrorIsSafe(t *testing.T) {
	const envName = "SENDIT_MISSING_WEBSOCKET_PASSWORD"
	t.Setenv(envName, "")
	headers, err := authHeaders(policy.WithAccess(t.Context(), policy.Trusted()), config.AuthConfig{
		Type:        "basic",
		Username:    "operator",
		PasswordEnv: envName,
	})
	if headers != nil {
		t.Errorf("headers = %v, want nil", headers)
	}
	if !errors.Is(err, errPasswordResolution) {
		t.Fatalf("error = %v, want constant password-resolution error", err)
	}
	if strings.Contains(err.Error(), envName) {
		t.Errorf("error exposed password environment identifier: %v", err)
	}
}

type authBoundaryTransport struct{ calls int }

func (tr *authBoundaryTransport) RoundTrip(*http.Request) (*http.Response, error) {
	tr.calls++
	return nil, errors.New("unexpected request")
}
