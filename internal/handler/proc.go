//go:build unix

package handler

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// procSpec describes a child process run.
type procSpec struct {
	Path    string
	Args    []string
	Dir     string
	Env     []string
	Stdin   []byte
	Timeout time.Duration
	// Graceful sends SIGINT first (lets Claude Code end its turn), then SIGTERM, then SIGKILL.
	Graceful bool
}

type procResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	TimedOut bool
}

// Grace periods between escalating signals. Variables so tests can shorten them.
var (
	interruptGrace = 10 * time.Second
	termGrace      = 5 * time.Second
)

// runProc runs a process in its own process group and kills the whole group
// on timeout or cancellation.
func runProc(ctx context.Context, s procSpec) (procResult, error) {
	cmd := exec.Command(s.Path, s.Args...)
	cmd.Dir = s.Dir
	cmd.Env = s.Env
	cmd.Stdin = bytes.NewReader(s.Stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return procResult{}, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	timer := time.NewTimer(s.Timeout)
	defer timer.Stop()

	var waitErr error
	timedOut := false
	select {
	case waitErr = <-done:
	case <-timer.C:
		timedOut = true
		waitErr = stopGroup(cmd.Process.Pid, done, s.Graceful)
	case <-ctx.Done():
		timedOut = true
		waitErr = stopGroup(cmd.Process.Pid, done, s.Graceful)
	}

	res := procResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), TimedOut: timedOut}
	var exitErr *exec.ExitError
	switch {
	case waitErr == nil:
	case errors.As(waitErr, &exitErr):
		res.ExitCode = exitErr.ExitCode()
	default:
		return res, waitErr
	}
	return res, nil
}

// stopGroup signals the process group with escalating signals until the
// process exits, and returns the Wait error.
func stopGroup(pid int, done <-chan error, graceful bool) error {
	steps := []struct {
		sig  syscall.Signal
		wait time.Duration
	}{
		{syscall.SIGTERM, termGrace},
		{syscall.SIGKILL, termGrace},
	}
	if graceful {
		steps = append([]struct {
			sig  syscall.Signal
			wait time.Duration
		}{{syscall.SIGINT, interruptGrace}}, steps...)
	}
	for _, st := range steps {
		_ = syscall.Kill(-pid, st.sig)
		select {
		case err := <-done:
			// The leader exited; make sure nothing in the group survives.
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			return err
		case <-time.After(st.wait):
		}
	}
	return <-done
}
