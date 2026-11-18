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
| `aharness/agent` | Runner construction, display events, history, state, agent definitions, spawner, and stop reasons. |
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

The module is `github.com/eddiectc/acode`. It depends on the tagged aharness
release pinned in `go.mod`.

## 2. Package layout

```text
go.mod
README.md
AGENTS.md
Makefile
cmd/acode/
  main.go              # os.Exit(run())
  run.go               # mode selection, batch engine wiring, exit codes
  config.go            # flags, environment, validation, discovery resolution
  fileconfig.go        # optional JSON config file: schema, load, layered resolution
  config_settings.go   # /settings read/write model
  config_settings_write.go # atomic config-file editing
  control.go           # operation controller and batch signal delivery
  models.go            # catalog wiring and list/refresh actions
  sessions.go          # session selection and metadata helpers
  tui.go               # TUI host, session shell, -ui-json wiring, summary
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

`cmd/acode` is wiring and I/O: parse, validate, choose a mode, open a session,
start a run, render the result, and map failures to exit codes. It does not
implement the loop or tools. `internal/tui` is a display-only front end over the
same engine, registry, and store; it adds no durable event type and no schema
version.

## 3. Composition

### Batch

`cmd/acode/run.go` opens the session through `engine.Open` in `ModeNew`,
`ModeResume`, or `ModeFork`, then calls `Host.BuildRunner` with a display
callback that writes streamed text through `textsink` and tool/notice activity
through the batch renderer. The runner and the session store are the same ones
the TUI uses. Batch never grants `delegate`; the execution grant withholds it
without rewriting the recorded tool contract.

### TUI host

`tuiHost` in `cmd/acode/tui.go` implements the consumer-side `tui.Host` and
`tui.Run` interfaces over:

- `session.Store` for the active log and its exclusive lock;
- `tools.Workspace` for the approved workspace root;
- the resolved model adapter for the selected dialect;
- `agent.Runner` for one run at a time;
- `agent.Spawner` for delegated children;
- the catalog source for the model picker and refresh.

The TUI calls `Host.Start` for each submitted message. While a run is active,
the composer becomes a steering input; `Esc` interrupts the in-flight model or
tool operation and a second `Ctrl-C` cancels the run. Idle `/new` and `/resume`
replace the active session only after the target has been validated, so a
failure leaves the current session intact. `/fork` is resolved through the
engine and creates a self-contained copy.

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
