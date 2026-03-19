package pcap

import (
	"bufio"
	"bytes"
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
