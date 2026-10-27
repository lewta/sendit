package pcap

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"testing"
	"time"

	"github.com/lewta/sendit/internal/task"
)

// FuzzWriteRecord fuzzes the PCAP record writer with arbitrary result fields,
// catching any encoding panic on unusual URL, type, status, or size values.
func FuzzWriteRecord(f *testing.F) {
	f.Add("https://example.com", "http", 200, int64(142), int64(1024))
	f.Add("dns://example.com", "dns", 0, int64(4), int64(0))
	f.Add("", "", -1, int64(0), int64(-1))
	f.Add("wss://echo.websocket.org", "websocket", 101, int64(50), int64(512))
	// Oversized payload to exercise snapLen truncation.
	f.Add("https://example.com/"+string(make([]byte, 70000)), "http", 200, int64(0), int64(70000))

	f.Fuzz(func(t *testing.T, url, typ string, status int, durationMs, bytesRead int64) {
		r := task.Result{
			Task:       task.Task{URL: url, Type: typ},
			StatusCode: status,
			Duration:   time.Duration(durationMs) * time.Millisecond,
			BytesRead:  bytesRead,
		}
		var buf bytes.Buffer
		bw := bufio.NewWriter(&buf)
		writePacket(bw, r, time.Now().UTC())
		// Flush must not panic.
		_ = bw.Flush()
	})
}

// FuzzExportProgress keeps malformed local JSONL inputs from reintroducing a
// decode loop that neither consumes input nor returns an error. Inputs are
// capped so the regression remains predictable under CI fuzzing.
func FuzzExportProgress(f *testing.F) {
	f.Add([]byte("{"))
	f.Add([]byte(""))
	f.Add([]byte("not-json\n"))
	f.Add([]byte{0xff, '\n'})
	f.Add([]byte(`{"ts":"2026-10-24T00:00:00Z","url":"https://one.example","type":"http"}` + "\n" +
		`{"ts":"2026-10-24T00:00:01Z","url":"https://two.example","type":"http"}` + "\n"))
	f.Add(bytes.Repeat([]byte{'{'}, 1<<20))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 2<<20 {
			t.Skip("keep fuzz resource use bounded")
		}
		dir := t.TempDir()
		inPath := dir + "/input.jsonl"
		if err := os.WriteFile(inPath, data, 0o600); err != nil {
			t.Fatal(err)
		}
		_ = Export(inPath, dir+"/output.pcap")
	})
}

type zeroProgressDecoder struct {
	calls int
}

func (d *zeroProgressDecoder) Decode(any) error {
	d.calls++
	if d.calls > 1 {
		return io.EOF
	}
	return nil
}

func (*zeroProgressDecoder) InputOffset() int64 { return 0 }

func TestDecodeRecordsRejectsSuccessfulZeroProgress(t *testing.T) {
	bw := bufio.NewWriter(&bytes.Buffer{})
	if _, err := decodeRecords(&zeroProgressDecoder{}, bw); err == nil {
		t.Fatal("successful decode without input progress was accepted")
	}
}
