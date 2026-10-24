package driver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/output"
	"github.com/lewta/sendit/internal/task"
)

func redirectTask(rawURL string, auth config.AuthConfig, version int) task.Task {
	return task.Task{
		URL:  rawURL,
		Type: "http",
		Config: config.TargetConfig{
			URL:  rawURL,
			Type: "http",
			Auth: auth,
			HTTP: config.HTTPConfig{
				HTTPVersion:             version,
				Method:                  http.MethodGet,
				TimeoutS:                2,
				AllowCrossHostRedirects: true,
			},
		},
	}
}

func TestHTTPRedirectDowngradeDeniedBeforeDestination(t *testing.T) {
	const marker = "redirect-integrity-marker"
	tests := []struct {
		name string
		auth config.AuthConfig
	}{
		{name: "no auth"},
		{name: "bearer", auth: config.AuthConfig{Type: "bearer", Token: marker}},
		{name: "basic", auth: config.AuthConfig{Type: "basic", Username: "operator", Password: marker}},
		{name: "custom header", auth: config.AuthConfig{Type: "header", HeaderName: "X-API-Key", Token: marker}},
		{name: "query", auth: config.AuthConfig{Type: "query", ParamName: "api_key", Token: marker}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			var authorization, customHeader, query string
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				authorization = r.Header.Get("Authorization")
				customHeader = r.Header.Get("X-API-Key")
				query = r.URL.Query().Get("api_key")
				w.WriteHeader(http.StatusNoContent)
			}))
			defer destination.Close()

			source := versionServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, destination.URL+"/cleartext", http.StatusFound)
			}))
			driver := NewHTTPDriver()
			trustVersionServers(t, driver, source)

			result := driver.Execute(context.Background(), redirectTask(source.URL+"/start", tc.auth, 0))
			if result.Error == nil {
				t.Fatal("HTTPS-to-HTTP redirect succeeded; want a fail-closed error")
			}
			if hits.Load() != 0 {
				t.Fatalf("cleartext destination received %d requests, want 0", hits.Load())
			}
			if authorization != "" || customHeader != "" || query != "" {
				t.Fatalf("cleartext destination received credentials: authorization=%q header=%q query=%q", authorization, customHeader, query)
			}
			if strings.Contains(result.Error.Error(), marker) {
				t.Fatal("redirect rejection leaked credential in its error")
			}
		})
	}
}

func TestHTTPRedirectNonDowngradeCompatibility(t *testing.T) {
	t.Run("HTTP to HTTP", func(t *testing.T) {
		var hits atomic.Int32
		destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			w.WriteHeader(http.StatusNoContent)
		}))
		defer destination.Close()
		source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, destination.URL+"/next", http.StatusFound)
		}))
		defer source.Close()

		result := NewHTTPDriver().Execute(context.Background(), redirectTask(source.URL+"/start", config.AuthConfig{}, 0))
		if result.Error != nil || result.StatusCode != http.StatusNoContent || hits.Load() != 1 {
			t.Fatalf("HTTP redirect changed: hits=%d result=%+v", hits.Load(), result)
		}
	})

	t.Run("HTTPS to HTTPS preserves same-host bearer auth", func(t *testing.T) {
		const marker = "https-redirect-marker"
		var hits atomic.Int32
		var authorization string
		destination := versionServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			authorization = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusNoContent)
		}))
		source := versionServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, destination.URL+"/next", http.StatusFound)
		}))
		driver := NewHTTPDriver()
		trustVersionServers(t, driver, source, destination)

		result := driver.Execute(context.Background(), redirectTask(source.URL+"/start", config.AuthConfig{Type: "bearer", Token: marker}, 0))
		if result.Error != nil || result.StatusCode != http.StatusNoContent || hits.Load() != 1 {
			t.Fatalf("HTTPS redirect changed: hits=%d result=%+v", hits.Load(), result)
		}
		if authorization != "Bearer "+marker {
			t.Fatalf("same-host HTTPS redirect authorization=%q, want bearer token", authorization)
		}
	})
}

func TestHTTP2RedirectDowngradeRemainsBlockedBeforeDestination(t *testing.T) {
	var hits atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits.Add(1)
	}))
	defer destination.Close()
	source := versionServer(t, true, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/cleartext", http.StatusTemporaryRedirect)
	}))
	driver := NewHTTPDriver()
	trustVersionServers(t, driver, source)

	result := driver.Execute(context.Background(), redirectTask(source.URL+"/start", config.AuthConfig{Type: "bearer", Token: "h2-secret"}, 2))
	if result.Error == nil {
		t.Fatal("forced HTTP/2 downgrade succeeded")
	}
	if hits.Load() != 0 {
		t.Fatalf("cleartext destination received %d forced HTTP/2 requests, want 0", hits.Load())
	}
}

func TestHTTPRedirectCrossHostDowngradeDeniedBeforeDestination(t *testing.T) {
	var hits atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits.Add(1)
	}))
	defer destination.Close()
	crossHostURL := strings.Replace(destination.URL, "127.0.0.1", "localhost", 1)
	source := versionServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, crossHostURL+"/cleartext", http.StatusFound)
	}))
	driver := NewHTTPDriver()
	trustVersionServers(t, driver, source)

	result := driver.Execute(context.Background(), redirectTask(source.URL+"/start", config.AuthConfig{}, 0))
	if result.Error == nil {
		t.Fatal("cross-host HTTPS-to-HTTP redirect succeeded")
	}
	if hits.Load() != 0 {
		t.Fatalf("cleartext cross-host destination received %d requests, want 0", hits.Load())
	}
}

func TestHTTPRedirectDowngradeDeniedAfterSecureIntermediate(t *testing.T) {
	var hits atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits.Add(1)
	}))
	defer destination.Close()
	intermediate := versionServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/cleartext", http.StatusTemporaryRedirect)
	}))
	source := versionServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, intermediate.URL+"/second-hop", http.StatusTemporaryRedirect)
	}))
	driver := NewHTTPDriver()
	trustVersionServers(t, driver, source, intermediate)

	result := driver.Execute(context.Background(), redirectTask(source.URL+"/start", config.AuthConfig{Type: "header", HeaderName: "X-API-Key", Token: "multi-hop-secret"}, 0))
	if result.Error == nil {
		t.Fatal("multi-hop HTTPS-to-HTTP redirect succeeded")
	}
	if hits.Load() != 0 {
		t.Fatalf("cleartext destination received %d multi-hop requests, want 0", hits.Load())
	}
}

func TestReplayHTTPRedirectDowngradeDeniedBeforeDestination(t *testing.T) {
	var hits atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits.Add(1)
	}))
	defer destination.Close()
	source := versionServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/cleartext", http.StatusFound)
	}))
	driver := NewHTTPDriver()
	trustVersionServers(t, driver, source)
	replayed := output.ReplayRequest{
		URL:  source.URL + "/start",
		Type: "http",
		HTTP: &output.ReplayHTTP{
			Method:                  http.MethodGet,
			TimeoutS:                2,
			AllowCrossHostRedirects: true,
		},
	}.Task()

	result := driver.Execute(context.Background(), replayed)
	if result.Error == nil {
		t.Fatal("replayed HTTPS-to-HTTP redirect succeeded")
	}
	if hits.Load() != 0 {
		t.Fatalf("cleartext destination received %d replay requests, want 0", hits.Load())
	}
}
