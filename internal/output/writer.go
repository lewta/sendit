package output

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/task"
	"github.com/rs/zerolog/log"
)

const chanBuf = 512

// Writer serialises task.Result values to a file in JSONL or CSV format.
// Send is non-blocking; results are dropped (with a warning) if the internal
// buffer is full. Close drains the buffer and flushes the file.
type Writer struct {
	ch   chan task.Result
	done chan struct{}
}

// New opens the output file and starts the background writer goroutine.
// The caller must call Close() when done.
func New(cfg config.OutputConfig) (*Writer, error) {
	flag := os.O_CREATE | os.O_WRONLY
	if cfg.Append {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}

	f, err := os.OpenFile(cfg.File, flag, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening output file %q: %w", cfg.File, err)
	}

	w := &Writer{
		ch:   make(chan task.Result, chanBuf),
		done: make(chan struct{}),
	}
	go w.run(f, cfg.Format, cfg.Append)
	return w, nil
}

// Send enqueues a result for writing. Non-blocking; drops if buffer is full.
func (w *Writer) Send(r task.Result) {
	select {
	case w.ch <- r:
	default:
		log.Warn().Msg("output writer buffer full, dropping result")
	}
}

// Close drains the channel and closes the file.
func (w *Writer) Close() {
	close(w.ch)
	<-w.done
}

func (w *Writer) run(f *os.File, format string, appendMode bool) {
	defer close(w.done)
	bw := bufio.NewWriter(f)
	defer func() {
		_ = bw.Flush()
		_ = f.Close()
	}()

	switch format {
	case "csv":
		w.runCSV(bw, appendMode)
	default: // jsonl
		w.runJSONL(bw)
	}
}

type record struct {
	TS         string `json:"ts"`
	URL        string `json:"url"`
	Type       string `json:"type"`
	Status     int    `json:"status"`
	DurationMs int64  `json:"duration_ms"`
	Bytes      int64  `json:"bytes"`
	Error      string `json:"error,omitempty"`
}

func toRecord(r task.Result) record {
	errStr := ""
	if r.Error != nil {
		errStr = r.Error.Error()
	}
	return record{
		TS:         time.Now().UTC().Format(time.RFC3339),
		URL:        r.Task.URL,
		Type:       r.Task.Type,
		Status:     r.StatusCode,
		DurationMs: r.Duration.Milliseconds(),
		Bytes:      r.BytesRead,
		Error:      errStr,
	}
}

func (w *Writer) runJSONL(bw *bufio.Writer) {
	enc := json.NewEncoder(bw)
	for r := range w.ch {
		if err := enc.Encode(toRecord(r)); err != nil {
			log.Warn().Err(err).Msg("output writer: failed to encode result")
			continue
		}
		_ = bw.Flush()
	}
}

func (w *Writer) runCSV(bw *bufio.Writer, appendMode bool) {
	cw := csv.NewWriter(bw)
	if !appendMode {
		_ = cw.Write([]string{"ts", "url", "type", "status", "duration_ms", "bytes", "error"})
		cw.Flush()
	}
	for r := range w.ch {
		rec := toRecord(r)
		row := []string{
			rec.TS,
			rec.URL,
			rec.Type,
			fmt.Sprintf("%d", rec.Status),
			fmt.Sprintf("%d", rec.DurationMs),
			fmt.Sprintf("%d", rec.Bytes),
			rec.Error,
		}
		if err := cw.Write(row); err != nil {
			log.Warn().Err(err).Msg("output writer: failed to write CSV row")
			continue
		}
		cw.Flush()
	}
}
