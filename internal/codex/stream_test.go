package codex

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"pkt.systems/centaurx/schema"
)

func TestCombinedStreamEmitsStderr(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()
	stream := newCombinedStream(ctx, stdoutR, stderrR)

	go func() {
		_, _ = fmt.Fprintln(stdoutW, `{"type":"thread.started","thread_id":"thread-1"}`)
		_ = stdoutW.Close()
	}()
	go func() {
		_, _ = fmt.Fprintln(stderrW, "stderr boom")
		_ = stderrW.Close()
	}()

	var sawThread bool
	var sawStderr bool
	for {
		event, err := stream.Next(ctx)
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("Next: %v", err)
		}
		switch event.Type {
		case schema.EventThreadStarted:
			if event.ThreadID == "thread-1" {
				sawThread = true
			}
		case schema.EventError:
			if event.Message == "stderr boom" {
				sawStderr = true
			}
		}
	}
	if !sawThread || !sawStderr {
		t.Fatalf("expected thread and stderr events (thread=%t stderr=%t)", sawThread, sawStderr)
	}
}

func TestCombinedStreamEmitsInvalidJSON(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()
	stream := newCombinedStream(ctx, stdoutR, stderrR)

	go func() {
		_, _ = fmt.Fprintln(stdoutW, "not json")
		_, _ = fmt.Fprintln(stdoutW, `{"type":"thread.started","thread_id":"thread-2"}`)
		_ = stdoutW.Close()
	}()
	go func() {
		_ = stderrW.Close()
	}()

	var sawInvalid bool
	var sawThread bool
	for {
		event, err := stream.Next(ctx)
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("Next: %v", err)
		}
		switch event.Type {
		case schema.EventError:
			if event.Message == "not json" {
				sawInvalid = true
			}
		case schema.EventThreadStarted:
			if event.ThreadID == "thread-2" {
				sawThread = true
			}
		}
	}

	if !sawInvalid || !sawThread {
		t.Fatalf("expected invalid json and thread events (invalid=%t thread=%t)", sawInvalid, sawThread)
	}
}
