# acode host/engine cleanup build plan

- **Date:** 2026-11-18 (revised 2026-11-20 after aharness v0.4.1; first
  revised after v0.2.0)
- **Status:** implemented. Phase 1 (SDK adoption) shipped with the v0.2.0
  bump. Phase 2 (engine-owned TUI composition), Phase 3 (display cleanups),
  and Phase 4 (documentation) landed on 2026-11-20 against aharness v0.4.1.
- **Source:** full review of the acode host against aharness v0.1.0, a
  re-review against v0.2.0, and a 2026-11-20 re-review against v0.3.0 and
  v0.4.1. The SDK requests were filed in the aharness repository; v0.2.0
  implemented almost all of them. v0.3.0 (identity, prompt tag) and v0.4.0
  (workspace instructions, appended provenance) changed the session inputs
  Phase 2 must carry; v0.4.1 is a patch with no public API change. The
  remaining work is still host-side.
- **Target:** Go 1.27.4, module `acode`, aharness v0.4.1.
- **Architecture:** [architecture.md](../architecture.md) defines the host
  boundary. [tui.md](../tui.md) stays authoritative for the shipped surface.
- **Testing:** [testing-strategy.md](../testing-strategy.md) plus §8 here.
- **Definition of done:** [definition-of-done.md](../definition-of-done.md).

## Decision summary

aharness v0.2.0 closed the SDK gaps, and no release since has reopened one.
**No remaining finding needs an SDK release.** What is left is one structural
host change (Phase 2), a small set of display cleanups, and documentation.
Phase 2 has grown to carry the v0.3.0/v0.4.x prompt inputs — `Identity`,
`AppendPrompt`/`AppendedPrompt`, and the engine-computed `contract_override` —
that the hand-built TUI session path currently reaches only by copying fields.

| Phase | Contents | Status |
|---|---|---|
| 1 | Bump to v0.2.0, migrate the breaking changes, adopt the new helpers | **Shipped** with the v0.2.0 bump |
| 2 | Make `engine.Host` own TUI session composition and run assembly; carry the v0.3.0–v0.4.1 prompt inputs | **Shipped** in this change |
| 3 | Residual display cleanups (channel close, run identity, headless settle) | **Shipped** in this change |
| 4 | Documentation and verification | **Shipped** in this change |

Phase 1 deleted about 300 net lines from `cmd/acode` and `internal/tui` without
changing the shipped CLI or TUI surface.

Re-review (2026-11-20, aharness v0.4.1): every open finding below was checked
against the current tree and is still open. No later release closed one, and
Phase 2 gained scope:

- v0.3.0's `Identity` is set at both construction sites (`run.go:254`,
  `run.go:418`) and must become `Spec.Identity` when the TUI opens a `Host`.
- v0.4.0's workspace instructions are composed by the app
  (`seedWorkspaceInstructions`) because the TUI writes its own
  `session_start`; `engine.Open` plus `Spec.AppendPrompt` replaces that with
  the SDK's fold, record, and fresh-child propagation.
- v0.4.0 records `session_start.appended_prompt`; the TUI's manual fork path
  must inherit it exactly as `engine.Open` does, or fork children lose the
  workspace text.
- v0.4.1 restores Responses reasoning summaries with no API change, so the
  Phase 3 thinking-block work now has real text on that dialect; the
  real-provider smoke remains pending.

## 1. Finding status after v0.4.1

Every **Open** row was re-checked on 2026-11-20 against aharness v0.4.1; the
parenthetical is the current evidence.

| # | Finding | Status |
|---|---|---|
| 1 | TUI builds sessions by hand (`session.Open`/`OpenShared`/`CreateNew`, `agent.Inspect`, `agent.WriteSessionStart`, `store.CopyHistory`) | **Fixed** — the TUI opens and switches sessions through `engine.Inspect`/`engine.Open`; the store, workspace, contract, and work directory belong to the engine host |
| 2 | TUI builds runs by hand (`agent.NewSpawner`, `tools.NewRegistry`, `SetGrant`, `SeedTodo`, `agent.New`) | **Fixed** — `Start` uses `Host.BuildRunnerWithSpawner`; the engine builds the registry, spawner, root grant, and todo seed |
| 3 | Five engine helpers copied verbatim plus `workPaths` | **Fixed** — `runnerConfig` and every copied helper are deleted |
| 4 | Fresh TUI runs record `resumed_from_seq = 1` | **Fixed** — the SDK derives the boundary; acode no longer computes it (`run.go`, `tui.go`) |
| 5 | TUI fork never records `contract_override` | **Fixed** — the fork path is `engine.Open`, which records the override when the prompt or tools differ |
| 6 | Terminal result can lose `reason`/`duration` | **Fixed** — `Result.Reason`/`Result.Duration` now drive `applyResult` |
| 7 | TUI closes the display channel | **Fixed** — the app never closes it; after the runner returns it drains buffered events in order, and a stop channel releases a blocked read |
| 8 | Steering acknowledged by text equality | **Fixed** — SDK steering IDs, `MessageRecorded.ID`, `SubagentMessageRecorded` |
| 9 | `newCompleterFor` ignores the `baseURL` argument and silently maps unknown dialects | **Fixed** — the factory takes the base URL and rejects an unknown dialect |
| 10 | `ModelStarted.Attempt` ignored | **Fixed** — the thinking block is settled only on a new turn or attempt |
| 11 | Catalog eligibility re-derived five times; `Lookup` error re-wrapped | **Fixed** — `CatalogModel.Validate`, `ErrModelUnavailable`, `Lookup`'s own error |
| 12 | Pricing needed `source`/`fetchedAt` beside the map; `Catalog` reassembled | **Fixed** — one `model.Catalog` field, `Catalog.Price` |
| 13 | Picker re-implemented `engine.List` | **Fixed** — `engine.ListSummaries` with `Workspace`/`Limit`/`MaxEvents` |
| 14 | Tool grant rebuilt from `SchemasFor` + `"delegate"` | **Fixed** — `tools.NamesFor(version, tools.NameDelegate)` |
| 15 | Todo/delegate/intent wire shapes re-declared | **Fixed** — `ParseTodoDetails`, `ParseDelegateArgs`, `IntentLine`; the todo test now runs the real tool |
| 16 | `cloneAgentOverride` field-by-field copy | **Fixed** — `AgentOverride.Clone` |
| 17 | `ModelChoice` copies `model.CatalogModel` | **Fixed** — `tui.ModelChoice` embeds the SDK struct and keeps only `Switchable` |
| 18 | `UsageLine` copies `model.Usage` | **Keep** — the `-ui-json` schema is app-owned; the Phase 3 drift test now fails when the SDK adds a field |
| 19 | Tool-name literals and `"ok"` | **Fixed** — `tools.NameDelegate`, `tools.NameTodo`, `tools.CodeOK`; the transcript keeps `tools.Code` |
| 20 | Second path-containment implementation | **Keep** — images use `Workspace.LocalDisplayPath`; `withinWorkspace` stays because the app rule also refuses a path that is lexically outside but symlinks *into* the workspace, which `Workspace.Contains` does not answer (DEC2) |
| 21 | Notice severity lost on replay | **Fixed** — `HistoryItem.NoticeKind` |
| 22 | Mid-run turns/context re-derived | **Fixed** — `ResponseCompleted` cumulative fields |
| 23 | Stop-reason failure table and empty-status invention | **Fixed** — `StopReason.Failure`, guaranteed non-empty `Result.Status` |
| 24 | Only `Store()` for history/todo/last-seq | **Fixed** — `History`, `Todo`, `LastSeq`, `State`, and `Snapshot` read through the host; `Store()` is used only for the per-run `Sync`; no `store.Events()` call remains in the app |
| 25 | Work-dir cleanup copied | **Fixed** — the engine resets the work tree per run; the app calls `Host.CleanupWork` when a run ends and `Host.Close` removes it at process end |
| 26 | Construct-to-validate catalog URL; endpoint rules duplicated | **Fixed** — `ValidateCatalogURL`; the base-URL check stays deliberately stricter and says so |
| 27 | Auth hint via `StatusCode == 401` | **Fixed** — `errors.Is(err, model.ErrAuth)` |
| 28 | Dead code: discarded history decode, `var _ = model.Usage{}`, `_ = fromDisplay`, `lastSeq` | **Fixed** — `uiRichSet`/`uiExpandSet`, the `contextLimit` catalog fallback, and `_ = history` are deleted |
| 29 | Header context limit stale after a model pick | **Fixed** — `SelectModel` updates `info.ContextLimit`; the display reads only that value |
| 30 | Docs describe the TUI as engine-composed | **Fixed** — `architecture.md` now describes `engine.Open` + `BuildRunnerWithSpawner`, app policy vs engine composition, and the per-run selection |

Additional findings from the v0.2.0 review:

| # | Finding | Status |
|---|---|---|
| 31 | `engine.Spec.ExecutionGrant` was removed; batch resume can no longer narrow the root for one process | Resolved by design: batch records no root grant and configures no spawner, so `delegate` returns `unavailable`. Documented in [architecture.md](../architecture.md). |
| 32 | `engine.Spec.UserAgent` is now required | Fixed: the batch spec sets it; the engine fills `ToolOptions.UserAgent`. |
| 33 | `agent.Config.Steering` changed to `<-chan agent.SteeringLine` | Fixed: the TUI assigns IDs and acks by ID. |

Re-review deltas from v0.3.0 and v0.4.x (scope, not new defects):

| # | Delta | Effect on this plan |
|---|---|---|
| 34 | The TUI composes the system prompt itself because it writes `session_start` by hand: `seedWorkspaceInstructions` renders the base, joins `"\n\n"`, and caps. | **Adopted.** `engine.Open` with `Spec.AppendPrompt`/`AppendPromptSet` for `ModeNew` folds and records the same text; the composition half is deleted and only `readWorkspaceInstructions` remains. |
| 35 | Prompt inputs have multiplied: `Spec.Identity` (set), `Spec.PromptTag` (unused under tier 1), `Spec.AppendPrompt` (new sessions), and `agent.Config.AppendedPrompt` (record-only). The hand-built `runnerConfig` copies some of them and can drift from the engine. | **Adopted.** The TUI builds the engine spec once from `config`; `runnerConfig` is deleted with the manual session path. |
| 36 | v0.4.1 restores Responses thinking text (`Response.Reasoning`, `ReasoningDelta`, `ResponseCompleted.Reasoning`) with no public API change. | **Adopted.** P3.5 keys the thinking-block reset on `ModelStarted.Attempt` and turn; the streamed-text path is covered by the fake provider and the real-provider smoke remains pending. |

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
| P2.1 | Hold `host *engine.Host`; replace `store`/`ws`/`sessionID` reads with `host.WorkspaceHandle()`, `host.ID()`, `host.Path()`, `host.StateDir()`, `host.History()`, `host.Todo()`, `host.LastSeq()`, `host.State()`, `host.Snapshot()`, `host.Spec()`, and `host.Close()`. Keep `tuiHost` accessors so the TUI-facing interface is unchanged; `host.Store()` is only the per-run `Sync`/lifecycle handle if the runner does not already own it. | No `store.Events()` call remains outside the host. |
| P2.2 | Open the session through `engine.Open` like the batch path: `engine.Inspect` → app policy (`applySessionDefaults`, `validateSessionRules`) → `engine.Open(spec, mode)`; map errors with `engineExitCode`. Pass the new-session workspace text as `Spec.AppendPrompt`/`AppendPromptSet` (`ModeNew` only) so the engine folds, caps, records `system_prompt` and `appended_prompt`, and propagates it to fresh children; delete the three manual mode branches, `sessionFailure`, and the render/join half of `seedWorkspaceInstructions`. Resume and fork replay the recorded contract. | New/resume/fork open exactly as batch does, with the same exit codes. Fork records `contract_override` when prompt or tools differ; a fork that inherits the prompt also carries the recorded `appended_prompt`; a new session records the workspace text once. |
| P2.3 | Build the session spec from `cfg` + definitions: `Spec.Identity = acodeIdentity`, `Spec.PromptTag` stays empty under tier 1, `Spec.AppendPrompt` for `ModeNew`, `Spec.Agent` for replay, `Spec.Definitions`, `Spec.Agents`, `Spec.ResolveModel`, `Spec.MaxSubagents`, `Spec.UserAgent`, `Spec.ToolOptions = toolOptions(cfg, "", "")`. | Delegation, web tools, thinking effort, identity, and workspace instructions unchanged; a new session records the acode identity line inside `system_prompt`, prompt-v3, and `appended_prompt` exactly once. |
| P2.4 | Replace the hand-built spawner/registry/grant/todo/runner with `BuildRunnerWithSpawner`. Delete `sessionGrant`. The engine fills the work dirs, root grant, todo seed, and carries the recorded `appended_prompt` on the config it hands the spawner. | One construction path; child steering, history, and workspace-instruction propagation still work. |
| P2.5 | Introduce `runSelection`. `SelectModel`/`prepareRunLocked` update it and `info.ContextLimit`; they no longer mutate the session `cfg`. Pass it as `RunSpec`. | A picker selection applies to the next run only; the header ratio is correct immediately. |
| P2.6 | Adopt the grant split (`Grant` + `RootGrant`) for batch: record `Grant` as the full contract and `RootGrant` as `all-but-delegate` for `ModeNew`/`ModeFork`. **Only after P2.1–P2.5**, because the TUI must honour `RootTools` through `BuildRunner`; doing it earlier would let a TUI resume of a batch session dispatch `delegate` that the record forbids. | `session_start` records `tools` (full) and `root_tools` (narrowed); a TUI resume of a batch session cannot delegate. |
| P2.7 | Delete the copied engine helpers: `prepareStateDir`, `checkWritable`, `openWorkspace`, `openSessionWorkspace`, `workPaths`, `resetWorkDirs`, `removeWorkDir`, `removeTree`, `restoreOwnerAccess`, `runnerConfig`, and the render/join/cap half of `seedWorkspaceInstructions`. Keep `readWorkspaceInstructions` (the SDK does not discover files) and `toolOptions`. Remove the redundant batch preflight. | `grep` shows no copied engine helper; `readWorkspaceInstructions` is the only app-side prompt I/O; `host.Close()` removes the work tree. |
| P2.8 | Simplify `newCompleterFor` to take `(dialect, baseURL, token, timeout, userAgent, sessionID)` and return a rejecting completer for an unknown dialect. Root and child runs use the passed `baseURL`. v0.4.1 still has exactly `DialectChat` and `DialectResponses`, so the reject arm is defensive. | A model switch to a different base URL sends to the new base URL; an unknown dialect fails with a wrapped error. |
| P2.9 | Work-dir timing: `BuildRunner` already resets the work dirs, so remove the app's `resetWorkDirs` call; removal happens at `Host.Close` (or `CleanupWork` when a run ends and the host stays idle). Update the `/cancel` wording. | Tests assert the spill file is gone at the documented point. |
| P2.10 | `NewSession`/`ResumeSession` open a new `Host`, rebuild `Info` from `State`/`Snapshot`, read history/todo through the host, then close the old host. `/new` seeds the workspace text through `Spec.AppendPrompt`, never by re-rendering; a resume or fork carries the recorded `appended_prompt`. Reject same-session resume before opening. | Failed switches leave the old session intact; `/new` records a fresh contract, and a resume/fork replays the recorded one. |

## 3. Phase 3 — residual display cleanups (M)

| Task | Change | Acceptance |
|---|---|---|
| P3.1 | Stop closing the display channel. Drain buffered events and complete when empty. The close is currently justified by the SDK's display-after-return invariant, but the host owns the channel lifetime, not the display. | A runner that emits a final event and returns still renders it; `-race` passes. |
| P3.2 | Embed `model.CatalogModel` in `tui.ModelChoice`, keeping only `Switchable`. | `modelChoice` is a two-field conversion instead of nine copies. |
| P3.3 | Delete the remaining dead state: `uiRichSet`/`uiExpandSet`, the `contextLimit` catalog fallback in `internal/tui/view.go`, and the discarded `agent.History` decode (`_ = history`) in `openTUIHost`. | No write-only fields; the header ratio comes from `Info.ContextLimit`. |
| P3.4 | Use `RunStarted.RunID`/`RunFinished.RunID` instead of `m.nextRun`/`m.run.number` for stale-event discard. | A stale event from a previous run cannot touch the current header. |
| P3.5 | Key the thinking-block reset on `ModelStarted.Attempt` (and turn) rather than settling on every start. | A retry and a new turn are distinguishable in a test; v0.4.1's restored Responses reasoning text exercises the streamed case. |
| P3.6 | Add a `UsageLine` drift test: set every `model.Usage` field to a unique value and assert the protocol line carries each one. | A new SDK usage field fails the test until the protocol is updated. |
| P3.7 | Investigate a render/emulator barrier for `headlessSettle`; if none is reliable, keep it, scope it to `-ui-json`, and record the rationale in [tui.md](../tui.md). | Decision recorded; frame tests pass repeatedly. |

## 4. Phase 4 — documentation and verification (S)

| Task | Change |
|---|---|
| P4.1 | Rewrite the TUI host section of [architecture.md](../architecture.md) after Phase 2: `engine.Open` + `BuildRunnerWithSpawner`, app policy vs engine composition, `Spec.AppendPrompt` and the recorded contract, per-run selection. |
| P4.2 | Keep [commands.md](../commands.md) and [tui.md](../tui.md) accurate for the work-dir timing, steering IDs, and `appended_prompt` replay. |
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

Shipped in this change (Phase 2/3):

8. Fork records `contract_override` when the prompt or tools differ. The engine
   computes it; acode's own fork path inherits both, so an acode fork records
   one only when a future explicit override does.
9. The work tree is cleared per run start and on host close.
10. Batch-created sessions record `root_tools` and are permanently
    non-delegating; TUI-created sessions stay delegating.

Landed independently since this plan (not Phase 2 work):

11. The v0.4.0 workspace-instruction seed records `appended_prompt` on new TUI
    and batch sessions and propagates it to fresh children
    ([delegation-fidelity-build.md](delegation-fidelity-build.md) §3.2). Phase
    2 moved the TUI half behind `engine.Open` without changing the recording.
12. v0.4.1 restores Responses thinking text with no acode change; the Phase 3
    work is the only remaining thinking-surface code.

No CLI flag, environment variable, config key, slash command, protocol version,
or exit code changes. `-ui-json` output changes only where a notice is styled as
an error and where the header counters now come from the SDK.

## 6. Decisions

| ID | Decision | Recommendation |
|---|---|---|
| DEC1 | Record `RootGrant` for batch, or accept "offered but unavailable" on resume? | Recorded in Phase 2 (P2.6). A batch-created session records the full session grant and a root grant without `delegate`. |
| DEC2 | Keep the app's `withinWorkspace` although `Workspace.Contains` exists? | Keep. The app rule also refuses an operator path that is lexically outside but resolves into the workspace; `Contains` only answers the inside direction. Revisit if the SDK gains a "resolves inside" helper. |
| DEC3 | Keep `validateBaseURL` although `model.ValidateBaseURL` exists? | Keep. The app version rejects userinfo and query, which the SDK version allows; a query would be lost when the dialect path is appended. |
| DEC4 | Adopt `engine.ListSummaries` with `MaxEvents`? | Done. It replaced the hand-rolled scan and reads only the head. |
| DEC5 | Keep `headlessSettle`? | Kept. Bubble Tea exposes no renderer-complete barrier, so the 30 ms settle remains a `-ui-json`-only frame flush, documented in [tui.md](../tui.md). |
| DEC6 | Compose the workspace text in the TUI, or pass it through `engine.Open`? | Passed through `Spec.AppendPrompt` in Phase 2. The TUI no longer renders or joins prompt text; `readWorkspaceInstructions` is the only app-side prompt I/O. |

## 7. Risks and mitigations

| Risk | Mitigation |
|---|---|
| Large TUI refactor regresses resume/fork/steering | Add the Phase 2 pins first (multi-run, switch, picker, delegation, interrupt); land Phase 2 as one reviewed change. |
| `RootGrant` recorded before the TUI honours it | P2.6 is explicitly last in Phase 2; the TUI uses `BuildRunner`, which applies `RootTools`. |
| The manual `session_start` drifts from `engine.Open` | P2.2 deletes the manual path; until then the delegation-fidelity tests pin `appended_prompt`, resume replay, and fork inheritance. |
| Workspace text is composed twice or lost on a fork | `Spec.AppendPrompt` is set for `ModeNew` only; resume carries the recorded text and a fork inherits or extends it. A fork with an explicit `SystemPrompt` records no inherited appendix, and acode never sets the append on a fork. |
| `engine.Open` is stricter than the manual paths | Map errors through `engineExitCode`; add exit-code tests for missing/locked/invalid sessions. |
| Catalog model validation is stricter than the old checks | `CatalogModel.Validate` requires a dialect, base URL, and positive context window; fixtures and operator-facing messages now name the defect. |
| `agent.NewLimits` rejects `inputLimit > maxContext` | `validateEffective` clamps the catalog ceiling to the effective window before calling it. |
| Model switch with a different base URL hits the old one | P2.8 uses the `baseURL` argument for root and children; the factory is pinned against two fake servers and the selection against two endpoints. |

## 8. Test plan

| Layer | Coverage |
|---|---|
| Unit | `applyResult`, steering by ID, limits clamping, catalog price/validation, status helpers, grant helper. |
| Contract | Todo details via a real `tools.Registry`; usage-line drift (P3.6); tool-name constants exist in `tools.Names()`; `session_start` fields — prompt version, `appended_prompt` on new TUI/batch, retained on resume, inherited on fork — plus the acode identity line inside the recorded `system_prompt`, and `contract_override` on a fork. |
| Batch integration | Both dialects including `resumed_from_seq: 0`; cache-write warning; resume boundary; `appended_prompt` on a new session, with the text inherited inside a child's `system_prompt` and no child `appended_prompt`. |
| TUI integration | Existing pins; Phase 2 adds multi-run, switch, picker, delegation, interrupt, work-dir timing, and workspace-instruction seed/inheritance. Phase 3 adds the stale-run and thinking-attempt cases. |
| Headless | Existing `-ui-json` frame/snapshot tests; repeat `-count=10` after P3.1/P3.7; v0.4.1 reasoning deltas render as thinking text. |
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

Re-review (2026-11-20, aharness v0.4.1, documentation only):

- The pin is v0.4.1. v0.4.1 is a patch with no public API change and no
  migration; v0.4.0 was already adopted by
  [delegation-fidelity-build.md](delegation-fidelity-build.md) Phase 2, and
  v0.3.0's identity is already set at both construction sites.
- Every **Open** finding above was re-checked against the current tree and
  remains open; no release since v0.2.0 closed one.
- Phase 2 now carries `Spec.Identity`, `Spec.AppendPrompt`/`AppendedPrompt`,
  and the engine-computed `contract_override` (P2.2–P2.4), and adds the
  render/join half of `seedWorkspaceInstructions` to the deletion list (P2.7).
- `make dod`: pass (goimports, docs-check, vet, golangci-lint, `go test -race
  -count=1`, build, govulncheck).
- Real-provider smoke: **pending** (no credentials in this environment).
- Real-terminal smoke: **pending**.

Implementation (2026-11-20, aharness v0.4.1):

- Phase 2: `tuiHost` holds one `engine.Host`; open and every idle switch use
  `engine.Inspect` + app policy + `engine.Open`; `Start` uses
  `BuildRunnerWithSpawner` with a `runSelection` RunSpec. The copied helpers,
  `runnerConfig`, `sessionGrant`, `batchTools`, and the manual
  `session_start` path are deleted; the app contains no `store.Events()` call
  and no prompt composition. A new TUI session records the workspace text
  through `Spec.AppendPrompt`; `/new` does the same; resume and fork replay the
  recorded contract.
- Phase 2 grant split: a new or forked batch session records the full session
  grant and `root_tools` without `delegate`; a TUI resume of it is pinned by
  `TestTUIResumeOfBatchSessionCannotDelegate`, and a TUI-created session still
  records no root grant.
- Phase 3: the display channel is never closed (stop channel releases a blocked
  read, which takes one more buffered event before reporting drained, so the
  order is deterministic; buffered events then drain in order); `ModelChoice`
  embeds `model.CatalogModel`; the dead `uiRichSet`/`uiExpandSet`, catalog
  context fallback, and discarded history decode are gone; stale events are
  keyed by the SDK run ID; the thinking reset is keyed by
  `ModelStarted.Attempt` and turn; the usage-line drift test reflects over every
  `model.Usage` field; the `headlessSettle` decision is documented in
  [tui.md](../tui.md).
- Routing pins: `TestNewCompleterForUsesThePassedBaseURL` drives two fake
  servers through the factory, and `TestTUISelectModelSwitchesBaseURL` pins
  that a picker switch changes only the next run's endpoint.
- Contract and integration tests added: engine exit codes for
  missing/locked/invalid session opens, the batch grant split, the TUI
  non-delegation resume, the fresh TUI contract recording no root grant,
  post-return display draining through the stop-woken read, stale-run discard,
  same-attempt vs retry thinking, and the usage-line drift.
- `make dod`: pass (goimports, docs-check, vet, golangci-lint, `go test -race
  -count=1`, build, govulncheck).
- Real-provider smoke: **pending** (no credentials in this environment).
- Real-terminal smoke: **pending**.
- Deviation: an unreadable or unwritable state directory now surfaces through
  `engineExitCode` as a setup failure (1) instead of the old app preflight's
  configuration failure (2). This matches the documented setup/state class in
  [architecture.md](../architecture.md#7-exit-codes) and is what batch now does
  too; the workspace-open, lock, and session-shape errors are unchanged.
