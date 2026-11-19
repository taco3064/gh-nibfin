package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

type callerOnlyContext struct {
	context.Context
	t *testing.T
}

func TestMaintenanceCallerCancellationStopsStartedProcess(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "ready")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "started-management.py")
	program := "#!/usr/bin/python3 -I\nimport signal\nwith open(" +
		strconv.Quote(fifo) + ", 'wb', buffering=0) as ready:\n" +
		"    ready.write(b'1')\n    signal.pause()\n"
	if err := os.WriteFile(path, []byte(program), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	process := coreResultProcess{ctx: ctx,
		resolve: func(string) (string, error) { return path, nil }}
	finished := make(chan error, 1)
	go func() {
		output, err := process.Run(maintenanceBridge, nil, nil)
		if len(output) != 0 {
			finished <- errors.New("cancelled child disclosed output")
			return
		}
		finished <- decodeCoreSourceMaintenance(output, err)
	}()
	ready, err := os.Open(fifo)
	if err != nil {
		t.Fatal(err)
	}
	defer ready.Close()
	var marker [1]byte
	if _, err := ready.Read(marker[:]); err != nil || marker[0] != '1' {
		t.Fatal("child failed to start", err)
	}
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) ||
		coreOperationCause(err) != nil {
		t.Fatal("started management did not retain caller cancellation", err)
	}
}

func (ctx callerOnlyContext) Deadline() (time.Time, bool) {
	ctx.t.Fatal("management operation attempted to add a deadline")
	return time.Time{}, false
}

func TestMaintenanceProcessAddsNoDeadlineAndPreservesCallerCancellation(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(strconv.FormatBool(deadline), func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			var ctx context.Context = callerOnlyContext{parent, t}
			if deadline {
				// The caller's expired deadline is authoritative; no child starts.
				expired, stop := context.WithDeadline(parent, time.Time{})
				defer stop()
				ctx = expired
			}
			path := filepath.Join(t.TempDir(), "management-child.py")
			program := "#!/usr/bin/python3 -I\nimport sys\nsys.stdout.write('ready')\n"
			if err := os.WriteFile(path, []byte(program), 0700); err != nil {
				t.Fatal(err)
			}
			process := coreResultProcess{ctx: ctx,
				resolve: func(string) (string, error) { return path, nil }}
			output, err := process.Run(maintenanceBridge, nil, nil)
			if deadline {
				if !errors.Is(err, context.DeadlineExceeded) || len(output) != 0 {
					t.Fatal("caller deadline was lost", string(output), err)
				}
				return
			}
			if err != nil || string(output) != "ready" {
				t.Fatal("deadline-free management failed", string(output), err)
			}
			cancel()
			output, err = process.Run(maintenanceBridge, nil, nil)
			if !errors.Is(err, context.Canceled) || len(output) != 0 ||
				coreOperationCause(err) != nil {
				t.Fatal("caller cancellation acquired success/API result", string(output), err)
			}
		})
	}
}
