# cmkfs

A terminal UI front-end for the `mkfs.*` family of filesystem creation
tools, in the same spirit that `cfdisk` is a TUI front-end for disk
partitioning.

cmkfs guides you through selecting a block device, choosing a filesystem
(ext4, XFS, or Btrfs), configuring a curated set of options with built-in
help, previewing the exact `mkfs.*` command that will run, and executing it
with live output.

cmkfs never implements filesystem creation itself. It is a command generator
and executor: its entire job is to build a correct argv for the system's
`mkfs.ext4`, `mkfs.xfs`, or `mkfs.btrfs` binary and run it as a subprocess.
No shell is ever involved.

## Safety is the core feature

- Refuses mounted devices, active swap, and read-only devices.
- Refuses anything backing the running system (`/`, `/boot`, `/boot/efi`, `/usr`).
- Detects devices held by LVM, dm-crypt, md, or multipath (transitively).
- Detects existing filesystem signatures and partition tables; overwriting
  them requires typing the device name, and only then is the backend's
  force flag injected.
- Always shows the exact command before execution.
- Re-checks everything immediately before spawning `mkfs`: if the device was
  mounted, changed, or claimed between your confirmation and execution, the
  run is aborted (nothing ever executes against a stale confirmation).
- A single Ctrl+C never kills a running format; a deliberate
  double-Ctrl+C + typed `ABORT` flow always can.

## Requirements

- Linux (amd64 or arm64). The release binary is fully static.
- Root (`sudo cmkfs`).
- `lsblk` from util-linux ≥ 2.33 (present on effectively every system).
- Whichever backends you want to use: `mkfs.ext4` (e2fsprogs),
  `mkfs.xfs` (xfsprogs), `mkfs.btrfs` (btrfs-progs). Missing backends are
  simply greyed out in the picker.
- A terminal of at least 80x24.

## Usage

```
sudo cmkfs                 # full flow starting at the device list
sudo cmkfs /dev/sdb1       # skip the device list (all safety checks still apply)
sudo cmkfs -p /dev/sdb1    # after confirmation, print the command instead of running it
sudo cmkfs --show-loop     # include loop devices in the list
cmkfs --version            # version, commit, embedded schema ids
```

There is deliberately no `--yes` / non-interactive mode: scripting users
should use `mkfs` directly — press `p` on the confirm screen (or use
`--print`) and cmkfs hands you the exact, copy-paste-runnable command.

## Keys

| Key | Action |
|---|---|
| ↑/↓, j/k | Move selection |
| Enter | Select / advance |
| Esc | Back one screen (disabled during execution) |
| q, F10 | Quit (confirmation prompt once past the filesystem pick) |
| ? | Help overlay |
| r | Refresh device list (Screen 1) |
| h | Extended help for the focused option (Screen 3) |
| a | Advanced — Extra Arguments (Screen 3) |
| p | Print the command and exit instead of executing (Screen 4) |

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Normal exit (including "backend ran and failed" — reported in-UI) |
| 2 | Usage error |
| 3 | Environment error (lsblk missing/unparseable, no TTY) |
| 4 | Not root |
| 5 | Positional device argument is blocked by a safety finding |
| 6 | Internal error |

## Building

```
CGO_ENABLED=0 go build ./cmd/cmkfs
```

Go ≥ 1.22. The only third-party dependencies are the Charm TUI modules
(bubbletea, bubbles, lipgloss); everything else is the standard library.

Run the test suite:

```
go test ./...                                   # unit tests, no root needed
sudo go test -tags integration ./integration/   # loop-device tests, root + Linux
```

## Release status

v0.1.0-beta.1 is a beta: the automated suites (unit, fuzz, loop-device
integration) pass in CI, but the manual hardware checklist (real USB
sticks, live abort testing, terminal-resize scenarios) has not been signed
off yet. Treat it accordingly — and read the confirm screen before typing
the device name.
