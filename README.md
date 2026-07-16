# cmkfs

A terminal UI front-end for the `mkfs.*` family of filesystem creation
tools, in the same spirit that `cfdisk` is a TUI front-end for disk
partitioning.

![cmkfs walkthrough: device list with safety findings → device information → filesystem picker → options → confirm → live mkfs output](docs/demo.gif)

cmkfs guides you through selecting a block device, choosing a filesystem
(ext4, XFS, Btrfs, FAT32/vfat, exFAT, or F2FS), configuring a curated set
of options with built-in help, previewing the exact `mkfs.*` command that
will run, and executing it with live output.

cmkfs never implements filesystem creation itself. It is a command generator
and executor: its entire job is to build a correct argv for the system's
`mkfs.*` binary and run it as a subprocess. No shell is ever involved.

## Installation

Pre-built **static** binaries for Linux amd64 and arm64 are attached to every
[release](https://github.com/ethanpil/cmkfs/releases). Each block below is
copy-paste ready: the first line resolves the latest release version
automatically (set `VER` by hand instead to pin a specific one), and swap
`amd64` for `arm64` on ARM.

**Debian / Ubuntu (`.deb`)**

```sh
VER=$(curl -fsSL https://api.github.com/repos/ethanpil/cmkfs/releases/latest | sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p')
curl -LO https://github.com/ethanpil/cmkfs/releases/download/v$VER/cmkfs_${VER}_linux_amd64.deb
sudo dpkg -i cmkfs_${VER}_linux_amd64.deb
```

**Fedora / RHEL / openSUSE (`.rpm`)**

```sh
VER=$(curl -fsSL https://api.github.com/repos/ethanpil/cmkfs/releases/latest | sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p')
curl -LO https://github.com/ethanpil/cmkfs/releases/download/v$VER/cmkfs_${VER}_linux_amd64.rpm
sudo rpm -i cmkfs_${VER}_linux_amd64.rpm
```

**Alpine (`.apk`)** — the package is unsigned, so `--allow-untrusted` is required:

```sh
VER=$(curl -fsSL https://api.github.com/repos/ethanpil/cmkfs/releases/latest | sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p')
curl -LO https://github.com/ethanpil/cmkfs/releases/download/v$VER/cmkfs_${VER}_linux_amd64.apk
sudo apk add --allow-untrusted ./cmkfs_${VER}_linux_amd64.apk
```

**Any distro (tarball)**

```sh
VER=$(curl -fsSL https://api.github.com/repos/ethanpil/cmkfs/releases/latest | sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p')
curl -LO https://github.com/ethanpil/cmkfs/releases/download/v$VER/cmkfs_${VER}_linux_amd64.tar.gz
tar xzf cmkfs_${VER}_linux_amd64.tar.gz
sudo install -m 0755 cmkfs /usr/local/bin/
```

**From source** (Go ≥ 1.25, no cgo):

```sh
git clone https://github.com/ethanpil/cmkfs
cd cmkfs
CGO_ENABLED=0 go build -o cmkfs ./cmd/cmkfs
sudo install -m 0755 cmkfs /usr/local/bin/
```

## Usage

```
sudo cmkfs                 # full flow starting at the device list
sudo cmkfs /dev/sdb1       # skip the device list (all safety checks still apply)
sudo cmkfs -p /dev/sdb1    # after confirmation, print the command instead of running it
sudo cmkfs --show-loop     # include loop devices in the list
cmkfs --version            # version, commit, embedded schema ids
man cmkfs                  # full reference (packages only; docs/cmkfs.8 in the tarball)
```

There is deliberately no `--yes` / non-interactive mode: scripting users
should use `mkfs` directly — press `p` on the confirm screen (or use
`--print`) and cmkfs hands you the exact, copy-paste-runnable command.

## Requirements

- Linux (amd64 or arm64). The release binary is fully static.
- Root (`sudo cmkfs`).
- `lsblk` from util-linux ≥ 2.33 (present on effectively every system).
- Whichever backends you want to use: `mkfs.ext4` (e2fsprogs),
  `mkfs.xfs` (xfsprogs), `mkfs.btrfs` (btrfs-progs), `mkfs.fat`
  (dosfstools), `mkfs.exfat` (exfatprogs — the modern kernel-exFAT tools,
  not the legacy fuse exfat-utils), `mkfs.f2fs` (f2fs-tools). Missing
  backends are simply greyed out in the picker.
- A terminal of at least 80x24. Serial/VM consoles that advertise a
  colorless `TERM` (vt100/vt220, common under QEMU/UTM) get basic colors
  forced automatically; `NO_COLOR=1` disables colors anywhere and
  `CLICOLOR_FORCE=1` forces them anywhere.

## Keys

| Key | Action |
|---|---|
| ↑/↓, j/k | Move selection |
| Enter | Select / advance |
| Esc | Back one screen (disabled during execution) |
| q, F10 | Quit (confirmation prompt once past the filesystem pick) |
| ? | Help overlay |
| r | Refresh device list (Screen 1) |
| i | Full information for the highlighted device (Screen 1) |
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

## Safety

- Refuses mounted devices, active swap, and read-only devices.
- Refuses anything backing the running system (`/`, `/boot`, `/boot/efi`, `/usr`).
- Detects devices held by LVM, dm-crypt, md, or multipath (transitively).
- Detects existing filesystem signatures and partition tables; overwriting
  them requires typing the device name, and only then is the backend's
  force flag injected. (FAT tools overwrite signatures unconditionally and
  have no force flag — there the typed confirmation is the guard.)
- Always shows the exact command before execution.
- Re-checks everything immediately before spawning `mkfs`: if the device was
  mounted, changed, or claimed between your confirmation and execution, the
  run is aborted (nothing ever executes against a stale confirmation).
- A single Ctrl+C never kills a running format; a deliberate
  double-Ctrl+C + typed `ABORT` flow always can.

## Development

Build from source with `CGO_ENABLED=0 go build ./cmd/cmkfs` (see
[Installation](#installation)). Go ≥ 1.25; the only third-party dependencies
are the Charm modules (bubbletea, bubbles, lipgloss, and x/ansi) —
everything else is the standard library, and CI asserts the direct
dependency graph stays Charm-only.

Run the test suite:

```
go test ./...                                   # unit tests, no root needed
sudo go test -tags integration ./integration/   # loop-device tests, root + Linux
```

The demo GIF at the top of this README is stitched from the real screen
captures in `docs/screenshots`, numbered in flow order. Add or replace a
shot there and rebuild it with:

```
go run ./internal/gendemo docs/demo.gif
```

## Changelog

**v0.3.0** — a device information screen and a rebuilt device list.

Press `i` on any device for everything cmkfs knows about it: model, serial,
transport, UUID, partition table, parent and children, free space per
mountpoint, block size, and drive temperature where the kernel exposes one.
It reads that off the UI thread, because `statfs` can hang on a wedged mount
and a temperature read wakes a sleeping drive.

The device list is now laid out with lipgloss/table. Columns size to their
content, so short paths leave the space to MODEL; when the total doesn't
fit, the columns with the most slack give theirs up and values truncate with
an ellipsis. Widths are counted in display cells, so a CJK label can no
longer overrun the window.

Fixes from a review of the above, all of which shipped in the two commits
before it: one long path or mountpoint no longer erases columns from every
*other* row (a `/dev/mapper/luks-…` device could hide whether an unrelated
disk was mounted); block size and temperature now resolve for partitions,
where `filepath.Join` had been quietly eating the `..` that reaches the
parent disk; the information screen scrolls instead of pushing the device
path off a 24-row terminal; the help overlay lines up on a colour terminal;
and the key hints sit directly under the table rather than moving with the
findings. The man page is new, and the demo GIF is now real captures rather
than a synthetic render.

**v0.2.2** — the device list fits an 80-column terminal: columns were
narrowed and every row is clamped to the window. Console colors are forced
on `vt*` terminals (QEMU/UTM serial consoles advertise no color capability
but render it fine), the text-heavy screens wrap to the window, and the
options form shows concrete defaults instead of placeholders.

**v0.2.1** — fixes from a post-release review: `-I` for mkfs.fat is now
supplied for any whole device carrying a partition table (partitioned loop
devices and md arrays, not only disks); `-I` can no longer be smuggled in
via Extra Arguments; exFAT labels accept 11 characters (not 11 bytes) and
F2FS labels up to 512 bytes; the F2FS version probe was removed (mkfs.f2fs
offers no safe way to print its version, so the permanent "version could
not be determined" warning was noise); backend probes now run concurrently
at startup.

**v0.2.0** — three new filesystems: FAT32/vfat (`mkfs.fat`), exFAT
(`mkfs.exfat`, requires exfatprogs), and F2FS (`mkfs.f2fs`). FAT and exFAT
tools overwrite existing signatures unconditionally (they have no force
flag), so for those the typed device-name confirmation is the guard; on
whole-disk FAT targets cmkfs supplies `-I` automatically after
confirmation. The manual hardware checklist from the beta still has not
been signed off.

**v0.1.0-beta.1** — first beta: the automated suites (unit, fuzz,
loop-device integration) pass in CI, but the manual hardware checklist
(real USB sticks, live abort testing, terminal-resize scenarios) has not
been signed off yet. Treat it accordingly — and read the confirm screen
before typing the device name.

## Testing

"Battle-tested" is earned over years of real-world use on countless
machines. cmkfs is new, AI-assisted, and it formats disks — so skepticism
is fair, and we won't pretend to a track record we haven't earned yet.
What we can do is test relentlessly, gate every release on it, and show you
exactly what that covers so you can judge for yourself. The source and the
suite are open; every check below is reproducible.

Every tagged release must pass the entire automated suite in CI before any
artifact is published — nothing ships red — and the release tag re-runs the
unit suite before the binaries are built. On top of that, we validate the
real binary on real disks (see *On real hardware*).

### The harness

| Layer | Command | When |
|---|---|---|
| Unit + UI state-machine + fuzz seeds | `go test ./...` | every push / PR, no root |
| UI again, with color forced | `CLICOLOR_FORCE=1 go test ./internal/ui/` | every push / PR — without a TTY every style is a no-op, so a layout bug that only exists once escape sequences are in the string passes unnoticed |
| Fuzz smoke | `go test -fuzz=FuzzBuild -fuzztime=30s ./internal/cmdgen/` | every push / PR |
| Loop-device integration | `sudo go test -tags integration ./integration/` | CI, as root, against the real `mkfs.*` backends |
| Lint + static build matrix | `golangci-lint` + `mandoc -T lint` + amd64/arm64 build | asserts a static binary, compiled-in schema, a Charm-only dependency graph, and a man page that renders |

### What is verified — and why it matters

**The command builder** — the one function between your keystrokes and a destructive `mkfs`:
- Byte-exact golden command for every filesystem and option combination — the exact argv is pinned, so a schema edit can't silently change what runs.
- Fuzzed with random and hostile input — never panics; the device path is always the final argument and appears exactly once; no unsubstituted placeholder ever leaks into the command; force / whole-disk flags land only in their correct position.
- Extra-argument guardrails — a token that is empty, contains a newline, equals the device path, starts with `/dev/`, or duplicates an app-controlled flag is rejected, so the "advanced" escape hatch can't retarget the command or smuggle a flag past the safety flow.
- Deterministic and shell-safe — identical input yields identical output, and the previewed / `--print` command is copy-paste-safe and byte-identical to what executes.

**Schema integrity** — every filesystem definition is checked against the structural rules (unique IDs, symmetric conflicts, valid defaults, well-formed flag templates, help-length caps). A malformed schema fails the build, not your disk.

**Refusals — the accidents that lose data:**
- Mounted devices, active swap, and read-only devices.
- Anything backing the running system (`/`, `/boot`, `/usr`) — including when it's reached through a child partition of a whole disk you selected.
- Devices held by LVM, dm-crypt, md, or multipath (verified against real stacks).
- Devices another process holds exclusively (`O_EXCL`).
- Targets too small for the chosen filesystem.

Each is proven to stop the run *before* any format begins.

**Confirmation and execution:**
- Existing filesystem signatures and partition tables are detected and require you to type the device name before the backend's force flag is injected.
- A final re-check runs immediately before spawning `mkfs`: if the device was mounted, changed, or claimed since you confirmed, the run aborts and the disk is left untouched.
- A failing `mkfs` is reported with its exit code and output — never dressed up as success.
- A running format survives a stray Ctrl+C; only a deliberate double-Ctrl+C then typed `ABORT` cancels it.

**The terminal UI** — the whole screen flow is driven by synthetic keystrokes and asserted (navigation, live validation, quit prompts, exit codes), and every screen is checked to fit an 80×24 terminal in both dimensions, measured in display cells so a wide-glyph label counts for what it actually occupies. The suite runs a second time with color forced, because styles are no-ops without a TTY and a screen that only misaligns once it is colored would otherwise pass. The UI is the last thing between a user and a mistake; it has to behave on the smallest terminal we support.

**On real hardware** — loop devices and fixtures only go so far, so we also drive the *actual* binary through a real terminal on an Alpine Linux VM with real disks and the real backends: format real disks as all six filesystems; confirm version detection against the shipped tool versions; reproduce every refusal above against genuine LVM, dm-crypt, and mdadm setups; confirm whole-disk handling against a `mkfs.fat` that really does refuse without `-I`; and confirm color on a bare serial console plus correct layout at 80 columns. It is the closest we get to "does it behave on a real machine."

### What testing can't do

None of this replaces years in the field, and we don't claim it does. That
is exactly why cmkfs always shows you the exact command and makes you
confirm before it touches anything: the tests are how we build our own
confidence, but the last check is yours. Read the confirm screen.
