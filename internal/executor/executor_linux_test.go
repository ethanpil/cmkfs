//go:build linux

package executor

import (
	"context"
	"testing"
	"time"

	"github.com/ethanpil/cmkfs/internal/safety"
)

func passGate() (safety.Report, bool) { return safety.Report{}, true }

func collect(t *testing.T, ch <-chan Event) (lines []string, done Event) {
	t.Helper()
	timeout := time.After(30 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return lines, done
			}
			if ev.Line != "" {
				lines = append(lines, ev.Line)
			}
			if ev.Done {
				done = ev
			}
		case <-timeout:
			t.Fatal("executor did not finish")
		}
	}
}

func TestCombinedOutputOrder(t *testing.T) {
	// stdout and stderr share one pipe, so interleaving is preserved.
	ch := Run(context.Background(), []string{"sh", "-c", "echo one; echo two >&2; echo three"}, passGate)
	lines, done := collect(t, ch)
	if done.Exit != 0 || done.Aborted {
		t.Fatalf("bad done event: %+v", done)
	}
	want := []string{"one", "two", "three"}
	if len(lines) != 3 {
		t.Fatalf("want %v, got %v", want, lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("want %v, got %v", want, lines)
		}
	}
}

func TestExitCode(t *testing.T) {
	ch := Run(context.Background(), []string{"sh", "-c", "exit 7"}, passGate)
	_, done := collect(t, ch)
	if done.Exit != 7 {
		t.Fatalf("want exit 7, got %+v", done)
	}
}

func TestAbort(t *testing.T) {
	old := killGrace
	killGrace = 500 * time.Millisecond
	defer func() { killGrace = old }()

	ctx, cancel := context.WithCancel(context.Background())
	// Trap TERM so the SIGKILL escalation path is exercised too.
	ch := Run(ctx, []string{"sh", "-c", "trap '' TERM; echo started; sleep 300"}, passGate)

	// Wait for the process to be alive, then abort.
	deadline := time.After(10 * time.Second)
	for started := false; !started; {
		select {
		case ev := <-ch:
			if ev.Line == "started" {
				started = true
			}
			if ev.Done {
				t.Fatalf("finished before abort: %+v", ev)
			}
		case <-deadline:
			t.Fatal("child never started")
		}
	}
	cancel()

	_, done := collect(t, ch)
	if !done.Aborted {
		t.Fatalf("want Aborted, got %+v", done)
	}
	if done.Exit != -1 {
		t.Fatalf("want Exit -1 on abort, got %d", done.Exit)
	}
}
