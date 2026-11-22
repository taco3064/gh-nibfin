package cli

import (
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"unsafe"
)

func terminalAttributes(fd uintptr, request uintptr, value *syscall.Termios) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request,
		uintptr(unsafe.Pointer(value)))
	if errno != 0 {
		return errno
	}
	return nil
}

// openHiddenTerminal opens the controlling terminal with echo, line editing and
// keyboard signals off, so Ctrl-C reaches the reader instead of killing the
// process mid-input. restore puts the saved mode back on every exit path,
// including a termination signal, which is re-raised after restoring.
func openHiddenTerminal() (*os.File, func(), error) {
	refused := errors.New("REBUILD_CLAUDE_CREDENTIAL_REQUIRES_TERMINAL")
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, refused
	}
	fd := tty.Fd()
	var saved syscall.Termios
	if terminalAttributes(fd, syscall.TCGETS, &saved) != nil {
		tty.Close()
		return nil, nil, refused
	}
	hidden := saved
	hidden.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG
	hidden.Cc[syscall.VMIN], hidden.Cc[syscall.VTIME] = 1, 0
	signals := make(chan os.Signal, 1)
	done := make(chan struct{})
	var once sync.Once
	restore := func() {
		once.Do(func() {
			_ = terminalAttributes(fd, syscall.TCSETS, &saved)
			signal.Stop(signals)
			close(done)
			tty.Close()
		})
	}
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
	go func() {
		select {
		case s := <-signals:
			restore()
			signal.Reset(s)
			_ = syscall.Kill(syscall.Getpid(), s.(syscall.Signal))
		case <-done:
		}
	}()
	if terminalAttributes(fd, syscall.TCSETS, &hidden) != nil {
		restore()
		return nil, nil, refused
	}
	return tty, restore, nil
}
