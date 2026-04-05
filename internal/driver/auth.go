package driver

import (
	"fmt"
	"net/http"
	"net/url"
	"os"

	"github.com/lewta/sendit/internal/config"
)

// applyAuth mutates req to add authentication headers or query parameters
// as specified by cfg. It resolves token values from literals or env vars at
// call time. Returns an error if a required env var is unset.
func applyAuth(req *http.Request, cfg config.AuthConfig) error {
	if cfg.Type == "" {
		return nil
	}

	switch cfg.Type {
	case "bearer":
		token, err := resolveValue(cfg.Token, cfg.TokenEnv, "token")
		if err != nil {
			return fmt.Errorf("auth: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)

	case "basic":
		username, err := resolveValue(cfg.Username, cfg.UsernameEnv, "username")
		if err != nil {
			return fmt.Errorf("auth: %w", err)
		}
		password, _ := resolveValue(cfg.Password, cfg.PasswordEnv, "password") // password is optional
		req.SetBasicAuth(username, password)

	case "header":
		token, err := resolveValue(cfg.Token, cfg.TokenEnv, "token")
		if err != nil {
			return fmt.Errorf("auth: %w", err)
		}
		req.Header.Set(cfg.HeaderName, token)

	case "query":
		token, err := resolveValue(cfg.Token, cfg.TokenEnv, "token")
		if err != nil {
			return fmt.Errorf("auth: %w", err)
		}
		q := req.URL.Query()
		q.Set(cfg.ParamName, token)
		req.URL.RawQuery = q.Encode()
	}

	return nil
}

// authHeaders returns an http.Header with the auth credentials applied, for
// use by drivers (e.g. WebSocket) that pass headers separately from the request.
func authHeaders(cfg config.AuthConfig) (http.Header, error) {
	if cfg.Type == "" || cfg.Type == "query" {
		return nil, nil
	}

	// Build a throwaway request so we can reuse applyAuth.
	req, _ := http.NewRequest(http.MethodGet, "http://placeholder", nil)
	if err := applyAuth(req, cfg); err != nil {
		return nil, err
	}
	return req.Header, nil
}

// authQueryURL returns urlStr with the auth query parameter appended, for use
// by drivers that embed credentials in the URL rather than a header.
func authQueryURL(urlStr string, cfg config.AuthConfig) (string, error) {
	if cfg.Type != "query" {
		return urlStr, nil
	}
	token, err := resolveValue(cfg.Token, cfg.TokenEnv, "token")
	if err != nil {
		return "", fmt.Errorf("auth: %w", err)
	}
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("auth: parsing url: %w", err)
	}
	q := u.Query()
	q.Set(cfg.ParamName, token)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// resolveValue returns literal if non-empty, otherwise looks up envVar in the
// environment. Returns an error if both are empty.
func resolveValue(literal, envVar, fieldName string) (string, error) {
	if literal != "" {
		return literal, nil
	}
	if envVar != "" {
		val := os.Getenv(envVar)
		if val == "" {
			return "", fmt.Errorf("env var %q (auth.%s_env) is not set", envVar, fieldName)
		}
		return val, nil
	}
	return "", fmt.Errorf("%s: neither literal value nor env var configured", fieldName)
}
