# Contributing to cmkfs

## The one hard rule: the schema is data, never logic

`internal/schema/definitions.go` contains only composite literals — no
function calls (the `i64` helper defined in `schema.go` is the single
exception), no conditionals, no arithmetic, no computed values, and no
references to anything outside the schema package's own types.

Relationships (`Conflicts`, `Requires`, composites) are declared as data;
their enforcement lives in the Go code that consumes the schema (`cmdgen`,
the form UI, `TestSchemas`). Any change that adds an evaluation construct to
the schema is rejected on principle, in code review, every time. The moment
the schema grows a conditional it becomes an ad-hoc programming language,
and every guarantee about validating it up front evaporates.

`TestSchemas` (`internal/schema/schema_test.go`) enforces every semantic
rule in CI; a violation fails `go test`, which fails the release.

## Adding or changing an option

1. Edit `internal/schema/definitions.go` (literals only).
2. Write the `Description` (≤ 200 bytes) and, where useful, `LongHelp`
   (≤ 2000 bytes) by hand — never scrape or copy man pages (wrong license,
   wrong register). The help must answer "what is this and should I touch
   it" without external docs.
3. Add golden cases to `internal/cmdgen/cmdgen_test.go` for the new flag's
   emission.
4. New filesystem: verify against the backend's source whether it refuses
   to overwrite existing signatures. Set `ForceFlag` to its force flag when
   it does; leave it empty **only** when the backend overwrites
   unconditionally (nothing catches a forgotten `ForceFlag` at authoring
   time — the mistake merely fails safe at runtime).
5. Schema changes (new option, new filesystem) bump the minor version;
   schema fixes bump patch.

## Dependency policy

The four Charm modules (bubbletea, bubbles, lipgloss, and their transitive
requirements) are the entire allowed third-party surface. No config-parsing
and no shell-lexing libraries: the schema is native Go and extra arguments
are entered pre-tokenized, so neither problem exists. CI asserts the direct
dependency graph is Charm-only.

## Execution model invariants

- argv goes straight to `exec`; there is no string concatenation of the
  command anywhere in the execution path, and no shell, ever.
- The force flag (`-F`/`-f`) is app-controlled, never user-facing: it is
  injected only when the safety flow confirmed an overwrite of detected
  signatures (`Report.NeedsForce()`). A schema with `ForceFlag: ""` means
  the backend overwrites signatures unconditionally (mkfs.fat, mkfs.exfat) —
  there is nothing to inject, and the signature warning plus typed
  confirmation is the only guard; do not assume force gates signatures for
  every filesystem. `WholeDiskFlag` (e.g. mkfs.fat `-I`) is likewise
  app-controlled, injected when the target is an entire disk or a whole
  device carrying a partition table (`Report.NeedsWholeDiskFlag()` —
  dosfstools refuses any device with partitions, not just disks). It is
  rejected from Extra Arguments the same way the force flag is.
- `safety.FinalGate` runs immediately before spawn, and its O_EXCL probe is
  probe-and-release — never hold the fd across the spawn.
- Nothing is ever killed automatically; only the user's double-Ctrl+C +
  typed-ABORT flow cancels the executor context.

## Tests

```
go test ./...                                   # unit + golden + UI state machine
go test -run=NONE -fuzz=FuzzBuild -fuzztime=30s ./internal/cmdgen/
sudo go test -tags integration ./integration/   # real loop devices, Linux + root
```

The TUI is tested by feeding `tea.KeyMsg` sequences to the pure `Update`
function — no PTY, no tmux, no expect. Keep it that way.
