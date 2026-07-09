package executor

import (
	"context"
	"testing"
	"time"

	"github.com/ethanpil/cmkfs/internal/safety"
)

// TestGateFailureSpawnsNothing: a failing gate must emit exactly one event
// carrying the report, and never spawn (argv here would fail loudly if run).
func TestGateFailureSpawnsNothing(t *testing.T) {
	report := safety.Report{Findings: []safety.Finding{
		{Severity: safety.Blocker, Code: "MOUNTED", Message: "mounted"},
	}}
	gate := func() (safety.Report, bool) { return report, false }

	ch := Run(context.Background(), []string{"/nonexistent/never-runs"}, gate)

	var events []Event
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				goto done
			}
			events = append(events, ev)
		case <-timeout:
			t.Fatal("executor did not finish")
		}
	}
done:
	if len(events) != 1 {
		t.Fatalf("want exactly 1 event, got %d: %+v", len(events), events)
	}
	ev := events[0]
	if !ev.Done || ev.Exit != -1 || ev.Gate == nil {
		t.Fatalf("bad gate event: %+v", ev)
	}
	if !ev.Gate.Has("MOUNTED") {
		t.Fatalf("gate report lost: %+v", ev.Gate)
	}
}

// TestSpawnFailure: an unspawnable binary reports Done with Err.
func TestSpawnFailure(t *testing.T) {
	gate := func() (safety.Report, bool) { return safety.Report{}, true }
	ch := Run(context.Background(), []string{"/nonexistent/no-such-binary-cmkfs"}, gate)

	sawErr := false
	for ev := range ch {
		if ev.Err != nil {
			sawErr = true
		}
		if ev.Done {
			if ev.Exit != -1 {
				t.Errorf("want Exit -1 on spawn failure, got %d", ev.Exit)
			}
			if ev.Gate != nil {
				t.Error("Gate must be nil when the gate passed")
			}
		}
	}
	if !sawErr {
		t.Error("expected an Err event for spawn failure")
	}
}
