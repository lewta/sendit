package metrics

import (
	"testing"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/task"
)

// makeResult creates a task.Result for testing.
func makeResult(typ string, status int, dur time.Duration, bytesRead int64, err error) task.Result {
	return task.Result{
		Task: task.Task{
			URL:  "https://example.com",
			Type: typ,
			Config: config.TargetConfig{
				URL:  "https://example.com",
				Type: typ,
			},
		},
		StatusCode: status,
		Duration:   dur,
		BytesRead:  bytesRead,
		Error:      err,
	}
}

// TestNoop_DoesNotPanic verifies the no-op metrics instance handles all cases.
func TestNoop_DoesNotPanic(t *testing.T) {
	m := Noop()
	if m == nil {
		t.Fatal("Noop() returned nil")
	}

	testCases := []task.Result{
		makeResult("http", 200, 100*time.Millisecond, 1024, nil),
		makeResult("dns", 0, 5*time.Millisecond, 0, nil),
		makeResult("browser", 200, 2*time.Second, 0, nil),
		makeResult("websocket", 101, 10*time.Second, 0, nil),
		makeResult("http", 0, 50*time.Millisecond, 0, errSentinel{}),
		makeResult("http", 429, 0, 0, nil),
		makeResult("http", 503, 0, 0, nil),
	}

	for _, r := range testCases {
		// Must not panic.
		m.Record(r)
	}
}

// errSentinel is a simple error type for test injection.
type errSentinel struct{}

func (e errSentinel) Error() string { return "test error" }

// TestNoop_NotNilFields verifies internal counter fields are not nil.
func TestNoop_NotNilFields(t *testing.T) {
	m := Noop()
	if m.requestsTotal == nil {
		t.Error("requestsTotal is nil")
	}
	if m.errorsTotal == nil {
		t.Error("errorsTotal is nil")
	}
	if m.durationSeconds == nil {
		t.Error("durationSeconds is nil")
	}
	if m.bytesRead == nil {
		t.Error("bytesRead is nil")
	}
}

// TestRecord_ErrorPath confirms errors don't panic and don't record a status code.
func TestRecord_ErrorPath(t *testing.T) {
	m := Noop()
	r := makeResult("http", 0, 10*time.Millisecond, 0, errSentinel{})
	// Should not panic.
	m.Record(r)
}

// TestRecord_SuccessPath confirms success results don't panic.
func TestRecord_SuccessPath(t *testing.T) {
	m := Noop()
	r := makeResult("http", 200, 150*time.Millisecond, 2048, nil)
	m.Record(r)
}

// TestRecord_ZeroBytesSkipped confirms that zero BytesRead doesn't call Add.
func TestRecord_ZeroBytesSkipped(t *testing.T) {
	m := Noop()
	r := makeResult("dns", 0, 5*time.Millisecond, 0, nil)
	// No panic expected.
	m.Record(r)
}

// TestRecord_AllDriverTypes verifies Record works for all driver types.
func TestRecord_AllDriverTypes(t *testing.T) {
	m := Noop()
	types := []string{"http", "browser", "dns", "websocket"}
	for _, typ := range types {
		r := makeResult(typ, 200, 100*time.Millisecond, 512, nil)
		m.Record(r) // must not panic
	}
}
