# acode definition of done

- **Date:** 2026-11-18
- **Status:** normative for every task, fix, feature, refactor, and
  documentation change
- **Applies to:** every change in this repository, and every dated record in
  [docs/plans](plans/)
- **Related:** [Go coding standard](coding-standard.md),
  [application architecture](architecture.md),
  [testing strategy](testing-strategy.md), [limits](limits.md),
  [TUI](tui.md), [commands](commands.md), and the [documentation map](README.md)

A change is done only when every applicable item below holds. Do not silently
skip an item: state why it does not apply.

Precedence: the [coding standard](coding-standard.md) wins on Go practice; the
current reference documents ([architecture](architecture.md),
[limits](limits.md), [testing strategy](testing-strategy.md)) win on structure
and runtime contracts. A dated plan or research document records intent and
evidence; where one disagrees with the current reference, the reference wins.
Plans and research are frozen: later behavior changes update the current
reference, never the dated record.

## 1. Behavior and scope

- [ ] **Requested behavior.** The task works end to end, and every acceptance
      criterion in the issue or change request is satisfied.
- [ ] **No unfinished work.** No stubs, placeholders, fake fallbacks,
      `TODO: implement`, or misleading "scaffold" claims. If a real
      implementation needs unavailable information or credentials, name the
      missing prerequisite exactly.
- [ ] **Clean cutover.** Every caller is migrated; obsolete code, aliases,
      re-exports, and compatibility shims are removed.
- [ ] **App boundary.** The change stays in `cmd/acode`, `internal/tui`,
      scripts, or app docs. It does not copy loop, projection, tool, provider,
      or session-schema logic out of the aharness SDK. An SDK change is a
      separate change in the sibling aharness repository, followed by a module
      update here.
- [ ] **SDK contracts.** acode consumes `Result.Status`, display events,
      history, state, catalog, and tool DTOs as the SDK defines them. It does
      not reinterpret the session schema, prompt/tool grant, projection, or
      concurrency model, and it does not add a second durable event type.
- [ ] **CLI/TUI compatibility.** New flags, environment variables, config keys,
      slash commands, protocol fields, and exit codes are documented in the
      current reference and the README.
- [ ] **Local replace.** `go.mod` keeps the sibling `../aharness` replace only
      while the SDK is unreleased. A published-version change removes it.
- [ ] **Errors and pairing.** Errors wrap with `%w` and are classified with
      `errors.Is`/`errors.As`; every tool call has exactly one result.

## 2. Verification

- [ ] **Narrowest check.** The test, command, or public API surface that
      exercises the change was run and passed once. Do not claim a check that
      was not run.
- [ ] **Automated DoD checks.** `make dod` passes. It runs the
      [coding standard](coding-standard.md) §8 gates (`goimports`, `go vet`,
      `golangci-lint`, `go test -race -count=1 ./...`,
      `go build -trimpath -o bin/acode ./cmd/acode`, and `govulncheck ./...`)
      plus `docs-check`.
- [ ] **Fuzzing.** Touched trust boundaries have fuzz coverage, or the existing
      fuzz targets still pass.
- [ ] **Provider behavior.** Wire-format, streaming, or model-control changes
      pass the fake-provider tests and a real-provider smoke. If credentials
      are unavailable, record the exact pending check and do not describe it as
      verified.

## 3. Contracts and documentation

- [ ] **Current reference updated in the same change.** Use the single source
      of truth for each topic: [architecture](architecture.md) for app
      structure, modes, and exit codes; [configuration](configuration.md) for
      config keys; [limits](limits.md) for app caps; [tui](tui.md) and
      [commands](commands.md) for the shipped surface;
      [testing strategy](testing-strategy.md) for test layers.
- [ ] **No stale authority.** A plan does not claim to be normative for
      behavior that ships; it records intent, milestones, deviations, and
      verification.
- [ ] **Frozen records.** A dated plan or research document is not rewritten for
      later behavior; the current reference carries the shipped truth.
- [ ] **User-facing surface.** A user-visible change updates the README
      feature description, examples, or CLI contract as applicable.
- [ ] **New interfaces.** New flags, environment variables, config keys, exit
      codes, slash commands, and protocol fields are documented in the current
      reference and the README where users see them.
- [ ] **Honest docs.** Documented behavior matches exercised behavior; pending
      checks and environment limits are named, not implied.

## 4. Safety, limits, and cleanup

- [ ] **Workspace containment.** Paths go through `os.Root` and
      `filepath.IsLocal`; `.git` components and symlink escapes are rejected.
- [ ] **Secrets.** No token, secret, full environment, header, or uncapped body
      reaches logs, the transcript, or child environments.
- [ ] **Encoding and caps.** Invalid UTF-8 is sanitized before it reaches the
      transcript or log; payload and output caps are enforced.
- [ ] **Process and runtime cleanup.** Timeout and cancellation kill the whole
      process group; no child process survives; the per-run work directory
      cleanup is attempted on normal, error, and signal paths.
- [ ] **Limit semantics.** The [limits](limits.md) table reflects any changed
      app value; SDK-owned caps link to the aharness reference and are not
      restated with different numbers.
- [ ] **Dependencies.** A new runtime dependency satisfies
      [coding standard §3](coding-standard.md#3-dependencies); otherwise use
      the standard library.

## 5. Repository hygiene

- [ ] **Focused diff.** The change touches only intended files; unrelated
      formatting, generated noise, and drive-by refactors are absent.
- [ ] **No scaffolding.** Throwaway scripts, debug output, temporary files, and
      local-fetch helpers are removed.
- [ ] **Module files.** `go.mod` and `go.sum` change only for an approved
      dependency change.
- [ ] **Branding and state.** User-visible names, `ACODE_*` variables,
      `~/.acode`, and the `acode` binary stay consistent. No migration code for
      an older agent's state is added.
- [ ] **Placement.** New documents follow the placement rules in the
      [documentation map](README.md).
