package pcap_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/output"
	"github.com/lewta/sendit/internal/pcap"
	"github.com/lewta/sendit/internal/task"
)

func TestWriter_GlobalHeader(t *testing.T) {
	f, err := os.CreateTemp("", "sendit-pcap-*.pcap")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	_ = f.Close()
	defer os.Remove(path)

	w, err := pcap.New(path)
	if err != nil {
		t.Fatal(err)
	}
	w.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 24 {
		t.Fatalf("file too short for global header: %d bytes", len(data))
	}

	magic := binary.LittleEndian.Uint32(data[0:4])
	if magic != 0xa1b2c3d4 {
		t.Errorf("bad magic: %#x, want 0xa1b2c3d4", magic)
	}
	lt := binary.LittleEndian.Uint32(data[20:24])
	if lt != 147 {
		t.Errorf("expected linktype 147, got %d", lt)
	}
}

func TestWriter_Packet(t *testing.T) {
	f, err := os.CreateTemp("", "sendit-pcap-*.pcap")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	_ = f.Close()
	defer os.Remove(path)

	w, err := pcap.New(path)
	if err != nil {
		t.Fatal(err)
	}
	w.Send(task.Result{
		Task: task.Task{
			URL:    "https://example.com",
			Type:   "http",
			Config: config.TargetConfig{},
		},
		StatusCode: 200,
		Duration:   42 * time.Millisecond,
		BytesRead:  1024,
	})
	w.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// 24-byte global header + 16-byte record header + at least 1 byte payload
	if len(data) < 24+16+1 {
		t.Fatalf("file too short: %d bytes", len(data))
	}

	// incl_len is at offset 24+8 in the first packet record header.
	inclLen := binary.LittleEndian.Uint32(data[24+8 : 24+12])
	if inclLen == 0 {
		t.Error("captured packet length is 0")
	}

	payload := string(data[40:])
	for _, want := range []string{"https://example.com", "status=200", "duration_ms=42", "bytes=1024"} {
		if !strings.Contains(payload, want) {
			t.Errorf("payload missing %q: %q", want, payload)
		}
	}
}

func TestWriter_BufferFull(t *testing.T) {
	f, err := os.CreateTemp("", "sendit-pcap-*.pcap")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	_ = f.Close()
	defer os.Remove(path)

	w, err := pcap.New(path)
	if err != nil {
		t.Fatal(err)
	}
	// Send more than chanBuf without draining — must not block or panic.
	for i := 0; i < 600; i++ {
		w.Send(task.Result{Task: task.Task{URL: "https://example.com", Type: "http"}})
	}
	w.Close()
}

func TestExport(t *testing.T) {
	in, err := os.CreateTemp("", "sendit-results-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	inPath := in.Name()
	defer os.Remove(inPath)

	type rec struct {
		TS         string `json:"ts"`
		URL        string `json:"url"`
		Type       string `json:"type"`
		Status     int    `json:"status"`
		DurationMs int64  `json:"duration_ms"`
		Bytes      int64  `json:"bytes"`
	}
	enc := json.NewEncoder(in)
	_ = enc.Encode(rec{TS: time.Now().UTC().Format(time.RFC3339), URL: "https://example.com", Type: "http", Status: 200, DurationMs: 10, Bytes: 512})
	_ = enc.Encode(rec{TS: time.Now().UTC().Format(time.RFC3339), URL: "example.com", Type: "dns", Status: 200, DurationMs: 3, Bytes: 0})
	_ = in.Close()

	outPath := inPath[:strings.LastIndex(inPath, ".")] + ".pcap"
	defer os.Remove(outPath)

	if err := pcap.Export(inPath, outPath); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 24+16+1 {
		t.Fatalf("output too short: %d bytes", len(data))
	}

	magic := binary.LittleEndian.Uint32(data[0:4])
	if magic != 0xa1b2c3d4 {
		t.Errorf("bad magic: %#x", magic)
	}

	// Verify second packet exists by finding it past the first.
	firstIncl := int(binary.LittleEndian.Uint32(data[24+8 : 24+12]))
	off2 := 24 + 16 + firstIncl
	if off2+16 > len(data) {
		t.Fatal("second packet missing from output")
	}
}

func TestExportRejectsOutputAliasesWithoutTruncation(t *testing.T) {
	inPath := filepath.Join(t.TempDir(), "input.jsonl")
	line := `{"ts":"2026-10-25T00:00:00Z","url":"https://example.com","type":"http","status":200}` + "\n"
	if err := os.WriteFile(inPath, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, link := range map[string]func(string, string) error{"symlink": os.Symlink, "hardlink": os.Link} {
		t.Run(name, func(t *testing.T) {
			victim := filepath.Join(t.TempDir(), "victim")
			if err := os.WriteFile(victim, []byte("preserve"), 0o600); err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(t.TempDir(), "output.pcap")
			if err := link(victim, alias); err != nil {
				t.Skipf("%s unavailable: %v", name, err)
			}
			if err := pcap.Export(inPath, alias); err == nil {
				t.Fatal("output alias accepted")
			}
			got, err := os.ReadFile(victim)
			if err != nil || string(got) != "preserve" {
				t.Fatalf("denial modified victim: %q, %v", got, err)
			}
		})
	}
}

func TestExportReplayEnvelope(t *testing.T) {
	inPath := t.TempDir() + "/capture.jsonl"
	f, err := os.Create(inPath)
	if err != nil {
		t.Fatal(err)
	}
	capture, err := task.NewCaptureClock().Next()
	if err != nil {
		t.Fatal(err)
	}
	r := task.Result{Task: task.Task{URL: "https://example.com", Type: "http"}, Capture: capture, StatusCode: 200, Duration: 42 * time.Millisecond}
	if err := output.EncodeJSONL(json.NewEncoder(f), r); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir() + "/capture.pcap"
	if err := pcap.Export(inPath, dest); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "duration_ms=42") {
		t.Fatal("additive envelope broke PCAP export")
	}
}

func TestExportRejectsInputOutputAliasBeforeTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capture.jsonl")
	want := []byte(`{"ts":"2026-01-01T00:00:00Z","url":"https://example.com","type":"http","status":200}` + "\n")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pcap.Export(path, path); err == nil {
		t.Fatal("input/output alias accepted")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("alias rejection modified input: %q, %v", got, err)
	}
}

func TestExportMalformedInputReturnsPromptly(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{name: "canonical non-progressing seed", data: []byte("{")},
		{name: "truncated record", data: []byte(`{"ts":"2026-10-24T00:00:00Z"`)},
		{name: "valid then truncated", data: []byte(`{"ts":"2026-10-24T00:00:00Z","url":"https://one.example","type":"http"}` + "\n{")},
		{name: "invalid JSON", data: []byte("not-json\n")},
		{name: "invalid UTF-8", data: []byte{0xff, '\n'}},
		{name: "complete wrong type then valid", data: []byte(`{"status":"wrong"}` + "\n" +
			`{"ts":"2026-10-24T00:00:00Z","status":200}` + "\n")},
		{name: "lone closing delimiter", data: []byte("]")},
		{name: "trailing closing delimiter", data: []byte(`{"ts":"2026-10-24T00:00:00Z"}` + "\n}")},
		{name: "oversized malformed fragment", data: bytes.Repeat([]byte{'{'}, 1<<20)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			inPath := filepath.Join(dir, "input.jsonl")
			outPath := filepath.Join(dir, "output.pcap")
			if err := os.WriteFile(inPath, tc.data, 0o600); err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestExportMalformedInputHelper$") //nolint:gosec // re-executes this test binary with a fixed test selector
			cmd.Env = append(os.Environ(), "SENDIT_PCAP_TEST_INPUT="+inPath, "SENDIT_PCAP_TEST_OUTPUT="+outPath)
			cmd.Stdout = io.Discard
			cmd.Stderr = io.Discard
			err := cmd.Run()
			if ctx.Err() == context.DeadlineExceeded {
				t.Fatal("export did not return within 2s")
			}
			if err != nil {
				t.Fatalf("export helper failed: %v", err)
			}
		})
	}
}

func TestExportMalformedInputHelper(t *testing.T) {
	inPath := os.Getenv("SENDIT_PCAP_TEST_INPUT")
	if inPath == "" {
		t.Skip("subprocess helper")
	}
	if err := pcap.Export(inPath, os.Getenv("SENDIT_PCAP_TEST_OUTPUT")); err == nil {
		t.Fatal("malformed input was accepted")
	}
}

func TestExportBlankAndMultipleRecords(t *testing.T) {
	tests := []struct {
		name        string
		data        string
		wantPackets int
	}{
		{name: "blank", data: " \n\t\r\n", wantPackets: 0},
		{name: "multiple", data: `{"ts":"2026-10-24T00:00:00Z","url":"https://one.example","type":"http","status":200}` + "\r\n\n" +
			`{"ts":"2026-10-24T00:00:01Z","url":"https://two.example","type":"http","status":204}`, wantPackets: 2},
		{name: "adjacent JSON stream", data: `{"ts":"2026-10-24T00:00:00Z"}{"ts":"2026-10-24T00:00:01Z"}`, wantPackets: 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			inPath := filepath.Join(dir, "input.jsonl")
			outPath := filepath.Join(dir, "output.pcap")
			if err := os.WriteFile(inPath, []byte(tc.data), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := pcap.Export(inPath, outPath); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(outPath)
			if err != nil {
				t.Fatal(err)
			}
			if got := packetCount(t, data); got != tc.wantPackets {
				t.Fatalf("packet count = %d, want %d", got, tc.wantPackets)
			}
		})
	}
}

func TestExportMalformedInputLeavesOnlyCompletedPackets(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "input.jsonl")
	outPath := filepath.Join(dir, "output.pcap")
	input := `{"ts":"2026-10-24T00:00:00Z","url":"https://one.example","type":"http","status":200}` + "\n{"
	if err := os.WriteFile(inPath, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pcap.Export(inPath, outPath); err == nil {
		t.Fatal("truncated second record was accepted")
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := packetCount(t, data); got != 1 {
		t.Fatalf("packet count = %d, want completed first packet only", got)
	}
}

func packetCount(t *testing.T, data []byte) int {
	t.Helper()
	if len(data) < 24 {
		t.Fatalf("PCAP shorter than global header: %d", len(data))
	}
	count := 0
	for offset := 24; offset < len(data); count++ {
		if offset+16 > len(data) {
			t.Fatalf("truncated packet header at byte %d", offset)
		}
		length := int(binary.LittleEndian.Uint32(data[offset+8 : offset+12]))
		offset += 16 + length
		if offset > len(data) {
			t.Fatalf("packet extends past output: %d > %d", offset, len(data))
		}
	}
	return count
}
