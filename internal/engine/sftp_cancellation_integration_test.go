//go:build integration

package engine_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/engine"
	"github.com/lewta/sendit/internal/metrics"
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
