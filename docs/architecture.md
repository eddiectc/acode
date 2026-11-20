# acode architecture

- **Date:** 2026-11-18
- **Status:** normative for the acode coding-agent application
- **Related:** [configuration](configuration.md), [TUI](tui.md),
  [commands](commands.md), [limits](limits.md),
  [testing strategy](testing-strategy.md), and the
  [aharness SDK architecture](https://github.com/eddiectc/aharness/blob/main/docs/architecture.md)
- **Agent entry point:** [AGENTS.md](../AGENTS.md)

acode is the first-party terminal coding agent over the aharness SDK. This
document defines the application structure: CLI and TUI composition, modes,
configuration layers, session lifecycle, host contracts, and exit codes. The
loop, model adapters, built-in tools, session schema, and public SDK contracts
are defined in the aharness repository and are not duplicated here.

## 1. Role and boundaries

acode depends on the aharness module and imports its component packages:

| SDK package | acode uses it for |
|---|---|
| `aharness/agent` | Runner construction, display events, history, state, agent definition discovery, spawner, and stop reasons. |
| `aharness/engine` | New/resume/fork session composition, workspace and work-directory setup, lock-free inspection, and `BuildRunner`. |
| `aharness/model` | Canonical model types, Chat and Responses adapters, catalog discovery, and pricing. |
| `aharness/session` | The durable JSONL store, locks, replay, and session IDs. |
| `aharness/tools` | The registry, workspace root, built-in tools, web configuration, and todo/delegation state. |

The dependency direction is one-way: acode imports aharness, never the reverse.
acode owns flags, environment and config-file parsing, mode selection, signals,
terminal I/O, presentation, exit codes, and all user-facing behavior. There is
no loop, projection, tool implementation, provider wire format, or session
schema in this repository. A change to those belongs in aharness and is then
picked up through the module dependency.

Two pieces of prompt surface are product content and live here, because the SDK
ships them for no host: the five role definitions in `cmd/acode/agents/` are
the base that `agent.LoadAgents` seeds and the operator's
`<state-dir>/agents/*.md` files override by name, and the identity line names
`acode` for parents and delegated children alike. The prompt body itself — the
loop invariants, coding policy, coordination guidance, and the agent catalog —
is the SDK's default (`prompt-v3`) rendered per session, so no prompt text is
duplicated in this repository.

The module is `github.com/eddiectc/acode`. It depends on the tagged aharness
release pinned in `go.mod`.

### Workspace instructions

On a **new** session only, acode reads the workspace's root `AGENTS.md` through
the SDK workspace handle (`tools.Workspace.ReadDisplayFile`), which applies the
same path policy as the file tools: a workspace-relative path, no symlink
components, no `.git` component, and a regular file. The read is capped at 16
KiB. The bytes are invalid-UTF-8 sanitized and passed to the SDK's prompt input:
both batch and the TUI set `engine.Spec.AppendPrompt`, and the engine folds the
text after the rendered prompt and records it.

Both fields are recorded once in `session_start`: `system_prompt` is the exact
prompt the model saw, and `appended_prompt` is the workspace text that was
composed into it. `appended_prompt` is provenance, never a replay key: SDK
projection keeps using `system_prompt`, and the recorded value is not
necessarily a suffix after the SDK's prompt cap. A fresh delegated child
composes the parent's `appended_prompt` after its own rules and role body into
its recorded `system_prompt`, and the child records no `appended_prompt`; a
continued child appends nothing.

Resume and fork never re-read the file. They replay the recorded
`system_prompt` and carry the recorded `appended_prompt` to their own children.
A log written before the field has none to carry, so its new children get no
seed; the SDK's v3 read rule remains their fallback, and `/new` is the way to
mint a fresh contract. A missing or empty file changes nothing. A larger file,
a symlink, a directory, or an unreadable file is skipped with one warning on
stderr in batch or one TUI notice; the session still starts. The text is
workspace content, not executable configuration: acode does not parse it or
run anything from it, and a cloned repository can therefore inject
instructions into the model's context.

## 2. Package layout

```text
go.mod
README.md
AGENTS.md
Makefile
Dockerfile             # container image; see docs/container.md
.dockerignore
.github/workflows/
  ci.yml               # make dod: the coding standard's §8 gates
  release.yml          # tarballs on a tag; ghcr.io image: latest, semver
cmd/acode/
  main.go              # os.Exit(run())
  run.go               # mode selection, batch engine wiring, exit codes
  engine.go            # engine spec inputs, per-run selection, completer factory
  config.go            # flags, environment, validation, discovery resolution
  fileconfig.go        # optional JSON config file: schema, load, layered resolution
  config_settings.go   # /settings read/write model
  config_settings_write.go # atomic config-file editing
  control.go           # operation controller and batch signal delivery
  agents.go            # acode identity and the five embedded role definitions
  agents/*.md          # the shipped roles; operator files override them by name
  workspace_instructions.go # root AGENTS.md reader for a new session
  models.go            # catalog wiring and list/refresh actions
  sessions.go          # session switching, TUI open policy, and metadata helpers
  tui.go               # TUI host over engine.Host, -ui-json wiring, summary
  textsink.go          # streamed stdout newline handling
  version.go           # build metadata
internal/tui/
  app.go               # tea.Model, Host/Run contracts, modes, keys, lifecycle
  commands.go          # slash-command parser, completion, dispatch, help
  model.go             # transcript, composer, status, steering, caps
  view.go              # Lip Gloss layout, transcript/tool/notice views
  todo.go              # persistent todo widget and restore decode
  markdown.go          # Glamour rendering, raw/rich toggle, table extraction
  widgets.go           # table/file/model/effort/session overlays and focus
  agents_overlay.go    # /agent-hub and /agents browsers
  settings_overlay.go  # /settings config-file browser and validation
  child_view.go        # focused child transcript and delegation steering
  image.go             # workspace image resolver, ntcharts picture, viewer
  protocol.go          # -ui-json v1 commands, events, frames, snapshots
  headless.go          # Bubble Tea fake input/output + x/vt driver
  screen.go            # x/vt cell/style/cursor projection and SVG snapshot
  sanitize.go          # terminal-safe text sanitizer
  terminal.go          # terminal capability and input-mode helpers
scripts/
  acode-bwrap          # optional bubblewrap sandbox launcher
docs/
```

`cmd/acode` is wiring and I/O: parse, validate, choose a mode, open a session
through `engine.Open`, start a run through the host, render the result, and map
failures to exit codes. It does not implement the loop or tools. `internal/tui`
is a display-only front end over the host's engine-built runner, registry, and
store; it adds no durable event type and no schema version.

## 3. Composition

### Batch

`cmd/acode/run.go` opens the session through `engine.Open` in `ModeNew`,
`ModeResume`, or `ModeFork`, then calls `Host.BuildRunner` with a display
callback that writes streamed text through `textsink` and tool/notice activity
through the batch renderer. The runner and the session store are the same ones
the TUI uses. A new or forked batch session records the full toolset as its
session grant and narrows only the root through `root_tools`: every tool of the
recorded version except `delegate`. The recorded root grant is inherited by
resume, so a session first created by batch is permanently non-delegating, in
batch or in the TUI. A resumed session keeps its recorded contract, and batch
configures no spawner, so `delegate` can only return `unavailable` even when
the recorded grant offers it.

### TUI host

`tuiHost` in `cmd/acode/tui.go` implements the consumer-side `tui.Host`,
`tui.SessionManager`, and the optional capability interfaces over one
`engine.Host`. The engine host owns the durable session store and lock, the
workspace handle, the per-session work directory, and the resolved contract:
prompt, appended prompt, prompt and toolset versions, agent, and grants.
`tuiHost` owns only app policy and display state: the config, the loaded
catalog and source, the shipped and operator agent definitions and overrides,
and the session header.

Opening a session follows the batch path. `engine.Inspect` reads the recorded
state without taking the lock, app policy runs (`applySessionDefaults`,
`validateSessionRules`, the custom-tool refusal), and `engine.Open` validates
and records the contract. A new TUI session reads the workspace-root
`AGENTS.md` and passes it as `Spec.AppendPrompt`; the engine renders the prompt,
folds and caps the text, records `system_prompt` and `appended_prompt`, and
propagates the text to fresh children. Resume and fork replay the recorded
contract; a fork records `contract_override` when the engine finds the prompt
or tools differ. `Spec.Identity` is `acode`, `Spec.PromptTag` stays unset under
tier 1, and `Spec.UserAgent` is the acode user agent.

`Host.Start` builds each run with `Host.BuildRunnerWithSpawner` and an
`engine.RunSpec` carrying the per-run model choice: model, dialect, base URL,
limits, pricing, and thinking effort. That choice lives in `runSelection`; the
model picker and a next-run effort change it and the header context limit, never
the session contract. The engine builds the registry, spawner, root grant, todo
seed, and work directories for the run. A session first created by batch records
a full session grant and a root grant without `delegate`, so a TUI resume of it
cannot delegate; a TUI-created session records no root grant and stays
delegating.

While a run is active, the composer becomes a steering input; `Esc` interrupts
the in-flight model or tool operation and a second `Ctrl-C` cancels the run. The
engine recreates the work tree at each run start and removes it when the run
ends; `Host.Close` removes it at process end. Idle `/new` and `/resume` open the
new `engine.Host` and read its history and counters before closing the old one,
so a failed switch leaves the active session intact. A startup `-fork` is
resolved through the same engine path and creates a self-contained copy.

### Headless `-ui-json`

`-ui-json` drives the same Bubble Tea program with a fake input/output pair and
an `x/vt` screen. It exposes a versioned JSON command/event protocol, visual
frames, and SVG snapshots for integration tests and automation. It adds no
durable behavior; the session log remains the only durable record.

## 4. Modes and selection

Terminal actions run before session creation: `-version`, then
`-refresh-models`, then `-list-models`. They require no task, session, or
provider token.

Mode selection then runs in this order:

1. explicit `-ui-json`;
2. explicit `-batch`;
3. TUI when stdin and stdout are both terminals and `TERM` is set and not
   `dumb`;
4. batch otherwise.

`-batch` and `-ui-json` are mutually exclusive. TUI and `-ui-json` create a new
session eagerly; the task is optional and the first submitted message supplies
it. Batch new/fork requires exactly one task source. Batch resume allows at
most one task source and requires a task when the last run ended `completed`.
`-resume` and `-fork` are mutually exclusive.

## 5. Host contracts

The TUI depends on the SDK, never the reverse. Its `Host` capabilities are
split by feature so the app can omit one:

| Capability | Purpose |
|---|---|
| `Host` | `Info`, `Models`, `History`, `Start`, `Close`. |
| `ModelRefresher` | Explicit catalog refresh (`Ctrl-R`). |
| `ModelSelector` | Next-run model selection. |
| `SessionManager` | Idle `/new` and `/resume` plus the resume picker. |
| `WorkspaceProvider` | File picker and workspace images. |
| `TodoProvider` | Restore the todo widget from the recorded result. |
| Agent browser | Definition overrides and enable/disable state. |
| Child view | Focused child history, steering, and status. |

`agent.Config.Display` is the single runner-to-UI path. Events are display-only
and carry no durable truth. `agent.History` reconstructs the transcript from the
log; the TUI uses it for the active session and for a focused child log read
without taking the child's lock.

## 6. Sessions and state

The default state directory is `~/.acode`, selectable with `-state-dir` or
`ACODE_STATE_DIR`. The layout is inherited from the SDK:

```text
<state>/sessions/<session-id>.jsonl   # durable v2 log
<state>/cache/models.dev/<provider>.json
<state>/config.json                   # optional, unless -config changes it
<state>/work/<session-id>/            # per-run temporary state and tool spill
```

One process owns one active session and one exclusive lock. Resume opens the
recorded log for append; fork takes a shared lock on the parent and creates a
new self-contained log. The TUI and batch share the same store semantics.

acode is a new project. It does not migrate a previous agent's state,
configuration, or environment variables; use `-state-dir` to point at an
existing located state directory when needed.

## 7. Exit codes

| Code | Meaning |
|---|---|
| 0 | Terminal run status `completed`, or a terminal action succeeded. |
| 1 | Setup/state failure before or outside a run (workspace, log, cleanup). |
| 2 | Configuration, usage, or validation error; no run started. |
| 3 | Provider or transport failure, or retries exhausted. |
| 4 | Protocol error, malformed provider/catalog response, or `stuck`. |
| 130 | Interrupted by a signal or an explicit cancel. |

The mapping lives in `cmd/acode`; the SDK returns typed `Result.Status` values
and does not choose an exit code.

## 8. Limits and dependencies

Caps and timeouts are split by owner. CLI, input, TUI, and `-ui-json` caps are
in [limits.md](limits.md). SDK-owned loop, transport, session, and tool caps
are in the [aharness limits reference](https://github.com/eddiectc/aharness/blob/main/docs/limits.md).

Runtime dependencies are the aharness SDK plus the approved TUI stack: Bubble
Tea v2, Bubbles v2, Lip Gloss v2, Glamour v2, ntcharts v2, `x/vt`, their direct
support packages, and `golang.org/x/term`. The TUI is the only
application-framework exception; see [coding-standard.md §3](coding-standard.md#3-dependencies).
