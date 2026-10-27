package driver

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/lewta/sendit/internal/config"
)

func TestSFTPConnectionFailureClosesBeforeReleasingWaiter(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	conn := &sftpConnection{
		raw: client, operation: make(chan struct{}, 1), closed: make(chan struct{}),
	}
	conn.operation <- struct{}{}

	activeStarted := make(chan struct{})
	releaseActive := make(chan struct{})
	activeDone := make(chan sftpExecuteResult, 1)
	operationErr := errors.New("operation failed")
	go func() {
		bytesRead, started, err := conn.execute(context.Background(), func() (int64, error) {
			close(activeStarted)
			<-releaseActive
			return 0, operationErr
		})
		activeDone <- sftpExecuteResult{bytesRead: bytesRead, started: started, err: err}
	}()
	<-activeStarted

	waiterSelecting := make(chan struct{})
	waiterCtx := &observedDoneContext{Context: context.Background(), observed: waiterSelecting}
	waiterOperationStarted := make(chan struct{}, 1)
	waiterDone := make(chan sftpExecuteResult, 1)
	go func() {
		bytesRead, started, err := conn.execute(waiterCtx, func() (int64, error) {
			waiterOperationStarted <- struct{}{}
			return 1, nil
		})
		waiterDone <- sftpExecuteResult{bytesRead: bytesRead, started: started, err: err}
	}()
	<-waiterSelecting

	close(releaseActive)
	active := <-activeDone
	if !active.started || !errors.Is(active.err, operationErr) {
		t.Fatalf("active result = %+v, want started operation failure", active)
	}
	select {
	case <-waiterOperationStarted:
		t.Fatal("waiting operation started on the failed connection")
	case waiter := <-waiterDone:
		if waiter.started || !errors.Is(waiter.err, errSFTPConnectionClosed) {
			t.Fatalf("waiter result = %+v, want pre-start closed connection for transparent retry", waiter)
		}
	case <-time.After(time.Second):
		t.Fatal("waiting operation did not wake when the active connection failed")
	}
}

func TestSFTPOperationFailureReconnectsWaitingCaller(t *testing.T) {
	oldRaw, oldPeer := net.Pipe()
	defer oldPeer.Close()
	replacementRaw, replacementPeer := net.Pipe()
	defer replacementPeer.Close()

	cfg := config.SFTPConfig{Username: "testuser", Password: "secret", Insecure: true}
	newConnection := func(raw net.Conn) *sftpConnection {
		conn := &sftpConnection{
			raw: raw, authMaterial: sftpAuthMaterial(cfg),
			operation: make(chan struct{}, 1), closed: make(chan struct{}),
		}
		conn.operation <- struct{}{}
		return conn
	}
	old := newConnection(oldRaw)
	replacement := newConnection(replacementRaw)
	defer replacement.close()

	const cacheKey = "waiting-retry"
	driver := NewSFTPDriver()
	driver.conns[cacheKey] = old
	operationErr := errors.New("active operation failed")
	activeStarted := make(chan struct{})
	releaseActive := make(chan struct{})
	activeDone := make(chan sftpExecuteResult, 1)
	go func() {
		bytesRead, started, phase, _, err := driver.executeSFTPOperation(
			context.Background(), "unused", cacheKey, cfg, make(map[string]string), old,
			func(*sftpConnection) (int64, error) {
				close(activeStarted)
				<-releaseActive
				return 0, operationErr
			},
		)
		activeDone <- sftpExecuteResult{bytesRead: bytesRead, started: started, phase: phase, err: err}
	}()
	<-activeStarted

	waiterSelecting := make(chan struct{})
	waiterCtx := &observedDoneContext{Context: context.Background(), observed: waiterSelecting}
	waiterDone := make(chan sftpExecuteResult, 1)
	go func() {
		bytesRead, started, phase, used, err := driver.executeSFTPOperation(
			waiterCtx, "unused", cacheKey, cfg, make(map[string]string), old,
			func(conn *sftpConnection) (int64, error) {
				if conn != replacement {
					return 0, errors.New("waiter started on failed connection")
				}
				return 7, nil
			},
		)
		if used != replacement && err == nil {
			err = errors.New("waiter did not use replacement connection")
		}
		waiterDone <- sftpExecuteResult{bytesRead: bytesRead, started: started, phase: phase, err: err}
	}()
	<-waiterSelecting

	// Model another caller installing the fresh connection before the failed
	// call performs its identity-checked cache eviction.
	driver.mu.Lock()
	driver.conns[cacheKey] = replacement
	driver.mu.Unlock()
	close(releaseActive)

	select {
	case active := <-activeDone:
		if !active.started || active.phase != "operation" || !errors.Is(active.err, operationErr) {
			t.Fatalf("active result = %+v, want operation failure", active)
		}
	case <-time.After(time.Second):
		t.Fatal("failed active operation did not return")
	}
	select {
	case waiter := <-waiterDone:
		if !waiter.started || waiter.phase != "operation" || waiter.err != nil || waiter.bytesRead != 7 {
			t.Fatalf("waiter result = %+v, want successful replacement operation", waiter)
		}
	case <-time.After(time.Second):
		t.Fatal("waiting operation did not reconnect and finish")
	}
}

func TestSFTPPreStartRetryConnectionFailureReportsConnectPhase(t *testing.T) {
	closed := make(chan struct{})
	close(closed)
	failed := &sftpConnection{operation: make(chan struct{}, 1), closed: closed}
	driver := NewSFTPDriver()
	cfg := config.SFTPConfig{
		Username: "testuser", Password: "secret", Insecure: true,
		Operation: sftpOperationList,
	}

	_, started, phase, _, err := driver.executeSFTPOperation(
		context.Background(), "127.0.0.1:not-a-port", "retry-connect-failure",
		cfg, make(map[string]string), failed, func(*sftpConnection) (int64, error) {
			t.Fatal("operation started before reconnection")
			return 0, nil
		},
	)
	if err == nil {
		t.Fatal("pre-start retry unexpectedly connected")
	}
	if started {
		t.Fatal("pre-start retry reported an operation start")
	}
	if phase != "connect" {
		t.Fatalf("retry failure phase = %q, want connect", phase)
	}
}

type sftpExecuteResult struct {
	bytesRead int64
	started   bool
	phase     string
	err       error
}

type observedDoneContext struct {
	context.Context
	once     sync.Once
	observed chan struct{}
}

func (c *observedDoneContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}
