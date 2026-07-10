// Command cmkfs is a TUI front-end for the mkfs.* family of filesystem
// creation tools, in the spirit of cfdisk (spec §1).
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ethanpil/cmkfs/internal/device"
	"github.com/ethanpil/cmkfs/internal/safety"
	"github.com/ethanpil/cmkfs/internal/schema"
	"github.com/ethanpil/cmkfs/internal/ui"
)

// Injected via -ldflags at release time (spec §14).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// cmkfs's own exit codes (spec §12). The backend's failure is reported
// in-UI, not via cmkfs's exit code.
const (
	exitOK       = 0
	exitUsage    = 2
	exitEnv      = 3
	exitNotRoot  = 4
	exitBlocked  = 5
	exitInternal = 6
)

func main() {
	os.Exit(run())
}

func run() (code int) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "cmkfs: internal error: %v\n", r)
			code = exitInternal
		}
	}()

	// QEMU/UTM and other serial consoles advertise TERMs whose terminfo has
	// no color capability (vt100/vt220/...), so the styles silently degrade
	// to monochrome even though the emulator renders color fine. Force basic
	// ANSI there via CLICOLOR_FORCE, which termenv reads live when lipgloss
	// resolves the profile at first render. An explicit NO_COLOR or
	// CLICOLOR_FORCE from the user always wins. (Setting the env, rather than
	// calling into termenv, keeps the direct dependency graph Charm-only per
	// CONTRIBUTING.) os.Setenv only fails on a malformed name, which this is
	// not, so the error is not actionable.
	if strings.HasPrefix(os.Getenv("TERM"), "vt") &&
		os.Getenv("NO_COLOR") == "" && os.Getenv("CLICOLOR_FORCE") == "" {
		_ = os.Setenv("CLICOLOR_FORCE", "1")
	}

	fs := flag.NewFlagSet("cmkfs", flag.ExitOnError) // flag exits 2 on bad flags
	printCmd := fs.Bool("print", false, "after Confirm, print the command instead of executing")
	fs.BoolVar(printCmd, "p", *printCmd, "shorthand for --print")
	showLoop := fs.Bool("show-loop", false, "include loop devices in enumeration")
	showVersion := fs.Bool("version", false, "print version and embedded schema ids, then exit")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: cmkfs [flags] [device]\n\n")
		fmt.Fprintf(fs.Output(), "A TUI front-end for mkfs: ext4, XFS, Btrfs, FAT32, exFAT, and F2FS.\n\n")
		fmt.Fprintf(fs.Output(), "  [device]      optional target, e.g. /dev/sdb1: skips the device list\n\nFlags:\n")
		fs.PrintDefaults()
	}
	_ = fs.Parse(os.Args[1:])

	if *showVersion {
		ids := make([]string, len(schema.Schemas))
		for i, s := range schema.Schemas {
			ids[i] = s.ID
		}
		fmt.Printf("cmkfs %s (commit %s, built %s)\nschemas: %v\n", version, commit, date, ids)
		return exitOK
	}

	if fs.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "cmkfs: at most one device argument is allowed")
		return exitUsage
	}

	// Root check before anything else (spec §9).
	if err := requireRoot(); err != nil {
		fmt.Fprintf(os.Stderr, "cmkfs: %v\n", err)
		return exitNotRoot
	}

	devices, err := device.Discover(*showLoop)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cmkfs: %v\n", err)
		return exitEnv
	}

	// The probes are independent one-shot execs; run them concurrently so
	// startup pays for the slowest backend, not the sum of all six.
	backends := map[string]device.Backend{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, s := range schema.Schemas {
		wg.Add(1)
		go func(bin string) {
			defer wg.Done()
			b := device.ProbeBackend(bin)
			mu.Lock()
			backends[bin] = b
			mu.Unlock()
		}(s.Binary)
	}
	wg.Wait()

	sys := safety.System{ProbeExcl: safety.ProbeExclusive}

	cfg := ui.Config{
		Schemas:        schema.Schemas,
		Backends:       backends,
		Sys:            sys,
		Discover:       device.Discover,
		ShowLoop:       *showLoop,
		PrintMode:      *printCmd,
		InitialDevices: devices, // reuse the startup enumeration
		Version:        version,
	}

	// Positional device: explicit user intent, so it skips Screen 1 — all
	// safety checks still apply (spec §12).
	if fs.NArg() == 1 {
		target := fs.Arg(0)
		var found *device.Device
		for i := range devices {
			if devices[i].Path == target {
				found = &devices[i]
				break
			}
		}
		if found == nil {
			fmt.Fprintf(os.Stderr, "cmkfs: unknown device %s\n", target)
			return exitUsage
		}
		report := sys.Check(safety.Params{Device: *found, All: devices})
		if report.Blocked() {
			for _, f := range report.Blockers() {
				fmt.Fprintf(os.Stderr, "cmkfs: %s\n", f.Message)
			}
			return exitBlocked
		}
		cfg.InitialDevicePath = target
	}

	app := ui.NewApp(cfg)
	prog := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "cmkfs: %v (a real terminal is required)\n", err)
		return exitEnv
	}

	// The user chose to print the command instead of executing (p key or
	// --print); it must be copy-paste runnable.
	if app.PrintOut != "" {
		fmt.Println(app.PrintOut)
	}
	return exitOK
}
