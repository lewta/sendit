package driver

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/ratelimit"
	"github.com/lewta/sendit/internal/task"
)

func TestHTTPBodyRead(t *testing.T) {
	for _, tc := range []struct {
		name    string
		length  string
		body    string
		wantErr error
	}{
		{"truncated", "100", "x", io.ErrUnexpectedEOF},
		{"complete", "2", "ok", nil},
		{"empty", "0", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, token := range []string{"", "secret/value+?", "EOF"} {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if token != "" && r.URL.Query().Get("api_key") != token {
						t.Error("query authentication missing")
					}
					w.Header().Set("Content-Length", tc.length)
					w.WriteHeader(http.StatusAccepted)
					_, _ = io.WriteString(w, tc.body)
				}))
				defer srv.Close()
				original := task.Task{URL: srv.URL + "?keep=yes", Type: "http", Config: config.TargetConfig{HTTP: config.HTTPConfig{TimeoutS: 2}}}
				if token != "" {
					original.Config.Auth = config.AuthConfig{Type: "query", ParamName: "api_key", Token: token}
				}
				d := NewHTTPDriver()
				defer d.transports[0].CloseIdleConnections()
				result := d.Execute(context.Background(), original)
				if !errors.Is(result.Error, tc.wantErr) {
					t.Errorf("token=%q: error = %v, want %v", token, result.Error, tc.wantErr)
				}
				if token == "EOF" && tc.wantErr != nil {
					assertBodyErrorRedaction(t, result.Error, token)
				}
				if result.StatusCode != http.StatusAccepted || result.BytesRead != int64(len(tc.body)) || result.Meta["http_protocol"] != "HTTP/1.1" || result.Duration <= 0 {
					t.Errorf("response fields lost: %+v", result)
				}
				if !reflect.DeepEqual(result.Task, original) {
					t.Errorf("original task changed: %+v", result.Task)
				}
				if result.Error != nil && (strings.Contains(result.Error.Error(), "secret") || strings.Contains(result.Error.Error(), "api_key")) {
					t.Errorf("body error leaked authentication: %v", result.Error)
				}
			}
		})
	}
}

func TestHTTPBodyReadContext(t *testing.T) {
	for _, tc := range []struct {
		name     string
		token    string
		deadline bool
	}{
		{"canceled", "secret", false},
		{"deadline", "secret", true},
		{"canceled_token_collision", "canceled", false},
		{"deadline_token_collision", "deadline", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			wantErr := context.Canceled
			if tc.deadline {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), time.Second)
				wantErr = context.DeadlineExceeded
			}
			defer cancel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", "100")
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, "x")
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer srv.Close()
			d := NewHTTPDriver()
			defer d.transports[0].CloseIdleConnections()
			// Cancel only after the driver has received the partial body.
			d.client.Transport = bodyCancelTransport{base: d.client.Transport, cancel: cancel, deadline: tc.deadline}
			original := task.Task{URL: srv.URL, Type: "http", Config: config.TargetConfig{Auth: config.AuthConfig{Type: "query", ParamName: "api_key", Token: tc.token}}}
			result := d.Execute(ctx, original)
			if !errors.Is(result.Error, wantErr) || ratelimit.ClassifyError(result.Error) != ratelimit.ErrorClassFatal {
				t.Errorf("error = %v, want classifiable %v", result.Error, wantErr)
			}
			if tc.token != "secret" {
				assertBodyErrorRedaction(t, result.Error, tc.token)
			}
			if result.BytesRead != 1 || result.StatusCode != http.StatusOK || result.Meta["http_protocol"] != "HTTP/1.1" || !reflect.DeepEqual(result.Task, original) {
				t.Errorf("partial response fields lost: %+v", result)
			}
		})
	}
}

func assertBodyErrorRedaction(t *testing.T, err error, token string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("expected visibly redacted body error, got %v", err)
	}
	for current := err; current != nil; current = errors.Unwrap(current) {
		if strings.Contains(current.Error(), token) || strings.Contains(current.Error(), url.QueryEscape(token)) {
			t.Errorf("unwrap chain exposed token %q: %v", token, current)
		}
	}
}

func TestHTTPBodyReadRedactsQueryAuth(t *testing.T) {
	const token = "secret/value+?"
	d := NewHTTPDriver()
	d.client.Transport = bodyErrorTransport{message: token + " " + url.QueryEscape(token)}
	original := task.Task{URL: "http://example.test", Type: "http", Config: config.TargetConfig{Auth: config.AuthConfig{Type: "query", ParamName: "api_key", Token: token}}}
	result := d.Execute(context.Background(), original)
	if result.Error == nil {
		t.Fatal("body read error discarded")
	}
	for err := result.Error; err != nil; err = errors.Unwrap(err) {
		if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), url.QueryEscape(token)) {
			t.Errorf("error chain leaked query authentication: %v", err)
		}
	}
	if !reflect.DeepEqual(result.Task, original) || result.StatusCode != http.StatusOK || result.Meta["http_protocol"] != "HTTP/1.1" {
		t.Errorf("response fields lost: %+v", result)
	}
}

type bodyErrorTransport struct{ message string }

func (tr bodyErrorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Proto: "HTTP/1.1", Body: io.NopCloser(tr)}, nil
}

func (tr bodyErrorTransport) Read([]byte) (int, error) {
	return 0, errors.New(tr.message)
}

type bodyCancelTransport struct {
	base     http.RoundTripper
	cancel   context.CancelFunc
	deadline bool
}

func (tr bodyCancelTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := tr.base.RoundTrip(r)
	if err == nil && !tr.deadline {
		resp.Body = &bodyCancelReader{ReadCloser: resp.Body, cancel: tr.cancel}
	}
	return resp, err
}

type bodyCancelReader struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r *bodyCancelReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 {
		r.cancel()
	}
	return n, err
}
