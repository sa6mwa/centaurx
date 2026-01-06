package runnergrpc

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"pkt.systems/centaurx/core"
)

type noopRunner struct{}

func (noopRunner) Run(context.Context, core.RunRequest) (core.RunHandle, error) {
	return nil, errors.New("not implemented")
}

func (noopRunner) RunCommand(context.Context, core.RunCommandRequest) (core.CommandHandle, error) {
	return nil, errors.New("not implemented")
}

func TestServerKeepaliveExpires(t *testing.T) {
	t.Parallel()
	socketPath := filepath.Join(t.TempDir(), "runner.sock")
	srv := NewServer(Config{
		SocketPath:        socketPath,
		KeepaliveInterval: 20 * time.Millisecond,
		KeepaliveMisses:   2,
	}, noopRunner{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe(ctx)
	}()

	waitForSocketReady(t, socketPath, 200*time.Millisecond)

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("server exited with error: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("server did not exit after keepalive misses")
	}
}

func TestServerKeepalivePingKeepsAlive(t *testing.T) {
	t.Parallel()
	socketPath := filepath.Join(t.TempDir(), "runner.sock")
	srv := NewServer(Config{
		SocketPath:        socketPath,
		KeepaliveInterval: 50 * time.Millisecond,
		KeepaliveMisses:   2,
	}, noopRunner{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe(ctx)
	}()

	waitForSocketReady(t, socketPath, 200*time.Millisecond)
	client, err := Dial(context.Background(), socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	pingDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		timeout := time.After(200 * time.Millisecond)
		for {
			select {
			case <-timeout:
				close(pingDone)
				return
			case <-ticker.C:
				_ = client.Ping(context.Background())
			}
		}
	}()

	select {
	case err := <-errCh:
		t.Fatalf("server exited early: %v", err)
	case <-pingDone:
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("server exit error after cancel: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("server did not exit after cancel")
	}
}

func waitForSocketReady(t *testing.T, socketPath string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		conn, err := net.Dial("unix", socketPath)
		if err == nil {
			_ = conn.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("socket not ready: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
