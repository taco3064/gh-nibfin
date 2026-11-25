package host

import (
	"context"
	"errors"
	"sync"

	"golang.org/x/sys/unix"
)

// waitPID waits with kqueue until the process pid exits; a process that has
// already exited returns at once. A cancelled ctx ends the wait.
func waitPID(ctx context.Context, pid int) error {
	kq, err := unix.Kqueue()
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	done := make(chan struct{})
	defer func() {
		close(done)
		wg.Wait()
		unix.Close(kq)
	}()

	var user [1]unix.Kevent_t
	unix.SetKevent(&user[0], 0, unix.EVFILT_USER, unix.EV_ADD|unix.EV_CLEAR)
	if _, err := unix.Kevent(kq, user[:], nil, nil); err != nil {
		return err
	}
	var proc [1]unix.Kevent_t
	unix.SetKevent(&proc[0], pid, unix.EVFILT_PROC, unix.EV_ADD|unix.EV_ONESHOT)
	proc[0].Fflags = unix.NOTE_EXIT
	if _, err := unix.Kevent(kq, proc[:], nil, nil); err != nil {
		if errors.Is(err, unix.ESRCH) {
			return nil
		}
		return err
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
			var t [1]unix.Kevent_t
			unix.SetKevent(&t[0], 0, unix.EVFILT_USER, 0)
			t[0].Fflags = unix.NOTE_TRIGGER
			unix.Kevent(kq, t[:], nil, nil)
		case <-done:
		}
	}()
	var out [1]unix.Kevent_t
	for {
		n, err := unix.Kevent(kq, nil, out[:], nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if n == 1 && out[0].Filter == unix.EVFILT_PROC {
			return nil
		}
		if n == 1 && out[0].Filter == unix.EVFILT_USER {
			return ctx.Err()
		}
	}
}
