package driver_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"testing"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/driver"
	"github.com/lewta/sendit/internal/task"
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
				case <-time.After(2 * time.Second):
					t.Error("SFTP setup did not stop on cancellation/deadline")
					closeServer()
					<-done
				}
			})
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
