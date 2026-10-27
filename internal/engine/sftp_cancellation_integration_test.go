//go:build integration

package engine_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/engine"
	"github.com/lewta/sendit/internal/metrics"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func TestIntegrationSFTPShutdownDuringSetup(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer lis.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := lis.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	cfg := testCfg([]config.TargetConfig{{
		URL: "sftp://" + lis.Addr().String() + "/", Type: "sftp", Weight: 1,
		SFTP: config.SFTPConfig{Username: "test", Password: "secret", Insecure: true, TimeoutS: 30},
	}})
	cfg.Limits.MaxWorkers = 1
	eng, err := engine.New(cfg, metrics.Noop())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		eng.Run(ctx)
		close(done)
	}()
	select {
	case conn := <-accepted:
		defer conn.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("engine shutdown blocked on stalled SFTP handshake")
			_ = conn.Close()
			<-done
		}
	case <-time.After(5 * time.Second):
		t.Fatal("engine did not connect to SFTP server")
	}
}

func TestIntegrationSFTPShutdownDuringOperation(t *testing.T) {
	server := startStalledSFTPOperationServer(t)
	cfg := testCfg([]config.TargetConfig{{
		URL: "sftp://" + server.addr + "/", Type: "sftp", Weight: 1,
		SFTP: config.SFTPConfig{
			Username: "testuser", Password: "secret", Insecure: true,
			TimeoutS: 30, Operation: "list",
		},
	}})
	cfg.Limits.MaxWorkers = 1
	eng, err := engine.New(cfg, metrics.Noop())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		eng.Run(ctx)
		close(done)
	}()
	select {
	case <-server.entered:
	case err := <-server.errs:
		cancel()
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("engine did not begin the stalled SFTP operation")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("engine shutdown blocked on a stalled SFTP operation")
	}
	select {
	case <-server.exited:
	case <-time.After(time.Second):
		t.Fatal("SFTP operation remained active after engine shutdown")
	}
}

type stalledSFTPOperationServer struct {
	addr     string
	listener net.Listener
	entered  chan struct{}
	exited   chan struct{}
	errs     chan error
	close    sync.Once
}

func startStalledSFTPOperationServer(t *testing.T) *stalledSFTPOperationServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		_ = listener.Close()
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
	server := &stalledSFTPOperationServer{
		addr: listener.Addr().String(), listener: listener,
		entered: make(chan struct{}), exited: make(chan struct{}), errs: make(chan error, 1),
	}
	t.Cleanup(func() { _ = listener.Close() })
	go server.serve(sshConfig)
	return server
}

func (s *stalledSFTPOperationServer) serve(sshConfig *ssh.ServerConfig) {
	raw, err := s.listener.Accept()
	if err != nil {
		return
	}
	defer raw.Close()
	serverConn, channels, requests, err := ssh.NewServerConn(raw, sshConfig)
	if err != nil {
		s.errs <- err
		return
	}
	peerClosed := make(chan struct{})
	go func() {
		_ = serverConn.Wait()
		close(peerClosed)
	}()
	go ssh.DiscardRequests(requests)
	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}
		channel, requestStream, err := newChannel.Accept()
		if err != nil {
			s.errs <- err
			return
		}
		for request := range requestStream {
			var payload struct{ Subsystem string }
			if request.Type != "subsystem" || ssh.Unmarshal(request.Payload, &payload) != nil || payload.Subsystem != "sftp" {
				_ = request.Reply(false, nil)
				continue
			}
			_ = request.Reply(true, nil)
			base := sftp.InMemHandler()
			requestServer := sftp.NewRequestServer(channel, sftp.Handlers{
				FileGet: base.FileGet, FilePut: base.FilePut, FileCmd: base.FileCmd,
				FileList: stalledSFTPLister{base: base.FileList, entered: s.entered, exited: s.exited, peerClosed: peerClosed, close: &s.close},
			})
			_ = requestServer.Serve()
			_ = requestServer.Close()
			return
		}
	}
}

type stalledSFTPLister struct {
	base       sftp.FileLister
	entered    chan struct{}
	exited     chan struct{}
	peerClosed <-chan struct{}
	close      *sync.Once
}

func (l stalledSFTPLister) Filelist(request *sftp.Request) (sftp.ListerAt, error) {
	l.close.Do(func() { close(l.entered) })
	defer close(l.exited)
	select {
	case <-request.Context().Done():
		return nil, request.Context().Err()
	case <-l.peerClosed:
		return nil, net.ErrClosed
	}
}
