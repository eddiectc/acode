# acode prompt and request transparency build plan

- **Date:** 2026-11-19
- **Status:** implemented 2026-11-22 for Phases 0–4 against aharness v0.5.0.
  Phases 1–3 shipped together; the automated gates in §4.1 pass and the
  manual smokes are recorded there as open.
- **Source:** a request to make acode transparent about the prompts it sends,
  a 2026-11-19 survey of acode and aharness v0.3.0, and a 2026-11-19 web
  research pass over popular coding agents (§2).
- **Target:** Go 1.27.4, module `acode`, aharness v0.5.0 baseline. Phase 0
  shipped the exchange reader and the prompt-cap signal in aharness v0.5.0
  (§3.4); acode pins that tagged release and uses no `replace` fork or app-side
  shim.
- **Architecture:** [architecture.md](../architecture.md) defines the host
  boundary. [tui.md](../tui.md) and [commands.md](../commands.md) stay
  authoritative for the shipped surface; this plan does not change them until
  the corresponding phase lands.
- **Standard:** [coding-standard.md](../coding-standard.md);
  **Definition of done:** [definition-of-done.md](../definition-of-done.md);
  **Testing:** [testing-strategy.md](../testing-strategy.md).

## Decision summary

The useful feature is smaller than "log everything": acode already records the
exact system prompt, and it already records every model response plus an
opt-in projection of every model request. The missing piece is a first-class
way to read those records from the running TUI.

| # | Decision | Why |
|---|---|---|
| D1 | Add two idle-and-running slash commands: `/prompt` shows the active session's exact system prompt, and `/request` shows its latest projected model attempt — the newest recorded request and, when one exists, its response. | This is the smallest surface that answers "what did the model actually see?" without a new log format, config key, or provider path. Commands are discoverable through the existing `/` menu and `F1`. |
| D2 | Read through `session.ReadSnapshot` on the active log path, not through `session.Store.Events`. | The SDK documents `ReadSnapshot` as the lock-free read for a log another process may still append to; it opens its own read-only descriptor and takes no `flock`, so it does not contend with the active store's exclusive lock in this process (§3.4). `engine.Inspect` also wraps `ReadSnapshot`, but it returns state, not the raw events `/request` must feed to `agent.LatestModelExchange`; the host therefore uses the lower-level reader and still calls `agent.Inspect` for the prompt. |
| D3 | Keep request-body recording opt-in through the existing `log_requests` setting. Do not record requests by default. | A request projection contains every message and tool schema; a 1 MiB cap per turn is real disk and secret-exposure cost. Responses are already recorded unconditionally. |
| D4 | Reuse the Help/Details overlay stack and key handling, but add a wrap-aware render path for the new views. Add `prompt` and `request` overlay kinds; add no new durable event, protocol version, dependency, or keybinding. | The existing overlay stack already handles scrolling, mouse wheel, `Esc`, frames, and snapshots. The existing `renderScroll`/`fitLines` path truncates long lines, so prompt and JSON bodies need wrapping instead (§3.5). |
| D5 | Call the data "projected", not "raw wire bytes", in the command and the docs. | `model_request` is the SDK's canonical projection before adapter encoding; `model_response` is the normalized response. HTTP headers, auth, and exact wire bytes are not recorded anywhere in acode or aharness. |
| D6 | Defer a CLI print mode and upstream wire capture. | A CLI action for a new prompt would either duplicate SDK rendering or create a throwaway session log. Exact wire capture belongs in aharness behind its own debug contract, not in an app-side shim. |
| D7 | Put latest-exchange pairing and the prompt-cap signal in aharness, not in acode. | Event ordering and the prompt cap are session-schema contracts. [AGENTS.md](../../AGENTS.md) requires a missing SDK capability to be added upstream, not shimmed in the app. acode should consume `agent.LatestModelExchange(events)` and `State.PromptCapped` and format only their results. |

Phases:

| Phase | Contents | Status |
|---|---|---|
| 0 | Upstream: `agent.LatestModelExchange` plus its pairing/run-boundary tests, in aharness | Shipped in aharness v0.5.0 |
| 1 | Host data plane: public TUI views, prompt extraction, exchange formatting, pretty JSON, caps; adopts the tagged Phase 0 release | Implemented 2026-11-22 |
| 2 | `/prompt` and `/request` commands plus overlays, wrap-aware rendering, and `-ui-json` visibility | Implemented 2026-11-22 |
| 3 | Current-reference and README updates | Implemented 2026-11-22 |
| 4 | Verification: unit, TUI, headless integration, race, `make dod` | Automated gates pass; manual smokes pending (§4.1) |
| 5 | Deferred follow-ups: CLI/automation view, exact wire capture upstream | Deferred |

## 1. What acode already has

Most of the plan builds on existing SDK contracts. The missing SDK pieces are
a latest-exchange reader and a prompt-cap flag; §3.4 proposes both upstream
rather than encoding event ordering or cap detection in the app.

| Fact | Evidence | Consequence |
|---|---|---|
| A new session records `session_start` eagerly, before the first run. | `engine.openNew` calls `agent.WriteSessionStart` before returning (`engine/engine.go`). The TUI and batch open through the engine or the same writer. | `/prompt` works as soon as the TUI opens, not only after a message. |
| The resolved system prompt is recorded verbatim, capped at 32 KiB with a visible `[truncated N bytes]` suffix. | `agent.WriteSessionStart` writes `SystemPrompt: systemPromptText(cfg)` (`agent/runner.go`); `capText` appends the suffix over `maxSystemPromptBytes = 32 << 10` (`agent/prompt.go`, `agent/events.go`). | The system prompt is already durable and authoritative; the SDK should expose whether it capped the prompt rather than make the app string-match the text. |
| The SDK exposes the recorded prompt through `agent.State`. | `agent.Inspect` returns `State.SystemPrompt`, `PromptVersion`, and `PromptTag` (`agent/state.go`). | The host can use the public state projection instead of decoding `session_start` itself. |
| A log written before the prompt was recorded has an empty `State.SystemPrompt`; the SDK replays it by rendering the recorded `PromptVersion`. | `resolveInherited` / `resolvedNewPrompt` fall back to `agent.RenderSystemPrompt` when the recorded prompt is empty (`engine/engine.go`). | `/prompt` can offer a reconstruction using the same public renderer, clearly labeled because current identity/agent definitions may differ from what ran. |
| `log_requests` records one `model_request` event per projected request, capped at 1 MiB. | `agent.Runner.recordModelRequest` (`agent/retry.go`); `maxModelRequestBytes = 1 << 20` (`agent/events.go`); flag/env/config plumbing in `cmd/acode/config.go`, `cmd/acode/run.go`, `cmd/acode/tui.go`. | The request side already exists behind an opt-in switch; the app only needs to read it. |
| A `model_response` event is recorded for every successful model call, with text, tool calls, finish reason, usage, optional cost, latency, attempt, and streamed. | `recordModelResponse` (`agent/runner.go`); `modelResponseData` (`agent/events.go`). | The response side is always available, even when request logging is off. |
| Both event types are public constants. | `session.TypeModelRequest` and `session.TypeModelResponse` are exported in `session/event.go`. | The upstream reader names them without re-declaring them; acode never reads these event types directly. |
| `agent.History` deliberately ignores `model_request` payloads. | `agent/history.go`: `case session.TypeFinal, session.TypeModelRequest:` has no display item, with the comment that requests are debug payloads. | No existing transcript or overlay can show a request; a new explicit surface is required. |
| There is no public "latest request/response" projection. | `agent.Inspect` tracks state, usage, and pending calls but not the last exchange; `agent.History` drops requests. | The app would have to encode event ordering to pair them. D7 puts that in aharness instead. |
| `/details` shows the prompt version but not the prompt text. | `cmd/acode/tui.go` `Details()`; `internal/tui/widgets.go` `openDetailsOverlay`. | The UI currently reports provenance, not content. |
| `/settings` already exposes `log_requests`. | `cmd/acode/config_settings.go` registry and read/write cases; `docs/configuration.md`. | No new setting is needed. Saved config values apply at the next startup. |
| The Help/Details overlay is a scrollable `[]string` with mouse-wheel handling. | `internal/tui/widgets.go` `overlayState`, `renderScroll`, `openHelpOverlay`, `openDetailsOverlay`. | The new views can reuse the overlay stack and keys, but need a wrap-aware render path (§3.5). |
| `-ui-json` exposes the top overlay as a string in each frame. | `internal/tui/screen.go` `Frame.Overlay`; `internal/tui/app.go` `snapshot`; `docs/tui.md` protocol section. | The new overlays appear in frames without a protocol version bump, but the documented enumeration must widen (§3.6). |
| The TUI is display-only; the host owns the store and workspace. | [architecture.md](../architecture.md) §1–§5. | Prompt/exchange extraction belongs in `cmd/acode/tui.go` behind optional Host capabilities; formatting and wrapping belong in `internal/tui`. |

`session.ReadSnapshot` is the SDK's documented lock-free reader: it captures
the file size before reading, ignores bytes written after the capture, and
treats a partial final line inside the snapshot as torn rather than corrupt
(`session/reader.go`). It opens a separate read-only descriptor and does not
call `flock`, so the active store's exclusive lock in the same process is not a
conflict. The spawner and `engine.Inspect` already use it. This is the basis for
D2.

## 2. How other coding agents expose this

The research question was narrow: where does a user go to see the system
prompt, the request, or the response?

Currency caveat: these are vendor docs and source observed on 2026-11-19.
Command names and flags change between releases; the plan adopts the pattern,
not a promise that any listed command remains stable. The two sources that
matter most for the design are the in-session inspection pattern (Claude Code,
Gemini CLI) and the explicit debug command (Codex CLI, Aider).

The products split into four patterns.

| Pattern | Products | Strength | Cost |
|---|---|---|---|
| In-session inspection command | Claude Code `/context`; Gemini CLI `/chat debug` and `/memory show` | Available where the user is already working; no external tooling | Needs a host read path, display caps, and a clear "projected vs wire" label |
| CLI debug subcommand | Codex CLI `codex debug prompt-input`; Aider `--show-prompts` | Deterministic, scriptable, easy to diff in CI | Not available mid-session; may construct a separate prompt path |
| Opt-in raw-body log | Claude Code OpenTelemetry raw bodies; Goose `llm_request.*.jsonl`; Continue Console | Complete payloads for deep debugging | Storage, rotation, secret handling, and a separate viewer |
| General debug log/console | Claude Code `--debug`/`--debug-file`; Gemini CLI `--debug` and `F12`; OpenCode log files | Good for failures and transport issues | Noisy; not a product-level answer to "what prompt is this?" |

### 2.1 Claude Code

- `/context` shows everything occupying the context window, broken down by
  category including **system prompt**, system tools, MCP tools, subagents,
  memory files, skills, and conversation messages
  ([debug-your-config](https://code.claude.com/docs/en/debug-your-config)).
- `/debug` enables debug logging for the session; `--debug` and
  `--debug-file <path>` do the same from the CLI
  ([CLI reference](https://code.claude.com/docs/en/cli-reference)).
- Raw request and response bodies are an **opt-in telemetry** feature, not a
  default transcript feature: `OTEL_LOG_RAW_API_BODIES=1` emits inline bodies,
  and `OTEL_LOG_RAW_API_BODIES=file:<dir>` writes untruncated request/response
  files plus an `index.jsonl` pairing index
  ([monitoring](https://code.claude.com/docs/en/monitoring-usage)).
- A complete system prompt is emitted as a `claude_code.system_prompt` event
  only under detailed beta tracing with `OTEL_LOG_USER_PROMPTS=1`
  ([monitoring](https://code.claude.com/docs/en/monitoring-usage)).

Takeaway: Claude Code separates **context inspection** (always available,
categorical) from **raw body capture** (opt-in telemetry). acode can copy the
separation without the telemetry stack.

### 2.2 OpenAI Codex CLI

- `codex debug prompt-input` renders the model-visible prompt input list as
  JSON, optionally with a prompt and images
  ([developer commands](https://learn.chatgpt.com/docs/developer-commands);
  implementation in
  [main.rs](https://raw.githubusercontent.com/openai/codex/main/codex-rs/cli/src/main.rs)).
  It builds the prompt input through `codex_core::build_prompt_input`, so it
  shows composition rather than replaying a live request.
- `codex debug models` prints the raw model catalog Codex sees
  ([developer commands](https://learn.chatgpt.com/docs/developer-commands)).
- The inspected data is explicitly "model-visible prompt input", not an HTTP
  body.

Takeaway: a dedicated, `debug`-named inspection command is an accepted
surface. Codex favors a separate CLI path; acode already has a live session,
so it can inspect in place.

### 2.3 Gemini CLI

- `/chat debug` (also reached through `/resume debug` in current builds)
  exports the most recent API request as a JSON payload
  ([commands](https://geminicli.com/docs/reference/commands/)).
- `/memory show` displays the full, concatenated hierarchical memory loaded
  from `GEMINI.md` files, so the user can inspect the instructional context
  ([commands](https://geminicli.com/docs/reference/commands/)).
- `/stats` exposes session, model, and tool statistics separately
  ([commands](https://geminicli.com/docs/reference/commands/)).
- `--debug` gives detailed output, and interactive mode has an `F12` debug
  console ([troubleshooting](https://geminicli.com/docs/resources/troubleshooting/)).

Takeaway: "show the last request" is useful enough to be a first-class slash
command, and "show the loaded instructions" is separate from "show usage".
This is the closest model for acode's two commands.

### 2.4 Aider

- `--show-prompts` prints the system prompts and exits; it is a debug option
  in the main options reference
  ([options](https://aider.chat/docs/config/options.html)).
- `--llm-history-file` logs the conversation with the LLM to a file
  ([options](https://aider.chat/docs/config/options.html)).
- The docs describe `--show-prompts` among the debug/exit flags, not as an
  interactive feature.

Takeaway: prompt printing is cheap and scriptable. acode can add it later as
a CLI action if a real need appears; the in-session command is the better first
step because acode's prompt is session-specific and already recorded.

### 2.5 Goose

- Goose writes raw LLM request and response logs to
  `~/.local/state/goose/logs/llm_request.*.jsonl`, with a numbered rotation
  keeping the 10 most recent completed requests. Each file contains model
  configuration, input payload, response data, and token usage
  ([logs](https://goose-docs.ai/docs/guides/logs/)).
- Session records and system logs are separate from the request logs.

Takeaway: always-on raw request logging is viable, but it is a durable,
rotating, sensitive artifact. acode's `log_requests` is the same idea with an
explicit opt-in and the session log as the store.

### 2.6 Continue and OpenCode

- Continue exposes prompt logs/analytics through an opt-in **Continue
  Console** in VS Code, plus verbose developer logs
  ([troubleshooting](https://docs.continue.dev/troubleshooting)).
- OpenCode writes server/client logs to a known path and provides
  `opencode debug` tooling, but its CLI documentation does not advertise a
  first-class raw request viewer; users inspect logs or the API
  ([troubleshooting](https://opencode.ai/docs/troubleshooting/),
  [CLI commands](https://opencode.ai/docs/cli/commands/)).

Takeaway: a log file alone is discoverable only to experts. A command that
points at the exact record is the improvement.

### 2.7 What this means for acode

1. **Inspect the live session, not a reconstructed one.** acode already has
   the exact recorded prompt and the exact event payloads. No separate prompt
   builder is needed.
2. **Separate prompt visibility from request visibility.** The prompt is
   always recorded; the request is opt-in. The UI should say which is which.
3. **Use an explicit command, not only a setting.** A setting can turn
   recording on; it cannot answer the question at the moment the user asks it.
4. **Label provenance and limits.** "System prompt", "projected request",
   "projected response", and "not recorded" are different states; the overlay
   must not blur them.
5. **Do not compete with a log file.** The session log remains the durable
   record; the overlay is a view of it.

## 3. Proposed surface

### 3.1 Commands

| Command | Argument | Scope | Summary |
|---|---|---|---|
| `/prompt` | none (extra words ignored) | idle and running | show the exact system prompt recorded for the active session |
| `/request` | none (extra words ignored) | idle and running | show the latest projected model attempt: the newest recorded request and, when present, its response |

Both commands are display-only. They do not start a run, write an event, change
the next run, or touch the transcript. They open a modal overlay, like
`/details` and `/help`.

No new keybinding is proposed. The existing `/` menu, `F1` help, and slash
autocomplete already make commands discoverable; a function key would consume
a scarce binding for a diagnostic view.

### 3.2 `/prompt`

Overlay title: `system prompt · <version>`; for example
`system prompt · prompt-v3`.

Body:

```text
prompt:   prompt-v3
bytes:    10,240
source:   recorded in session_start

You are acode, a coding harness working in the workspace "…".
…
```

- A `tag:` row appears only when the session recorded a prompt tag. acode sets
  none under the current tier, so the shipped binary normally shows no tag row.
- The body is the recorded prompt from `agent.State.SystemPrompt`, sanitized
  for terminal display. It is not Markdown-rendered.
- If the log predates the recorded prompt, the host calls
  `agent.RenderSystemPrompt(state.PromptVersion, state.Workspace, acodeIdentity, cfg.Agents)`
  and labels the source `reconstructed from prompt version; the log predates
  the recorded prompt`. This uses today's identity line and agent catalog, so
  the result can differ from what originally ran; the label says so.
- If the version is unsupported or rendering fails, the overlay shows the
  version and the wrapped error instead of pretending to have the text.
- The prompt is shown whole up to the SDK's 32 KiB cap. `agent.State` carries
  `PromptCapped` (Phase 0) so the overlay can report `recorded prompt was
  capped by the SDK` without matching a suffix in prompt text. A partial prompt
  is never presented as the whole one.
- Works before the first run because the new session's `session_start` is
  written eagerly.

### 3.3 `/request`

Overlay title: `last model attempt · run <n> · turn <n> · projected`.

"Latest attempt" means the newest `model_request` or `model_response` event in
the session log, whichever came last. That is deliberately not "latest
response": a request that is in flight, or whose call failed before a response,
must still be visible.

Body sections, in order:

```text
request  seq 42 · 123,456 bytes
{ ... pretty-printed model_request payload ... }

response seq 43 · 1,234 bytes
{ ... pretty-printed model_response payload ... }
```

- The request is the raw `Event.Data` of the selected `model_request`,
  indented with `encoding/json/jsontext` (`Value.Indent`). It is not
  re-marshaled from a local struct, so field order and number formatting are
  preserved.
- The response is the raw `Event.Data` of the selected `model_response`,
  formatted the same way. The `bytes` value in each section header is the
  recorded UTF-8 length before display sanitization, matching the cap's unit.
- Selection and pairing live in aharness, not in acode: `agent.LatestModelExchange(events)`
  (§3.4). The SDK owns the event ordering, `run_start` boundaries, and the
  definition of one attempt. acode only formats the returned payloads.
- Required semantics for that reader:
  1. Find the newest `model_request` or `model_response` event. That event is
     the selected attempt. Its `Turn` is the envelope `Turn`; its `Run` is the
     count of `run_start` events up to and including it. A zero run (no
     `run_start`, as in a malformed or very old log) renders as `run ?`.
  2. If the newest event is a `model_request`, return it with
     `HasResponse=false`. This covers in-flight and terminally failed calls.
  3. If the newest event is a `model_response`, scan backward for the nearest
     `model_request`, stopping at an earlier `model_response`, a `run_start`,
     or `session_start`. Accept only a request whose envelope `Turn` equals the
     response's. If none is found, return the response with
     `HasRequest=false`.
  4. A returned pair must share one `Turn`. Do not synthesize a turn from
     ordering or from payload fields.
- If the selected response has no request, show the response and a request
  section that says `not recorded for this turn`, explains that
  `log_requests` was off, and points to `/settings` or `-log-requests`.
  Saved settings apply at the next startup, so the hint says so.
- If the selected attempt is a request without a response, show the request
  and `response not recorded (in flight or failed)`. This is useful while a run
  is active.
- If the session has no model events, report `no model exchange recorded in
  this session yet` in the status line rather than opening an empty overlay.
- The response is always the SDK's normalized response; the request is the
  SDK's projection. The overlay footer states: `projected payloads; HTTP
  headers and exact wire bytes are not recorded`.
- `model_retry` events are not included in v1. The latest attempt is the
  requested unit; a retry-history view can be a later addition.

### 3.4 Data contract

#### Upstream addition (Phase 0)

Add one small reader to aharness `agent`, where `agent.Inspect` and
`agent.History` already live. It owns event ordering; acode does not. Phase 0
also adds `State.PromptCapped bool`, set by `agent.Inspect` when the recorded
prompt carries the SDK's cap suffix, so the app never string-matches prompt
content. The upstream test renders a prompt over 32 KiB through the real
`agent.WriteSessionStart` path and asserts the flag.

```go
// ModelExchange is the latest projected model attempt in a session log.
// Raw payloads are the recorded event JSON, not wire bytes.
type ModelExchange struct {
    Request     json.RawMessage
    Response    json.RawMessage
    RequestSeq  uint64
    ResponseSeq uint64
    Run         int
    Turn        int
    HasRequest  bool
    HasResponse bool
}

// LatestModelExchange returns the newest model attempt (a request or a
// response, whichever is later) with the pairing and run-boundary rules in
// §3.3. It returns an error for a malformed event envelope or non-JSON Data.
func LatestModelExchange(events []session.Event) (ModelExchange, error)
```

The upstream reader has its own unit tests for the ordering cases (in-flight,
failed, retry, logging off, resumed run). It copies `Event.Data`; it does not
need to decode the request/response payload structs, which keeps it robust to
payload-field additions. The app consumes only this result.

#### App-side views

Add two optional Host capabilities to `internal/tui`. Names are illustrative;
the implementation may adjust fields as long as the semantics hold.

```go
// PromptView is the resolved system prompt recorded for the active session.
type PromptView struct {
    Text          string
    Version       string
    Tag           string
    Bytes         int // recorded UTF-8 length, before display sanitization
    Reconstructed bool
    Capped        bool // agent.State.PromptCapped
}

// PromptProvider is an optional Host capability for /prompt.
type PromptProvider interface {
    Prompt() (PromptView, error)
}

// ExchangeView is the latest projected model attempt in the active session.
// Empty payloads plus the Has* flags distinguish "not recorded" from an empty
// payload.
type ExchangeView struct {
    Request     string
    Response    string
    RequestSeq  uint64
    ResponseSeq uint64
    Run         int
    Turn        int
    HasRequest  bool
    HasResponse bool
    LogRequests bool // current effective setting, for the hint
}

// ExchangeProvider is an optional Host capability for /request.
type ExchangeProvider interface {
    Exchange() (ExchangeView, error)
}
```

#### Host implementation

`cmd/acode/tui.go`:

1. Takes `h.mu`, checks `h.closed`, copies `h.store.Path()`, and releases the
   lock. `/new` and `/resume` are idle-only and dispatched from the same serial
   Bubble Tea update loop, so a session switch cannot interleave with this
   command. A concurrent close does not invalidate the path.
2. Calls `session.ReadSnapshot(path)` outside the lock. The reader opens its
   own read-only descriptor, captures the size, and does not take the store's
   `flock`; it returns a consistent durable prefix while the same process's
   runner appends.
3. Uses `agent.Inspect(events)` for the prompt fields. It does not decode
   `session_start` locally.
4. Uses `agent.LatestModelExchange(events)` for the exchange fields. It does
   not walk event types or pair requests and responses in the app.
5. Pretty-prints each raw payload by copying it into a `jsontext.Value` and
   calling `value.Indent(jsontext.WithIndent("  "))`.
6. Returns errors wrapped with `%w`; the TUI converts them to a status-line
   notice.

The host returns structured views; `internal/tui` owns sanitization, the
head+tail cap, wrapping, and line construction. This keeps the TUI
display-only, avoids a second session schema, and keeps event-ordering
contract logic in the SDK.

### 3.5 Display policy and caps

| Item | Policy |
|---|---|
| Sanitization | The full overlay text passes through `tui.Sanitize` (preserves newlines, expands tabs, visible control glyphs, strips bidi controls). No ANSI from the log can reach the terminal. |
| Prompt length | Up to the SDK's 32 KiB recorded-prompt cap; displayed whole. If the SDK's `[truncated N bytes]` marker is present, the header reports that the recorded prompt was capped. |
| Exchange length | 64 KiB of recorded UTF-8 per request and per response, applied before display sanitization: keep 32 KiB from the head and 32 KiB from the tail with an omission marker in the middle, so the system/tool prefix and the latest turn both survive. Sanitization can expand tabs and control glyphs, so rendered bytes may be larger than the recorded cap; the omission count is recorded bytes. This needs a small `CapTextMiddle` helper rather than the head-only `CapText`. The SDK request event itself stays capped at 1 MiB. |
| Line width | Prompt lines and JSON lines wrap at the overlay width with the existing `github.com/charmbracelet/x/ansi` `Hardwrap` helper; horizontal truncation would hide the content the command exists to show. This is a new wrap-aware path in the overlay renderer, not a behavior change to Help/Details. The overlay keeps a row-based scroll offset when wrapping changes on resize. |
| Scroll | `Up`/`Down`, `PgUp`/`PgDn`, and the mouse wheel scroll the overlay; `Esc` closes it. No new key behavior. |
| Missing capability | If the Host does not implement the optional interface, the status line says the view is unavailable; the command does not open an empty overlay. |
| Errors | A snapshot/read/format error is a status-line notice capped at 512 bytes, never a crash or a partial overlay. |

No new config key. `log_requests` remains the switch that determines whether a
future request is recorded. The plan does not make it live-updatable: doing so
would change the `/settings` contract for one key. If live toggling is wanted
later, it should be a separate config-surface decision.

### 3.6 `-ui-json`

- The commands are ordinary composer text plus `enter`; no protocol command
  is added.
- Frames expose `"overlay": "prompt"` or `"overlay": "request"` while the
  overlay is open, and `"none"` after `esc`.
- No new semantic `event` is emitted. The overlay content is visible through
  frames and snapshots, exactly like Help and Details.
- The protocol version stays 1, but this is a documented contract change: the
  `overlay` field is a closed list today (`docs/tui.md`, `docs/commands.md`).
  Phase 3 adds `prompt` and `request` and states that clients must treat an
  unknown overlay string as opaque. A client that validates the old closed
  list needs the docs update. This is additive and display-only, which is why
  it does not justify a version bump; the plan does not claim no contract
  change.
- The exchange overlay in `-ui-json` shows the same sanitized, capped,
  wrapped content as the terminal overlay.

### 3.7 Security and privacy

- The request projection includes messages, tool calls, and tool schemas. It
  can contain file contents, command output, environment-derived data, and
  user text. `log_requests` therefore remains opt-in; the session log is
  mode 0600 under `<state-dir>/sessions`.
- The SDK does not record authorization headers, provider tokens, or raw HTTP
  frames. The overlay must not claim that it does.
- The overlay is local display only. It makes no network call and writes no
  new durable event.
- `/request` should not render a request from a different attempt as if it
  belonged to the selected response; the pairing rule in §3.3 is a correctness
  requirement, not a presentation detail.
- Session logs already persist after the process exits. The docs must warn
  that enabling `log_requests` writes model-visible content to disk, including
  tool results.

## 4. Milestones

### Phase 0 — upstream exchange reader (M, aharness)

| Task | Change | Acceptance |
|---|---|---|
| P0.1 | Add `agent.ModelExchange` and `agent.LatestModelExchange(events)` in aharness, implementing §3.3's newest-attempt semantics and run boundary. Also add `State.PromptCapped` to `agent.Inspect`, derived from the SDK's own cap rule. | The reader is public, copies raw payloads, and returns wrapped errors for malformed event data. `PromptCapped` requires no app-side suffix matching. |
| P0.2 | Add upstream tests for: newest response with matching request; newest response with logging off; newest request in flight; newest request terminally failed; retry between request and response; compaction between turns; resumed run; earlier failed run's request not paired with a later response; no model events; and a prompt over 32 KiB recorded through the real `agent.WriteSessionStart` path. | Every case is asserted in aharness, not in acode; the cap test pins the real suffix/flag behavior. |
| P0.3 | Tag and release aharness with Phase 0. | A tagged release exists; no `replace`, fork, or vendored copy is used. |

If upstream declines the reader, this plan is blocked: acode must not add the
pairing logic as an app-side exception without a new recorded decision. The
review should treat that as a scope change, not an implementation detail.

Sequencing with [engine-host-build.md](engine-host-build.md): its Phase 2 may
replace `tuiHost`'s direct `store`/`ws` fields with an `engine.Host`. If that
lands first, implement the same two methods over `host.Store().Path()` and the
engine accessors. The `PromptProvider`/`ExchangeProvider` interfaces, the
overlay contract, and the upstream reader are unaffected. If this plan lands
first, the later engine-host migration must keep both methods and their tests.

### Phase 1 — host data plane (M)

| Task | Change | Acceptance |
|---|---|---|
| P1.0 | Bump the aharness dependency to the tagged release carrying Phase 0. | `go.mod` pins the tagged release; no `replace`, fork, or vendored copy. |
| P1.1 | Add `PromptView`, `PromptProvider`, `ExchangeView`, and `ExchangeProvider` to `internal/tui/app.go` next to `DetailsProvider`. | The TUI can be built against a fake host that implements either, both, or neither capability. |
| P1.2 | Implement `Prompt()` on `tuiHost` using `session.ReadSnapshot` and `agent.Inspect`; add the old-log `RenderSystemPrompt` fallback and map `State.PromptCapped` to `PromptView.Capped`. | A new session, a resumed session, and a forked session return the prompt recorded in their own `session_start`; a legacy log is labeled reconstructed; a capped prompt is labeled capped. |
| P1.3 | Implement `Exchange()` by calling `agent.LatestModelExchange` and formatting its raw payloads; do not pair events in the app. | A turn with logging off returns the response with `HasRequest=false`; a logging-on turn returns the matching pair; the newest in-flight or failed request returns `HasResponse=false`; a failed earlier run's request is never paired with a later response. |
| P1.4 | Add `jsontext.Value.Indent` formatting and a unit-test helper for the payloads. | Field order and JSON number/string spelling survive formatting; malformed payload data returns a wrapped error rather than a panic. |
| P1.5 | Add host tests for empty sessions, missing/corrupt session_start, unsupported prompt versions, and session switching. | No test reads the operator's state directory; all stores use `t.TempDir`. |
| P1.6 | Add `CapTextMiddle` (or equivalent) to the display helpers: 32 KiB head and 32 KiB tail of recorded UTF-8, one omission marker reporting recorded bytes omitted. | A payload over the cap keeps its first and last content; the marker reports the omitted recorded byte count. |

### Phase 2 — commands and overlays (M)

| Task | Change | Acceptance |
|---|---|---|
| P2.1 | Register `/prompt` and `/request` in `slashCommands`; dispatch them in `handleSlashCommand`. | `/prompt` and `/request` parse with no argument; extra words are ignored; unknown lookalikes remain ordinary messages. |
| P2.2 | Add `OverlayPrompt` and `OverlayRequest`; build the line slices in `internal/tui` (display-only) from the host views, apply the head+tail cap on the recorded payload, then sanitize and wrap with the existing `ansi.Hardwrap`. Add a wrap-aware render path used by these two overlays without changing Help/Details behavior. | `Esc` closes; `Up`/`Down`, `PgUp`/`PgDn`, and the wheel scroll; long prompt and JSON lines are readable, not horizontally cut; the overlay never leaks ESC or bidi controls. |
| P2.3 | Add the no-data, missing-capability, and read-error notices. | A missing request explains `log_requests` and restart; an empty session does not open an empty modal. |
| P2.4 | Add TUI command tests using a fake host with prompt/exchange capabilities. | The overlay kind and content are asserted; no run starts; a host without the capability reports a notice. |
| P2.5 | Add `-ui-json` headless coverage: drive `/prompt` and `/request` with `text` + `enter`, assert the frame `overlay` value and visible content, then `esc`. | Protocol version remains 1 and no new event type is emitted; the documented overlay list is updated in the same change (P3.2). |

Phases 2 and 3 land as one change. The current-reference updates in P3.1–P3.4
block the Phase 2 merge, because the new commands and the widened `overlay`
enum are user-visible contracts. There is no intermediate commit that ships an
undocumented command or protocol value.

### Phase 3 — documentation (S)

| Task | Change | Acceptance |
|---|---|---|
| P3.1 | Update [commands.md](../commands.md): summary rows, full sections, scope, `-ui-json` mapping, and the "projected, not wire" note. | The command reference and the actual parser agree. |
| P3.2 | Update [tui.md](../tui.md): add `prompt` and `request` to the `overlay` list, state that unknown overlay strings are opaque, and document key/scope behavior, caps, and availability during a run. | The frame `overlay` list includes the new values and no longer implies a closed set that rejects unknown values. |
| P3.3 | Update [limits.md](../limits.md): `log_requests` row, prompt display cap, exchange head+tail cap, and line-wrap behavior. | No SDK cap is restated as an app value; the 1 MiB request-event cap stays linked to aharness. |
| P3.4 | Update [configuration.md](../configuration.md) and the README config section to say that `log_requests` populates `/request` and saved changes apply at the next startup. | A user can find the setting from the command docs and vice versa. |
| P3.5 | Update [architecture.md](../architecture.md) §5 with the two optional host capabilities, and [README.md](../../README.md) with the two commands. | The architecture table names the capabilities; the README lists the commands without claiming more than shipped behavior. |
| P3.6 | Keep this plan's [docs/README.md](../README.md) index row in sync with its status. | The row reads `Proposed 2026-11-19` until implementation starts, then reflects the phase status. |

### Phase 4 — verification (S)

| Task | Change | Acceptance |
|---|---|---|
| P4.1 | Run the narrow tests first: `go test -run 'Prompt|Request|Debug' ./internal/tui ./cmd/acode`. | The new tests pass. |
| P4.2 | Run `go test -race -count=1 ./...` with a test that appends through a real `session.Store` (with its exclusive lock) while `Exchange()` reads through `session.ReadSnapshot`. Use a bounded wait (`select` with a timeout), not `time.Sleep` polling, so a lock regression fails fast with a diagnostic. | No race on `Store` or the host; the same-process, exclusive-lock read path is exercised and does not block or error. |
| P4.3 | Run `make dod`. | `goimports`, `go vet`, `golangci-lint`, race tests, build, `govulncheck`, and `docs-check` pass. |
| P4.4 | Manual fake-provider smoke: start `-ui-json`, submit a task with `-log-requests`, wait idle, run `/prompt` and `/request`, inspect frames; repeat without `-log-requests` and confirm the response-only notice. | The overlay content matches the session log; the no-request path is honest. |
| P4.5 | Real-provider smoke if a token is available; otherwise record the exact pending check. | The plan records what was and was not verified. |

Plan-only verification (2026-11-19): the plan itself added no code, so the code
gates above were not run; `make docs-check` covered the new links. The
implementation's verdict is in §4.1.

### 4.1 Deviations and verification (2026-11-22)

Deviations from the design above:

1. Phase 0 shipped in aharness v0.5.0, which supersedes the mistagged v0.4.2;
   acode pins v0.5.0.
2. `Prompt()` and `Exchange()` copy the log path, the agent catalog, and the
   effective `log_requests` value under the host lock, then call
   `promptView(path, agents)` or `exchangeView(path, logRequests)`. The split
   keeps the lock out of the read and lets the legacy, capped, and corrupt log
   cases be tested without an engine host.
3. `PromptView` carries no error field. A prompt version that cannot be
   rendered returns the version plus a wrapped error, and the overlay shows
   `prompt text unavailable: <error>`; a read error is a status-line notice.
   This satisfies §3.2 and the §5 matrix row at the same time.
4. The scroll area is one row shorter than the overlay box so the title stays
   visible when the body fills it. The overlay stack saves and restores the
   previous panel's body, title, and scroll position, so a stacked `F1` or
   `Ctrl-X` no longer leaves the new overlay's title above the old body. Help
   and Details keep their existing truncating scroll path.
5. The `bytes` value in each `/request` section header is the length of the
   pretty-printed payload — the same string the 64 KiB display cap measures —
   not the length of the raw event JSON.

Verification:

- `go.mod` pins aharness v0.5.0 and `go mod tidy` reports no drift; the go.sum
  hashes match the public checksum database.
- Coverage: prompt before the first run; recorded, reconstructed, capped, and
  unsupported prompts; resumed recordings replayed verbatim and a fork's own
  log read; pairing, logging-off, in-flight, and cross-run attempts;
  the `/request` header matching the recorded `model_request` payload; the
  head-and-tail cap; sanitization; wrap and title visibility; scroll and close;
  missing capabilities; no new protocol event; `-ui-json` overlay values.
- `go test -race -count=1 ./...` includes a real locked `session.Store`
  appending while `Exchange` reads through `session.ReadSnapshot`.
- `make dod` passes: docs-check, goimports, vet, golangci-lint, race tests,
  build, and govulncheck.

Still open:

- Real-provider smoke (§4.5). No provider token was available, so the overlays
  were verified against a fake provider only.
- Real-terminal smoke of the interactive overlay (hand-driven keys, wheel,
  resize). The renderer is covered by unit tests and `-ui-json` frames only.

### Phase 5 — deferred follow-ups

These are deliberately out of scope for the first implementation:

- **CLI print mode.** A future `acode -print-prompt` could inspect an existing
  session with `engine.Inspect` and print the recorded prompt, or a future
  `acode debug prompt-input`-style command could render a new prompt. It needs
  a product decision about whether it may create a session and what it does
  about the model/catalog requirements. If that path is built, it should call
  the SDK's renderer exactly as `/prompt` already does; it should not grow a
  second prompt-composition implementation in the app.
- **Exact wire capture.** If users need the actual HTTP request/response body
  rather than the projection, that must be an aharness feature (an adapter
  hook or a debug tap), gated and capped upstream. acode must not wrap the
  transport to add it.
- **Request history.** `/request all` or a retry view can wait until the
  single-exchange view ships and users ask for more.
- **Live `log_requests` toggling.** A future `/settings` change could apply
  this key to the next run. That is a settings-contract change, not part of
  this feature.

## 5. Test matrix

| Layer | Case | Expectation |
|---|---|---|
| Upstream unit | Newest response with a matching request | `HasRequest=true`, `HasResponse=true`, `RequestSeq < ResponseSeq`, same run and turn. |
| Upstream unit | Newest response with logging off | `HasResponse=true`, `HasRequest=false`, no older request paired. |
| Upstream unit | Newest request after a completed exchange (in flight) | `HasRequest=true`, `HasResponse=false`; the older completed pair is not returned. |
| Upstream unit | Newest request whose call failed | `HasRequest=true`, `HasResponse=false`; no synthetic response. |
| Upstream unit | Earlier run failed after recording a request, later run has logging off | The earlier request is not paired with the later response. |
| Upstream unit | Retry between request and response | The retry event is ignored for pairing; the pair is still returned. |
| Upstream unit | Compaction between turns | Compaction does not break the run boundary or the pair selection. |
| Upstream unit | Resumed run | Pairing does not cross the new `run_start`. |
| Upstream unit | No model events | Empty result, not an error. |
| Upstream unit | Malformed event data | Wrapped error; no panic. |
| Upstream unit | Prompt over 32 KiB recorded through `agent.WriteSessionStart` | `State.PromptCapped=true`; the SDK's actual suffix is pinned by the test. |
| Host unit | New session before the first run | `Prompt()` returns the prompt written in `session_start`; `Exchange()` reports no model events. |
| Host unit | Resumed session | The resumed log's prompt wins over current definitions; `PromptVersion`/`Tag` match. |
| Host unit | Forked session | The fork's own `session_start` prompt is shown, not the parent's history prompt. |
| Host unit | Legacy log with empty `SystemPrompt` | Prompt is reconstructed with `agent.RenderSystemPrompt` and labeled. |
| Host unit | Prompt flagged `PromptCapped` | `PromptView.Capped=true` and the header says the recorded prompt was capped. |
| Host unit | Unsupported legacy prompt version | `Prompt()` returns a wrapped error; the TUI shows a notice, no panic. |
| Host unit | Payload over the recorded cap | First and last recorded content are present, with one omission marker between them; the marker counts recorded bytes. |
| Host unit | Snapshot while the real locked store is open | `ReadSnapshot` succeeds; no flock contention with the active `Store`. |
| TUI | `/prompt` and `/request` parse and dispatch | Overlay kinds are set; no run starts; extra words ignored. |
| TUI | Missing capability | Status notice; overlay stays closed. |
| TUI | Scroll and close | `Esc` closes; wheel/PgUp/PgDn change the offset; prompt and JSON lines wrap. |
| TUI | Sanitization | ESC/control/bidi bytes from a payload render as safe glyphs or are removed; tabs expand. |
| Headless | `-ui-json` frame after `/prompt` | `overlay="prompt"`, content visible, no new event. |
| Headless | `-ui-json` frame after `/request` | `overlay="request"`, request/response sections visible, no new event. |
| Integration | Fake provider with `-log-requests` | The request payload in the overlay matches the `model_request` event in the JSONL log. |
| Integration | Fake provider without `-log-requests` | Response is shown; request section says it was not recorded. |
| Race | Append through a real `session.Store` while `Exchange()` reads | `-race` passes; `ReadSnapshot` returns a consistent prefix without lock contention. |
| Docs | `make docs-check` | Relative links resolve. |

## 6. Risks and mitigations

| Risk | Mitigation |
|---|---|
| The word "raw" oversells the data. | Use "projected request/response" in the UI, command docs, README, and limits; state explicitly that headers and wire bytes are not recorded. |
| Request payloads contain secrets or sensitive file content. | Keep `log_requests` off by default; keep the session log 0600; document the exposure next to the setting and the command; never send the overlay anywhere. |
| Reading a huge live log on every command is slow. | Read only on demand outside `h.mu`; cap the displayed payload; rely on `session.ReadSnapshot`; if profiling shows a problem, request a tail-read helper upstream rather than copying session logic. |
| A response gets paired with the wrong request, or an in-flight request is hidden. | Put the newest-attempt pairing in the upstream reader, test every ordering case there, and keep the app limited to formatting. |
| The upstream reader is not accepted. | Treat it as a blocker and record a new decision; do not add app-side pairing logic as a silent exception to the SDK-boundary rule. |
| Same-process reading contends with the store's exclusive lock. | `ReadSnapshot` opens a separate read-only descriptor and does not call `flock`; test it against a real locked store under `-race`. |
| Old logs render a prompt that differs from what ran. | Label reconstructed prompts; never silently present a rebuild as recorded text. |
| A cap-truncated prompt is presented as exact. | `State.PromptCapped` comes from the SDK's own cap rule and is tested through the real recording path; the app does not suffix-match prompt text. |
| A head-only cap hides the latest turn in a large request. | Keep head and tail with an omission marker. |
| A future SDK changes the event payload shape. | The upstream reader copies `Event.Data` without decoding it; the app formats the returned raw JSON. |
| New overlay strings break a strict `-ui-json` client. | Document the widened overlay list and require clients to treat unknown overlay strings as opaque; keep the version at 1 because the change is additive and display-only. |

## 7. Open questions

1. **Should `/prompt` show the tool schemas too?** The system prompt does not
   contain them; `/request` does. Keep the separation and let `/request` show
   the model-visible tool list.
2. **Should `/request` include `model_retry` events?** Not in v1. The first
   version shows the latest attempt; a retry timeline can follow.
3. **Should there be a `debug` command group?** Two flat commands are more
   discoverable and fit the one-argument parser. A future `/debug` group can
   alias them without changing the underlying providers.
4. **Should a CLI print mode ship alongside?** Deferred (D6). It needs a
   decision about session creation and catalog access; the TUI path proves the
   data contract first.
5. **Should the overlay support copy?** Terminal Shift-selection already
   copies visible text; adding a clipboard dependency is not justified by
   [coding-standard.md §3](../coding-standard.md#3-dependencies).
6. **Should `log_requests` apply live?** Out of scope. The command tells the
   user that a saved setting takes effect at the next startup.
7. **What is the upstream release cadence?** Phase 0 needs a tagged aharness
   release before Phase 1 can pin it. If the reader lands with other changes,
   the plan's target version moves; the API shape and tests stay the same.

## 8. References

### acode and aharness

- [architecture.md](../architecture.md) — host boundary and optional
  capabilities.
- [commands.md](../commands.md) — slash-command reference.
- [configuration.md](../configuration.md) — `log_requests` and the config
  precedence.
- [limits.md](../limits.md) — app and SDK caps.
- [tui.md](../tui.md) — overlays and the `-ui-json` protocol.
- [prompt-optimization-build.md](prompt-optimization-build.md) — how the
  recorded prompt and prompt versions came to be.
- aharness `agent/state.go` — `State.SystemPrompt`, `PromptVersion`,
  `PromptTag`, and `agent.Inspect`.
- aharness `agent/prompt.go` — `maxSystemPromptBytes` and
  `RenderSystemPrompt`.
- aharness `agent/retry.go` / `agent/events.go` — `recordModelRequest`,
  `modelRequestData`, `modelResponseData`, and the 1 MiB request cap.
- aharness `session/reader.go` — `ReadSnapshot` concurrency contract.
- aharness `session/event.go` — exported `TypeModelRequest` and
  `TypeModelResponse`.
- aharness `agent/history.go` — requests are excluded from display history.
- aharness `agent/exchange.go` (new in Phase 0) — `ModelExchange` and
  `LatestModelExchange`.

### External research (retrieved 2026-11-19)

- Claude Code, "Debug your configuration": <https://code.claude.com/docs/en/debug-your-config>
- Claude Code, "CLI reference": <https://code.claude.com/docs/en/cli-reference>
- Claude Code, "Monitoring": <https://code.claude.com/docs/en/monitoring-usage>
- OpenAI Codex CLI, "Developer commands": <https://learn.chatgpt.com/docs/developer-commands>
- OpenAI Codex CLI source, `codex-rs/cli/src/main.rs`: <https://raw.githubusercontent.com/openai/codex/main/codex-rs/cli/src/main.rs>
- Gemini CLI, "CLI commands": <https://geminicli.com/docs/reference/commands/>
- Gemini CLI, "Troubleshooting guide": <https://geminicli.com/docs/resources/troubleshooting/>
- Aider, "Options reference": <https://aider.chat/docs/config/options.html>
- Goose, "goose Logging System": <https://goose-docs.ai/docs/guides/logs/>
- Continue, "Troubleshooting": <https://docs.continue.dev/troubleshooting>
- OpenCode, "Troubleshooting": <https://opencode.ai/docs/troubleshooting/>
- OpenCode, "CLI commands": <https://opencode.ai/docs/cli/commands/>
