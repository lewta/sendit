package features_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/cucumber/godog"
	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/driver"
	"github.com/lewta/sendit/internal/task"
)

// scenarioState holds per-scenario mutable state.
type scenarioState struct {
	server      *httptest.Server
	lastHeaders http.Header
	lastQuery   map[string]string
	target      config.TargetConfig
	result      task.Result
}

func newScenarioState() *scenarioState {
	return &scenarioState{}
}

// ── Background ───────────────────────────────────────────────────────────────

func (s *scenarioState) aRunningHTTPServerThatRecordsRequestHeadersAndQueryParams() {
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.lastHeaders = r.Header.Clone()
		s.lastQuery = make(map[string]string)
		for k, v := range r.URL.Query() {
			s.lastQuery[k] = v[0]
		}
		w.WriteHeader(http.StatusOK)
	}))
	s.target = config.TargetConfig{
		URL:    s.server.URL,
		Weight: 1,
		Type:   "http",
	}
}

// ── Env var helpers ───────────────────────────────────────────────────────────

func (s *scenarioState) environmentVariableIsSetTo(name, value string) {
	os.Setenv(name, value) //nolint:errcheck,gosec
}

func (s *scenarioState) environmentVariableIsUnset(name string) {
	os.Unsetenv(name) //nolint:errcheck,gosec
}

// ── Target setup ─────────────────────────────────────────────────────────────

func (s *scenarioState) anHTTPTargetWithAuthTypeAndToken(authType, token string) {
	s.target.Auth = config.AuthConfig{Type: authType, Token: token}
}

func (s *scenarioState) anHTTPTargetWithAuthTypeAndTokenEnv(authType, tokenEnv string) {
	s.target.Auth = config.AuthConfig{Type: authType, TokenEnv: tokenEnv}
}

func (s *scenarioState) anHTTPTargetWithBasicAuthLiteral(username, password string) {
	s.target.Auth = config.AuthConfig{Type: "basic", Username: username, Password: password}
}

func (s *scenarioState) anHTTPTargetWithBasicAuthEnv(usernameEnv, passwordEnv string) {
	s.target.Auth = config.AuthConfig{Type: "basic", UsernameEnv: usernameEnv, PasswordEnv: passwordEnv}
}

func (s *scenarioState) anHTTPTargetWithHeaderAuth(headerName, token string) {
	s.target.Auth = config.AuthConfig{Type: "header", HeaderName: headerName, Token: token}
}

func (s *scenarioState) anHTTPTargetWithHeaderAuthEnv(headerName, tokenEnv string) {
	s.target.Auth = config.AuthConfig{Type: "header", HeaderName: headerName, TokenEnv: tokenEnv}
}

func (s *scenarioState) anHTTPTargetWithQueryAuth(paramName, token string) {
	s.target.Auth = config.AuthConfig{Type: "query", ParamName: paramName, Token: token}
}

func (s *scenarioState) anHTTPTargetWithQueryAuthEnv(paramName, tokenEnv string) {
	s.target.Auth = config.AuthConfig{Type: "query", ParamName: paramName, TokenEnv: tokenEnv}
}

func (s *scenarioState) anHTTPTargetWithNoAuthConfigured() {
	s.target.Auth = config.AuthConfig{}
}

func (s *scenarioState) theTargetHasAnExplicitHeaderWithValue(name, value string) {
	if s.target.HTTP.Headers == nil {
		s.target.HTTP.Headers = make(map[string]string)
	}
	s.target.HTTP.Headers[name] = value
}

// ── Execution ────────────────────────────────────────────────────────────────

func (s *scenarioState) theDriverExecutesTheRequest() {
	d := driver.NewHTTPDriver()
	t := task.Task{
		URL:    s.target.URL,
		Type:   s.target.Type,
		Config: s.target,
	}
	s.result = d.Execute(context.Background(), t)
}

// ── Assertions ───────────────────────────────────────────────────────────────

func (s *scenarioState) theServerShouldReceiveHeaderWithValue(header, value string) error {
	got := s.lastHeaders.Get(header)
	if got != value {
		return fmt.Errorf("expected header %q = %q, got %q", header, value, got)
	}
	return nil
}

func (s *scenarioState) theServerShouldReceiveAValidBasicAuthHeaderForUser(username string) error {
	auth := s.lastHeaders.Get("Authorization")
	if !strings.HasPrefix(auth, "Basic ") {
		return fmt.Errorf("expected Basic auth header, got %q", auth)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(auth, "Basic "))
	if err != nil {
		return fmt.Errorf("decoding Basic auth: %w", err)
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if parts[0] != username {
		return fmt.Errorf("expected username %q, got %q", username, parts[0])
	}
	return nil
}

func (s *scenarioState) theServerShouldReceiveQueryParameterWithValue(param, value string) error {
	got, ok := s.lastQuery[param]
	if !ok {
		return fmt.Errorf("query parameter %q not present", param)
	}
	if got != value {
		return fmt.Errorf("expected query param %q = %q, got %q", param, value, got)
	}
	return nil
}

func (s *scenarioState) theServerShouldNotReceiveAnAuthorizationHeader(header string) error {
	if got := s.lastHeaders.Get(header); got != "" {
		return fmt.Errorf("expected no %q header, but got %q", header, got)
	}
	return nil
}

func (s *scenarioState) theResultShouldContainAnErrorMentioning(substr string) error {
	if s.result.Error == nil {
		return fmt.Errorf("expected an error containing %q, but result had no error", substr)
	}
	if !strings.Contains(s.result.Error.Error(), substr) {
		return fmt.Errorf("expected error to mention %q, got %q", substr, s.result.Error.Error())
	}
	return nil
}

// ── Cleanup ───────────────────────────────────────────────────────────────────

func (s *scenarioState) cleanup() {
	if s.server != nil {
		s.server.Close()
	}
}

// ── Suite wiring ─────────────────────────────────────────────────────────────

func initScenario(ctx *godog.ScenarioContext) {
	s := newScenarioState()

	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		s.cleanup()
		return ctx, nil
	})

	// Background
	ctx.Step(`^a running HTTP server that records request headers and query params$`, s.aRunningHTTPServerThatRecordsRequestHeadersAndQueryParams)

	// Env var management
	ctx.Step(`^environment variable "([^"]*)" is set to "([^"]*)"$`, s.environmentVariableIsSetTo)
	ctx.Step(`^environment variable "([^"]*)" is unset$`, s.environmentVariableIsUnset)

	// Target setup — bearer/query (single token field)
	ctx.Step(`^an HTTP target with auth type "([^"]*)" and token "([^"]*)"$`, s.anHTTPTargetWithAuthTypeAndToken)
	ctx.Step(`^an HTTP target with auth type "([^"]*)" and token_env "([^"]*)"$`, s.anHTTPTargetWithAuthTypeAndTokenEnv)

	// Target setup — basic
	ctx.Step(`^an HTTP target with auth type "basic", username "([^"]*)" and password "([^"]*)"$`, s.anHTTPTargetWithBasicAuthLiteral)
	ctx.Step(`^an HTTP target with auth type "basic", username_env "([^"]*)" and password_env "([^"]*)"$`, s.anHTTPTargetWithBasicAuthEnv)

	// Target setup — header
	ctx.Step(`^an HTTP target with auth type "header", header_name "([^"]*)" and token "([^"]*)"$`, s.anHTTPTargetWithHeaderAuth)
	ctx.Step(`^an HTTP target with auth type "header", header_name "([^"]*)" and token_env "([^"]*)"$`, s.anHTTPTargetWithHeaderAuthEnv)

	// Target setup — query
	ctx.Step(`^an HTTP target with auth type "query", param_name "([^"]*)" and token "([^"]*)"$`, s.anHTTPTargetWithQueryAuth)
	ctx.Step(`^an HTTP target with auth type "query", param_name "([^"]*)" and token_env "([^"]*)"$`, s.anHTTPTargetWithQueryAuthEnv)

	// Target setup — no auth / explicit headers
	ctx.Step(`^an HTTP target with no auth configured$`, s.anHTTPTargetWithNoAuthConfigured)
	ctx.Step(`^the target has an explicit header "([^"]*)" with value "([^"]*)"$`, s.theTargetHasAnExplicitHeaderWithValue)

	// Execution
	ctx.Step(`^the driver executes the request$`, s.theDriverExecutesTheRequest)

	// Assertions
	ctx.Step(`^the server should receive header "([^"]*)" with value "([^"]*)"$`, s.theServerShouldReceiveHeaderWithValue)
	ctx.Step(`^the server should receive a valid Basic auth header for user "([^"]*)"$`, s.theServerShouldReceiveAValidBasicAuthHeaderForUser)
	ctx.Step(`^the server should receive query parameter "([^"]*)" with value "([^"]*)"$`, s.theServerShouldReceiveQueryParameterWithValue)
	ctx.Step(`^the server should not receive an "([^"]*)" header$`, s.theServerShouldNotReceiveAnAuthorizationHeader)
	ctx.Step(`^the result should contain an error mentioning "([^"]*)"$`, s.theResultShouldContainAnErrorMentioning)
}

func TestAuthFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: initScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"auth.feature"},
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("godog auth feature tests failed")
	}
}
