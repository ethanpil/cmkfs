package executor

import (
	"context"
	"strings"
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

// TestScanOutputLines: the splitter handles \n, \r (in-place progress), \r\n,
// and force-flushes terminator-less runs so the scanner can never overflow
// (an overflow would close the pipe and SIGPIPE-kill the running mkfs).
func TestScanOutputLines(t *testing.T) {
	scan := func(in string, atEOF bool) (int, string) {
		adv, tok, err := scanOutputLines([]byte(in), atEOF)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return adv, string(tok)
	}

	if adv, tok := scan("hello\nworld", false); adv != 6 || tok != "hello" {
		t.Errorf("newline split: adv=%d tok=%q", adv, tok)
	}
	if adv, tok := scan("42/100\rmore", false); adv != 7 || tok != "42/100" {
		t.Errorf("carriage-return split: adv=%d tok=%q", adv, tok)
	}
	if adv, tok := scan("line\r\nnext", false); adv != 6 || tok != "line" {
		t.Errorf("crlf split: adv=%d tok=%q", adv, tok)
	}
	// Trailing \r with more data possibly coming: wait.
	if adv, _ := scan("partial\r", false); adv != 0 {
		t.Errorf("trailing CR must wait for more data, adv=%d", adv)
	}
	// Trailing \r at EOF: emit.
	if adv, tok := scan("partial\r", true); adv != 8 || tok != "partial" {
		t.Errorf("trailing CR at EOF: adv=%d tok=%q", adv, tok)
	}
	// Terminator-less run at the threshold force-flushes instead of growing.
	big := strings.Repeat("x", lineFlushThreshold)
	if adv, tok := scan(big, false); adv != len(big) || tok != big {
		t.Errorf("threshold flush failed: adv=%d len(tok)=%d", adv, len(tok))
	}
	// Below threshold, no terminator, not EOF: request more data.
	if adv, _ := scan("short", false); adv != 0 {
		t.Errorf("short run must request more data, adv=%d", adv)
	}
	if adv, tok := scan("short", true); adv != 5 || tok != "short" {
		t.Errorf("EOF flush: adv=%d tok=%q", adv, tok)
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
