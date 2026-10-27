package pcap

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/lewta/sendit/internal/policy"
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

type recordDecoder interface {
	Decode(any) error
	InputOffset() int64
}

type jsonRecord struct {
	TS         string `json:"ts"`
	URL        string `json:"url"`
	Type       string `json:"type"`
	Status     int    `json:"status"`
	DurationMs int64  `json:"duration_ms"`
	Bytes      int64  `json:"bytes"`
	Error      string `json:"error"`
}

// New opens the PCAP file and starts the background writer goroutine.
// The caller must call Close() when done.
func New(path string) (*Writer, error) {
	prepared, err := policy.PrepareOutput(policy.Trusted(), path, false)
	if err != nil {
		return nil, fmt.Errorf("opening pcap file %q: %w", path, err)
	}
	f, err := prepared.Commit()
	if err != nil {
		return nil, fmt.Errorf("opening pcap file %q: %w", path, err)
	}
	return NewFromFile(f), nil
}

// NewFromFile starts a PCAP writer using an already prepared output.
func NewFromFile(f *os.File) *Writer {
	w := &Writer{
		ch:   make(chan task.Result, chanBuf),
		done: make(chan struct{}),
	}
	go w.run(f)
	return w
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

	prepared, err := policy.PrepareExplicitOutput(outPath, false)
	if err != nil {
		return fmt.Errorf("opening output %q: %w", outPath, err)
	}
	inInfo, err := in.Stat()
	if err != nil {
		return errors.Join(err, prepared.Abort())
	}
	if prepared.SameInfo(inInfo) {
		return errors.Join(errors.New("input and output refer to the same file"), prepared.Abort())
	}
	out, err := prepared.Commit()
	if err != nil {
		return fmt.Errorf("opening output %q: %w", outPath, err)
	}

	bw := bufio.NewWriter(out)
	writeGlobalHeader(bw)

	dec := json.NewDecoder(in)
	count, decodeErr := decodeRecords(dec, bw)
	flushErr := bw.Flush()
	closeErr := out.Close()
	if flushErr != nil {
		flushErr = fmt.Errorf("flushing pcap output: %w", flushErr)
	}
	if closeErr != nil {
		closeErr = fmt.Errorf("closing pcap output: %w", closeErr)
	}
	if err := errors.Join(decodeErr, flushErr, closeErr); err != nil {
		return err
	}

	fmt.Printf("Exported %d packets → %s\n", count, outPath)
	return nil
}

func decodeRecords(dec recordDecoder, bw *bufio.Writer) (int, error) {
	count := 0
	for record := 1; ; record++ {
		before := dec.InputOffset()
		var rec jsonRecord
		if err := dec.Decode(&rec); err != nil {
			if errors.Is(err, io.EOF) {
				return count, nil
			}
			return count, fmt.Errorf("decoding JSONL record %d at byte %d: %w", record, before, err)
		}
		if after := dec.InputOffset(); after <= before {
			return count, fmt.Errorf("decoding JSONL record %d at byte %d made no progress", record, before)
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
	inclLen := uint32(len(payload)) //nolint:gosec // PCAP snaplen cap below keeps this in range
	origLen := inclLen
	if inclLen > snapLen {
		inclLen = snapLen
	}
	hdr := make([]byte, 16)
	binary.LittleEndian.PutUint32(hdr[0:], uint32(ts.Unix()))            //nolint:gosec // PCAP format requires 32-bit timestamp; Y2038 is a known PCAP limitation
	binary.LittleEndian.PutUint32(hdr[4:], uint32(ts.Nanosecond()/1000)) //nolint:gosec // Nanosecond()/1000 ∈ [0, 999999], safe for uint32
	binary.LittleEndian.PutUint32(hdr[8:], inclLen)
	binary.LittleEndian.PutUint32(hdr[12:], origLen)
	_, _ = bw.Write(hdr)
	_, _ = bw.Write(payload[:inclLen])
}
