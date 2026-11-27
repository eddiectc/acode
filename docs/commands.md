# Slash commands (shipped)

- **Date:** 2026-11-27
- **Status:** implemented for acode. The reference for composer slash commands in
  the interactive TUI and the `-ui-json` headless driver.
- **Related:** [tui.md](tui.md) for the TUI surface and each overlay,
  [configuration.md](configuration.md) for the config file `/settings` and
  `/agents` write, [architecture.md](architecture.md) for the loop and CLI
  contracts, and [limits.md](limits.md) for caps.

Batch mode has no composer and therefore no slash commands. A command is
display/control input and appends no command event of its own: next-run commands
change the next `run_start`, session commands replace the active store, and
run-control commands act through the runner and produce the same durable events as
their key equivalents. The `F1` overlay lists every command with a one-line
summary; this document adds invocation, arguments, scope, and the `-ui-json`
mapping.

## Invocation

- A seeded `-task`/`-task-file` is always literal text, even when it starts with
  `/`.
- The first word after `/` must match a registered name or alias. Names are
  case-insensitive and arguments are case-sensitive; only recognized commands are
  intercepted, and any other text, including an unknown `/word`, is submitted as a
  normal message.
- Prefix a recognized command with a space to send it literally, for example as
  steering during a run.
- One optional argument is accepted, with surrounding whitespace trimmed. `/model`
  and `/effort` validate it; other commands ignore extra words. The submission cap
  is in [limits.md](limits.md).
- Commands work during a run, except next-run settings, editor-only actions, and
  session switching (`/model`, `/effort`, `/files`, `/refresh`, `/new`, `/resume`),
  which are disabled with a status-line notice.
- Feedback uses the status line or an overlay. `/model` and `/effort` emit
  display-only `model_changed` and `notice` events, `/new` and `/resume` emit a
  `notice` with kind `session`, and none is written to the session log.

## Autocomplete

Typing `/` opens a completion menu filtered by command name or alias: `Up`/`Down`
move the highlight (clamped, no wrap), `Tab` completes and appends a space after
`/model` and `/effort`, `Enter` runs the highlighted command,
`Shift-Enter`/`Alt-Enter` insert a composer newline, and `Esc` dismisses the menu
without clearing the composer. A space after the name starts argument mode and
closes the menu, and history recall does not reopen it until the text is edited. The
menu's row cap is in [limits.md](limits.md).

In `-ui-json`, autocomplete is ordinary input: send `text` to type and `key` `enter`,
`tab`, `up`, `down`, or `esc` to drive it, with no protocol change.

## Commands

| Command | Argument | Key | Scope |
|---|---|---|---|
| `/help` | — | `F1` | help overlay with every key and command |
| `/model [id]` | optional exact model ID | `Ctrl-P` | next-run model |
| `/effort [value]` | optional effort value or `default` | `F2` | next-run thinking effort |
| `/provider` | — | `Ctrl-X` | active provider plus configuration details |
| `/details` | — | `Ctrl-X` | session, terminal, and configuration details |
| `/prompt` | — | — | system prompt recorded for the session |
| `/request` | — | — | latest projected model request and response |
| `/new` | — | — | fresh session in this workspace (idle) |
| `/resume` | — | — | pick a previous session in this workspace (idle) |
| `/files`, `/file` | — | `Ctrl-O` | workspace file picker (idle) |
| `/table` | — | `Ctrl-T` | most recent markdown table |
| `/agent-hub` | — | — | live delegated-child roster and focus |
| `/agents` | — | — | agent definitions and routing overrides |
| `/settings` | — | — | config-file settings browser |
| `/image` | — | `Ctrl-G` | most recent image block |
| `/rich` / `/raw` | — | `Ctrl-R` toggles | rich or raw assistant rendering |
| `/repaint` | — | `Ctrl-L` | full terminal repaint |
| `/refresh` | — | `Ctrl-P` then `Ctrl-R` | catalog refresh (idle) |
| `/interrupt` | — | `Esc` during a run | interrupt the in-flight operation |
| `/cancel` | — | second `Ctrl-C` | cancel the active run |
| `/quit`, `/exit` | — | `Ctrl-D` | quit; cancels an active run and exits 130 |

## Session and configuration

### `/help`

Opens the help overlay, idle or running. `Esc` closes it.

### `/provider`

Reports the active provider in the status line and opens the details overlay, which
shows the provider and base URL under configuration. Informational only:
`-provider`/`ACODE_PROVIDER` selects `opencode-go` at process start, so there is no
in-session switch, and an explicit provider on resume or fork must match the recorded
one.

### `/details`

Opens the details overlay, idle or running; `Esc` closes it. The fields it shows are
listed in [tui.md § Status line and details](tui.md#status-line-and-details).

### `/prompt`

Opens the system prompt recorded in the active session's `session_start`: version,
recorded byte count, prompt tag, source, and body. The body is the recorded text,
wrapped rather than truncated. A log written before the prompt was recorded is
rebuilt from its prompt version and labelled `reconstructed`; an SDK truncation is
reported in the header. Display-only: no run starts and no event is written.

### `/request`

Opens the newest recorded `model_request` or `model_response` and its pair from the
same turn, pretty-printed in SDK field order with each section's displayed byte
length. `log_requests` decides whether the request half was recorded; with it off,
that section says so and names `-log-requests` and `/settings`. These are projected
payloads, not wire bytes: no headers, auth, or exact frames. Display-only; the
display cap is in [limits.md](limits.md).

### `/new`

Creates a fresh session log in the same state directory and workspace, replacing the
active session for the rest of the process and resetting the transcript, counters,
run numbering, and display caches. The current next-run model and effort are written
into the new `session_start`. It mints a fresh contract: current prompt and toolset
versions, no recorded agent or grant, and a fresh read of the workspace-root
`AGENTS.md`, folded and capped by `Spec.AppendPrompt` and recorded as
`system_prompt` and `appended_prompt`, with a warning notice when the file is
skipped. A session created this way stays delegating, unlike one first created by
batch. Idle only; it writes no command event and copies no history or todo state.

### `/resume`

Opens the picker described in [tui.md § Session picker](tui.md#session-picker). The
harness opens the log under `<state>/sessions` with an exclusive lock, validates it,
and loads history and counters before closing the current log, so a locked, corrupt,
or incompatible session leaves the active one untouched. The resumed session
inherits its recorded model, dialect, base URL, context and output limits, and
effort; explicit run budgets and timeouts from this invocation stay in effect,
matching `-resume`. Idle only.

### `/quit` and `/exit`

Idle: exits 0. Active: cancels the run, writes the terminal events, and exits 130.

## Next-run settings

`/model` and `/effort` change only the next run: never the active run, the existing
transcript, or historical log events. Both are disabled during a run with a notice.

### `/model [id]`

Without an argument, opens the picker described in
[tui.md § Model picker](tui.md#model-picker); with an ID, it selects that exact
catalog entry for the next run.

- The ID must be a usable catalog entry. Deprecated models stay selectable and are
  labeled; unavailable or unknown models are rejected with a notice.
- The next run takes the entry's dialect, base URL, context and input limits, and
  effort values. An explicit context limit is clamped to the new window, and an
  explicit output budget is sent unchanged.
- A next-run effort is kept when the new model advertises it, and cleared to the
  provider default with a warning otherwise.
- On resume without a catalog fetch, only the recorded model may be available until
  the picker's `Ctrl-R` refresh succeeds.

### `/effort [value]`

Without an argument, opens the picker described in
[tui.md § Thinking effort](tui.md#thinking-effort); with a value, it applies that
value to the next run.

- Accepted values are the effective model's advertised values, in catalog order.
  There is no harness-wide enum.
- `default` clears the setting, so the next request omits the field and uses the
  provider/model default.
- An unsupported value is rejected with a notice naming the model and its advertised
  values; a model with none reports that it advertises none.
- The next `run_start` records the effective value, and `-ui-json` also receives a
  display-only `notice`.

### `/refresh`

Opens the picker and performs the same unconditional refresh as `Ctrl-R` there and
as `-refresh-models`: no stale fallback, with the timeout and body caps in
[limits.md](limits.md). Idle only. The footer, in-flight, cancel-on-close, and
retained-entry behavior are in [tui.md § Model picker](tui.md#model-picker).

## Workspace and content

### `/files` (alias `/file`)

Opens the workspace file picker, equivalent to `Ctrl-O`, and inserts a
workspace-relative path into the composer. Idle only; the listing rules are in
[tui.md § File picker](tui.md#file-picker).

### `/table`

Opens the most recent markdown table from assistant text, equivalent to `Ctrl-T`. If
there is none, the status line reports it and no overlay opens. The overlay scrolls
horizontally under the display caps in [limits.md](limits.md).

### `/image`

Opens the viewer for the most recent workspace image block, equivalent to `Ctrl-G`.
Display-only: images are resolved with `os.Root` and `filepath.IsLocal`, never
fetched, and unsupported or oversized files become placeholders.
`ACODE_IMAGE_PROTOCOL` selects auto, Kitty, glyph, or none; `-ui-json` uses glyph.

## Display

### `/rich` and `/raw`

Set rich or raw assistant rendering; `Ctrl-R` toggles. Display-only: the transcript,
session log, and model input are unchanged, and the status line shows the mode.

### `/settings`

Opens the settings browser ([tui.md § Settings](tui.md#settings)). Rows, source
column, validation, masking, the atomic write, and when a saved value takes effect are
[configuration.md §2](configuration.md#2-config-file). Idle or running. In `-ui-json`
the overlay is `frame.overlay: "settings"`, and a client may send `r` as a
one-character text message.

### `/repaint`

Requests a full terminal repaint, equivalent to `Ctrl-L`; it changes nothing else.

## Delegation

### `/agent-hub`

Opens the live roster of the current run's children; idle, it reports that no child
is running. Display-only and available during a run. Roster rows, column dropping,
focus, and steering are in [tui.md § `/agent-hub` roster](tui.md#agent-hub-roster) and
[tui.md § Child focus](tui.md#child-focus-and-delegation-steering). It adds no
protocol event.

### `/agents`

Opens the definitions browser
([tui.md § `/agents` browser](tui.md#agents-browser)), which shows the effective
model, effort, and `max_turns` with a `*` for an operator override; `m`, `e`, `t`,
`d`, and `r` edit that row's override. The override keys, provenance, and atomic
writer are [configuration.md §2.2](configuration.md#22-agent-overrides). Tool grants
and prompt bodies stay file-only, so this surface cannot widen authority. In
`-ui-json`, a client may send the editing keys as one-character `text` messages.

## Run control

### `/interrupt`

Interrupts the in-flight model call or tool, equivalent to `Esc` during a run after
overlays and the completion menu are closed. An interrupted model call appends
`interrupt` and an `interrupt-hint` message and continues without counting a turn;
an interrupted tool kills its process group, pairs the call with `canceled`, and
continues. Idle: reports that there is no operation to interrupt.

### `/cancel`

Cancels the active run, equivalent to the second `Ctrl-C`: the runner writes `final`
and `run_end`, syncs, the host removes the per-session work directory, and the TUI
returns to idle. Idle: reports that no run is active.

## `-ui-json` mapping

Commands are composer text, not a protocol command:

```json
{"v":1,"type":"text","text":"/model"}
{"v":1,"type":"key","key":"enter"}
```

The same text/key stream drives autocomplete (`tab`, `up`, `down`, `esc`). Overlays
appear in `frame.overlay` (`help`, `details`, `prompt`, `request`, `model`, `effort`,
`session`, `file`, `table`, `image`, `agent-hub`, `agents`, `settings`), and status
feedback appears in the frame lines. `/prompt` and `/request` emit no protocol event
and no session event; `/model` and `/effort` emit the existing `model_changed` and
`notice` events; `/new` and `/resume` emit a `notice` after a successful switch; and
`tool_started`/`tool_finished` carry the optional `subagent_tasks` array for a
`delegate` call. There is no new event kind and the protocol version stays 1.

## Caps

The command surface adds no durable cap: submissions, the completion menu, and the
overlays use the display caps in [limits.md §2](limits.md#2-tui-and--ui-json).
