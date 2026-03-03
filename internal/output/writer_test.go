package output

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/task"
)

func makeResult(url, typ string, status int, dur time.Duration, bytes int64, err error) task.Result {
	return task.Result{
		Task:       task.Task{URL: url, Type: typ, Config: config.TargetConfig{URL: url, Type: typ}},
		StatusCode: status,
		Duration:   dur,
		BytesRead:  bytes,
		Error:      err,
	}
}

func TestWriter_JSONL(t *testing.T) {
	f := t.TempDir() + "/out.jsonl"
	w, err := New(config.OutputConfig{File: f, Format: "jsonl"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w.Send(makeResult("https://example.com", "http", 200, 42*time.Millisecond, 1024, nil))
	w.Close()

	data, err := os.ReadFile(f)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var rec record
	if err := json.Unmarshal([]byte(strings.TrimRight(string(data), "\n")), &rec); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if rec.URL != "https://example.com" {
		t.Errorf("URL = %q, want https://example.com", rec.URL)
	}
	if rec.Type != "http" {
		t.Errorf("Type = %q, want http", rec.Type)
	}
	if rec.Status != 200 {
		t.Errorf("Status = %d, want 200", rec.Status)
	}
	if rec.DurationMs != 42 {
		t.Errorf("DurationMs = %d, want 42", rec.DurationMs)
	}
	if rec.Bytes != 1024 {
		t.Errorf("Bytes = %d, want 1024", rec.Bytes)
	}
	if rec.Error != "" {
		t.Errorf("Error = %q, want empty", rec.Error)
	}
}

func TestWriter_JSONL_ErrorField(t *testing.T) {
	f := t.TempDir() + "/out.jsonl"
	w, err := New(config.OutputConfig{File: f, Format: "jsonl"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w.Send(makeResult("https://example.com", "http", 0, time.Millisecond, 0, errors.New("connection refused")))
	w.Close()

	data, _ := os.ReadFile(f)
	var rec record
	json.Unmarshal([]byte(strings.TrimRight(string(data), "\n")), &rec) //nolint:errcheck

	if !strings.Contains(rec.Error, "connection refused") {
		t.Errorf("Error = %q, want to contain 'connection refused'", rec.Error)
	}
}

func TestWriter_CSV(t *testing.T) {
	f := t.TempDir() + "/out.csv"
	w, err := New(config.OutputConfig{File: f, Format: "csv", Append: false})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w.Send(makeResult("https://example.com", "http", 200, 42*time.Millisecond, 512, nil))
	w.Close()

	data, _ := os.ReadFile(f)
	rows, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil {
		t.Fatalf("csv.ReadAll: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows (header + 1 record), got %d", len(rows))
	}

	// Header
	if rows[0][0] != "ts" || rows[0][1] != "url" || rows[0][3] != "status" {
		t.Errorf("unexpected header: %v", rows[0])
	}

	// Data row
	if rows[1][1] != "https://example.com" {
		t.Errorf("url = %q, want https://example.com", rows[1][1])
	}
	if rows[1][3] != "200" {
		t.Errorf("status = %q, want 200", rows[1][3])
	}
	if rows[1][4] != "42" {
		t.Errorf("duration_ms = %q, want 42", rows[1][4])
	}
}

func TestWriter_CSV_AppendNoHeader(t *testing.T) {
	f := t.TempDir() + "/out.csv"

	// First write: append:false writes header + row.
	w1, _ := New(config.OutputConfig{File: f, Format: "csv", Append: false})
	w1.Send(makeResult("https://a.example.com", "http", 200, time.Millisecond, 0, nil))
	w1.Close()

	// Second write: append:true must NOT write another header.
	w2, _ := New(config.OutputConfig{File: f, Format: "csv", Append: true})
	w2.Send(makeResult("https://b.example.com", "http", 200, time.Millisecond, 0, nil))
	w2.Close()

	data, _ := os.ReadFile(f)
	rows, _ := csv.NewReader(strings.NewReader(string(data))).ReadAll()

	// Expect: 1 header + 2 data rows.
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows (1 header + 2 data), got %d: %v", len(rows), rows)
	}
	if rows[1][1] != "https://a.example.com" {
		t.Errorf("row 1 url = %q, want a.example.com", rows[1][1])
	}
	if rows[2][1] != "https://b.example.com" {
		t.Errorf("row 2 url = %q, want b.example.com", rows[2][1])
	}
}

func TestWriter_Truncate(t *testing.T) {
	f := t.TempDir() + "/out.jsonl"

	// Write first record.
	w1, _ := New(config.OutputConfig{File: f, Format: "jsonl", Append: false})
	w1.Send(makeResult("https://a.example.com", "http", 200, time.Millisecond, 0, nil))
	w1.Close()

	// Overwrite with append:false — prior content must be gone.
	w2, _ := New(config.OutputConfig{File: f, Format: "jsonl", Append: false})
	w2.Send(makeResult("https://b.example.com", "http", 200, time.Millisecond, 0, nil))
	w2.Close()

	data, _ := os.ReadFile(f)
	content := string(data)
	if strings.Contains(content, "a.example.com") {
		t.Errorf("truncate mode: old content still present in file")
	}
	if !strings.Contains(content, "b.example.com") {
		t.Errorf("truncate mode: new content not found in file")
	}
}

func TestWriter_CloseDrainsBuffer(t *testing.T) {
	f := t.TempDir() + "/out.jsonl"
	w, _ := New(config.OutputConfig{File: f, Format: "jsonl"})

	const n = 20
	for i := range n {
		w.Send(makeResult(fmt.Sprintf("https://example.com/%d", i), "http", 200, time.Millisecond, 0, nil))
	}
	w.Close()

	data, _ := os.ReadFile(f)
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != n {
		t.Errorf("expected %d lines after Close, got %d", n, len(lines))
	}
}
