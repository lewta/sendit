package output_test

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/output"
	"github.com/lewta/sendit/internal/task"
)

func TestWriter_URLUserinfoRedaction(t *testing.T) {
	const omitted = "error omitted: target URL contains userinfo"
	tests := []struct {
		name, url, wantURL string
		withError          bool
		userinfo           bool
	}{
		{"valid", "https://alice:secret-password@example.com/path?q=1#part", "https://example.com/path?q=1#part", true, true},
		{"malformed port", "https://alice:secret-password@example.com:bad/path", "https://example.com:bad/path", true, true},
		{"malformed escape", "https://alice:secret-password@example.com/%zz", "https://example.com/%zz", true, true},
		{"scheme relative", "//alice:secret-password@example.com/path", "//example.com/path", true, true},
		{"no error", "sftp://alice:secret-password@example.com/file", "sftp://example.com/file", false, true},
		{"normal error", "https://example.com/path?q=1#part", "https://example.com/path?q=1#part", true, false},
		{"normal success", "https://example.com/path", "https://example.com/path", false, false},
		{"at outside authority", "https://example.com/alice@example.net?q=a@b#c@d", "https://example.com/alice@example.net?q=a@b#c@d", true, false},
	}
	for _, format := range []string{"csv", "jsonl"} {
		for _, tt := range tests {
			t.Run(format+"/"+tt.name, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "results."+format)
				w, err := output.New(config.OutputConfig{File: path, Format: format})
				if err != nil {
					t.Fatal(err)
				}
				r := task.Result{
					Task:       task.Task{URL: tt.url, Type: "http", Config: config.TargetConfig{URL: tt.url, Type: "http"}},
					StatusCode: 200, Duration: 42 * time.Millisecond, BytesRead: 123,
					Meta:    map[string]string{"test_metadata": "unchanged"},
					Capture: task.Capture{RunID: "redaction-test", Sequence: 1, StartedAt: time.Now()},
				}
				wantError := ""
				if tt.withError {
					wantError = fmt.Sprintf("request to %s failed: connection refused", tt.url)
					r.Error = errors.New(wantError)
					if tt.userinfo {
						wantError = omitted
					}
				}
				w.Send(r)
				w.Close()
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if tt.userinfo && (strings.Contains(string(data), "alice") || strings.Contains(string(data), "secret-password")) {
					t.Error("exported file contains URL credentials")
				}
				var gotURL, gotError string
				if format == "csv" {
					rows, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
					if err != nil {
						t.Fatal(err)
					}
					if len(rows) != 2 || len(rows[1]) != 7 {
						t.Fatalf("expected header and one seven-column row, got %v", rows)
					}
					gotURL, gotError = rows[1][1], rows[1][6]
					if rows[1][2] != "http" || rows[1][3] != "200" || rows[1][4] != "42" || rows[1][5] != "123" {
						t.Errorf("ordinary result fields changed: %v", rows[1])
					}
				} else {
					var rec struct {
						URL, Error   string
						Type         string
						Status       int
						DurationMs   int64 `json:"duration_ms"`
						Bytes        int64
						TestMetadata string `json:"test_metadata"`
						Replay       output.ReplayEnvelope
					}
					if err := json.Unmarshal(data, &rec); err != nil {
						t.Fatal(err)
					}
					gotURL, gotError = rec.URL, rec.Error
					if rec.Type != "http" || rec.Status != 200 || rec.DurationMs != 42 || rec.Bytes != 123 || rec.TestMetadata != "unchanged" {
						t.Errorf("ordinary result fields changed: %+v", rec)
					}
					if tt.userinfo {
						if rec.Replay.Replayable || rec.Replay.Reason != "url_userinfo" || rec.Replay.Request != nil {
							t.Errorf("credential-bearing request must remain excluded from replay: %+v", rec.Replay)
						}
					} else if !rec.Replay.Replayable || rec.Replay.Request == nil || rec.Replay.Request.URL != tt.url {
						t.Errorf("ordinary request must remain replayable: %+v", rec.Replay)
					}
				}
				if gotURL != tt.wantURL || gotError != wantError {
					t.Errorf("URL/error = %q / %q, want %q / %q", gotURL, gotError, tt.wantURL, wantError)
				}
			})
		}
	}
}
