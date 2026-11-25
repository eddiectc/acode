# acode subagent live-view build plan

- **Date:** 2026-11-25
- **Status:** implemented 2026-11-25 for Phases 0–3 against aharness v0.6.0.
  The automated gates in §9 pass; the real-provider smoke remains open.
- **Source:** aharness v0.6.0 (2026-11-25), its CHANGELOG and PR #8
  (`Add subagent observation and session transcripts`), and acode's current
  `/agent-hub` view. The SDK release is additive: session schema `2`,
  `prompt-v3`, `toolset-v2`, event types, and the blocking depth-one delegation
  model are unchanged.
- **Target:** Go 1.27.4, module `acode`, aharness v0.5.0 baseline; Phase 0
  adopts the tagged v0.6.0 release.
- **Architecture:** [architecture.md](../architecture.md) §1 (host boundary),
  §5 (TUI host capabilities), and §7 (session log);
  [tui.md](../tui.md) §"Child focus and delegation steering";
  [commands.md](../commands.md) §`/agent-hub`.
- **Standard:** [coding-standard.md](../coding-standard.md);
  **Definition of done:** [definition-of-done.md](../definition-of-done.md);
  **Testing:** [testing-strategy.md](../testing-strategy.md).
- **Index entry:** [docs/README.md](../README.md).

## 1. Verdict

The SDK gap is closed, so this is now an app-side change, not an upstream ask.
v0.6.0 exposes a per-child display stream and a transcript reader for any
session. acode already has the right view — `/agent-hub` roster plus a focused
child transcript — but that view is a snapshot: it refreshes only on
`SubagentProgress` and `SubagentFinished`, which are response boundaries. The
missing piece is a live child event path into the TUI and a small live
projection for the focused child.

The adoption is feasible without changing the SDK contracts or adding an
app-side shim. The work is five things:

1. Bump `go.mod`/`go.sum` to aharness v0.6.0.
2. Set `engine.RunSpec.ObserveChild` for TUI runs and route each child's
   events to the TUI (batch stays unobserved; see D1).
3. Carry child identity app-side, because `agent.TextDelta` and
   `agent.ReasoningDelta` carry no session and `agent.DisplayEvent` cannot be
   implemented outside the SDK (D2).
4. Project live child events into the existing `agent.HistoryItem` renderer,
   converging on the canonical `SubagentHistory` snapshot at response
   boundaries (D4).
5. Update the child-view tests and the reference docs; keep `-ui-json`
   unchanged (D6).

The one behavioral trade is not an implementation detail: registering
`ObserveChild` makes every delegated child stream, and a transport failure
after the first assistant-text delta becomes terminal instead of retried. D1
recommends accepting that for TUI runs only and leaving batch runs exactly as
they are today.

## 2. What v0.6.0 provides

The fields below are the public surface acode can use. The full contract lives
in the aharness CHANGELOG and `delegate.go`; this table only names the parts
that affect the host.

| Facility | Contract | acode relevance |
|---|---|---|
| `DelegationConfig.ObserveChild func(ChildTarget) func(Event) error` | One registration per child that starts a run, before its first event; the returned sink receives that child's own text, thinking, tools, notices, run start/finish, stamped with `EventMeta.Session`; parent-level `Subagent*` events are not delivered. | The facade path, not used by acode directly. |
| `agent.SpawnerOptions.Observe func(ChildObservation) ChildSink` | The component equivalent. The sink receives the child's sanitized `agent.DisplayEvent`s in that child's order, including `ModelStarted`; distinct children's sinks and the parent display may run concurrently. | The path acode's `engine.RunSpec` reaches. |
| `engine.Spec.ObserveChild` / `engine.RunSpec.ObserveChild` | Plumbing for the spawner hook; `RunSpec` wins when both are set. | Set it in `tuiHost.Start` only. |
| `Transcript(ctx, stateDir, sessionID)` | Lock-free, torn-line-tolerant display transcript for a root, child, or fork; `ErrSessionNotFound` / `ErrInvalidSessionID`. | Not needed by the current child view (D7). |
| `Agent.Transcript()` | The same read for the live root; `ErrClosed` after `Close`. | Not needed for this change. |
| `TranscriptItem` / `TranscriptKind` | Facade copy of `agent.HistoryItem`. | Not needed; acode already imports the component. |

Two SDK rules drive the app design:

- **Observation changes streaming.** `Spawner.childConfig` and
  `continuedConfig` set `Stream: s.opts.Observe != nil`. An observed child uses
  `CompleteStream` when the adapter supports it and emits one buffered
  `TextDelta` otherwise. Unobserved children keep `Stream: false`, the full
  retry budget, and the counter-only `Subagent*` stream. The trade is the root
  rule: once partial text has been shown, a transport failure is terminal.
- **Sinks are synchronous and per child.** One sink is never entered
  concurrently with itself; distinct sinks and the parent display may run
  concurrently. A blocking sink throttles only that child. A sink error or
  panic terminates that child as `internal_error`; siblings and the run are
  untouched. A pre-start failure registers no sink.

## 3. What acode has today

| Piece | Current behavior | Consequence |
|---|---|---|
| `cmd/acode/tui.go:730` `tuiHost.Start` | Builds an `engine.RunSpec` and calls `BuildRunnerWithSpawner`; no `ObserveChild`. | No child events reach the TUI; only the parent display does. |
| `cmd/acode/run.go:328` batch | `BuildRunner(engine.RunSpec{Stream: cfg.Stream}, display, nil, nil)`; no observation. | Batch children keep the existing retry budget and no live view. |
| `internal/tui/app.go:119` `Host` / `Start` | The only display callback is `func(agent.DisplayEvent) error`; `startRun` (`:1067`) wraps it in a 256-slot channel and `waitDisplayCmd` reads it. | The channel element has no child identity, and `agent.DisplayEvent` has an unexported marker, so a child event cannot be wrapped into it. |
| `internal/tui/app.go:293` `SubagentFocus` | Optional host capability with `SteerSubagent` and `SubagentHistory`. | The child view is a snapshot provider, not a live stream. |
| `internal/tui/child_view.go:33` `childFocus` | Holds `[]agent.HistoryItem`; `refreshChildFocus` (`:282`) replaces the slice from `SubagentHistory` on `SubagentProgress` and `SubagentFinished` (`app.go:1401-1412`). | Between response boundaries nothing new can appear; child text is one model call behind. |
| `cmd/acode/tui.go:385` `SubagentHistory` | Delegates to `spawner.ChildHistory`; the last spawner is kept after the run. | Remains correct and lock-free; v0.6.0 does not change it. |
| `internal/tui/child_view.go:111` `renderChildHistory` | Renders `agent.HistoryItem` run headers, tasks, assistant markdown, tool calls with capped output, and notices. | The live projection can reuse the renderer unchanged if it produces the same items. |

The reference docs state the snapshot behavior explicitly:
[tui.md](../tui.md) says `SubagentProgress` and `SubagentFinished` refresh the
view and that it never polls a timer; [architecture.md](../architecture.md)
says `agent.Config.Display` is the single runner-to-UI path. Both sentences
become false with live observation and must be updated in the same change.

## 4. What has to change

| Area | File | Change |
|---|---|---|
| Dependency | `go.mod`, `go.sum` | Pin `github.com/eddiectc/aharness v0.6.0`; `go mod tidy`; no other module changes (the v0.6.0 `go.mod` is unchanged). |
| Engine wiring | `cmd/acode/tui.go` | Set `ObserveChild` in the `engine.RunSpec` built by `tuiHost.Start`. The hook receives `agent.ChildObservation` and returns an `agent.ChildSink` that forwards `ChildEvent`s to the TUI. Guard the sink with the run context so cancellation returns nil, never an error. |
| TUI run path | `internal/tui/app.go` | Add `ChildEvent` and an optional `ChildObserver` host capability; extend `startRun` to a `displayMsg` envelope carrying an optional child observation; change `displayEventMsg`, `handleDisplay`, `nextDisplayCmd`, and `handleRunDone`/`completeRun` to the envelope; dispatch child messages to the focused child. |
| TUI run state | `internal/tui/model.go` | Change `liveRun.events` from `chan agent.DisplayEvent` to `chan displayMsg`. |
| Live projection | `internal/tui/child_view.go` | Add the per-child live state, event-to-`agent.HistoryItem` updates, `baseTurn` de-duplication, and coalesced redraws. Keep `refreshChildFocus` as the canonical replacement at progress/finish, and update its “never on a timer” comment to the coalesced rule. |
| Fake hosts | `internal/tui/app_test.go`, `headless_test.go`, `wait_test.go`, `fold_test.go` | `manualHost` implements `StartObserved` and records the child callback; adapt `deliver` and the direct `run.events` write to the envelope. `fakeHost`, `rosterHost`, `blockingHost`, and `reasoningHost` stay `Start`-only to pin the snapshot fallback and the existing wait/drain behavior. |
| Integration tests | `cmd/acode/tui_integration_test.go` | A fake provider that streams delegated child text: the focused view shows deltas before `SubagentFinished`; the parent transcript gains nothing. |
| Docs | `README.md`, `docs/tui.md`, `docs/commands.md`, `docs/architecture.md`, `docs/limits.md`, `docs/testing-strategy.md`, `docs/README.md` | Describe the live child view, the TUI-only streaming trade, the sink backpressure rule, the tests, and this plan's index row. |

No change is needed in `internal/tui/headless.go`, `protocol.go`, or the
`-ui-json` semantic events: the focused view already renders in frame lines,
and live child updates add no protocol event or version bump. The roster in
`internal/tui/agents_overlay.go` is unchanged: `SubagentProgress` and
`SubagentFinished` remain its inputs, and the parent-finish refresh in
`applyResult` stays as the last snapshot for a focused child.

## 5. Decisions

### D1 — Observe TUI runs only; batch stays unobserved

`tuiHost.Start` sets `engine.RunSpec.ObserveChild`; `cmd/acode/run.go` keeps
`BuildRunner(engine.RunSpec{Stream: cfg.Stream}, ...)` with no hook.

Why: registering an observer forces all delegated children to stream and
removes the retry after partial text. Batch has no consumer for the deltas and
an unattended run should keep the full retry budget. The TUI is interactive;
the live view is the reason to pay the trade. Batch is already non-delegating
for an independent reason — its recorded `root_tools` omits `delegate` — so this
decision neither depends on nor changes that rule; a TUI resume of a
batch-created session still cannot delegate, with or without `ObserveChild`.

Consequence: TUI child logs record `run_start.stream = true` and
`model_response.streamed` when the adapter supports streaming; batch child logs
keep the current values. This is an intentional, documented difference.

### D2 — Carry child identity in an app-side envelope

`agent.DisplayEvent` is a sealed interface (`isDisplayEvent` is unexported), and
`agent.TextDelta`/`agent.ReasoningDelta` carry no session, call ID, or agent.
Forwarding those events through the existing parent display would append child
text to the parent transcript. The SDK already made identity structural by
registering one sink per child; acode must keep that structure across its host
boundary.

Add in `internal/tui`:

```go
// ChildEvent is one delegated child's display event, stamped with the child
// identity the SDK's delta events do not carry.
type ChildEvent struct {
    Child agent.ChildObservation
    Event agent.DisplayEvent
}

// ChildObserver is an optional Host capability for live delegated-child
// events. A host that omits it keeps the snapshot-only child view.
type ChildObserver interface {
    StartObserved(sub Submission, opts RunOptions, steer <-chan agent.SteeringLine,
        display func(agent.DisplayEvent) error,
        child func(ChildEvent) error) (Run, error)
}
```

`tuiHost` implements `StartObserved` by setting `RunSpec.ObserveChild`; `Start`
delegates to it with a nil child callback and still satisfies `tui.Host`. The
assertion site is `startRun`: it calls `StartObserved` only when
`m.host.(ChildObserver)` succeeds and the callback it passes is non-nil;
otherwise it calls `Start` and the run is unobserved. A host may implement
`StartObserved` with a nil `child` callback (the run is then unobserved); a nil
`display` callback remains the existing “no parent display” case. The
alternative — changing `Host.Start`'s signature — is smaller in line count but
breaks every fake and makes the optional capability look mandatory; the
optional interface matches the existing `SubagentFocus`, `SettingsProvider`, and
`SessionManager` pattern.

Using `agent.ChildObservation` at the TUI boundary is acceptable: `internal/tui`
already imports `agent` for `DisplayEvent`, `HistoryItem`, and `SteeringLine`,
and this change stays at the component boundary acode already owns. A
facade-only host would use `aharness.ChildTarget` instead.

### D3 — One display channel, one envelope, child identity carried per message

`startRun` changes `chan agent.DisplayEvent` to `chan displayMsg`:

```go
type displayMsg struct {
    ev    agent.DisplayEvent
    child *agent.ChildObservation // nil for the parent stream
}
```

The parent display enqueues `displayMsg{ev: ev}`; the observe hook in
`tuiHost.Start` returns a sink that enqueues `displayMsg{ev: ev, child: &obs}`.
`waitDisplayCmd` and `drainDisplayCmd` read the envelope; `handleDisplay`
dispatches a child message to `handleChildDisplay` and a parent message to the
existing switch.

The enqueue helper selects on the 256-slot channel, the TUI's run `stop`
channel, and the host's run context. A full channel applies backpressure to that
producer only; a stopping run drops the message and returns nil. The existing
parent display already has the `stop` arm (`app.go:1070-1077`); the child
callback needs the same arm, and the host's `ObserveChild` sink also returns nil
on `runCtx.Done()`.

Lifetime is bounded by construction: the spawner forwards a child's events on
that child's own goroutine, and the runner joins the child goroutines before
`Run` returns. So after the parent `RunFinished`/`Done`, no child sink is still
in flight; `waitDisplayCmd`/`drainDisplayCmd` keep their current “empty buffer
after the runner returns” rule. A blocked send can only delay the run, not
outlive it; the App keeps reading until `completeRun`, and the host context
guard releases a sink on cancellation.

Per-child order is preserved and the snapshot boundary is causally safe:
`childDisplay.display` calls the child sink first and then, on the same child
goroutine, emits `SubagentProgress` through the parent display. A focused
child's `ResponseCompleted` is therefore enqueued before that child's progress,
and the child cannot emit the next `ModelStarted` until both return.
Cross-child interleaving is arrival order, which is all the view claims.

The sink always returns nil. A display bug must not become a child
`internal_error`; rendering happens on the App goroutine, not inside the sink,
so a render panic cannot propagate through the sink. The SDK's sink-error path
is intentionally unused.

### D4 — Live projection into `agent.HistoryItem`, canonical refresh at boundaries

Reuse `agent.HistoryItem` as the live item type so `renderChildHistory`, its
markdown mode, and its tool caps stay unchanged. `childFocus` gains live state:

- `run` — the current child run number. `setItems` derives it from the highest
  `HistoryRun.Run` in the snapshot; `RunStarted` appends only when the last run
  header is absent or terminal, so a queued start event for a run the snapshot
  already shows cannot duplicate the header.
- `turn` — the current turn, updated from `ModelStarted` and
  `ResponseCompleted`.
- `baseTurn` — the highest `Turn` present in the last snapshot. A delta whose
  turn is at or below `baseTurn` is already in the snapshot and is ignored;
  this is the de-duplication boundary when focus opens mid-turn.
- `assistant` — the index of the `HistoryAssistant` item for the in-flight
  response, or -1 when the current turn has no assistant text yet.
- `pendingTools` — `map[callID]int` for the live tool items, so a completion
  pairs with its start without scanning and a call ID from an older turn cannot
  be confused with the current one.

Event mapping:

| Child event | Live effect |
|---|---|
| `RunStarted` | If the last run header is terminal or absent, append `HistoryRun` with the next run number, model, dialect, and no terminal status; otherwise ignore it as already snapshotted. |
| `ModelStarted` | Set the current turn; no visible item. |
| `TextDelta` | If `turn <= baseTurn`, ignore; otherwise create the assistant item for this turn on first text and append to it, sanitized and capped by `maxTranscriptItemText`. |
| `ResponseCompleted` | Set the assistant text to `ev.Text` when the turn has an assistant item; if the item was missed and `ev.Text` is non-empty and the turn is beyond `baseTurn`, append it; a tool-only turn with empty text appends no assistant item. Update the run header's turns and usage from the cumulative fields. |
| `ToolStarted` | Append a pending `HistoryTool` with the call ID, name, arguments, and turn; record its index in `pendingTools`. |
| `ToolCompleted` | Update the item at `pendingTools[callID]` and delete the entry; if the start was missed, append a completed item. |
| `MessageRecorded` | Append a `HistoryUser` item with its source so a delegation steer keeps its label, and drop the matching pending steer by ID. This is the only live append for a user message; the parent-stream `SubagentMessageRecorded` path keeps its current ack-only role (`app.go:1413-1418`), so the two cannot double-append. |
| `Notice` | Append `HistoryNotice` with its kind. |
| `RunFinished` | Set the run header's status, reason, turns, usage, and duration; mark the focus read-only and clear pending steers. `setItems` applies the same terminal rule on a later snapshot. |
| `ReasoningDelta` | Ignored initially. The focused child view has never rendered thinking; adding it is a separate presentation decision (Q2). |

`SubagentProgress` and `SubagentFinished` keep calling `refreshChildFocus`.
That snapshot is canonical and replaces the live list, which removes any stale
or duplicated item and resets `baseTurn`, `assistant`, and `pendingTools`. This
ordering is safe per child: `childDisplay.display` calls the sink and then the
parent progress forwarding on the same goroutine (D3), so the snapshot for turn
N cannot overtake turn N's `ResponseCompleted`, and the next turn's deltas
cannot be produced until that display call returns.

### D5 — Coalesce live redraws

`TextDelta` can arrive per token. Reuse the existing `coalesceScheduled` /
`coalesceDelay` (33 ms) mechanism and add a `childDirty` flag: a child event
marks the focus dirty and schedules one tick; `coalesceMsg` calls the existing
`ensureContent` for the parent and a new `rebuildChildFocusIfDirty` for the
focused child, so one burst produces at most one rebuild of each visible
viewport. This is the same shape the parent transcript already uses for
streamed text, and it is not a polling timer. Scrolling and `follow` stay with
`childFocus.view`/`childFocus.follow`; the tick only re-renders, and
`rebuildChildFocus` keeps its current “goto bottom when following” rule.

The live list is bounded between snapshots by one response's deltas. Assistant
text is capped per item by `maxTranscriptItemText` (64 KiB), matching the SDK
`History` item cap; the full child history is additionally bounded by the SDK
`History` caps (20,000 items / 16 MiB), which are the same numbers as the TUI's
own display caps. Tool bodies keep the existing `childToolBodyRows` /
`childToolBodyBytes` cap. Re-rendering the whole child list on each coalesced
tick is O(items); that is acceptable for the first cut because a focused child
is normally a short transcript, and the snapshot refresh already does the same
work at response boundaries. If it becomes visible, add per-item line caching
like `transcriptItem`; do not add a second transcript model.

### D6 — No new protocol, config key, or batch behavior

The focused view is not an overlay; `-ui-json` already exposes it through frame
lines and reports `overlay: "none"`. Live child updates change only the
rendered frame, not the semantic event stream. No new `ui_*` setting: the sink
is cheap, the TUI is the only consumer, and a knob would still need a default.
No new command; `/agent-hub` and `Enter` are the existing entry points.

### D7 — Keep `SubagentHistory` / `ChildHistory`; do not switch to facade `Transcript`

v0.6.0's `Transcript` is the right API for a facade-only host. acode's host is
built on `engine`, already imports `agent`, and uses
`(*agent.Spawner).ChildHistory`, which the release leaves unchanged. Switching
would add a state-directory path read and lose the spawner's parentage check
for no visible gain. `Agent.Transcript` is a root-session convenience and is
not needed by the TUI, whose parent history already comes from
`engine.Host.History`. The lock-free read is not a new pattern either:
`tuiHost.readState` (`cmd/acode/tui.go:568`) already takes a
`session.ReadSnapshot` for `/prompt` and `/request`. The reason to stay is the
parentage check and one fewer path, not a capability difference.

### D8 — Update the reference in the same change

The following current-reference sentences become false and must move with the
code:

- [tui.md](../tui.md) child focus: replace "`SubagentProgress` and
  `SubagentFinished` refresh the view; it never polls a timer" with the live
  delta path, the snapshot convergence, and the coalesced redraw rule.
- [commands.md](../commands.md) `/agent-hub`: state that the focused transcript
  streams while the child runs.
- [architecture.md](../architecture.md) §5: replace "`agent.Config.Display` is
  the single runner-to-UI path" with the two-path description: parent display
  plus per-child observation, both display-only, with display-only events.
- [limits.md](../limits.md): add the live child view's backpressure and
  coalescing to the TUI caps, or link the SDK's sink rule.
- [testing-strategy.md](../testing-strategy.md): add the live child view to the
  TUI and integration layers.
- [README.md](../README.md) delegated-subagents paragraph: say the focused
  child transcript updates while the child runs.
- [docs/README.md](../README.md): add this plan to the build-plan index.

## 6. Build phases

### Phase 0 — dependency

| Step | Detail |
|---|---|
| P0.1 | Run `go get github.com/eddiectc/aharness@v0.6.0` and `go mod tidy`. v0.6.0's `go.mod` is unchanged, so expect only the module and its two `go.sum` lines to move. |
| P0.2 | Run `make dod` with the release in place. The app does not set `ObserveChild` yet, so behavior is unchanged. |

The bump was checked on a scratch copy of this tree against the real v0.6.0
tag: `go build ./...` and `go test ./...` pass with only `go.mod`/`go.sum`
changed. The scratch check is not a substitute for the final `make dod`.

### Phase 1 — host and run path

| Step | Detail |
|---|---|
| P1.1 | Add `ChildEvent`, `ChildObserver`, and `displayMsg` in `internal/tui/app.go`. |
| P1.2 | Change `startRun` to the envelope channel; keep the parent display behavior identical. |
| P1.3 | Add `StartObserved` on `tuiHost`: set `engine.RunSpec.ObserveChild`, wrap the sink with the run-context guard, and translate `agent.ChildObservation` + `agent.DisplayEvent` into `ChildEvent`. `Start` delegates with a nil child callback. |
| P1.4 | Update `manualHost` to implement `StartObserved` and capture the child callback; leave `rosterHost` snapshot-only to keep the fallback covered. |

### Phase 2 — live child projection

| Step | Detail |
|---|---|
| P2.1 | Add the live state to `childFocus`; compute `baseTurn` and the next run number in `setItems`. |
| P2.2 | Implement `handleChildDisplay`: filter by focused session, map the events, mark dirty, schedule one coalesce tick, and call `ackPending` for a recorded steer. |
| P2.3 | Rebuild the child viewport in `coalesceMsg` when dirty; keep the direct rebuild on focus open and on snapshot refresh. |
| P2.4 | On `SubagentProgress` / `SubagentFinished`, keep the existing refresh; on a focused `SubagentFinished`, set status and read-only before refreshing. |

### Phase 3 — tests and reference

| Step | Detail |
|---|---|
| P3.1 | TUI unit tests: live text appears without a progress event; a snapshot refresh replaces the live item without duplication; tool start/complete pair; finish sets read-only; a second child's events do not touch the focused child; a sink after run stop returns nil. |
| P3.2 | Host integration: a fake streaming child provider; the focused view contains deltas before `SubagentFinished`; the parent transcript gains no child items; `-ui-json` still reports `overlay: "none"` and no new event kind. |
| P3.3 | Race coverage: two concurrent children with live sinks and a focused view under `-race`; the SDK already serializes each sink per child and runs children concurrently. |
| P3.4 | Update §D8's reference docs and the plan index; run `make dod`. |

## 7. Tests that must exist before the plan is done

| Layer | Case | Assertion |
|---|---|---|
| TUI unit | Live text before progress | A `TextDelta` for the focused child renders without a `SubagentProgress`; the parent transcript is unchanged. |
| TUI unit | Snapshot convergence | A `SubagentProgress`/`SubagentFinished` snapshot replaces the live item; the same text appears once. |
| TUI unit | Mid-turn focus | Focus opens after a completed turn; queued deltas for that turn are ignored; deltas for the next turn append. |
| TUI unit | Tool pairing | `ToolStarted` then `ToolCompleted` updates one item; a completion without a start appends a completed item. |
| TUI unit | Steer ack | `MessageRecorded` with `SourceSubagentSteer` drops the pending line and renders the recorded source. |
| TUI unit | Isolation | Two children stream concurrently; only the focused child's view changes; `-race` clean. |
| TUI unit | Shutdown | A sink invoked after the run stops returns nil and does not fail the child. |
| Host integration | Fake streaming child | The child view receives incremental deltas before `SubagentFinished`; the child log records `streamed: true`. |
| Host integration | Batch unchanged | A batch run's child log keeps `run_start.stream = false` and the full retry path. |
| Regression | Existing child focus/roster tests | The snapshot-only fake host still passes, proving the optional-capability fallback. |

## 8. Risks and open questions

| Risk | Mitigation |
|---|---|
| Duplicate text at the snapshot/live boundary. | `baseTurn` ignores deltas already present in the snapshot; every response boundary replaces the live list with the canonical `History` projection. |
| A blocking sink throttles a child. | The sink is synchronous by SDK contract. The 256-slot channel plus 33 ms coalescing bounds the UI's consumption; a full channel applies backpressure to that child only. Documented in the reference. |
| A sink error turns a UI bug into a child `internal_error`. | The sink returns nil on cancel and never propagates a rendering error; only an unrecoverable enqueue failure would be returned, and there is none in the design. |
| Observation removes the post-delta retry for TUI children. | D1 limits it to TUI runs; the user-visible trade is documented at the field and in the reference. Batch is untouched. |
| Whole-list re-render per coalesced tick. | Accept for the first cut with the SDK caps; add per-item caching only if it is measurable. |
| Live ordering across children looks arbitrary. | The SDK promises per-child order only. The TUI shows one child at a time, so no cross-child merge is claimed. |

**Q1 — always-on in the TUI, or a setting?** The recommendation is always-on.
The sink only enqueues when the TUI reads, the live view is the feature, and a
setting would add the full flag/env/file/settings surface for a default that
would still be on. Revisit if the retry trade proves costly in real runs.

**Q2 — render child thinking?** Not in this change. The focused view has never
shown `ReasoningDelta`, the log never stores it, and adding a thinking block is
a presentation change independent of the transport. The events are available
if a later decision wants them.

**Q3 — surface child deltas in `-ui-json`?** Not now. The view is already in
frame lines; a semantic event would need a protocol version and a client
contract for per-child ordering, which is exactly the host-side merge the SDK
does not promise.

## 9. Verification

Automated gates for the implementation: `make dod` (coding-standard §8 gates
plus `docs-check`), with the live-view cases under `-race`.

Targeted commands once the code exists:

```sh
go test ./internal/tui -run 'Child|AgentHub' -race -count=1
go test ./cmd/acode -run 'Subagent|TUI' -race -count=1
go test ./... -race -count=1
```

Manual, and required before claiming parity: a real-provider TUI run with a
delegated child that streams visible text into the focused view before
`SubagentFinished`. This needs a provider token and is pending in this
environment; the fake-provider tests cover the code path, not the wire.

**Not verified:** the manual real-provider live-child smoke above is pending.
The §7 batch row is unreachable as written: batch loads no agent definitions, so
a batch run cannot start a child at all (`delegate` reports `unavailable`); the
batch path registers no observer by construction, which is the property that
matters.

## 10. Evidence index

| Claim | Source |
|---|---|
| v0.6.0 adds the per-child sink and transcript reader, additive only. | aharness v0.6.0 CHANGELOG; PR #8 (`Add subagent observation and session transcripts`). |
| Registered observers force child streaming and remove the post-delta retry. | aharness `agent/spawn.go`, `childConfig` / `continuedConfig`; v0.6.0 CHANGELOG "Changed". |
| The engine hook is `RunSpec.ObserveChild`, with `RunSpec` winning over `Spec`. | aharness `engine/engine.go`, `RunSpec`; `engine/runner.go`, `BuildRunnerWithSpawner`. |
| `DisplayEvent` is sealed and deltas carry no identity. | aharness `agent/display.go`, `DisplayEvent`, `TextDelta`, `ReasoningDelta`. |
| acode's child view refreshes only at response boundaries. | `internal/tui/app.go:1401-1412`; `internal/tui/child_view.go:282-302`. |
| acode's start path has only the parent display. | `cmd/acode/tui.go:730-803`; `internal/tui/app.go:118-125`. |
| Batch has no observation hook. | `cmd/acode/run.go:324-328`. |
| The child view already renders `agent.HistoryItem`. | `internal/tui/child_view.go:111-143`; `agent/history.go`, `HistoryItem`. |
| The reference claims a single display path and snapshot refreshes. | `docs/architecture.md` §5; `docs/tui.md` §"Child focus and delegation steering". |
| The optional-capability pattern already exists in the TUI host. | `internal/tui/app.go`, `SubagentFocus`, `SettingsProvider`, `SessionManager`. |
| Session schema, prompt, toolset, and event types are unchanged. | aharness v0.6.0 CHANGELOG first paragraph; `ViewEvent` diff has no event-type change. |
