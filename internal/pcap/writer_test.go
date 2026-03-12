package pcap_test

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lewta/sendit/internal/config"
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
