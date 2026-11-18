# acode host/engine cleanup build plan

- **Date:** 2026-11-18 (revised after aharness v0.2.0)
- **Status:** in progress. Phase 1 (SDK adoption) shipped with the v0.2.0 bump;
  Phase 2 (engine-owned TUI composition) is proposed.
- **Source:** full review of the acode host against aharness v0.1.0, then a
  re-review against v0.2.0. The SDK requests were filed in the aharness
  repository; v0.2.0 implements almost all of them, so the remaining work is
  host-side.
- **Target:** Go 1.27.4, module `acode`, aharness v0.2.0.
- **Architecture:** [architecture.md](../architecture.md) defines the host
  boundary. [tui.md](../tui.md) stays authoritative for the shipped surface.
- **Testing:** [testing-strategy.md](../testing-strategy.md) plus §8 here.
- **Definition of done:** [definition-of-done.md](../definition-of-done.md).

## Decision summary

aharness v0.2.0 closed the SDK gaps. **No remaining finding needs an SDK
release.** What is left is one structural host change (Phase 2), a small set of
display cleanups, and documentation.

| Phase | Contents | Status |
|---|---|---|
| 1 | Bump to v0.2.0, migrate the breaking changes, adopt the new helpers | **Shipped** in this change |
| 2 | Make `engine.Host` own TUI session composition and run assembly | Proposed |
| 3 | Residual display cleanups (channel close, run identity, headless settle) | Proposed |
| 4 | Documentation and verification | Proposed |

Phase 1 deleted about 300 net lines from `cmd/acode` and `internal/tui` without
changing the shipped CLI or TUI surface.

## 1. Finding status after v0.2.0

| # | Finding | Status |
|---|---|---|
| 1 | TUI builds sessions by hand (`session.Create`/`OpenShared`, `agent.Inspect`, `agent.WriteSessionStart`, `store.CopyHistory`) | **Open** → Phase 2 |
| 2 | TUI builds runs by hand (`agent.NewSpawner`, `tools.NewRegistry`, `SetGrant`, `SeedTodo`, `agent.New`) | **Open** → Phase 2 |
| 3 | Five engine helpers copied verbatim plus `workPaths` | **Open** → Phase 2 |
| 4 | Fresh TUI runs record `resumed_from_seq = 1` | **Fixed** — the SDK derives the boundary; acode no longer computes it (`run.go`, `tui.go`) |
| 5 | TUI fork never records `contract_override` | **Open** → Phase 2 |
| 6 | Terminal result can lose `reason`/`duration` | **Fixed** — `Result.Reason`/`Result.Duration` now drive `applyResult` |
| 7 | TUI closes the display channel | **Open** → Phase 3 |
| 8 | Steering acknowledged by text equality | **Fixed** — SDK steering IDs, `MessageRecorded.ID`, `SubagentMessageRecorded` |
| 9 | `newCompleterFor` ignores the `baseURL` argument and silently maps unknown dialects | **Open** → Phase 2 |
| 10 | `ModelStarted.Attempt` ignored | **Open** → Phase 3 (the SDK documents retry semantics) |
| 11 | Catalog eligibility re-derived five times; `Lookup` error re-wrapped | **Fixed** — `CatalogModel.Validate`, `ErrModelUnavailable`, `Lookup`'s own error |
| 12 | Pricing needed `source`/`fetchedAt` beside the map; `Catalog` reassembled | **Fixed** — one `model.Catalog` field, `Catalog.Price` |
| 13 | Picker re-implemented `engine.List` | **Fixed** — `engine.ListSummaries` with `Workspace`/`Limit`/`MaxEvents` |
| 14 | Tool grant rebuilt from `SchemasFor` + `"delegate"` | **Fixed** — `tools.NamesFor(version, tools.NameDelegate)` |
| 15 | Todo/delegate/intent wire shapes re-declared | **Fixed** — `ParseTodoDetails`, `ParseDelegateArgs`, `IntentLine`; the todo test now runs the real tool |
| 16 | `cloneAgentOverride` field-by-field copy | **Fixed** — `AgentOverride.Clone` |
| 17 | `ModelChoice` copies `model.CatalogModel` | **Open** → Phase 3 |
| 18 | `UsageLine` copies `model.Usage` | **Keep** — the `-ui-json` schema is app-owned; add a drift test in Phase 3 |
| 19 | Tool-name literals and `"ok"` | **Fixed** — `tools.NameDelegate`, `tools.NameTodo`, `tools.CodeOK`; the transcript keeps `tools.Code` |
| 20 | Second path-containment implementation | **Partially fixed** — images use `Workspace.LocalDisplayPath`; `withinWorkspace` stays because the app rule also refuses a path that is lexically outside but symlinks *into* the workspace, which `Workspace.Contains` does not answer |
| 21 | Notice severity lost on replay | **Fixed** — `HistoryItem.NoticeKind` |
| 22 | Mid-run turns/context re-derived | **Fixed** — `ResponseCompleted` cumulative fields |
| 23 | Stop-reason failure table and empty-status invention | **Fixed** — `StopReason.Failure`, guaranteed non-empty `Result.Status` |
| 24 | Only `Store()` for history/todo/last-seq | **Open** → Phase 2 (`Host.History`/`Todo`/`LastSeq` exist now) |
| 25 | Work-dir cleanup copied | **Open** → Phase 2 (`Host.WorkDir`/`CleanupWork` exist now) |
| 26 | Construct-to-validate catalog URL; endpoint rules duplicated | **Fixed** — `ValidateCatalogURL`; the base-URL check stays deliberately stricter and says so |
| 27 | Auth hint via `StatusCode == 401` | **Fixed** — `errors.Is(err, model.ErrAuth)` |
| 28 | Dead code: discarded history decode, `var _ = model.Usage{}`, `_ = fromDisplay`, `lastSeq` | **Fixed**; `uiRichSet`/`uiExpandSet` and the `contextLimit` fallback remain → Phase 3 |
| 29 | Header context limit stale after a model pick | **Open** → Phase 3 |
| 30 | Docs describe the TUI as engine-composed | **Partially fixed** — the batch section is accurate; the TUI section still overstates engine use → Phase 4 |

Additional findings only visible against v0.2.0:

| # | Finding | Status |
|---|---|---|
| 31 | `engine.Spec.ExecutionGrant` was removed; batch resume can no longer narrow the root for one process | Resolved by design: batch records no root grant and configures no spawner, so `delegate` returns `unavailable`. Documented in [architecture.md](../architecture.md). |
| 32 | `engine.Spec.UserAgent` is now required | Fixed: the batch spec sets it; the engine fills `ToolOptions.UserAgent`. |
| 33 | `agent.Config.Steering` changed to `<-chan agent.SteeringLine` | Fixed: the TUI assigns IDs and acks by ID. |

## 2. Phase 2 — make `engine.Host` own the TUI session (L)

This is the only structural change left. After it, batch and TUI differ only in
display, steering, and per-run selection.

**Target shape**

```go
type tuiHost struct {
    host     *engine.Host // owns store, workspace, work dir, session contract
    next     runSelection // per-run model/dialect/base URL/limits/pricing/effort
    // app policy and display state only
    cfg, catalog, models, agents, baseAgents, agentOverrides, info, ...
}

func (h *tuiHost) Start(...) {
    runner, spawner, err := h.host.BuildRunnerWithSpawner(engine.RunSpec{
        Model: h.next.model, Dialect: string(h.next.dialect), BaseURL: h.next.baseURL,
        Limits: h.next.limits, Pricing: h.next.pricing,
        ThinkingEffort: h.next.effortOption(), Stream: true,
    }, display, steer, controller)
    ...
}
```

| Task | Change | Acceptance |
|---|---|---|
| P2.1 | Hold `host *engine.Host`; replace `store`/`ws`/`sessionID` reads with `host.Store()`, `host.WorkspaceHandle()`, `host.ID()`, `host.History()`, `host.Todo()`, `host.LastSeq()`, `host.State()`, `host.Snapshot()`. Keep `tuiHost` accessors so the TUI-facing interface is unchanged. | No `store.Events()` call remains outside the host. |
| P2.2 | Open the session through `engine.Open` like the batch path: `engine.Inspect` → app policy (`applySessionDefaults`, `validateSessionRules`) → `engine.Open(spec, mode)`; map errors with `engineExitCode`. Delete the three manual mode branches and `sessionFailure`. | New/resume/fork open exactly as batch does, with the same exit codes. Fork records `contract_override` when prompt or tools differ. |
| P2.3 | Build the session spec from `cfg` + definitions: `Spec.Definitions`, `Spec.Agents`, `Spec.ResolveModel`, `Spec.MaxSubagents`, `Spec.UserAgent`, `Spec.ToolOptions = toolOptions(cfg, "", "")`. | Delegation, web tools, and thinking effort unchanged. |
| P2.4 | Replace the hand-built spawner/registry/grant/todo/runner with `BuildRunnerWithSpawner`. Delete `sessionGrant`. | One construction path; child steering and history still work. |
| P2.5 | Introduce `runSelection`. `SelectModel`/`prepareRunLocked` update it and `info.ContextLimit`; they no longer mutate the session `cfg`. Pass it as `RunSpec`. | A picker selection applies to the next run only; the header ratio is correct immediately. |
| P2.6 | Adopt `ToolsConfig`/`Spec.RootGrant` for batch: record `Grant` as the full contract and `RootGrant` as `all-but-delegate` for `ModeNew`/`ModeFork`. **Only after P2.1–P2.5**, because the TUI must honour `RootTools` through `BuildRunner`; doing it earlier would let a TUI resume of a batch session dispatch `delegate` that the record forbids. | `session_start` records `tools` (full) and `root_tools` (narrowed); a TUI resume of a batch session cannot delegate. |
| P2.7 | Delete the copied engine helpers: `prepareStateDir`, `checkWritable`, `openWorkspace`, `openSessionWorkspace`, `workPaths`, `resetWorkDirs`, `removeWorkDir`, `removeTree`, `restoreOwnerAccess`, `runnerConfig`. Remove the redundant batch preflight. | `grep` shows no copied engine helper; `host.Close()` removes the work tree. |
| P2.8 | Simplify `newCompleterFor` to take `(dialect, baseURL, token, timeout, userAgent, sessionID)` and return a rejecting completer for an unknown dialect. Root and child runs use the passed `baseURL`. | A model switch to a different base URL sends to the new base URL; an unknown dialect fails with a wrapped error. |
| P2.9 | Work-dir timing: reset at each `BuildRunner`, removal at `Host.Close` (or `CleanupWork` when a run ends and the host stays idle). Update the `/cancel` wording. | Tests assert the spill file is gone at the documented point. |
| P2.10 | `NewSession`/`ResumeSession` open a new `Host`, rebuild `Info` from `State`/`Snapshot`, read history/todo through the host, then close the old host. Reject same-session resume before opening. | Failed switches leave the old session intact. |

## 3. Phase 3 — residual display cleanups (M)

| Task | Change | Acceptance |
|---|---|---|
| P3.1 | Stop closing the display channel. Drain buffered events and complete when empty. | A runner that emits a final event and returns still renders it; `-race` passes. |
| P3.2 | Embed `model.CatalogModel` in `tui.ModelChoice`, keeping only `Switchable`. | `modelChoice` is a two-field conversion. |
| P3.3 | Delete the remaining dead state: `uiRichSet`/`uiExpandSet`, the `contextLimit` catalog fallback, and the discarded `agent.History` decode in `openTUIHost`. | No write-only fields; the header ratio comes from `Info.ContextLimit`. |
| P3.4 | Use `RunStarted.RunID`/`RunFinished.RunID` instead of `m.nextRun` for stale-event discard. | A stale event from a previous run cannot touch the current header. |
| P3.5 | Key the thinking-block reset on `ModelStarted.Attempt` (and turn) rather than settling on every start. | A retry and a new turn are distinguishable in a test. |
| P3.6 | Add a `UsageLine` drift test: set every `model.Usage` field to a unique value and assert the protocol line carries each one. | A new SDK usage field fails the test until the protocol is updated. |
| P3.7 | Investigate a render/emulator barrier for `headlessSettle`; if none is reliable, keep it, scope it to `-ui-json`, and record the rationale in [tui.md](../tui.md). | Decision recorded; frame tests pass repeatedly. |

## 4. Phase 4 — documentation and verification (S)

| Task | Change |
|---|---|
| P4.1 | Rewrite the TUI host section of [architecture.md](../architecture.md) after Phase 2: `engine.Open` + `BuildRunnerWithSpawner`, app policy vs engine composition, per-run selection. |
| P4.2 | Keep [commands.md](../commands.md) and [tui.md](../tui.md) accurate for the work-dir timing and steering IDs. |
| P4.3 | Add the Phase 3 contract tests to [testing-strategy.md](../testing-strategy.md). |
| P4.4 | Update this plan's status and add the verification note. |

## 5. Intended behavior changes

Shipped in Phase 1:

1. **A fresh session records `resumed_from_seq: 0`** (was `1` in the TUI).
2. **A catalog cache-write failure is a warning**, not exit 2: `-refresh-models`
   succeeds with the fetched catalog and prints the warning.
3. **Delegation in a resumed batch session** is offered by the recorded grant
   but cannot execute, because batch configures no spawner. A new or forked
   batch session still records a grant without `delegate`.
4. **Steering acknowledgements are by ID**, so duplicate identical lines ack
   independently.
5. **A replayed error notice keeps its severity** after resume or fork.
6. **The live header uses the SDK's cumulative counters**, so it no longer
   jumps when the run ends.
7. **The auth hint covers 403 as well as 401** (`model.ErrAuth`).

Still to come in Phase 2/3:

8. Fork records `contract_override` when the contract differs.
9. The work tree is cleared per run start and on host close.
10. Batch-created sessions record `root_tools` and are permanently
    non-delegating; TUI-created sessions stay delegating.

No CLI flag, environment variable, config key, slash command, protocol version,
or exit code changes. `-ui-json` output changes only where a notice is styled as
an error and where the header counters now come from the SDK.

## 6. Decisions

| ID | Decision | Recommendation |
|---|---|---|
| DEC1 | Record `RootGrant` for batch, or accept "offered but unavailable" on resume? | Record it in Phase 2 (P2.6). Phase 1 keeps the current contract, which is unchanged for new sessions. |
| DEC2 | Keep the app's `withinWorkspace` although `Workspace.Contains` exists? | Keep. The app rule also refuses an operator path that is lexically outside but resolves into the workspace; `Contains` only answers the inside direction. Revisit if the SDK gains a "resolves inside" helper. |
| DEC3 | Keep `validateBaseURL` although `model.ValidateBaseURL` exists? | Keep. The app version rejects userinfo and query, which the SDK version allows; a query would be lost when the dialect path is appended. |
| DEC4 | Adopt `engine.ListSummaries` with `MaxEvents`? | Done. It replaced the hand-rolled scan and reads only the head. |
| DEC5 | Keep `headlessSettle`? | Investigate in P3.7; keep if no deterministic barrier exists, documented as a `-ui-json`-only frame flush. |

## 7. Risks and mitigations

| Risk | Mitigation |
|---|---|
| Large TUI refactor regresses resume/fork/steering | Add the Phase 2 pins first (multi-run, switch, picker, delegation, interrupt); land Phase 2 as one reviewed change. |
| `RootGrant` recorded before the TUI honours it | P2.6 is explicitly last in Phase 2; the TUI uses `BuildRunner`, which applies `RootTools`. |
| `engine.Open` is stricter than the manual paths | Map errors through `engineExitCode`; add exit-code tests for missing/locked/invalid sessions. |
| Catalog model validation is stricter than the old checks | `CatalogModel.Validate` requires a dialect, base URL, and positive context window; fixtures and operator-facing messages now name the defect. |
| `agent.NewLimits` rejects `inputLimit > maxContext` | `validateEffective` clamps the catalog ceiling to the effective window before calling it. |
| Model switch with a different base URL hits the old one | P2.8 uses the `baseURL` argument for root and children; add a two-model test with two fake servers. |

## 8. Test plan

| Layer | Coverage |
|---|---|
| Unit | `applyResult`, steering by ID, limits clamping, catalog price/validation, status helpers, grant helper. |
| Contract | Todo details via a real `tools.Registry`; usage-line drift (P3.6); tool-name constants exist in `tools.Names()`. |
| Batch integration | Both dialects including `resumed_from_seq: 0`; cache-write warning; resume boundary. |
| TUI integration | Existing pins; Phase 2 adds multi-run, switch, picker, delegation, interrupt, work-dir timing. |
| Headless | Existing `-ui-json` frame/snapshot tests; repeat `-count=10` after P3.1/P3.7. |
| Race | `go test -race -count=1 ./...` after each phase. |
| Real provider | New + resume + steer + interrupt + model switch, recorded as pending if credentials are unavailable. |

## 9. Verification

Phase 1 (2026-11-18):

- aharness v0.2.0.
- `make dod`: pass (goimports, docs-check, vet, golangci-lint, `go test -race
  -count=1`, build, govulncheck).
- Batch integration: both dialects, fresh-session `resumed_from_seq: 0`,
  cache-write warning path, delegation, web tools.
- TUI and headless: full suite.
- Real-provider smoke: **pending** (no credentials in this environment).
- Real-terminal smoke: **pending**.
- Deviations: `Workspace.Contains` and `model.ValidateBaseURL` are not adopted
  where the app rule is deliberately stricter (DEC2, DEC3). `RootGrant` is
  deferred to Phase 2 (DEC1).
