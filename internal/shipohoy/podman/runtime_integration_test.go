package podman

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"pkt.systems/centaurx/internal/shipohoy"
)

func TestRuntimeLifecycle(t *testing.T) {
	rt := newTestRuntime(t)
	ctx := context.Background()
	ensureTestImage(t, rt, "docker.io/library/busybox:1.36")
	name := fmt.Sprintf("centaurx-test-%d", time.Now().UnixNano())
	spec := shipohoy.ContainerSpec{
		Name:           name,
		Image:          "docker.io/library/busybox:1.36",
		Command:        []string{"sh", "-c", "echo ready; httpd -f -p 18081"},
		HostNetwork:    true,
		LogBufferBytes: 128 * 1024,
		Labels: map[string]string{
			"shipohoy.run_id": name,
		},
	}

	handle, err := rt.EnsureRunning(ctx, spec)
	if err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	t.Cleanup(func() {
		_ = rt.Stop(ctx, handle)
		_ = rt.Remove(ctx, handle)
	})

	if err := rt.WaitForLog(ctx, handle, shipohoy.WaitLogSpec{
		Text:    "ready",
		Stream:  shipohoy.LogStdout,
		Timeout: 10 * time.Second,
	}); err != nil {
		t.Fatalf("WaitForLog: %v", err)
	}

	if err := rt.WaitForPort(ctx, handle, shipohoy.WaitPortSpec{
		Port:    18081,
		Timeout: 10 * time.Second,
	}); err != nil {
		t.Fatalf("WaitForPort: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	result, err := rt.Exec(ctx, handle, shipohoy.ExecSpec{
		Command: []string{"sh", "-c", "echo exec-ok; echo err-ok 1>&2"},
		Stdout:  &stdout,
		Stderr:  &stderr,
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("Exec exit code: %d", result.ExitCode)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("exec-ok")) {
		t.Fatalf("Exec stdout missing marker: %q", stdout.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("err-ok")) {
		t.Fatalf("Exec stderr missing marker: %q", stderr.String())
	}

	if err := rt.Stop(ctx, handle); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := rt.Remove(ctx, handle); err != nil {
		t.Fatalf("Remove: %v", err)
	}
}

func TestRuntimeJanitor(t *testing.T) {
	rt := newTestRuntime(t)
	ctx := context.Background()
	ensureTestImage(t, rt, "docker.io/library/busybox:1.36")
	name := fmt.Sprintf("centaurx-janitor-%d", time.Now().UnixNano())
	spec := shipohoy.ContainerSpec{
		Name:           name,
		Image:          "docker.io/library/busybox:1.36",
		Command:        []string{"sh", "-c", "sleep 60"},
		HostNetwork:    true,
		LogBufferBytes: 128 * 1024,
		Labels: map[string]string{
			"shipohoy.run_id": name,
		},
	}
	handle, err := rt.EnsureRunning(ctx, spec)
	if err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	t.Cleanup(func() {
		_ = rt.Stop(ctx, handle)
		_ = rt.Remove(ctx, handle)
	})

	removed, err := rt.Janitor(ctx, shipohoy.JanitorSpec{
		LabelSelector: map[string]string{"shipohoy.run_id": name},
	})
	if err != nil {
		t.Fatalf("Janitor: %v", err)
	}
	if removed == 0 {
		t.Fatalf("Janitor removed 0 containers")
	}
}

func newTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping podman integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addr := os.Getenv("CENTAURX_PODMAN_ADDR")
	rt, err := New(ctx, Config{Address: addr})
	if err != nil {
		t.Fatalf("podman not available: %v", err)
	}
	return rt
}

func ensureTestImage(t *testing.T, rt *Runtime, image string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := rt.EnsureImage(ctx, image); err != nil {
		t.Fatalf("EnsureImage(%s): %v", image, err)
	}
}
