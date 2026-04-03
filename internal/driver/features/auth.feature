Feature: Authentication for HTTP and WebSocket targets

  The auth block adds credentials to requests without embedding them in the
  URL or driver-specific headers. Tokens can be supplied as literals (for test
  environments) or resolved from environment variables at dispatch time.

  Background:
    Given a running HTTP server that records request headers and query params

  # ── Bearer ────────────────────────────────────────────────────────────────

  Scenario: Bearer token from literal value
    Given an HTTP target with auth type "bearer" and token "literal-secret"
    When the driver executes the request
    Then the server should receive header "Authorization" with value "Bearer literal-secret"

  Scenario: Bearer token from environment variable
    Given environment variable "TEST_BEARER_TOKEN" is set to "env-bearer-secret"
    And an HTTP target with auth type "bearer" and token_env "TEST_BEARER_TOKEN"
    When the driver executes the request
    Then the server should receive header "Authorization" with value "Bearer env-bearer-secret"

  Scenario: Bearer token env var not set returns an error result
    Given environment variable "MISSING_TOKEN" is unset
    And an HTTP target with auth type "bearer" and token_env "MISSING_TOKEN"
    When the driver executes the request
    Then the result should contain an error mentioning "MISSING_TOKEN"

  # ── Basic ─────────────────────────────────────────────────────────────────

  Scenario: Basic auth with literal username and password
    Given an HTTP target with auth type "basic", username "alice" and password "s3cr3t"
    When the driver executes the request
    Then the server should receive a valid Basic auth header for user "alice"

  Scenario: Basic auth with username and password from environment variables
    Given environment variable "TEST_BASIC_USER" is set to "bob"
    And environment variable "TEST_BASIC_PASS" is set to "p4ssw0rd"
    And an HTTP target with auth type "basic", username_env "TEST_BASIC_USER" and password_env "TEST_BASIC_PASS"
    When the driver executes the request
    Then the server should receive a valid Basic auth header for user "bob"

  # ── Header ────────────────────────────────────────────────────────────────

  Scenario: Custom header auth with literal token
    Given an HTTP target with auth type "header", header_name "X-API-Key" and token "my-api-key"
    When the driver executes the request
    Then the server should receive header "X-API-Key" with value "my-api-key"

  Scenario: Custom header auth with token from environment variable
    Given environment variable "TEST_API_KEY" is set to "env-api-key"
    And an HTTP target with auth type "header", header_name "X-API-Key" and token_env "TEST_API_KEY"
    When the driver executes the request
    Then the server should receive header "X-API-Key" with value "env-api-key"

  # ── Query ─────────────────────────────────────────────────────────────────

  Scenario: Query parameter auth with literal token
    Given an HTTP target with auth type "query", param_name "api_key" and token "query-secret"
    When the driver executes the request
    Then the server should receive query parameter "api_key" with value "query-secret"

  Scenario: Query parameter auth with token from environment variable
    Given environment variable "TEST_QUERY_TOKEN" is set to "env-query-secret"
    And an HTTP target with auth type "query", param_name "api_key" and token_env "TEST_QUERY_TOKEN"
    When the driver executes the request
    Then the server should receive query parameter "api_key" with value "env-query-secret"

  # ── No auth ───────────────────────────────────────────────────────────────

  Scenario: No auth block sends no Authorization header
    Given an HTTP target with no auth configured
    When the driver executes the request
    Then the server should not receive an "Authorization" header

  # ── auth does not override explicit headers ────────────────────────────────

  Scenario: Explicit http.headers take precedence and auth is still applied
    Given an HTTP target with auth type "bearer" and token "bearer-value"
    And the target has an explicit header "X-Custom" with value "custom-value"
    When the driver executes the request
    Then the server should receive header "Authorization" with value "Bearer bearer-value"
    And the server should receive header "X-Custom" with value "custom-value"
