package driver

import (
	"context"
	"fmt"
	"time"

	"github.com/lewta/sendit/internal/task"
	"nhooyr.io/websocket"
)

// WebSocketDriver connects to a WebSocket endpoint, sends messages, and waits.
type WebSocketDriver struct{}

// NewWebSocketDriver creates a WebSocketDriver.
func NewWebSocketDriver() *WebSocketDriver {
	return &WebSocketDriver{}
}

// Execute opens a WebSocket connection, sends configured messages, optionally
// waits for expected messages, then holds the connection for duration_s.
func (d *WebSocketDriver) Execute(ctx context.Context, t task.Task) task.Result {
	cfg := t.Config.WebSocket

	durationS := cfg.DurationS
	if durationS <= 0 {
		durationS = 10
	}

	connCtx, cancel := context.WithTimeout(ctx, time.Duration(durationS+30)*time.Second)
	defer cancel()

	start := time.Now()

	conn, _, err := websocket.Dial(connCtx, t.URL, nil)
	if err != nil {
		return task.Result{Task: t, Duration: time.Since(start), Error: fmt.Errorf("dialing: %w", err)}
	}
	defer conn.CloseNow() //nolint:errcheck

	// Send configured messages.
	for _, msg := range cfg.SendMessages {
		if err := conn.Write(connCtx, websocket.MessageText, []byte(msg)); err != nil {
			return task.Result{Task: t, Duration: time.Since(start), Error: fmt.Errorf("sending message: %w", err)}
		}
	}

	// Read expected messages.
	received := 0
	readCtx, readCancel := context.WithTimeout(connCtx, time.Duration(durationS)*time.Second)
	defer readCancel()

	for received < cfg.ExpectMessages {
		_, _, err := conn.Read(readCtx)
		if err != nil {
			break
		}
		received++
	}

	// Hold the connection for the configured duration.
	holdCtx, holdCancel := context.WithTimeout(ctx, time.Duration(durationS)*time.Second)
	defer holdCancel()

	<-holdCtx.Done()

	conn.Close(websocket.StatusNormalClosure, "done") //nolint:errcheck,gosec

	return task.Result{
		Task:       t,
		StatusCode: 101, // Switching Protocols — connection established
		Duration:   time.Since(start),
	}
}
