package output

import (
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/task"
	"github.com/miekg/dns"
)

// ReplayEnvelope is the versioned, credential-excluding dispatch snapshot.
type ReplayEnvelope struct {
	Version    int            `json:"version"`
	RunID      string         `json:"run_id"`
	Sequence   uint64         `json:"sequence"`
	StartedAt  time.Time      `json:"started_at"`
	Replayable bool           `json:"replayable"`
	Reason     string         `json:"reason,omitempty"`
	Request    *ReplayRequest `json:"request,omitempty"`
}

// ReplayRequest deliberately cannot represent auth, headers or variable files.
type ReplayRequest struct {
	URL       string           `json:"url"`
	Type      string           `json:"type"`
	HTTP      *ReplayHTTP      `json:"http,omitempty"`
	Browser   *ReplayBrowser   `json:"browser,omitempty"`
	DNS       *ReplayDNS       `json:"dns,omitempty"`
	WebSocket *ReplayWebSocket `json:"websocket,omitempty"`
	GRPC      *ReplayGRPC      `json:"grpc,omitempty"`
}
type ReplayHTTP struct {
	HTTPVersion             int    `json:"http_version"`
	Method                  string `json:"method"`
	Body                    string `json:"body"`
	TimeoutS                int    `json:"timeout_s"`
	AllowCrossHostRedirects bool   `json:"allow_cross_host_redirects"`
}
type ReplayBrowser struct {
	Scroll          bool   `json:"scroll"`
	WaitForSelector string `json:"wait_for_selector"`
	TimeoutS        int    `json:"timeout_s"`
}
type ReplayDNS struct {
	Resolver   string `json:"resolver"`
	RecordType string `json:"record_type"`
}
type ReplayWebSocket struct {
	DurationS      int      `json:"duration_s"`
	SendMessages   []string `json:"send_messages"`
	ExpectMessages int      `json:"expect_messages"`
}
type ReplayGRPC struct {
	Body     string `json:"body"`
	TimeoutS int    `json:"timeout_s"`
	TLS      bool   `json:"tls"`
	Insecure bool   `json:"insecure"`
}

func replayEnvelope(r task.Result) *ReplayEnvelope {
	if r.Capture.RunID == "" || r.Capture.Sequence == 0 || r.Capture.StartedAt.IsZero() {
		return nil
	}
	e := &ReplayEnvelope{Version: 2, RunID: r.Capture.RunID, Sequence: r.Capture.Sequence, StartedAt: r.Capture.StartedAt}
	c := r.Task.Config
	switch {
	case c.Auth != (config.AuthConfig{}):
		e.Reason = "auth_configured"
	case len(c.HTTP.Headers) > 0:
		e.Reason = "custom_headers"
	default:
		if _, hasUserinfo := withoutURLUserinfo(r.Task.URL); hasUserinfo {
			e.Reason = "url_userinfo"
		}
	}
	if e.Reason != "" {
		return e
	}
	q := &ReplayRequest{URL: r.Task.URL, Type: r.Task.Type}
	positive := func(n, fallback int) int {
		if n <= 0 {
			return fallback
		}
		return n
	}
	switch q.Type {
	case "http":
		method := c.HTTP.Method
		if method == "" {
			method = http.MethodGet
		}
		q.HTTP = &ReplayHTTP{HTTPVersion: c.HTTP.HTTPVersion, Method: method, Body: c.HTTP.Body, TimeoutS: positive(c.HTTP.TimeoutS, 15), AllowCrossHostRedirects: c.HTTP.AllowCrossHostRedirects}
	case "browser":
		q.Browser = &ReplayBrowser{c.Browser.Scroll, c.Browser.WaitForSelector, positive(c.Browser.TimeoutS, 30)}
	case "dns":
		resolver := c.DNS.Resolver
		if resolver == "" {
			resolver = "8.8.8.8:53"
		}
		rt := strings.ToUpper(c.DNS.RecordType)
		if rt == "" {
			rt = "A"
		}
		q.DNS = &ReplayDNS{resolver, rt}
	case "websocket":
		q.WebSocket = &ReplayWebSocket{positive(c.WebSocket.DurationS, 10), append([]string{}, c.WebSocket.SendMessages...), c.WebSocket.ExpectMessages}
	case "grpc":
		q.GRPC = &ReplayGRPC{c.GRPC.Body, positive(c.GRPC.TimeoutS, 15), c.GRPC.TLS || strings.HasPrefix(q.URL, "grpcs://"), c.GRPC.Insecure}
	default:
		e.Reason = "unsupported_type"
		return e
	}
	if err := validateReplayRequest(*q); err != nil {
		e.Reason = "invalid_request"
		return e
	}
	e.Replayable = true
	e.Request = q
	return e
}

// withoutURLUserinfo isolates the authority without parsing the path or port.
// Malformed URLs can still contain credentials and appear in driver errors.
func withoutURLUserinfo(raw string) (string, bool) {
	start := 0
	if i := strings.Index(raw, "://"); i >= 0 {
		start = i + 3
	} else if strings.HasPrefix(raw, "//") {
		start = 2
	} else {
		return raw, false
	}
	end := len(raw)
	if i := strings.IndexAny(raw[start:], "/?#"); i >= 0 {
		end = start + i
	}
	if i := strings.LastIndexByte(raw[start:end], '@'); i >= 0 {
		return raw[:start] + raw[start+i+1:], true
	}
	return raw, false
}

// Task converts a validated snapshot without loading configuration or templates.
func (r ReplayRequest) Task() task.Task {
	c := config.TargetConfig{URL: r.URL, Type: r.Type}
	if v := r.HTTP; v != nil {
		c.HTTP = config.HTTPConfig{HTTPVersion: v.HTTPVersion, Method: v.Method, Body: v.Body, TimeoutS: v.TimeoutS, AllowCrossHostRedirects: v.AllowCrossHostRedirects}
	}
	if v := r.Browser; v != nil {
		c.Browser = config.BrowserConfig{Scroll: v.Scroll, WaitForSelector: v.WaitForSelector, TimeoutS: v.TimeoutS}
	}
	if v := r.DNS; v != nil {
		c.DNS = config.DNSConfig{Resolver: v.Resolver, RecordType: v.RecordType}
	}
	if v := r.WebSocket; v != nil {
		c.WebSocket = config.WebSocketConfig{DurationS: v.DurationS, SendMessages: append([]string{}, v.SendMessages...), ExpectMessages: v.ExpectMessages}
	}
	if v := r.GRPC; v != nil {
		c.GRPC = config.GRPCConfig{Body: v.Body, TimeoutS: v.TimeoutS, TLS: v.TLS, Insecure: v.Insecure}
	}
	return task.Task{URL: r.URL, Type: r.Type, Config: c}
}

func validReplaySeconds(n, extra int) bool {
	return n > 0 && int64(n) <= math.MaxInt64/int64(time.Second)-int64(extra) && n <= int(^uint(0)>>1)-extra
}

func validReplayPort(port string) bool {
	if port == "" {
		return false
	}
	for _, c := range port {
		if c < '0' || c > '9' {
			return false
		}
	}
	n, err := strconv.Atoi(port)
	return err == nil && n > 0 && n <= 65535
}

func validateReplayRequest(r ReplayRequest) error {
	fail := func(field string) error { return fmt.Errorf("request.%s is invalid", field) }
	if r.URL == "" || !utf8.ValidString(r.URL) {
		return fail("url")
	}
	blocks := 0
	for _, set := range []bool{r.HTTP != nil, r.Browser != nil, r.DNS != nil, r.WebSocket != nil, r.GRPC != nil} {
		if set {
			blocks++
		}
	}
	if blocks != 1 {
		return fail("driver block")
	}
	if r.Type != "dns" {
		schemes := map[string][]string{"http": {"http", "https"}, "browser": {"http", "https"}, "websocket": {"ws", "wss"}, "grpc": {"grpc", "grpcs"}}
		u, err := url.Parse(r.URL)
		if err != nil || u.Hostname() == "" || u.User != nil || !slices.Contains(schemes[r.Type], u.Scheme) {
			return fail("url")
		}
		if strings.HasSuffix(u.Host, ":") || (u.Port() != "" && !validReplayPort(u.Port())) {
			return fail("url.port")
		}
		if _, err := http.NewRequest(http.MethodGet, r.URL, nil); err != nil {
			return fail("url")
		}
		if r.Type == "grpc" {
			parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return fail("url.path")
			}
		}
	}
	switch r.Type {
	case "http":
		v := r.HTTP
		if v == nil {
			return fail("http")
		}
		if config.ValidateHTTPVersion(v.HTTPVersion) != nil {
			return fail("http.http_version")
		}
		if v.HTTPVersion == 2 && !strings.HasPrefix(strings.ToLower(r.URL), "https://") {
			return fail("http.http_version (2 requires HTTPS)")
		}
		if v.Method == "" {
			return fail("http.method")
		}
		if _, err := http.NewRequest(v.Method, r.URL, nil); err != nil {
			return fail("http.method")
		}
		if !validReplaySeconds(v.TimeoutS, 0) {
			return fail("http.timeout_s")
		}
		if !utf8.ValidString(v.Body) {
			return fail("http.body")
		}
	case "browser":
		v := r.Browser
		if v == nil {
			return fail("browser")
		}
		if !validReplaySeconds(v.TimeoutS, 0) {
			return fail("browser.timeout_s")
		}
		if !utf8.ValidString(v.WaitForSelector) {
			return fail("browser.wait_for_selector")
		}
	case "dns":
		v := r.DNS
		if v == nil {
			return fail("dns")
		}
		if _, ok := dns.IsDomainName(r.URL); !ok {
			return fail("url")
		}
		host, port, err := net.SplitHostPort(v.Resolver)
		if err != nil || host == "" || !validReplayPort(port) {
			return fail("dns.resolver")
		}
		if _, ok := dns.StringToType[v.RecordType]; !ok {
			return fail("dns.record_type")
		}
	case "websocket":
		v := r.WebSocket
		if v == nil {
			return fail("websocket")
		}
		if !validReplaySeconds(v.DurationS, 30) {
			return fail("websocket.duration_s")
		}
		if v.ExpectMessages < 0 {
			return fail("websocket.expect_messages")
		}
		if v.SendMessages == nil {
			return fail("websocket.send_messages")
		}
		for _, s := range v.SendMessages {
			if !utf8.ValidString(s) {
				return fail("websocket.send_messages")
			}
		}
	case "grpc":
		v := r.GRPC
		if v == nil {
			return fail("grpc")
		}
		if !validReplaySeconds(v.TimeoutS, 0) {
			return fail("grpc.timeout_s")
		}
		if !utf8.ValidString(v.Body) {
			return fail("grpc.body")
		}
	default:
		return fail("type")
	}
	return nil
}
