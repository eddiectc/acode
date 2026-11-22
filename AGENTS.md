# Agent instructions

This repository is **acode**, the first-party coding-agent application. It
depends on the [aharness SDK](https://github.com/eddiectc/aharness) for the
model/tool loop, model adapters, built-in tools, session store, and durable
contracts. acode owns the CLI, TUI, configuration, signals, presentation, and
exit codes. Do not copy SDK logic into this repository; change the aharness
module and update the dependency.

The Go coding standard is [docs/coding-standard.md](docs/coding-standard.md).
Read it before editing Go code. It is normative: **MUST** / **SHOULD** / **MAY**
are defined there, and it wins over other docs on Go practice. App structure,
host composition, modes, and exit codes are in
[docs/architecture.md](docs/architecture.md); the flag/env/config layers are in
[docs/configuration.md](docs/configuration.md); the TUI and protocol are in
[docs/tui.md](docs/tui.md) and [docs/commands.md](docs/commands.md); caps live
in [docs/limits.md](docs/limits.md); the testing strategy is in
[docs/testing-strategy.md](docs/testing-strategy.md); the container image and
the branch, commit, and release process are in
[docs/container.md](docs/container.md) and [docs/release.md](docs/release.md).
Every change verifies
against [docs/definition-of-done.md](docs/definition-of-done.md). The
[documentation map](docs/README.md) explains where reference, plans, and
research live.

Non-negotiables:

- Runtime dependencies stay at zero by default beyond the SDK and the approved
  TUI stack. Use the standard library; exceptions MUST satisfy the
  justification requirements in the coding standard §3. Do not add a second
  application framework, a theme system, another markdown/chart library, a CLI
  framework, or a UUID/JSON Schema/retry library.
- `cmd/acode` is wiring and I/O only. `internal/tui` is display-only. Do not
  implement a loop, projection, tool, provider adapter, or session schema here.
- Never work around aharness. The SDK is owned by the same author and open to
  suggestions, so a missing capability, wrong contract, or awkward API is fixed
  there, not here. Propose the change upstream; do not add app-side shims,
  patches, vendored copies, `replace` forks, or duplicated contract logic to
  compensate for the SDK.
- Pin the aharness dependency to a tagged release in `go.mod`.
- User-visible branding is acode: the `acode` binary, `ACODE_*` environment
  variables, `~/.acode` state, the user agent, temp names, and docs. Do not add
  migration code for an older agent's state.
- The published image carries no project toolchain: it is a run artifact, and a
  compiler in it would be dead weight for most workspaces. The stack image
  (`compose/Dockerfile`) is the build environment and does carry Go and gh. A
  workspace that needs another language copies that file and selects its own in
  `compose.local.yaml`
  ([container.md §6](docs/container.md#6-building-the-image)). Never add a
  toolchain to the published `Dockerfile`, and never work around a missing
  compiler by adding one to an image that only runs.
- Route workspace paths through the SDK's `tools.Workspace`/`os.Root` policy;
  do not add a second path root or symlink bypass in the app.
- Wrap errors with `%w`; branch with `errors.Is`/`errors.As`; never match error
  strings. Every tool call must be paired with exactly one result.
- Kill the whole process group on command timeout or cancellation through the
  SDK; never leave child processes behind in the app.
- Use `encoding/json/v2` + `jsontext` for JSONL and protocol JSON; sanitize
  invalid UTF-8 before it reaches the transcript or log.
- TUI tests use the fake host and Bubble Tea fake input/output; integration
  tests use fake provider/catalog/search servers and `t.TempDir`. No real
  network or `time.Sleep`-based waiting.
- Before finishing, run `make dod` and fix failures. It runs the standard's
  §8 gates (`goimports`, `go vet`, `golangci-lint`, `go test -race -count=1`,
  `go build`, and `govulncheck`) plus `docs-check`.
- Keep the docs as lean as the code. A reference document states what is true on
  `main` and why: a change adds the sentence it made necessary, deletes the one
  it made false, and stops. Do not use docs as a ledger — no transcript of what
  you ran, tried, rejected, or verified, no restating a rule the document already
  makes, and no second explanation of one mechanism. A verification section lists
  open gaps, not a diary. If the prose a change needs is longer than the change,
  the prose is wrong: prefer a code comment, a commit message, or
  [docs/plans/](docs/plans/).

When in doubt, prefer the smaller stdlib solution. If other docs conflict, the
coding standard wins on Go practice; note the conflict in the change.
