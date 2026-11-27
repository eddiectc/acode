# acode TUI (shipped)

- **Date:** 2026-11-27
- **Status:** implemented, covering the model and effort pickers, transcript
  folding, the todo widget, themes, and the delegated-child roster and focus view.
  Caps live in [limits.md](limits.md), commands in [commands.md](commands.md), and
  the `F1` overlay is the complete key and command listing.
- **Verified:** fake-provider TUI/`-ui-json` integration, headless `x/vt` frames
  and SVG snapshots, race tests, and a manual PTY smoke under `script(1)` at
  80x24; the real-provider TUI smoke is pending provider credentials.
- **Not verified:** Kitty graphics and the real-terminal 200x60/tmux matrix;
  real-provider streaming and folding against the hosted endpoint.

## Modes

| Invocation | Mode |
|---|---|
| `-version` | print build info and exit 0 before mode selection |
| `-ui-json` | headless TUI on stdio; no TTY/`TERM` requirement; no alternate screen or raw mode |
| `-batch` | one-shot CLI, stdout/stderr contract unchanged |
| stdin and stdout are terminals | TUI (default) |
| either is not a terminal | batch automatically |

`-interactive` is gone; it is an unknown-flag error. Selection order, exit codes,
and eager session creation are [architecture.md §4](architecture.md#4-modes-and-selection)
and [§7](architecture.md#7-exit-codes). The TUI and `-ui-json` always stream
(`-stream=false` is a config error), auto-submit a seeded `-task` after the first
frame, and never print final text to stdout.

## Keys

`Enter` submits the composer, or steers an active run or focused child; `Esc` closes
an overlay, clears a non-empty composer, then interrupts; `Ctrl-C` interrupts first
and cancels second; `Ctrl-D` quits. The `F1` overlay lists every other key, overlay
binding, and command and is the shipped truth for them.

The composer grows to six rows and then scrolls, `Shift-Enter`/`Alt-Enter` inserts a
newline, and `Up`/`Down` recall history. `F4` edits the draft in `$VISUAL`/`$EDITOR`
while idle, and an empty transcript shows a tips block naming `F1`, `Shift`+drag,
`Ctrl+Shift+V`, the wheel, and the pickers that disappears with the first transcript
item. The escape timeout and the composer cap are in [limits.md](limits.md), and a
modified sequence such as Option-`[` never interrupts.

## Slash commands

Typing `/` opens a filtered completion menu: `Up`/`Down` select, `Tab` completes,
`Enter` runs, and `Esc` dismisses before the normal `Esc` action. Commands work
during a run, but next-run settings and session switching are disabled with a
notice. [commands.md](commands.md) owns invocation, syntax, arguments, scope, and
the `-ui-json` mapping.

## Settings

`/settings` opens a browser over the active config file.
`Up`/`Down`/`PgUp`/`PgDn` and the wheel move, `Enter`/`Space` edit a text, integer,
duration, or secret row or advance a toggle and enum, `Left`/`Right` cycle those,
`r` removes the saved key, and `Esc` cancels an edit before closing. Rows, source
column, validation, masking, the atomic write, and the display preferences that apply
to this process immediately are [configuration.md §2](configuration.md#2-config-file).

## Themes

`ui_theme` selects `acode`, `solarized-dark`, or `solarized-light`
([configuration.md §2](configuration.md#2-config-file)). Markdown follows the
palette's light/dark mode, `NO_COLOR` strips color and background but keeps
attributes, no session event or protocol field records the choice, and `Ctrl-X` names
the active theme.

## Session picker

`/resume` opens a filtered list of previous sessions for the same canonical
workspace, newest first and excluding the active one. Each row shows the first
task, session ID prefix, modification time, and recorded model; typing filters,
`Up`/`Down` select, `Enter` resumes, and `Esc` closes without switching. A
successful switch replays the target's history and counters and emits a display-only
`notice`; a locked, corrupt, or other-workspace target reports the error and leaves
the active session open.

## Model picker

`Ctrl-P` opens the catalog-backed picker while idle; a run disables it with a
notice. Entries are the usable models.dev models sorted by ID, each row showing the
display name, ID, dialect, context and input limits, advertised output ceiling,
normalized effort values, and `deprecated`/`alpha`/`beta` and `translated` markers;
the advertised ceiling is metadata, not the request budget.

Selecting changes only the next run's dialect, base URL, and limits: an explicit
context limit is clamped to the new window, an explicit output budget is sent
unchanged, and a next-run effort survives only if the new model advertises it. The
header ratio and `Ctrl-X` update immediately, and `max output: unset` appears when
no budget applies. `Ctrl-R` refreshes the catalog as `-refresh-models` does, and
success keeps the highlighted model and filter when they still exist. On resume
without a model the picker may skip the network, leaving only the recorded model
until a refresh succeeds.

## File picker

`Ctrl-O` and `/files` use a Bubbles list over `tools.Workspace.ListDisplayDir`
because Bubbles' `filepicker` reads directories directly and cannot enforce
`os.Root`, `.git`, and symlink containment. It lists the current directory, never
reads contents, and inserts a workspace-relative path; `Esc` leaves the composer
unchanged.

## Thinking effort

The public name is *thinking effort*; the provider and catalog call it *reasoning
effort*. Values come from the effective model's normalized catalog entry, so there
is no harness-wide enum. `F2` opens the picker for the next run while idle (during a
run it shows the model picker's disabled notice) and lists `provider default`
first, then the advertised values in canonical order; a model that advertises none
shows a notice and opens no overlay, and `default` is a clear sentinel, never a
catalog level.

A selection changes only the next run and is recorded in the next `run_start`;
`provider default` clears it so the request omits the wire field.

## Thinking

Model thinking (provider reasoning) renders as a display-only block indented under
the assistant text it precedes, headed `thinking…` until assistant text arrives or
the response completes and `thinking` afterwards. It is never logged, replayed on
resume or fork, printed in batch mode, or carried as a protocol event, so a resumed
or forked session shows only new thinking.

- Blocks are folded by default: header, one elision row with the hidden rendered-row
  count, and the body tail described under [tool blocks](#tool-blocks). `F3` expands
  or collapses every block, is sticky for this process, writes no config file, and is
  inert while an overlay is open; `/settings` persists the initial mode as
  `ui_expand_all`.
- Expanded text is bounded by the per-item display cap in [limits.md](limits.md), and
  the truncation marker stays visible in both states; the elision row has no
  session-log sentence because thinking is never logged. The body wraps and is never
  parsed as markdown, so `Ctrl-R` rich/raw does not apply to it.
- A model that exposes no thinking produces no block; a buffered response shows its
  reasoning once, before the text.
- `-ui-json` sees thinking only in `frame` lines and snapshots, so a collapsed block
  contributes its header, elision row, and tail until the client sends `key` `f3`.

## Tool blocks

Each tool call renders as one folded block headed
`tool <name> · <intent> · <status>`, with elapsed time when known and the result
code, truncation, and timeout markers on failure. The model supplies the title as
the shared `intent` argument: display metadata, recorded only inside the tool-call
arguments, never acted on by the tool. A call without one falls back to the raw
argument preview, and both are sanitized and display-capped
([limits.md](limits.md)). A collapsed block shows one elision row with the hidden
rendered-row count and the last rendered rows of the body; an expanded body is
bounded to a head and the remainder stays in the session log. `F3` folds tool and
thinking blocks together.

### Delegated calls

A `delegate` call is one folded block with one nested status line per task,
`[N] <agent> · <status> · [elapsed] · <model> · effort <value> · <turns> turns ·
ctx <tokens> · <tokens> tok · cost <estimate>`, in task-index order even when the
tasks run concurrently. The lines come from the live roster while the call runs and
from the recorded `details` afterwards, so a resumed session shows the same table
without a claim that anything is live; reports stay in the folded body, one
`[task N agent=… status=… session=…]` section each. Task, report, and concurrency caps
are the [SDK's](https://github.com/eddiectc/aharness/blob/main/docs/limits.md).

### `/agent-hub` roster

`/agent-hub` opens an overlay with one row per child: `AGENT`, `INTENT`, `MODEL`,
`EFFORT`, `ELAPSED`, `CTX`, `TURNS`, `COST`, and `STATUS`. Columns drop as the
terminal narrows (intent and turns below 60, context tokens below 70, model and
effort below 80, cost below 90), and a row never exceeds the width. Rows are keyed by
child session and ordered by task index, and `Up`/`Down`/`PgUp`/`PgDn`/wheel scroll.
A finished child keeps its terminal status for the rest of the run, the roster is
empty while idle, and elapsed keeps moving because it is computed from the TUI's
receipt of the start event. `Esc` closes without interrupting a child.

### Child focus and delegation steering

`Enter` on a roster row replaces the parent transcript with the child's, rendered
from `agent.History`: run headers, the delegated task, assistant text in the active
rich/raw mode, tool calls with a capped excerpt, and notices. The log is read
without the child's lock, so the view works while the child writes and a torn final
line is ignored ([architecture.md §5](architecture.md#5-host-contracts)).

A composer submission while the child runs is steering for that child: recorded as
a `user_message` with `source=subagent-steer`, labelled `delegation steer`, and
absent from the parent transcript, with `queued…` shown until the child accepts it.
A progress or finish boundary replaces the live list with the canonical log
snapshot, and a finished child opens read-only and refuses steering with a notice.
`Esc` returns to the parent without interrupting the child, clearing a non-empty
composer first, and `PgUp`/`PgDn`/wheel scroll the child transcript. The view adds no
protocol event: `-ui-json` renders it in frame lines with `frame.overlay: none`.

### `/agents` browser

`/agents` opens the definitions browser described in
[commands.md](commands.md#agents), with `*` marking an operator override. `Enter`
expands a row to show the tool list, the first prompt line, each setting's
provenance, and a malformed file's parse error; `m`, `e`, `t`, `d`, and `r` edit
that row's override, and `Esc` cancels an edit before closing.

## Todo widget

The TUI keeps the checklist published by the `todo` tool in a persistent widget
between the transcript and the composer, rebuilt from the structured `details` of
the last successful `todo` result rather than the rendered tool output.

- A non-empty list shows a `todo · <done>/<total> done` header and one row per
  visible item (`[ ]`, `[~]`, `[x]`), completed rows dim, the in-progress row
  accented, each item one line truncated to the width; the full text stays in the
  tool transcript.
- The window keeps the in-progress item, or the first pending one, visible and reports
  the rest as `· N more`; its height is a display cap ([limits.md](limits.md)). An
  empty list hides it, as does an open slash-command menu.
- A resumed or forked session, or a `/resume` switch, restores it from the last
  successful `todo` result in the log. It adds no durable or protocol event and
  appears in `-ui-json` frame lines and snapshots like other frame text.

## Status line and details

The header shows `effort <value>` for the next run, or `effort default`. The status
line shows `turns N` and `ctx N/L`: `turns` counts successful model responses
cumulatively and renders `turns N/M` only for an explicit positive `-max-turns`; `ctx`
is the latest response's input plus output tokens against `L`, the host-resolved
per-call context window. A model pick updates `L` immediately, the ratio is omitted
when the window is unknown, and unlimited budgets never render as `0/0`.

The details overlay (`Ctrl-X`) shows the observed `turns`, cumulative `tokens`, `ctx`,
and observational `cost`, the effective `thinking effort`, and the configured
`max turns`, `total tokens`, `run timeout`, `model timeout`, and `http timeout`. Run
and model timeouts render `none` rather than `0` ("no harness deadline"), a resumed or
forked session shows the new run's effective values, and the cost line is marked
`(incomplete)` when a response lacked a price. `-ui-json` usage objects may carry the
additive `cached_input_tokens`, `cache_write_input_tokens`, `reasoning_output_tokens`,
`reported`, and `cost_micro_usd` members with no version change.

## `-ui-json` protocol v1

Every stdin line is one JSON object with `"v":1` and `"type"`; unknown fields are
ignored, and an unknown version, type, or key is a protocol error (exit 2). Commands:
`text`, `key`, `paste`, `resize`, `wait`, `frame`, `snapshot`, `quit`, with EOF
equivalent to `quit`. `key` accepts every key in the `F1` overlay plus `left`, `right`,
`home`, `end`, `backspace`, `delete`, `tab`, `shift-tab`, `shift-enter`, and
`alt-enter`; the external editor (`f4`) is unavailable here and reports a notice.
Slash commands are ordinary composer text ([commands.md](commands.md#-ui-json-mapping)).

Output lines:

- `frame`: `seq`, `width`, `height`, `state`, `overlay`, `cursor`, `lines` (exactly
  height entries), `styles` (coalesced `row`, `col`, `width`, `text`, `fg`, `bg`,
  `attrs`), `images`, `color_profile`, `no_color`, `image_protocol`, and
  `styles_truncated`. `overlay` is `none`, `help`, `details`, `prompt`, `request`,
  `table`, `file`, `model`, `effort`, `session`, `image`, `agent-hub`, `agents`, or
  `settings`; the list is additive and an unknown value is opaque, not an error.
  Frames carry no ANSI and no raw image bytes.
- `event`: `run_started`, `run_finished`, `tool_started`, `tool_finished`,
  `model_changed`, `notice`, and `error`. `run_started` and `model_changed` carry an
  optional `thinking_effort`, tool events carry an optional `subagent_tasks` array
  for a `delegate` call, an effort selection emits a `notice` with kind
  `thinking_effort`, and a session switch emits one with kind `session`. No new event
  type is introduced, and clients ignore unknown optional fields.
- `snapshot`: a deterministic SVG of the current `x/vt` cells at the current frame
  `seq`, with no timestamps, random IDs, durations, or ANSI. `error`: `code` and
  `message`; a wait timeout emits `error{code:"timeout"}` and leaves the run active.

Frames are emitted after every view-changing command, after a completed `wait`, and
after each non-text semantic transition; text deltas do not emit one, so request
`frame` to snapshot streaming text. Frames and snapshots are deterministic for a
fixed provider/tool response, size, color profile, and image protocol: `-ui-json`
hides the session ID, elapsed time, and durations.

The driver frames off its own command handling, not a renderer barrier: Bubble Tea
exposes none and `x/vt` is written from the renderer's goroutine, so the driver waits
out the frame settle in [limits.md](limits.md) before reading a frame. The app never
closes the display channel: when the runner returns, the host stops its display
callback and drains the buffered events in order, discarding a stale event from
another run ID. `-ui-size WxH` sets the initial frame size (default `100x30`);
`resize` and the accepted range are bounded in [limits.md](limits.md).

## Terminal safety and images

All model/tool/session/error text passes through the TUI sanitizer: invalid UTF-8 is
repaired, tabs expand, C0/C1 controls and ESC become visible glyphs, bidi overrides
and isolates are stripped, and OSC/CSI/DCS text cannot become a control. Raw HTML is
never interpreted, Glamour's OSC 8 hyperlinks are stripped, and tool output and
thinking text are preformatted.

Images are display-only workspace files resolved with `os.Root`, `filepath.IsLocal`,
no `.git` or symlink components, and no network fetch; SVG/XML and unsupported formats
become placeholders. ntcharts `picture` renders thumbnails as half-block glyphs, the
viewer can use Kitty when `ACODE_IMAGE_PROTOCOL=auto|kitty` selects it, `glyph` and
`none` force the fallback or placeholders, `-ui-json` always forces glyph, and
placements are cleared on viewer close, resize, and exit.

Bubble Tea owns raw mode, the alternate screen, bracketed paste, resize, cursor, and
restoration. Mouse reporting is limited to the scroll wheel: clicks, releases, motion,
and horizontal wheel events are ignored, and focus reporting stays off. The wheel
capture takes the terminal's own selection, so hold `Shift` while dragging (Option-drag
in iTerm2) to select and copy, and `Shift`+middle-click or `Ctrl+Shift+V` to paste.
