package driver_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/driver"
	"github.com/lewta/sendit/internal/task"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func TestSFTPSetupCancellation(t *testing.T) {
	for _, phase := range []string{"ssh", "subsystem", "version"} {
		for _, deadline := range []bool{false, true} {
			name := phase + "/cancel"
			if deadline {
				name = phase + "/timeout_s"
			}
			t.Run(name, func(t *testing.T) {
				addr, ready, closeServer := stalledSFTPSetup(t, phase)
				defer closeServer()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				cfg := config.SFTPConfig{Username: "testuser", Password: "secret", Insecure: true, TimeoutS: 1, Operation: "list"}
				done := make(chan task.Result, 1)
				go func() { done <- driver.NewSFTPDriver().Execute(ctx, sftpTask("sftp://"+addr+"/", cfg)) }()
				select {
				case <-ready:
				case <-time.After(3 * time.Second):
					t.Fatal("server did not reach stalled setup phase")
				}
				if !deadline {
					cancel()
				}
				select {
				case result := <-done:
					if result.StatusCode != 504 || result.Error != nil {
						t.Fatalf("result = %+v, want mapped status 504 without Go error", result)
					}
					if result.Meta["sftp_timeout_phase"] != "connect" {
						t.Fatalf("timeout phase = %q, want connect", result.Meta["sftp_timeout_phase"])
					}
				case <-time.After(2 * time.Second):
					t.Error("SFTP setup did not stop on cancellation/deadline")
					closeServer()
					<-done
				}
			})
		}
	}
}

func TestSFTPOperationTimeoutStopsStalledWork(t *testing.T) {
	for _, operation := range []string{"upload", "download", "list"} {
		t.Run(operation, func(t *testing.T) {
			server := startControlledSFTPServer(t)
			drv := driver.NewSFTPDriver()
			if operation == "download" {
				seed := sftpOperationResult(t, drv, server.addr, "upload", "/download.bin", 3)
				if seed.StatusCode != 200 {
					t.Fatalf("seeding download file: %+v", seed)
				}
			}

			run := server.gate.arm(operation)
			path := "/"
			if operation != "list" {
				path = "/" + operation + ".bin"
			}
			started := time.Now()
			done := make(chan task.Result, 1)
			go func() { done <- sftpOperationResult(t, drv, server.addr, operation, path, 1) }()
			waitForGate(t, run)

			result, returnedBeforeRelease := waitForOperationResult(done, run, 1500*time.Millisecond)
			if !returnedBeforeRelease {
				t.Error("operation did not return after its configured timeout")
			}
			if result.StatusCode != 504 || result.Error != nil {
				t.Fatalf("result = %+v, want mapped status 504 without Go error", result)
			}
			if result.Meta["sftp_timeout_phase"] != "operation" {
				t.Fatalf("timeout phase = %q, want operation", result.Meta["sftp_timeout_phase"])
			}
			if elapsed := time.Since(started); elapsed > 1600*time.Millisecond {
				t.Errorf("operation returned after %v, want prompt timeout", elapsed)
			}
			waitForGateExit(t, run)

			if operation == "upload" {
				check := sftpOperationResult(t, drv, server.addr, "download", path, 3)
				if check.StatusCode != 404 {
					t.Fatalf("timed-out upload continued and changed the remote file: %+v", check)
				}
			}
		})
	}
}

func TestSFTPOperationTimeoutEvictsConnectionAndRetrySucceeds(t *testing.T) {
	server := startControlledSFTPServer(t)
	drv := driver.NewSFTPDriver()
	if result := sftpOperationResult(t, drv, server.addr, "list", "/", 3); result.StatusCode != 200 {
		t.Fatalf("priming cached connection: %+v", result)
	}
	if got := server.accepted.Load(); got != 1 {
		t.Fatalf("connections after prime = %d, want 1", got)
	}

	run := server.gate.arm("list")
	done := make(chan task.Result, 1)
	go func() { done <- sftpOperationResult(t, drv, server.addr, "list", "/", 1) }()
	waitForGate(t, run)
	result, returnedBeforeRelease := waitForOperationResult(done, run, 1500*time.Millisecond)
	if !returnedBeforeRelease || result.StatusCode != 504 {
		t.Fatalf("timed operation = %+v, returned before release = %t", result, returnedBeforeRelease)
	}

	if result := sftpOperationResult(t, drv, server.addr, "list", "/", 3); result.StatusCode != 200 {
		t.Fatalf("retry after timeout: %+v", result)
	}
	if got := server.accepted.Load(); got != 2 {
		t.Fatalf("connections after retry = %d, want 2", got)
	}
}

func TestSFTPConcurrentCachedUsersSerializeAndWaitingCancelDoesNotEvict(t *testing.T) {
	server := startControlledSFTPServer(t)
	drv := driver.NewSFTPDriver()
	if result := sftpOperationResult(t, drv, server.addr, "list", "/", 3); result.StatusCode != 200 {
		t.Fatalf("priming cached connection: %+v", result)
	}

	run := server.gate.arm("list")
	activeDone := make(chan task.Result, 1)
	go func() { activeDone <- sftpOperationResult(t, drv, server.addr, "list", "/", 5) }()
	waitForGate(t, run)

	ctx, cancel := context.WithCancel(context.Background())
	waitingDone := make(chan task.Result, 1)
	go func() {
		cfg := operationConfig("list", 5)
		waitingDone <- drv.Execute(ctx, sftpTask("sftp://"+server.addr+"/", cfg))
	}()
	cancel()
	select {
	case result := <-waitingDone:
		if result.StatusCode != 504 {
			t.Fatalf("waiting result = %+v, want cancellation", result)
		}
		if phase := result.Meta["sftp_timeout_phase"]; phase != "connect" && phase != "operation" {
			t.Fatalf("waiting timeout phase = %q, want connect or operation based on cancellation race", phase)
		}
	case <-time.After(time.Second):
		run.releaseWork()
		t.Fatal("caller waiting for the cached connection ignored cancellation")
	}
	select {
	case result := <-activeDone:
		t.Fatalf("waiting caller disrupted active operation: %+v", result)
	default:
	}

	run.releaseWork()
	if result := <-activeDone; result.StatusCode != 200 {
		t.Fatalf("active operation after release: %+v", result)
	}
	if result := sftpOperationResult(t, drv, server.addr, "list", "/", 3); result.StatusCode != 200 {
		t.Fatalf("cached connection after waiting cancellation: %+v", result)
	}
	if got := server.accepted.Load(); got != 1 {
		t.Fatalf("connections = %d, want one reusable connection", got)
	}
}

func TestSFTPParentCancellationStopsActiveOperation(t *testing.T) {
	server := startControlledSFTPServer(t)
	drv := driver.NewSFTPDriver()
	run := server.gate.arm("list")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan task.Result, 1)
	go func() {
		done <- drv.Execute(ctx, sftpTask("sftp://"+server.addr+"/", operationConfig("list", 30)))
	}()
	waitForGate(t, run)
	cancel()
	result, returnedBeforeRelease := waitForOperationResult(done, run, time.Second)
	if !returnedBeforeRelease {
		t.Error("active SFTP operation ignored parent cancellation")
	}
	if result.StatusCode != 504 || result.Meta["sftp_timeout_phase"] != "operation" {
		t.Fatalf("result = %+v, want operation cancellation", result)
	}
	waitForGateExit(t, run)
}

func TestSFTPRepeatedTimeoutsKeepGoroutineGrowthBounded(t *testing.T) {
	server := startControlledSFTPServer(t)
	drv := driver.NewSFTPDriver()
	baseline := runtime.NumGoroutine()

	for range 8 {
		run := server.gate.arm("list")
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan task.Result, 1)
		go func() {
			done <- drv.Execute(ctx, sftpTask("sftp://"+server.addr+"/", operationConfig("list", 30)))
		}()
		waitForGate(t, run)
		cancel()
		result, returnedBeforeRelease := waitForOperationResult(done, run, time.Second)
		if !returnedBeforeRelease || result.StatusCode != 504 {
			t.Fatalf("repeated canceled operation = %+v, returned before release = %t", result, returnedBeforeRelease)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		if server.active.Load() == 0 && runtime.NumGoroutine() <= baseline+6 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("goroutines/connections did not settle: before=%d after=%d active_connections=%d", baseline, runtime.NumGoroutine(), server.active.Load())
}

func operationConfig(operation string, timeoutS int) config.SFTPConfig {
	return config.SFTPConfig{
		Username:  "testuser",
		Password:  "secret",
		Insecure:  true,
		TimeoutS:  timeoutS,
		Operation: operation,
	}
}

func sftpOperationResult(t *testing.T, drv *driver.SFTPDriver, addr, operation, path string, timeoutS int) task.Result {
	t.Helper()
	return drv.Execute(context.Background(), sftpTask("sftp://"+addr+path, operationConfig(operation, timeoutS)))
}

func waitForGate(t *testing.T, run *sftpGateRun) {
	t.Helper()
	select {
	case <-run.entered:
	case <-time.After(3 * time.Second):
		run.releaseWork()
		t.Fatal("server did not enter the stalled SFTP operation")
	}
}

func waitForGateExit(t *testing.T, run *sftpGateRun) {
	t.Helper()
	select {
	case <-run.exited:
	case <-time.After(time.Second):
		run.releaseWork()
		t.Fatal("canceled SFTP operation remained active after its connection closed")
	}
}

func waitForOperationResult(done <-chan task.Result, run *sftpGateRun, timeout time.Duration) (task.Result, bool) {
	select {
	case result := <-done:
		return result, true
	case <-time.After(timeout):
		run.releaseWork()
		return <-done, false
	}
}

type sftpGateRun struct {
	entered     chan struct{}
	release     chan struct{}
	exited      chan struct{}
	enterOnce   sync.Once
	releaseOnce sync.Once
	exitOnce    sync.Once
}

func (r *sftpGateRun) releaseWork() {
	r.releaseOnce.Do(func() { close(r.release) })
}

type sftpOperationGate struct {
	mu        sync.Mutex
	operation string
	run       *sftpGateRun
}

func (g *sftpOperationGate) arm(operation string) *sftpGateRun {
	run := &sftpGateRun{entered: make(chan struct{}), release: make(chan struct{}), exited: make(chan struct{})}
	g.mu.Lock()
	g.operation = operation
	g.run = run
	g.mu.Unlock()
	return run
}

func (g *sftpOperationGate) wait(operation string, ctx context.Context, peerClosed <-chan struct{}) error {
	g.mu.Lock()
	if g.operation != operation || g.run == nil {
		g.mu.Unlock()
		return nil
	}
	run := g.run
	g.run = nil
	g.operation = ""
	g.mu.Unlock()

	run.enterOnce.Do(func() { close(run.entered) })
	defer run.exitOnce.Do(func() { close(run.exited) })
	select {
	case <-run.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-peerClosed:
		return net.ErrClosed
	}
}

type stallingReader struct {
	base       sftp.FileReader
	gate       *sftpOperationGate
	peerClosed <-chan struct{}
}

func (h stallingReader) Fileread(request *sftp.Request) (io.ReaderAt, error) {
	if err := h.gate.wait("download", request.Context(), h.peerClosed); err != nil {
		return nil, err
	}
	return h.base.Fileread(request)
}

type stallingWriter struct {
	base       sftp.FileWriter
	gate       *sftpOperationGate
	peerClosed <-chan struct{}
}

func (h stallingWriter) Filewrite(request *sftp.Request) (io.WriterAt, error) {
	if err := h.gate.wait("upload", request.Context(), h.peerClosed); err != nil {
		return nil, err
	}
	return h.base.Filewrite(request)
}

type stallingLister struct {
	base       sftp.FileLister
	gate       *sftpOperationGate
	peerClosed <-chan struct{}
}

func (h stallingLister) Filelist(request *sftp.Request) (sftp.ListerAt, error) {
	if err := h.gate.wait("list", request.Context(), h.peerClosed); err != nil {
		return nil, err
	}
	return h.base.Filelist(request)
}

type controlledSFTPServer struct {
	addr     string
	listener net.Listener
	gate     *sftpOperationGate
	accepted atomic.Int64
	active   atomic.Int64
	mu       sync.Mutex
	conns    map[net.Conn]struct{}
}

func startControlledSFTPServer(t *testing.T) *controlledSFTPServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	sshConfig := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if conn.User() == "testuser" && string(password) == "secret" {
				return nil, nil
			}
			return nil, errors.New("invalid credentials")
		},
	}
	sshConfig.AddHostKey(signer)
	server := &controlledSFTPServer{
		addr: listener.Addr().String(), listener: listener, gate: &sftpOperationGate{}, conns: make(map[net.Conn]struct{}),
	}
	t.Cleanup(func() {
		_ = listener.Close()
		server.mu.Lock()
		for conn := range server.conns {
			_ = conn.Close()
		}
		server.mu.Unlock()
	})
	go server.serve(sshConfig)
	return server
}

func (s *controlledSFTPServer) serve(sshConfig *ssh.ServerConfig) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.accepted.Add(1)
		s.active.Add(1)
		s.mu.Lock()
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		go s.serveConn(conn, sshConfig)
	}
}

func (s *controlledSFTPServer) serveConn(conn net.Conn, sshConfig *ssh.ServerConfig) {
	defer func() {
		_ = conn.Close()
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
		s.active.Add(-1)
	}()
	serverConn, channels, requests, err := ssh.NewServerConn(conn, sshConfig)
	if err != nil {
		return
	}
	peerClosed := make(chan struct{})
	go func() {
		_ = serverConn.Wait()
		close(peerClosed)
	}()
	base := sftp.InMemHandler()
	handlers := sftp.Handlers{
		FileGet:  stallingReader{base: base.FileGet, gate: s.gate, peerClosed: peerClosed},
		FilePut:  stallingWriter{base: base.FilePut, gate: s.gate, peerClosed: peerClosed},
		FileCmd:  base.FileCmd,
		FileList: stallingLister{base: base.FileList, gate: s.gate, peerClosed: peerClosed},
	}
	go ssh.DiscardRequests(requests)
	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}
		channel, requestStream, err := newChannel.Accept()
		if err != nil {
			return
		}
		for request := range requestStream {
			var payload sshSubsystemRequest
			if request.Type != "subsystem" || ssh.Unmarshal(request.Payload, &payload) != nil || payload.Subsystem != "sftp" {
				_ = request.Reply(false, nil)
				continue
			}
			_ = request.Reply(true, nil)
			requestServer := sftp.NewRequestServer(channel, handlers)
			_ = requestServer.Serve()
			_ = requestServer.Close()
			return
		}
	}
}

func TestSFTPSetupAlreadyCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cfg := config.SFTPConfig{Username: "testuser", Password: "secret", Insecure: true}
	result := driver.NewSFTPDriver().Execute(ctx, sftpTask("sftp://127.0.0.1:1/", cfg))
	if result.StatusCode != 504 {
		t.Fatalf("status = %d, want 504", result.StatusCode)
	}
}

func TestSFTPSetupCachedConnectionSurvivesCancellation(t *testing.T) {
	root := t.TempDir()
	addr := startSFTPServer(t, root)
	drv := driver.NewSFTPDriver()
	cfg := config.SFTPConfig{Username: "testuser", Password: "secret", Insecure: true, Operation: "list"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := drv.Execute(ctx, sftpTask("sftp://"+addr+root, cfg))
	if first.StatusCode != 200 {
		t.Fatalf("first status = %d, want 200", first.StatusCode)
	}
	cancel()
	for range 10 {
		result := drv.Execute(context.Background(), sftpTask("sftp://"+addr+root, cfg))
		if result.StatusCode != 200 || result.Meta["sftp_host_key_type"] != "" {
			t.Fatalf("cached connection not reused successfully: %+v", result)
		}
	}
}

func stalledSFTPSetup(t *testing.T, phase string) (string, <-chan struct{}, func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)
	ready := make(chan struct{})
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := lis.Accept()
		if err != nil {
			return
		}
		accepted <- conn
		if phase == "ssh" {
			close(ready)
			_, _ = io.Copy(io.Discard, conn)
			return
		}
		server, chans, reqs, err := ssh.NewServerConn(conn, cfg)
		if err != nil {
			return
		}
		defer server.Close()
		go ssh.DiscardRequests(reqs)
		for ch := range chans {
			channel, requests, err := ch.Accept()
			if err != nil {
				return
			}
			for req := range requests {
				if req.Type == "subsystem" {
					if phase == "version" {
						_ = req.Reply(true, nil)
					}
					close(ready)
					_, _ = io.Copy(io.Discard, channel)
					return
				}
				_ = req.Reply(false, nil)
			}
		}
	}()
	return lis.Addr().String(), ready, func() {
		_ = lis.Close()
		select {
		case conn := <-accepted:
			_ = conn.Close()
		default:
		}
	}
}
