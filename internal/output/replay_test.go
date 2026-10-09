package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/task"
)

func capturedResult(typ, url string) task.Result {
	r := makeResult(url, typ, 200, 42*time.Millisecond, 1, nil)
	r.Capture = task.Capture{RunID: "405d88de-3fb2-42d8-bfb7-f365907ed78c", Sequence: 1, StartedAt: time.Date(2026, 10, 8, 0, 0, 0, 1, time.UTC)}
	return r
}

func TestReplaySnapshotDefaultsAndRoundTrip(t *testing.T) {
	for _, typ := range []string{"http", "browser", "dns", "websocket", "grpc"} {
		t.Run(typ, func(t *testing.T) {
			url := map[string]string{"http": "https://example.com", "browser": "https://example.com", "dns": "example.com", "websocket": "wss://example.com", "grpc": "grpcs://example.com/pkg.Service/Method"}[typ]
			r := capturedResult(typ, url)
			r.Task.Config.HTTP.Body = `{"nested":{"literal":"{{uuid}}"}}`
			before := r.Task.Config
			e := replayEnvelope(r)
			if e == nil || !e.Replayable || e.Request == nil {
				t.Fatalf("envelope = %+v", e)
			}
			got := e.Request.Task()
			if got.URL != url || got.Config.URL != url || got.Type != typ || got.Config.Type != typ {
				t.Fatalf("task = %+v", got)
			}
			switch typ {
			case "http":
				if got.Config.HTTP.Method != "GET" || got.Config.HTTP.TimeoutS != 15 || got.Config.HTTP.Body != before.HTTP.Body {
					t.Fatal(got.Config.HTTP)
				}
			case "browser":
				if got.Config.Browser.TimeoutS != 30 {
					t.Fatal(got.Config.Browser)
				}
			case "dns":
				if got.Config.DNS.Resolver != "8.8.8.8:53" || got.Config.DNS.RecordType != "A" {
					t.Fatal(got.Config.DNS)
				}
			case "websocket":
				if got.Config.WebSocket.DurationS != 10 || got.Config.WebSocket.SendMessages == nil {
					t.Fatal(got.Config.WebSocket)
				}
			case "grpc":
				if got.Config.GRPC.TimeoutS != 15 || !got.Config.GRPC.TLS {
					t.Fatal(got.Config.GRPC)
				}
			}
			if !reflect.DeepEqual(before, r.Task.Config) {
				t.Fatal("mutated source")
			}
		})
	}
}

func TestReplaySnapshotNondefaults(t *testing.T) {
	cases := []config.TargetConfig{
		{URL: "http://example.com/x", Type: "http", HTTP: config.HTTPConfig{Method: "PATCH", Body: "literal {{seq}}", TimeoutS: 3, AllowCrossHostRedirects: true}},
		{URL: "https://example.com", Type: "browser", Browser: config.BrowserConfig{Scroll: true, WaitForSelector: "#ready", TimeoutS: 4}},
		{URL: "example.com", Type: "dns", DNS: config.DNSConfig{Resolver: "127.0.0.1:5353", RecordType: "AAAA"}},
		{URL: "ws://example.com", Type: "websocket", WebSocket: config.WebSocketConfig{DurationS: 30, SendMessages: []string{"{{literal}}", "two"}, ExpectMessages: 2}},
		{URL: "grpc://example.com/pkg.Service/Call", Type: "grpc", GRPC: config.GRPCConfig{Body: "not JSON", TimeoutS: 4, TLS: true, Insecure: true}},
	}
	for _, cfg := range cases {
		t.Run(cfg.Type, func(t *testing.T) {
			r := capturedResult(cfg.Type, cfg.URL)
			r.Task.Config = cfg
			e := replayEnvelope(r)
			if !e.Replayable {
				t.Fatal(e.Reason)
			}
			got := e.Request.Task().Config
			if !reflect.DeepEqual(got, cfg) {
				t.Fatalf("got %+v want %+v", got, cfg)
			}
			if cfg.Type == "websocket" {
				got.WebSocket.SendMessages[0] = "changed"
				if cfg.WebSocket.SendMessages[0] != "{{literal}}" {
					t.Fatal("aliased messages")
				}
			}
		})
	}
}

func TestReplayEnvelopeExclusions(t *testing.T) {
	for i := 0; i < reflect.TypeFor[config.AuthConfig]().NumField(); i++ {
		r := capturedResult("http", "https://example.com")
		reflect.ValueOf(&r.Task.Config.Auth).Elem().Field(i).SetString("private-marker")
		e := replayEnvelope(r)
		if e.Replayable || e.Request != nil || e.Reason != "auth_configured" {
			t.Fatalf("field %d: %+v", i, e)
		}
		var b bytes.Buffer
		if err := EncodeJSONL(json.NewEncoder(&b), r); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(b.String(), "private-marker") {
			t.Fatal("auth leaked")
		}
	}
	for _, tc := range []struct {
		typ, url, reason string
		headers          bool
	}{
		{"http", "https://example.com", "custom_headers", true},
		{"dns", "example.com", "custom_headers", true},
		{"http", "https://private-marker@example.com", "url_userinfo", false},
		{"websocket", "wss://user:private-marker@example.com", "url_userinfo", false},
		{"sftp", "sftp://example.com/file", "unsupported_type", false},
		{"http", "ftp://example.com", "invalid_request", false},
	} {
		t.Run(tc.reason+tc.typ, func(t *testing.T) {
			r := capturedResult(tc.typ, tc.url)
			if tc.headers {
				r.Task.Config.HTTP.Headers = map[string]string{"X-Secret": "private-marker"}
			}
			r.Task.Config.SFTP.Password = "private-marker"
			if tc.reason == "url_userinfo" {
				r.Error = errors.New(tc.url)
			}
			e := replayEnvelope(r)
			if e.Replayable || e.Request != nil || e.Reason != tc.reason {
				t.Fatalf("%+v", e)
			}
			var b bytes.Buffer
			if err := EncodeJSONL(json.NewEncoder(&b), r); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(b.String(), "private-marker") {
				t.Fatal("secret leaked:", b.String())
			}
		})
	}
}

func TestReplayEncodingCompatibility(t *testing.T) {
	r := capturedResult("http", "https://example.com")
	r.Meta = map[string]string{"replay": "forged", "status": "bad", "extra": "kept"}
	var b bytes.Buffer
	if err := EncodeJSONL(json.NewEncoder(&b), r); err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if string(got["duration_ms"]) != "42" || string(got["status"]) != "200" || string(got["extra"]) != `"kept"` || !bytes.Contains(got["replay"], []byte(`"version":1`)) {
		t.Fatal(b.String())
	}
	r.Capture = task.Capture{}
	b.Reset()
	if err := EncodeJSONL(json.NewEncoder(&b), r); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), `"replay"`) {
		t.Fatal("fabricated or forged capture")
	}
}

func BenchmarkReplayEncoding(b *testing.B) {
	for _, captured := range []bool{false, true} {
		name := "ordinary"
		if captured {
			name = "captured"
		}
		b.Run(name, func(b *testing.B) {
			r := capturedResult("http", "https://example.com")
			if !captured {
				r.Capture = task.Capture{}
			}
			enc := json.NewEncoder(io.Discard)
			b.ReportAllocs()
			for b.Loop() {
				if err := EncodeJSONL(enc, r); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
