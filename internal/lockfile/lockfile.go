//go:build unix

// Package lockfile makes sure only one sockdock runs per data directory.
package lockfile

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// ErrLocked means another process holds the lock.
var ErrLocked = errors.New("another sockdock is already running")

// Lock is a held lock.
type Lock struct{ f *os.File }

// Acquire takes an exclusive, non-blocking lock on path and writes our PID.
func Acquire(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		pid := readPID(f)
		f.Close()
		if pid != "" {
			return nil, fmt.Errorf("%w (pid %s)", ErrLocked, pid)
		}
		return nil, ErrLocked
	}
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	return &Lock{f: f}, nil
}

// Release drops the lock.
func (l *Lock) Release() error {
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	return l.f.Close()
}

func readPID(f *os.File) string {
	b := make([]byte, 32)
	n, _ := f.ReadAt(b, 0)
	return strings.TrimSpace(string(b[:n]))
}
