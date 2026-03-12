package pcap

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/lewta/sendit/internal/task"
	"github.com/rs/zerolog/log"
)

const (
	chanBuf    = 512
	magicLE    = 0xa1b2c3d4 // little-endian, microsecond timestamps
	versionMaj = 2
	versionMin = 4
	snapLen    = 65535
	linkType   = 147 // LINKTYPE_USER0 — no IP/TCP framing
)

// Writer serialises task.Result values to a synthetic PCAP file.
// Each packet payload is a text record with per-request telemetry.
// The file uses LINKTYPE_USER0 (147); no CGO, libpcap, or root is required.
// Send is non-blocking; results are dropped (with a warning) if the internal
// buffer is full. Close drains the buffer and flushes the file.
type Writer struct {
	ch   chan task.Result
	done chan struct{}
}

// New opens the PCAP file and starts the background writer goroutine.
// The caller must call Close() when done.
func New(path string) (*Writer, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening pcap file %q: %w", path, err)
	}

	w := &Writer{
		ch:   make(chan task.Result, chanBuf),
		done: make(chan struct{}),
	}
	go w.run(f)
	return w, nil
}

// Send enqueues a result for writing. Non-blocking; drops if buffer is full.
func (w *Writer) Send(r task.Result) {
	select {
	case w.ch <- r:
	default:
		log.Warn().Msg("pcap writer buffer full, dropping result")
	}
}

// Close drains the channel and closes the file.
func (w *Writer) Close() {
	close(w.ch)
	<-w.done
}

func (w *Writer) run(f *os.File) {
	defer close(w.done)
	bw := bufio.NewWriter(f)
	defer func() {
		_ = bw.Flush()
		_ = f.Close()
	}()

	writeGlobalHeader(bw)
	for r := range w.ch {
		writePacket(bw, r, time.Now().UTC())
	}
}

// Export converts a JSONL result file (written by output.Writer) to a
// synthetic PCAP file. outPath overrides the default output path; when empty
// the input extension is replaced with ".pcap".
func Export(inPath, outPath string) error {
	if outPath == "" {
		if idx := strings.LastIndex(inPath, "."); idx >= 0 {
			outPath = inPath[:idx] + ".pcap"
		} else {
			outPath = inPath + ".pcap"
		}
	}

	in, err := os.Open(inPath)
	if err != nil {
		return fmt.Errorf("opening input %q: %w", inPath, err)
	}
	defer in.Close()

	out, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("opening output %q: %w", outPath, err)
	}

	bw := bufio.NewWriter(out)
	writeGlobalHeader(bw)

	type jsonRecord struct {
		TS         string `json:"ts"`
		URL        string `json:"url"`
		Type       string `json:"type"`
		Status     int    `json:"status"`
		DurationMs int64  `json:"duration_ms"`
		Bytes      int64  `json:"bytes"`
		Error      string `json:"error"`
	}

	dec := json.NewDecoder(in)
	count := 0
	for dec.More() {
		var rec jsonRecord
		if err := dec.Decode(&rec); err != nil {
			log.Warn().Err(err).Msg("pcap export: skipping malformed record")
			continue
		}
		ts, parseErr := time.Parse(time.RFC3339, rec.TS)
		if parseErr != nil {
			ts = time.Now().UTC()
		}
		payload := fmt.Sprintf("ts=%s url=%s type=%s status=%d duration_ms=%d bytes=%d error=%s\n",
			ts.Format(time.RFC3339), rec.URL, rec.Type, rec.Status, rec.DurationMs, rec.Bytes, rec.Error)
		writePktRaw(bw, ts, []byte(payload))
		count++
	}

	if err := bw.Flush(); err != nil {
		_ = out.Close()
		return fmt.Errorf("flushing pcap output: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("closing pcap output: %w", err)
	}

	fmt.Printf("Exported %d packets → %s\n", count, outPath)
	return nil
}

func writeGlobalHeader(bw *bufio.Writer) {
	buf := make([]byte, 24)
	binary.LittleEndian.PutUint32(buf[0:], magicLE)
	binary.LittleEndian.PutUint16(buf[4:], versionMaj)
	binary.LittleEndian.PutUint16(buf[6:], versionMin)
	binary.LittleEndian.PutUint32(buf[8:], 0)        // thiszone
	binary.LittleEndian.PutUint32(buf[12:], 0)       // sigfigs
	binary.LittleEndian.PutUint32(buf[16:], snapLen) // snaplen
	binary.LittleEndian.PutUint32(buf[20:], linkType)
	_, _ = bw.Write(buf)
}

func writePacket(bw *bufio.Writer, r task.Result, now time.Time) {
	errStr := ""
	if r.Error != nil {
		errStr = r.Error.Error()
	}
	payload := fmt.Sprintf("ts=%s url=%s type=%s status=%d duration_ms=%d bytes=%d error=%s\n",
		now.Format(time.RFC3339),
		r.Task.URL,
		r.Task.Type,
		r.StatusCode,
		r.Duration.Milliseconds(),
		r.BytesRead,
		errStr,
	)
	writePktRaw(bw, now, []byte(payload))
}

func writePktRaw(bw *bufio.Writer, ts time.Time, payload []byte) {
	inclLen := uint32(len(payload))
	origLen := inclLen
	if inclLen > snapLen {
		inclLen = snapLen
	}
	hdr := make([]byte, 16)
	binary.LittleEndian.PutUint32(hdr[0:], uint32(ts.Unix()))
	binary.LittleEndian.PutUint32(hdr[4:], uint32(ts.Nanosecond()/1000)) // microseconds
	binary.LittleEndian.PutUint32(hdr[8:], inclLen)
	binary.LittleEndian.PutUint32(hdr[12:], origLen)
	_, _ = bw.Write(hdr)
	_, _ = bw.Write(payload[:inclLen])
}
