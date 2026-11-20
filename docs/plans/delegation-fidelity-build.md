# acode delegation fidelity build plan

- **Date:** 2026-11-19
- **Status:** implemented 2026-11-19 for Phases 1–2 and the automated checks;
  probes R1 and R2 remain pending because this environment has no provider
  token or SearXNG endpoint. Phase 0 was resolved by aharness v0.4.0, and Phase
  2 adopts that release.
- **Source:** a review of acode session
  `01a1f489-4824-74c3-bfc5-600a81b1ccad` (2026-11-19, TUI, `prompt-v3`,
  `deepseek-v4.1-flash`), the aharness issue filed from it
  (`ISSUE-aharness-workspace-instructions.md`, kept in the filer's gitignored
  `.issues/` directory, so it is named here rather than linked), the pending
  probe pass in [prompt-optimization-build.md](prompt-optimization-build.md)
  §13, and the aharness v0.4.0 release with its
  `docs/plans/workspace-instructions-build.md`.
- **Target:** Go 1.27.4, module `acode`, aharness v0.3.0 baseline for Phase 1;
  Phase 2 adopts the tagged aharness v0.4.0 release, which carries Ask 1 in a
  revised shape: `Config.Instructions` propagates to fresh children and
  `agent.Config.AppendedPrompt` records provenance, while the append-on-write
  field is removed. This plan does not use a `replace` fork, a vendored copy, or
  an app-side shim.
- **Architecture:** [architecture.md](../architecture.md) §1 defines the host
  boundary: acode owns the CLI, TUI, configuration, wiring, and presentation;
  the loop, prompt composition, and session contracts stay in the SDK.
- **Standard:** [coding-standard.md](../coding-standard.md);
  **Definition of done:** [definition-of-done.md](../definition-of-done.md);
  **Testing:** [testing-strategy.md](../testing-strategy.md).
- **Index entry:** [docs/README.md](../README.md).

## Decision summary

| # | Decision | Why |
|---|---|---|
| D1 | Rewrite the `researcher` description to name its trigger: a multi-source survey the parent would otherwise run itself. | The catalog description is the parent's entire routing signal for role selection (§2; [prompt-optimization-build.md](prompt-optimization-build.md) §1.3). The current description names a capability, not a trigger, and its "both" predicate read as inapplicable to a web-heavy survey. |
| D2 | Keep the routing fix app-side. Do not ask for a `prompt-v4` sentence until probe R1 shows the description alone cannot move the behavior. | v0.3.0's v3 body already carries the independence criterion ([prompt-optimization-build.md](prompt-optimization-build.md) §4.5); a prompt version is a recorded contract and needs evidence, not one transcript. |
| D3 | acode reads the workspace's root `AGENTS.md` on **new** sessions and passes the bytes through the SDK's prompt input: `Spec.AppendPrompt` for batch, and, for the TUI's own `session_start` write, a helper that renders the base with `agent.RenderSystemPrompt`, composes the bytes, and records `agent.Config.AppendedPrompt`. Resume and fork follow the recorded `system_prompt` and `appended_prompt`, never a re-read. | The file is workspace data, not loop contract, so the host owns discovery and path policy; the SDK owns composition, propagation, recording, and replay. A changed file reaches the next new session, not a running one. |
| D4 | No new flag, environment variable, or config key in v1. Seeding is fixed policy. | A new key must join the flag/env/file layers, `/settings`, and their tests ([configuration.md](../configuration.md) §2.1). The smaller product is a fixed rule plus the recorded prompt; a knob is open question 1. |
| D5 | Seed only a regular `AGENTS.md` of at most 16 KiB; a larger file is skipped with one warning, not truncated. | 16 KiB is the `max` passed to `tools.Workspace.ReadDisplayFile`, which returns `ErrTooLarge` and no bytes rather than a partial file, so a partial read would need an upstream API. The cap also keeps the composed prompt under the SDK's 32 KiB prompt cap, and the v3 read rule still lets the model open a larger file itself. |
| D6 | Adopt the released v0.4.0 shape rather than the issue's literal mechanism: `agent.Config.AppendSystemPrompt` is gone; `Config.Instructions` propagates and `agent.Config.AppendedPrompt` records provenance. | acode `AGENTS.md`: never work around aharness. The owner chose the smaller cut — delete the engine-dead append field, keep one record-only field — and released it. acode adds no shim and no second append channel. |
| D7 | Bump `go.mod` to v0.4.0 as the first Phase 2 task; the bump is otherwise mechanical for acode. | Verified 2026-11-19 on a scratch copy: only `go.mod`/`go.sum` change, `go build ./...` succeeds, and the full existing suite passes against v0.4.0. Bumping with Phase 2 keeps Phase 1 independent of the release. |
| D8 | A log with a recorded `system_prompt` but no `appended_prompt` (every v0.3.0-era log) does not seed its new children; the bytes are not recoverable from the prompt. | The recorded value is provenance, never a diff. A resume or fork replays what was recorded; the v3 read rule remains the fallback, and `/new` is the way to mint a fresh contract. |

Phases:

| Phase | Contents | Status |
|---|---|---|
| 0 | Upstream: the aharness issue's Ask 1, released as v0.4.0 (child propagation and recorded provenance) | Done upstream 2026-11-19 |
| 1 | Research routing: the `researcher` description and its pin, plus probe R1 | Implemented 2026-11-19; probe R1 pending |
| 2 | Workspace instructions: read, cap, pass through, and verify on the parent and child paths; adopts the v0.4.0 release | Implemented 2026-11-19; structural P2.4 check passed; probes pending |
| 3 | Current-reference and README updates, the probe record, and `make dod` | Docs and `make dod` done 2026-11-19; probe records pending |

## 1. Evidence

The session is one transcript, not a rate. It is enough to name two defects
and to design a probe, not to declare a model-wide behavior.

| Fact | Evidence | Consequence |
|---|---|---|
| The parent did the web research itself. | 36 `web_search`/`web_fetch` calls over turns 13–20 and 28–29, all in the parent. | The most delegation-shaped work in the session never reached a child. |
| The parent can and does delegate. | Two `delegate` calls, both `reviewer` (turns 53 and 73), for independent review of the build plan. | The tool grant, the catalog, and the child path work; the failure is routing, not capability. |
| The `researcher` description was not a usable trigger. | "Answers questions needing both repository and web evidence. Call it when sources must be found and weighed…" (178 bytes, `cmd/acode/agents/researcher.md:2`). The parent already held the repository evidence; the work was web-heavy. | The description's "both" predicate read as inapplicable, and no other surface names when research should be delegated. |
| The SDK's delegation paragraph is a cost/independence filter. | `prompt-v3`: "…when the conclusion must be reached without the context that produced the work, or when it would otherwise flood this conversation with tool output, or when independent read-only tasks can run in parallel…" | A serial, context-dependent survey qualifies under none of the three. This is the same failure class the prompt-optimization plan diagnosed for review (§1.2 there), fixed for `reviewer` and not for research. |
| The parent read `AGENTS.md` voluntarily. | The first model turn ran `cat AGENTS.md` (and `cat README.md`); the file is 3,483 bytes. | The v3 sentence works when the model complies, which is why the seed is not about the parent's obedience. |
| The recorded prompt carries no workspace rules. | `session_start.system_prompt` is 2,636 bytes; the only `AGENTS.md` reference is the v3 rule line. | Nothing records that the rules were loaded, and a child starts with no history. |
| A host could not append workspace rules and reach a child at v0.3.0. | aharness issue §2.1: `Spawner.childConfig` dropped `AppendSystemPrompt`; `engine` folded it into `SystemPrompt` before the spawner saw it. v0.4.0 closes this (D6): the host input propagates and the recorded `appended_prompt` reaches fresh children. | The seed was blocked until the SDK carried the bytes; a parent-only seed would have been the bug, not the fix. |

## 2. What acode already has

| Fact | Evidence | Consequence |
|---|---|---|
| The five role definitions are acode product content. | `cmd/acode/agents/*.md`, loaded by `agent.LoadAgents` (`cmd/acode/tui.go`); operator files in `<state-dir>/agents/` override by name. | The description rewrite is an acode change, not an SDK ask. |
| The descriptions are pinned by test. | `cmd/acode/agents_test.go` pins the exact text, the grants, the bodies, and the byte counts (`:109`). | Any rewrite updates the pin; the pin is what makes the change deliberate. |
| The parent prompt renders the catalog from those descriptions. | `agent.AgentCatalogText` inside the SDK's `prompt-v3` body; the recorded prompt in the session ends with the five catalog lines. | The description is the only routing surface for role selection ([prompt-optimization-build.md](prompt-optimization-build.md) §1.3). |
| The TUI writes its own `session_start`. | `cmd/acode/tui.go:874` (fork), `:917` (new), and `cmd/acode/sessions.go:147` (`/new`) call `agent.WriteSessionStart`. | A seed must be supplied before those calls on a new contract, and not re-appended on the fork path. |
| The TUI builds its own spawner. | `cmd/acode/tui.go:621-636`, `Config: rcfg`; v0.4.0's `childConfig` reads `SpawnerOptions.Config.AppendedPrompt` (`agent/spawn.go:765`). | acode's `runnerConfig` must copy the resolved `AppendedPrompt` to the spawner, or the TUI's new children get no workspace text. It is a field copy, never a file read. |
| Batch opens through the engine. | `cmd/acode/run.go`, `batchEngineSpec` → `engine.Open`; `Spec.AppendPrompt` is the new-session input (`engine/engine.go:691-692`). | Batch sets the spec input once; the engine folds it, records `appended_prompt`, and carries it to children. No spawner-side change. |
| Resume and fork re-supply the recorded contract. | `cmd/acode/models.go:426` (`cfg.SystemPrompt = state.SystemPrompt`); `/new` clears it (`cmd/acode/sessions.go:140`); v0.4.0 adds `agent.State.AppendedPrompt` (`agent/state.go:42-46`). | The resumed or forked config must also carry `state.AppendedPrompt` for the spawner and the fork record. The seed must be attached at the new-contract sites, not inferred from a non-empty prompt: a legacy log with no recorded prompt is also empty. |
| The workspace path policy is the SDK's. | acode `AGENTS.md`: route workspace paths through `tools.Workspace`/`os.Root`. | Reading `AGENTS.md` is app I/O through the SDK handle, not `os.Open`. |

### 2.1 Upstream status: aharness v0.4.0 (2026-11-19)

The release carries Ask 1, in a shape the issue did not name. Every row was
checked at tag `v0.4.0` (`97d283f`); its changelog records the migration for
component hosts.

| Fact | Evidence | Consequence for acode |
|---|---|---|
| The append-on-write field is gone. | `agent.Config.AppendSystemPrompt` was engine-dead; v0.4.0 removes it (`agent/types.go:96-107`, CHANGELOG **Removed**). The agent layer never appends. | The plan's old "add the bytes to the append" wiring does not compile against v0.4.0 and is replaced by composition (D3). |
| The host input reaches every fresh child. | `Config.Instructions` → `engine.Spec.AppendPrompt` (`aharness.go:226-227`); `engine.Host.appendedPrompt` (`engine/engine.go:170-172`) → `runConfig` (`engine/runner.go:169`) → `SpawnerOptions.Config.AppendedPrompt` → `childSystemPrompt` after the role body (`agent/spawn.go:765`, `agent/prompt.go:186-212`). | Batch only sets `Spec.AppendPrompt`; the TUI must put the field on the config it hands its spawner. |
| Provenance is recorded once. | `session_start.appended_prompt` (`agent/events.go:39-42`), `agent.Config.AppendedPrompt` (`agent/types.go:99-105`), written by `WriteSessionStart` (`agent/runner.go:74`), exposed as `agent.State.AppendedPrompt` (`agent/state.go:42-46`). | The TUI's own `WriteSessionStart` call must set the field, or its sessions record no provenance — and its children get nothing. |
| Resume and fork carry the recorded value. | `resolveInherited` (`engine/engine.go:591-607`): resume carries `state.AppendedPrompt`; a fork that inherits the prompt carries or extends it; a fork with an explicit prompt records none. | The TUI resume/fork config must copy `state.AppendedPrompt`; the batch engine does it itself. |
| Children record nothing. | `childConfig` composes `parent.AppendedPrompt`; `continuedConfig` appends nothing (`agent/spawn.go:791-830`); `doc.go:84-85`. | A child prompt contains the text once, and a continuation cannot double it. |
| The SDK still does not discover workspace files. | `aharness.go:84-86`, `doc.go:79-81`. | The reader and cap remain acode's, exactly as designed here. |
| The prompt and tool contracts are unchanged. | CHANGELOG v0.4.0: `prompt-v3` unchanged, schema stays `2`, tool grants, schemas, and toolset version unchanged, old logs need no migration. | Phase 1's description change and probe are unaffected; the catalog cap is still 200 bytes. |
| The bump is compatible with acode's current code. | 2026-11-19, scratch copy: `go get github.com/eddiectc/aharness@v0.4.0` changed only `go.mod`/`go.sum`; `go build ./...` and `go test ./... -count=1` passed. | P2.0 is mechanical; no compile change is needed before the Phase 2 wiring. |
| The upstream plan records a pending acode check. | aharness `docs/plans/workspace-instructions-build.md` §7.4: a fresh acode session must record its prompt plus `appended_prompt`, and a delegated child's prompt must carry the text once after the role body with no child `appended_prompt`. | acode's P2.4 is that check; the result belongs in this plan's §8 as well as upstream. |

One wording note: the upstream check names `Config.Instructions` because it
assumes a facade host. The TUI is a component host, so acode reaches the same
recorded contract by composing `SystemPrompt` and setting
`agent.Config.AppendedPrompt`; batch reaches it through `Spec.AppendPrompt`.
The check is the same recorded state, not the same field name.

## 3. Design

### 3.1 Research routing: the description is the trigger

`cmd/acode/agents/researcher.md:2` becomes:

> `Answers questions needing sources from the workspace, the web, or both. Call it for a multi-source survey you would otherwise run yourself; claims come back cited, with inference marked.`

It replaces the shipped line (178 bytes, quoted from
[prompt-optimization-build.md](prompt-optimization-build.md) §6):

> `Answers questions needing both repository and web evidence. Call it when sources must be found and weighed; claims come back cited (file and line, or URL), with inference marked.`

186 bytes, under the SDK's 200-byte cap. Three changes:

1. **"sources from the workspace, the web, or both"** replaces "both
   repository and web evidence". The old phrase read as requiring both, so a
   web-heavy question looked inapplicable.
2. **"a multi-source survey you would otherwise run yourself"** is the trigger.
   It names the mistake the rule prevents — the parent spending its own turns
   on a serial survey — rather than a cost threshold the work does not cross.
3. The return half keeps "claims come back cited, with inference marked". The
   dropped "(file and line, or URL)" is already stated in the body the child
   loads (and the child has no catalog, so it is the only place that can carry
   it); keeping it would make the line 210 bytes, over the 200-byte cap.

The catalog is rendered into each new session's recorded prompt, so existing
logs replay their recorded description; only sessions created after the change
see the new line. Nothing else in the prompt moves.

Alternatives rejected:

| Alternative | Why not |
|---|---|
| Add "always delegate research" to the role body | A body loads only inside the child; it cannot route the parent. |
| Add an acode tier-2 prompt block | The parent's delegation guidance is SDK-owned. A tier-2 block would either duplicate it or smuggle product routing into prompt text, and a `PromptTag` would exist to attribute a sentence the SDK should own. |
| File a `prompt-v4` ask now | One transcript cannot justify a new recorded prompt version. Probe R1 decides; D2. |
| Change the `reviewer`, `explore`, or `task` descriptions | Out of scope; the reviewer trigger was the prompt-optimization plan's subject and is shipped. |

Probe R1, in the style of the prompt-optimization plan §13:

- **Fixture:** this repository at the phase's commit, plus a question of the
  class "how do other popular coding agents expose X?", needing 8–12 web
  sources and some repository evidence.
- **Arms:** A, the current description; B, the new description. Hold model,
  effort, fixture, and task text constant.
- **Pass:** in arm B the parent delegates the survey to `researcher` before
  writing its design, most fetches happen in the child, and the child's report
  cites sources. Arm A is the baseline; a single run per arm is a smoke check,
  not a rate.
- **Negative control:** a one-file lookup question must not spawn a
  `researcher`. The description says "multi-source survey", and the control
  catches an over-delegation trigger.
- **Record:** model, effort, fixture commit, arm, trace path, and the counts
  (parent fetches, child fetches, delegations). Store the outcome in this plan
  under §8.

### 3.2 Workspace instructions: seed the root `AGENTS.md`

**What is read.** `<workspace>/AGENTS.md` only: one root file, the convention
the SDK's v3 line already names. No nested hierarchy, no `CLAUDE.md` alias, no
`@import` expansion. A missing file is not an error and changes nothing.

**How it is read.** Through the SDK workspace handle,
`tools.Workspace.ReadDisplayFile(rel, max)` (`tools/root.go:322-357`): a
relative path, no `.git` component, a regular file, no symlink components, and
a caller-supplied size limit. The name says display, but it is the only
host-facing workspace read, and it applies the same path policy the tools use.
acode passes 16 KiB as `max`; the read returns `ErrTooLarge` with no bytes
above it, so the app cannot keep a head without an upstream API and skips with
a warning instead (D5). The read returns raw bytes; the app-side reader then
replaces invalid UTF-8 and passes the text on unchanged.

**When it is read.** A new session only, once, before its `session_start` is
written. The channel differs by host path, because acode writes `session_start`
itself in the TUI but uses the engine in batch:

| Path | Site | Behavior |
|---|---|---|
| TUI startup, new session | `cmd/acode/tui.go:917` | Read once through `ws`. When `cfg.SystemPrompt` is empty, render it with `agent.RenderSystemPrompt`; compose `base + "\n\n" + text`; set `cfg.AppendedPrompt`. `agent.WriteSessionStart` records both fields. |
| TUI `/new` | `cmd/acode/sessions.go:140-151` | Clear `cfg.SystemPrompt` and `cfg.AppendedPrompt`, then the same read and compose before the write. |
| Batch new session | `cmd/acode/run.go` `executeNew` → `batchEngineSpec` | Open the SDK workspace handle for the read, close it, and pass the text to `batchEngineSpec`, which sets `Spec.AppendPrompt`/`AppendPromptSet` for `ModeNew` only; the engine folds, caps, records `appended_prompt`, and carries it to children. Never set the spec append on resume or fork. |
| Resume | `cmd/acode/models.go:426` | No read. Replay `state.SystemPrompt` and copy `state.AppendedPrompt` into the config; `runnerConfig` passes the latter to the TUI spawner, and the batch engine carries it itself. |
| Fork | `cmd/acode/tui.go:872-874` | No read and no append. The inherited prompt and `state.AppendedPrompt` are recorded as the fork's contract; its new children get the inherited bytes. The batch fork follows the same SDK rule (`engine/engine.go:591-607`). |

The session's `AppendedPrompt` lives on the resolved `config` for the
session's lifetime. A new-session helper renders when needed, composes the text
into `SystemPrompt`, and sets the field; `runnerConfig` copies the field and
never reads the file, so a resume or fork can only carry what was recorded. The
helper is used at the three new-contract sites rather than a mode check inside a
shared function, because an empty `cfg.SystemPrompt` does not mean "new
session": a resumed or forked legacy log whose `session_start` predates the
recorded prompt also has an empty `SystemPrompt`, and the engine renders it.
Passing an append there would fail a batch resume (`engine/engine.go:359-361`)
or silently change a fork's inherited prompt. Re-reading inside `runnerConfig`
would also re-read on every run and could pick up a file that changed
mid-session.

**Recording.** The seeded text lands in the recorded
`session_start.system_prompt` and once more in the optional
`session_start.appended_prompt` (`agent/events.go:39-42`), capped
independently at 32 KiB (`agent/runner.go:74`). The field is provenance, never
a replay key: projection keeps using `system_prompt`
(`agent/context.go:385-394`), and after truncation the recorded value is not
necessarily a suffix of the prompt. `/prompt` in
[prompt-transparency-build.md](prompt-transparency-build.md) then shows the
exact text the model saw, with no new surface. Both new-session paths join with
`"\n\n"` (the engine's fold is `engine/engine.go:692`), so a TUI and a batch
session over the same file record the same prompt suffix below the cap.

**Cap and warning.** A regular file of at most 16 KiB is seeded whole. A larger
file, a symlink, a directory, or an unreadable file is skipped with one
warning on stderr in batch and one TUI notice; the session still starts,
because a broken `AGENTS.md` must not make the workspace unusable. Skipping
rather than truncating is deliberate: `ReadDisplayFile` has no partial mode,
and a half-file presented as the workspace rules is worse than a clear warning
plus the v3 read rule. The cap also keeps acode's body, catalog, and file under
the SDK's 32 KiB prompt cap.

**Trust.** The file is workspace content and is treated as operator-trusted:
the operator chose the workspace. It is prompt text, not executable config;
acode does not parse directives, run commands, or resolve paths from it. A
cloned repository can therefore inject instructions into the model's context —
that is the nature of the convention, and the README section says so.

**Cost.** A typical file is a few kilobytes (3.4 KiB here) on top of a 2.6 KiB
prompt, once per session, and it counts toward input tokens. Probe R2 and the
prompt-optimization plan §13.4 (read cost) are the evidence, not an assumption.

**Children.** On v0.4.0 every fresh child composes the parent's
`AppendedPrompt` after its rules and role body into its own recorded
`system_prompt`; the child records no `appended_prompt`
(`agent/spawn.go:765`, `agent/prompt.go:186-212`). A continued child keeps the
text through its recorded prompt and appends nothing
(`agent/spawn.go:791-830`). Children of a resumed or forked parent get the
recorded bytes; a log written before the field has none to give, and the v3
sentence remains their fallback (D8). A child that also reads the file costs
one redundant read and nothing else, because the text is identical.

### 3.3 What acode must not do

- Do not copy prompt text, role text, or the v3 rules into app code to work
  around the SDK. Call `agent.RenderSystemPrompt` for the base prompt; do not
  transcribe it.
- Do not add a second path root or read the file with `os.Open` outside the
  workspace handle.
- Do not treat `AppendedPrompt` as an append channel: never append it at the
  app layer, and never set the removed `AppendSystemPrompt`. Set it only to the
  bytes already composed into `SystemPrompt`.
- Do not re-read the file on resume or fork, and do not append on the fork
  path where the recorded prompt already contains it.
- Do not infer "new session" from an empty `SystemPrompt` or `AppendedPrompt`;
  both are also empty on a legacy log.

## 4. Milestones

### Phase 0 — upstream (done 2026-11-19)

| Task | Change | Acceptance |
|---|---|---|
| P0.1 | File the aharness issue (draft in `.issues/`): child propagation, the recorded append, and the schema shape. | Done: the aharness plan records the issue and the owner's decision. |
| P0.2 | The SDK carries Ask 1 in a tagged release. | Done: aharness `v0.4.0` (2026-11-19) propagates `Config.Instructions`, records `session_start.appended_prompt`, and removes the engine-dead append field (D6). |
| P0.3 | If declined, record the decision and the blocked phase here. | Not taken: the ask was accepted in revised shape. |
| P0.4 | Record the release's pending acode check. | Done here: v0.4.0's plan §7.4 check is acode's P2.4; the result is recorded in §8. |

### Phase 1 — research routing (S, unblocked)

| Task | Change | Acceptance |
|---|---|---|
| P1.1 | Rewrite the `researcher` description (186 bytes, §3.1); body and grant unchanged. | `cmd/acode/agents/researcher.md` carries the new line; `agent.LoadAgents` accepts it. |
| P1.2 | Update the pin in `cmd/acode/agents_test.go` (`:69` text, `:109` byte count 186). | The test pins the new text and bytes; the other four entries are untouched. |
| P1.3 | Run probe R1 with both arms and the negative control; record model, effort, fixture, and trace counts under §8. | The record exists and reads as a smoke check, not a rate. |
| P1.4 | Record the supersession of [prompt-optimization-build.md](prompt-optimization-build.md) §6's `researcher` line here, without editing that frozen plan. | This plan names the old text and why it changed. |

### Phase 2 — workspace instructions (M, unblocked)

| Task | Change | Acceptance |
|---|---|---|
| P2.0 | Bump `github.com/eddiectc/aharness` to v0.4.0. | `go.mod` pins v0.4.0; no `replace`, fork, or vendored copy; the existing suite passes unmodified (verified on a scratch copy, §8). |
| P2.1 | Add the app-side reader: root `AGENTS.md` through `ReadDisplayFile`, a 16 KiB limit, UTF-8 sanitization, one warning. Unit tests cover absent, empty, normal, at-cap, over-cap, invalid UTF-8, symlink, and a directory named `AGENTS.md`. | The reader is a small `cmd/acode` helper with no new dependency; tests are table-driven and use `t.TempDir`; an over-cap file is skipped, not truncated. |
| P2.2 | Add `AppendedPrompt` to the `config` struct. Read once at each of the three new-session sites (TUI startup, `/new`, batch). Add a new-session helper that renders the base prompt with `agent.RenderSystemPrompt(cfg.PromptVersion, cfg.Workspace, acodeIdentity, cfg.Agents)` when `cfg.SystemPrompt` is empty, composes the file after it with the same `"\n\n"` join the engine uses, and sets `cfg.AppendedPrompt`; call it at the TUI sites. Copy `AppendedPrompt` in `runnerConfig` (a field copy, never a read). Batch opens the workspace handle, reads, closes, and sets `Spec.AppendPrompt`/`AppendPromptSet` for `ModeNew` only. | A new TUI or batch session records `system_prompt` containing the file once and `appended_prompt` equal to it; a fork's inherited prompt is unchanged; a resume succeeds without an append, including on a legacy log. |
| P2.3 | Wire resume and fork to the recorded contract only: `applySessionDefaults` copies `state.AppendedPrompt` beside `state.SystemPrompt`; no read and no append at the call sites; the fork's `session_start` records the inherited `appended_prompt`. | A resumed or forked session with a recorded prompt keeps it byte-identical; a fresh child of either gets the recorded bytes once; a legacy log records none and its children get none (D8). |
| P2.4 | Verify the child path with a delegated `implementer` or `reviewer`; this is the fresh-acode check aharness v0.4.0 left pending in its plan §7.4. | The child's `session_start.system_prompt` contains the text exactly once, after its role body; the child records no `appended_prompt`. |
| P2.5 | Update the reference docs: [architecture.md](../architecture.md) §1 (what acode reads, the two recorded fields, and child propagation), [limits.md](../limits.md) (the 16 KiB cap, the skip rule, and `appended_prompt` as provenance), and README (a short "Workspace instructions" section before "Delegated subagents"). | `make docs-check` passes; the docs state new-only reads, the recorded contract, child propagation, the legacy-log rule, the skip rule, and the trust rule. |
| P2.6 | Update the prompt-shape tests that pin the recorded prompt and `session_start`; fixtures without `AGENTS.md` stay unchanged. | Existing shape pins pass unmodified for workspaces without the file (v0.4.0 omits an empty `appended_prompt`); new cases cover both fields with it. |

### Phase 3 — verification (S)

| Task | Change | Acceptance |
|---|---|---|
| P3.1 | Run `make dod`. | `goimports`, `go vet`, `golangci-lint`, `go test -race -count=1 ./...`, `go build`, `govulncheck`, and `docs-check` pass. |
| P3.2 | Run probe R2 (workspace rules in a child) and record it under §8. | A temp repo with an `AGENTS.md` rule ("write artifacts under `out/`, not the repo root") and an `implementer` child: the child obeys the rule; the same task without the file is the control. |
| P3.3 | Record deviations, the final status, and any follow-up upstream ask. | This plan's status is `implemented`, or `proposed` with the open probes recorded; the evidence is in §8. |

## 5. Test matrix

| Layer | Case | Expectation |
|---|---|---|
| Unit (reader) | No `AGENTS.md` | Empty text, no warning, no prompt change |
| Unit (reader) | File at the 16 KiB boundary | Copied whole; no warning |
| Unit (reader) | File over 16 KiB | Skipped with one warning; no prompt change |
| Unit (reader) | Invalid UTF-8 | Replaced with U+FFFD; no error |
| Unit (reader) | Symlink, directory, or unreadable file | Skipped with one warning; the session starts |
| Unit (agents) | Shipped definitions | `researcher` description and 186-byte count pinned; roster, grants, and bodies unchanged |
| Host unit | New TUI session with `AGENTS.md` | `system_prompt` contains the text once, after the catalog; `appended_prompt` equals the text |
| Host unit | New batch session with `AGENTS.md` | `system_prompt` contains the text once (the batch parent has no catalog); `appended_prompt` equals the text, through `Spec.AppendPrompt` |
| Host unit | New session with no `AGENTS.md` | `system_prompt` is byte-identical to today's render; `appended_prompt` is omitted |
| Host unit | Resume (either mode) with the file changed on disk | Recorded prompt and `appended_prompt` unchanged; no re-read |
| Host unit | Fork (either mode) with the file changed on disk | Inherited prompt unchanged and not duplicated; the fork records the inherited `appended_prompt`; new children get it |
| Host unit | Fork or resume of a legacy log with no recorded prompt | No seed is appended; the resume succeeds; the fork renders the recorded version and omits `appended_prompt` |
| Host unit | Fork or resume of a v0.3.0-era log (prompt recorded, no `appended_prompt`) | Resume succeeds; the fork omits `appended_prompt`; new children of either get no seed (D8) |
| Host unit | `/new` after a resume | Clears both fields, reads once, and records a fresh contract |
| Host unit | Delegated child after P2.4 | Child `session_start.system_prompt` contains the bytes once, after its role body; the child records no `appended_prompt` |
| Integration | Fake provider, binary `-batch` and `-ui-json` | `session_start.system_prompt` and `session_start.appended_prompt` asserted from the log; no network |
| Manual | Probe R1 (routing) | Arm B delegates the survey; the negative control does not |
| Manual | Probe R2 (obedience) | The child follows the seeded file rule; the control does not |

## 6. Risks

| Risk | Mitigation |
|---|---|
| The v0.4.0 shape differs from the issue's literal mechanism | D6: adopt the released shape; the SDK now records provenance and propagates it, and acode adds no shim or second append channel. |
| A v0.3.0-era log has no `appended_prompt` | D8: new children of a resumed or forked legacy log get no seed; the v3 read rule is the fallback, and `/new` mints a fresh contract. |
| The description change over-delegates cheap lookups | The trigger names a multi-source survey; probe R1's negative control catches a regression. |
| The routing change is model-specific | Probe R1 holds model and effort constant and records them; the description is text, not a model setting. |
| Prompt growth per session | 16 KiB cap, skip plus one warning above it, and the §13.4 read-cost probe; the recorded prompt and `appended_prompt` caps still apply. |
| The TUI composes the prompt itself (component host) | Call `agent.RenderSystemPrompt`; do not copy prompt text; a unit case pins the recorded bytes; batch stays engine-folded. |
| The engine's double cap (aharness plan §8.1) if a composed prompt exceeds 32 KiB | 16 KiB keeps acode's body, catalog, and file under the cap; the upstream defect is separate and unfiled. |
| A cloned repository injects instructions | The file is workspace content by convention; the docs state the trust rule and that nothing in it is executed or parsed. |
| A seeded child also reads `AGENTS.md` | One redundant read of the same file; accepted, and the fallback sentence is what makes a non-seeding host work. |
| Stale rules after the file changes | By design: the contract is fixed at `session_start`; `/new` is the way to pick up a change. Documented in P2.5. |

## 7. Open questions

1. **Should the seed be configurable?** Deferred (D4). There is no opt-out in
   v1, and that is the cost of the smaller surface: an operator in a repository
   whose `AGENTS.md` they do not want loaded has no switch. If that proves
   wrong, the smallest key is `instructions_file` (path, empty disables) with
   the flag and env layers and a `/settings` row, which is a separate change
   with its own tests.
2. **Nested `AGENTS.md` files and other names?** A host-side or SDK-side
   follow-up, not this plan. The current convention is root-only, and the
   aharness research records that read-only roles may skip project context to
   keep exploration cheap (`docs/research/subagent-research.md:375` in
   aharness).
3. **If probe R1 fails, what is the upstream ask?** A `prompt-v4` delegation
   sentence of the shape: "Delegate work that needs several sources gathered
   and weighed, even when the parent could run it serially; fresh context is
   the point." Filed with the probe evidence, not before it (D2).
4. **Should `/details` show a "workspace rules: 3.4 KiB" row?** The proposed
   `/prompt` view in the transparency plan would show the text; a details row
   is a separate display decision.
5. **Should the TUI adopt the engine for its new-session write?** No: the TUI
   is a component host by design (it owns the spawner, display, and steering),
   and v0.4.0 exposes the one composition helper it needs. Moving the TUI to
   the engine is a separate architecture change, not this plan.

## 8. Verification record

- **2026-11-19, Phase 1:** `cmd/acode/agents/researcher.md` carries the
  186-byte trigger line from §3.1, and `cmd/acode/agents_test.go` pins that text
  and byte count. The other four definitions, grants, and bodies are
  untouched. `go test ./cmd/acode -run TestDefaultAgentDefinitions -count=1`
  passed. P1.4: this supersedes the `researcher` line quoted in
  [prompt-optimization-build.md](prompt-optimization-build.md) §6; that frozen
  plan was not edited.
- **2026-11-19, P2.0:** `go get github.com/eddiectc/aharness@v0.4.0` followed
  by `go mod tidy` changed only `go.mod`/`go.sum`; there is no `replace`, fork,
  or vendored copy. `go build ./...` and the full suite passed against v0.4.0.
- **2026-11-19, P2.1:** `cmd/acode/workspace_instructions.go` reads the root
  `AGENTS.md` through `tools.Workspace.ReadDisplayFile` with a 16 KiB cap,
  replaces invalid UTF-8 with `sanitizeText`, treats an absent file as empty,
  and returns one warning for a skipped file. Table-driven tests cover absent,
  empty, normal, at-cap, over-cap, invalid UTF-8, symlink (live and dangling),
  directory, unreadable, and nested-only cases; the over-cap case is checked
  with `errors.Is(err, tools.ErrTooLarge)`. The seed helper tests pin the
  `"\n\n"` composition, catalog rendering, an explicit base prompt, and the
  no-change/no-warning behavior without a file. A binary test also checks the
  batch stderr warning for an over-cap file, and `-ui-json` tests check the
  startup and `/new` notices.
- **2026-11-19, P2.2–P2.3:** `config.AppendedPrompt` is copied by
  `runnerConfig` and by `applySessionDefaults`; TUI startup and `/new` call the
  seed helper, and batch sets `engine.Spec.AppendPrompt` only for `ModeNew`.
  Resume and fork never read the file. Binary tests with a fake provider assert
  both recorded fields, no re-read after the file changes, a fork's inherited
  prompt, `/new` minting a fresh contract, and the legacy-log rule (a recorded
  prompt without `appended_prompt` seeds no children; a log with no recorded
  prompt resumes or forks with no seed). Delegation tests cover children of a
  new, resumed, and forked parent: each child records the inherited text once
  after its role body and records no `appended_prompt`. The no-file prompt-shape
  pins pass unchanged: `appended_prompt` is omitted.
- **2026-11-19, P2.4 (structural):** a `-ui-json` fake-provider delegation test
  shows a new parent recording `appended_prompt`, its `explore` child recording
  the same text exactly once in `system_prompt` after the role body, and no
  `appended_prompt` on the child. The resumed, forked, and legacy-parent cases
  are covered separately. These are structural checks against the SDK's
  recorded contract, not a real-model run; the upstream §7.4 real-model check
  remains pending with R1/R2.
- **2026-11-19, P2.5–P2.6:** `docs/architecture.md` §1 and §2,
  `docs/limits.md`, `docs/commands.md` (`/new`), the README "Workspace
  instructions" section, and the docs index describe the new-only read, the
  16 KiB skip rule, the two recorded fields, child propagation, the legacy
  rule, and the trust rule. `make docs-check` passed.
- **2026-11-19, P3.1:** `make dod` passed, re-run after the added coverage:
  `go vet ./...`, `golangci-lint run` (0 issues),
  `go test -race -count=1 ./...`, `go build -trimpath -o bin/acode
  ./cmd/acode`, `govulncheck ./...` (no vulnerabilities), plus `docs-check`
  and the formatting gate.
- **Pending:** probe R1 (routing), probe R2 (obedience), and the real-model
  half of P2.4. This environment has neither a provider token
  (`ACODE_PROVIDER_TOKEN` unset) nor a SearXNG endpoint
  (`ACODE_SEARXNG_URL` unset), so those manual probes were not run.

## 9. References

- acode session `01a1f489-4824-74c3-bfc5-600a81b1ccad`, prompt-v3,
  `deepseek-v4.1-flash`; the recorded prompt is 2,636 bytes.
- aharness v0.4.0 (`97d283f`): `agent/prompt.go:64` (the v3 read rule),
  `agent/prompt.go:186-212` (the child prompt, host text after the role body),
  `agent/prompt.go:219-232` (the single cap; the agent layer never appends),
  `agent/spawn.go:751-786` (fresh-child config; `parent.AppendedPrompt`),
  `agent/spawn.go:791-830` (continued child; nothing appended),
  `agent/events.go:16-55` (`session_start` with `appended_prompt`),
  `agent/state.go:23`, `:42-46` (`State` and `AppendedPrompt`),
  `agent/runner.go:74` (the recorded value), `agent/types.go:96-105`
  (`SystemPrompt`/`AppendedPrompt`; the CHANGELOG records the removed
  `AppendSystemPrompt`),
  `engine/engine.go:170-172` (the host-carried text), `:591-607`
  (resume/fork provenance), `:682-696` (new-session prompt resolution),
  `engine/runner.go:156-191` (the run-config field list),
  `aharness.go:79-88` and `:226-227` (`Instructions` and its spec mapping),
  `tools/root.go:322-357` (the workspace read), and
  `agent/context.go:385-394` (the recorded prompt projection).
- aharness plans: `docs/plans/workspace-instructions-build.md` §7.4 (the
  pending fresh-acode check) and §8.1 (the double cap).
- acode: `cmd/acode/agents/researcher.md`, `cmd/acode/agents_test.go`,
  `cmd/acode/workspace_instructions.go`, `cmd/acode/tui.go:621-636`, `:874`,
  `:925`; `cmd/acode/run.go` (`executeNew`, `batchEngineSpec`,
  `runnerConfig`); `cmd/acode/config.go` (the `config` struct);
  `cmd/acode/models.go:428`; `cmd/acode/sessions.go:140-155`.
- Plans: [prompt-optimization-build.md](prompt-optimization-build.md) §1.2,
  §1.3, §4.5, §6, §13; [prompt-transparency-build.md](prompt-transparency-build.md)
  §3 (the `/prompt` view).
- aharness research: `docs/research/subagent-research.md:375` (read-only
  roles may skip inherited context) and `:552` (the deferred question).
