// Package runner runs maestro, echoing its output and keeping the end of it.
package runner

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync/atomic"
	"syscall"
)

// TailBytes is how much of each stream is kept. The end of the output is what
// explains a run that died before writing a report.
const TailBytes = 64 << 10

// Result is what maestro did.
type Result struct {
	ExitCode    int
	StdoutTail  string
	StderrTail  string
	Interrupted bool
}

// Run starts argv with stdin inherited, copies its stdout and stderr to the
// given writers while keeping the last TailBytes of each, forwards SIGINT and
// SIGTERM to it, and waits. The error is non-nil only if maestro could not be
// started or waited for.
func Run(argv []string, stdout, stderr io.Writer) (Result, error) {
	if len(argv) == 0 {
		return Result{}, errors.New("runner: nothing to run")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	outTail := &tail{max: TailBytes}
	errTail := &tail{max: TailBytes}
	cmd.Stdin = os.Stdin
	cmd.Stdout = io.MultiWriter(stdout, outTail)
	cmd.Stderr = io.MultiWriter(stderr, errTail)

	// Registered before Start so no signal can slip past between the two.
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)

	if err := cmd.Start(); err != nil {
		return Result{}, err
	}

	var interrupted atomic.Bool
	done := make(chan struct{})
	go func() {
		for {
			select {
			case s := <-sigs:
				interrupted.Store(true)
				// A terminal's Ctrl-C already reaches maestro through the process
				// group, but CI cancellation and `kill` reach only this process.
				// Forwarding lets maestro end its device session either way, and
				// the reporter stays alive long enough to write what happened.
				_ = cmd.Process.Signal(s)
			case <-done:
				return
			}
		}
	}()
	waitErr := cmd.Wait()
	close(done)

	res := Result{
		StdoutTail:  outTail.String(),
		StderrTail:  errTail.String(),
		Interrupted: interrupted.Load(),
	}
	var exitErr *exec.ExitError
	switch {
	case waitErr == nil:
	case errors.As(waitErr, &exitErr):
		res.ExitCode = exitErr.ExitCode()
		if res.ExitCode < 0 { // killed by a signal
			res.ExitCode = 1
			if res.Interrupted {
				res.ExitCode = 130
			}
		}
	default:
		return res, waitErr
	}
	return res, nil
}

// tail keeps the last max bytes written to it.
type tail struct {
	max int
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.max; over > 0 {
		t.buf = append(t.buf[:0], t.buf[over:]...)
	}
	return len(p), nil
}

func (t *tail) String() string { return string(t.buf) }
