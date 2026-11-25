package host

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestWaitPID(t *testing.T) {
	c := exec.Command("sleep", "0.3")
	if err := c.Start(); err != nil {
		t.Skip(err)
	}
	reaped := make(chan struct{})
	go func() { c.Wait(); close(reaped) }()
	done := make(chan error, 1)
	go func() { done <- waitPID(context.Background(), c.Process.Pid) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no exit event")
	}
	<-reaped
	// A process that is gone returns at once.
	if err := waitPID(context.Background(), c.Process.Pid); err != nil {
		t.Errorf("gone: %v", err)
	}

	c = exec.Command("sleep", "30")
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { c.Process.Kill(); c.Wait() }()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	if err := waitPID(ctx, c.Process.Pid); err != context.Canceled {
		t.Errorf("cancelled: %v", err)
	}
}
