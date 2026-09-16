//go:build !windows

package runner

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRun_PassesTheExitCodeThrough(t *testing.T) {
	res, err := Run([]string{"sh", "-c", "exit 3"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 3 {
		t.Fatalf("ExitCode = %d, want 3", res.ExitCode)
	}
}

func TestRun_EchoesOutputAndKeepsItsTail(t *testing.T) {
	var out, errOut bytes.Buffer
	res, err := Run([]string{"sh", "-c", "echo to-stdout; echo to-stderr 1>&2"}, &out, &errOut)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "to-stdout\n" || errOut.String() != "to-stderr\n" {
		t.Fatalf("echoed %q / %q", out.String(), errOut.String())
	}
	if res.StdoutTail != "to-stdout\n" || res.StderrTail != "to-stderr\n" {
		t.Fatalf("tails %q / %q", res.StdoutTail, res.StderrTail)
	}
}

func TestRun_TailIsBoundedAndKeepsTheEnd(t *testing.T) {
	script := `i=0; while [ $i -lt 20000 ]; do echo line-$i 1>&2; i=$((i+1)); done; echo LAST 1>&2`
	res, err := Run([]string{"sh", "-c", script}, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.StderrTail) > TailBytes {
		t.Fatalf("tail is %d bytes, cap is %d", len(res.StderrTail), TailBytes)
	}
	if !strings.HasSuffix(res.StderrTail, "LAST\n") {
		t.Fatalf("tail does not end with the last line: ...%q", res.StderrTail[len(res.StderrTail)-20:])
	}
}

func TestRun_AMissingProgramIsAnError(t *testing.T) {
	if _, err := Run([]string{"/nonexistent/maestro"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("want an error")
	}
}

func TestRun_ForwardsSIGTERMToMaestro(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	got := filepath.Join(dir, "got-term")
	script := `trap 'touch "` + got + `"; exit 42' TERM; touch "` + ready + `"; while true; do sleep 0.1; done`

	done := make(chan Result, 1)
	go func() {
		res, _ := Run([]string{"sh", "-c", script}, &bytes.Buffer{}, &bytes.Buffer{})
		done <- res
	}()

	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child never became ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Signal the test process itself: Run has registered for SIGTERM, so it is
	// caught and forwarded rather than killing the test binary.
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}

	select {
	case res := <-done:
		if res.ExitCode != 42 || !res.Interrupted {
			t.Fatalf("ExitCode = %d, Interrupted = %v; want 42, true", res.ExitCode, res.Interrupted)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("maestro stand-in did not exit after SIGTERM")
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatal("the child never saw SIGTERM")
	}
}
