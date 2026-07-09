// Package executor runs the generated mkfs argv as a subprocess with
// combined, order-preserving output streaming (spec §11). No shell is ever
// involved: argv goes straight to exec.
package executor

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"sync/atomic"
	"time"

	"github.com/ethanpil/cmkfs/internal/safety"
)

// Event is one item of executor output.
type Event struct {
	Line    string // one output line
	Err     error  // non-nil only on I/O failure
	Done    bool
	Exit    int            // valid when Done
	Aborted bool           // user-initiated kill (spec §10.3 Screen 5)
	Gate    *safety.Report // non-nil when the pre-exec gate aborted; nothing ran
}

// killGrace is how long SIGTERM gets before SIGKILL on abort.
var killGrace = 5 * time.Second

// Run executes argv with the given pre-exec gate. The gate is invoked as the
// executor's first action; on !ok Run emits a single Event{Done: true,
// Exit: -1, Gate: &report} and never spawns anything. Cancelling ctx sends
// SIGTERM to the process group, waits the grace period, then SIGKILLs; this
// is only ever triggered by the user's typed-ABORT flow — nothing is killed
// automatically.
func Run(ctx context.Context, argv []string, gate func() (safety.Report, bool)) <-chan Event {
	ch := make(chan Event, 64)
	go func() {
		defer close(ch)

		if gate != nil {
			report, ok := gate()
			if !ok {
				ch <- Event{Done: true, Exit: -1, Gate: &report}
				return
			}
		}

		// A single combined pipe: stdout and stderr both point at the same
		// write end so interleaving matches what a terminal would show.
		r, w, err := os.Pipe()
		if err != nil {
			ch <- Event{Err: err, Done: true, Exit: -1}
			return
		}
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stdout = w
		cmd.Stderr = w
		setProcAttr(cmd) // Setpgid on Linux: a stray terminal Ctrl+C never reaches mkfs

		if err := cmd.Start(); err != nil {
			w.Close()
			r.Close()
			ch <- Event{Err: err, Done: true, Exit: -1}
			return
		}
		w.Close() // parent's copy; the child holds its own

		var aborted atomic.Bool
		waitDone := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				aborted.Store(true)
				terminateGroup(cmd)
				select {
				case <-waitDone:
				case <-time.After(killGrace):
					killGroup(cmd)
				}
			case <-waitDone:
			}
		}()

		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			ch <- Event{Line: scanner.Text()}
		}
		if err := scanner.Err(); err != nil {
			ch <- Event{Err: err}
		}
		r.Close()

		err = cmd.Wait()
		close(waitDone)

		exit := 0
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				exit = ee.ExitCode()
			} else {
				exit = -1
				ch <- Event{Err: err}
			}
		}
		ch <- Event{Done: true, Exit: exit, Aborted: aborted.Load()}
	}()
	return ch
}
