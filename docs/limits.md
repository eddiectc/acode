# acode limits and timeouts

- **Date:** 2026-11-18
- **Status:** current for the acode CLI, TUI, and `-ui-json` application
- **Related:** [architecture](architecture.md),
  [configuration](configuration.md), [TUI](tui.md),
  [commands](commands.md), [coding standard](coding-standard.md), and the
  [aharness SDK limits](https://github.com/eddiectc/aharness/blob/main/docs/limits.md)

This document is the reference for caps, limits, and timeouts owned by the
acode application. The SDK owns the loop, transport, session, and tool caps;
the [aharness limits reference](https://github.com/eddiectc/aharness/blob/main/docs/limits.md)
is normative for those and is not duplicated here. Units are bytes, tokens, or
durations as noted. A value of **—** means the limit does not exist.

## 1. CLI, input, and configuration

Every configurable limit resolves per setting as **flag > environment > config
file > default**; see [configuration.md](configuration.md). The "Layer" column
names which sources exist for that row.

| Limit | Value | Layer | Behavior |
|---|---|---|---|
| Config file | `<state-dir>/config.json` | `-config`, `ACODE_CONFIG` | Optional JSON; an explicit path must exist, the implicit default may be absent; `-config ""` disables it; 1 MiB cap; unknown keys and invalid values are config errors (exit 2). `/settings` rewrites the active file atomically (0600 same-directory temporary file, sync, rename), preserves every other key, and refuses a path inside the workspace. |
| `-provider` / `ACODE_PROVIDER` | `opencode-go` | flag, env, `provider` | Only supported provider; an unknown value is a config error (exit 2) listing `opencode-go`. |
| `-list-models` | off | flag | Terminal action: prints `ID`, `NAME`, `DIALECT`, `CONTEXT`, `OUTPUT`, `STATUS`, `EFFORT` and exits; no session, task, or token. `EFFORT` is comma-separated normalized values or `-`. No separate output cap; bounded by the catalog size. |
| `-refresh-models` | off | flag | Terminal action: unconditional catalog fetch, atomic cache replace, one-line summary, then exits; no session, task, or token. Bounded by the catalog timeout/body caps; no new budget. |
| `-max-turns` | 0 (unlimited) | flag, `ACODE_MAX_TURNS`, `max_turns` | Cumulative when explicit and positive; `0`/omitted unlimited; negative is a config error. |
| `-max-total-tokens` | 0 (unlimited) | flag, `ACODE_MAX_TOTAL_TOKENS`, `max_total_tokens` | Cumulative when explicit and positive; `0`/omitted unlimited; negative is a config error. |
| `-max-context-tokens` | 0 (unset) | flag, `ACODE_MAX_CONTEXT_TOKENS`, `max_context_tokens` | Use the catalog context window. When explicitly set it must be positive and is clamped to the model ceiling with a warning. |
| `-max-output-tokens` | 0 (unset) | flag, `ACODE_MAX_OUTPUT_TOKENS`, `max_output_tokens` | Omit the request field. A positive value is sent unchanged; `0` clears an inherited budget; a negative value is a config error. |
| `-require-usage` | off | flag, `ACODE_REQUIRE_USAGE`, `require_usage` | Opt-in fail-closed accounting: a completed buffered response without provider usage is a `protocol_error` before tools run. Per-run, not inherited. |
| `-thinking-effort` / `ACODE_THINKING_EFFORT` | unset | flag, env, `thinking_effort` | Provider/model default. An explicit non-default value must be advertised by the effective model and match the 32-byte syntax rule; `default` clears. Resume/fork inherit the latest recorded value. Invalid or unsupported values exit 2. |
| `-session-timeout` | 0 (none) | flag, `ACODE_SESSION_TIMEOUT`, `session_timeout` | Per run when explicit and positive; `0`/omitted no deadline; negative is a config error. |
| `-model-timeout` | 0 (none) | flag, `ACODE_MODEL_TIMEOUT`, `model_timeout` | Per model attempt when explicit and positive; `0`/omitted no harness deadline; negative is a config error. |
| `-http-timeout` | 15 minutes | flag, `ACODE_HTTP_TIMEOUT`, `http_timeout` | Outer bound on a provider request, including body reads. Must be positive in every layer. |
| `-catalog-timeout` | 15 seconds | flag, `ACODE_CATALOG_TIMEOUT`, `catalog_timeout` | Bound on one catalog request, including body reads. Must be positive; valid for terminal catalog actions too. |
| `-log-requests` / `ACODE_LOG_REQUESTS` | off | flag, env, `log_requests` | Records each projected model request as a debug event; the aharness request cap applies (see the [SDK limits](https://github.com/eddiectc/aharness/blob/main/docs/limits.md)). `/request` displays the latest one. A saved change applies at the next startup. |
| `-task` / `-task-file` | no read cap | flag | Per-invocation only; effective transcript text is 64 KiB. |
| Workspace instructions | 16 KiB | — (fixed policy) | New sessions only: the workspace-root `AGENTS.md`, read once through `tools.Workspace.ReadDisplayFile`. An absent or empty file changes nothing. A larger, symlinked, non-regular, or unreadable file is skipped with one warning and the session still starts. The text is recorded in `session_start.system_prompt` and `session_start.appended_prompt`; `appended_prompt` is provenance, not a replay key. Resume and fork replay the recording and never re-read; fresh delegated children inherit the recorded text. |
| Steering message | 64 KiB | TUI | Composer submission while a run is active. |
| Session selection | UUIDv7 ID | flag | `-resume`/`-fork` resolve under the state directory; no arbitrary paths. |
| Provider token | no length cap | `ACODE_PROVIDER_TOKEN` only | In memory only; never logged or passed to tools; deliberately not a flag or config key. |
| Provider base URL | no length cap | flag, env, `provider_base_url` | Explicit override must be http(s) with a host; userinfo and query/fragment are rejected. Discovered URLs are HTTPS `opencode.ai`. |
| SearXNG base URL | 8 KiB | flag, `ACODE_SEARXNG_URL`, `searxng_url` | `web_search` backend. Absolute http(s); HTTP only for loopback hosts; userinfo, query, and fragment are rejected; a path prefix is allowed and trailing slashes are trimmed. Empty disables search while the tool remains registered. |
| Web request timeout | 30 seconds, max 120 | flag, `ACODE_WEB_TIMEOUT`, `web_timeout` | `web_fetch` request bound. Must be positive in every layer; zero, negative, or above 120 seconds is a config error. Independent of `http_timeout`. |
| `-web-fetch-provider` | `builtin` | flag, `ACODE_WEB_FETCH_PROVIDER`, `web_fetch_provider` | One of `builtin` or `crawl4ai`; selected once at startup with no fallback. |
| Web provider endpoint | 8 KiB | flag, `ACODE_WEB_FETCH_URL`, `web_fetch_url` | Empty for builtin; required for crawl4ai. Absolute http(s), no userinfo or fragment; loopback and private hosts are allowed because it is the operator's service. |
| Web provider key | 4 KiB | flag, `ACODE_WEB_FETCH_KEY`, `web_fetch_key` | Optional bearer token, sent only to the provider and never logged. Control characters are rejected. |
| Catalog URL | no length cap | env, `models_dev_url` | Default `https://models.dev/api.json`; must be HTTPS except loopback HTTP. |
| State directory path | OS path limit | flag, `ACODE_STATE_DIR` | No explicit application cap. Deliberately not a config key: it locates the default config file. |
| Session log retention | none | — | No rotation or automatic deletion. |

### 1.1 What is worth configuring

A limit belongs in the flag/env/config surface when two operators could
reasonably disagree for the same binary:

- **Worth configuring:** the HTTP client timeout, the per-call model timeout,
  the session wall clock, the turn/total-token budgets, the context/output
  budgets, thinking effort, the catalog timeout and URL, the SearXNG base URL,
  and the model / provider / base-URL / workspace selectors.
- **Intentionally fixed:** body and stream caps, tool result caps, the search
  response/result/output/snippet caps, the 15-second search timeout, the
  session line and schema versions, file modes, retention, the retry policy,
  the 16 KiB workspace-instructions cap and its skip rule, UI display caps,
  and the `-ui-json` protocol caps. Each defines the wire format, the replay
  contract, or a display-only safety boundary.

## 2. TUI and `-ui-json`

These caps are display-only; none changes the session log or the model
contract. See [tui.md](tui.md) for shipped behavior and
[commands.md](commands.md) for slash commands.

| Cap | Value | Behavior |
|---|---|---|
| Task/steering message | 64 KiB | Longer is rejected with a notice; the run continues. |
| External editor draft | 64 KiB | `F4` reads the edited temp file under the composer cap; an over-cap or failed edit leaves the composer unchanged. Idle only. |
| Transcript items | 20,000 | Oldest UI items are dropped with an "older history omitted" marker. |
| Transcript text | 16 MiB | Oldest UI items are dropped; the log is unchanged. |
| Assistant text per item | 64 KiB displayed | Truncation marker; the full response stays in the log. |
| Thinking text per item | 64 KiB displayed | Display-only; truncation marker. Never logged, so a resumed session shows no prior thinking. The active adapter keeps a separate bounded in-memory copy for tool-round continuation. |
| Markdown source per item | 64 KiB | Longer is rendered as plain text with a marker. |
| Markdown blocks per item | 4,000 | Excess input falls back to plain text with a notice. |
| Markdown nesting | 16 levels | Deeper input falls back to plain text with a notice. |
| Table columns / rows | 12 / 500 displayed | Column width cap 40 cells; excess scrolls/folds with a marker. |
| Widget list / file picker entries | 1,000 / 2,000 per directory | File picker does not recurse, read contents, follow symlinks, or list `.git`; filter query is 64 bytes. |
| Resume picker sessions / head | 100 per workspace / 64 events | Same canonical workspace only; the active session is excluded; unreadable, linked, or corrupt logs are skipped; each row preview is sanitized and cut to 160 bytes. |
| Overlay stack | 4 | Deeper requests close the oldest non-modal overlay with a notice. |
| Agent roster | 8 rows | Header and a `+N more` row; scrolls; every cell is sanitized and truncated to its column or the terminal width. |
| Agent settings editor | 200 bytes | One `agent_overrides` value; longer input is refused with a footer error, and an empty value clears the override. |
| System prompt view | SDK-recorded prompt, whole | `/prompt` shows the recorded text; when the SDK's record cap truncated it, the header says so. Long lines wrap at the overlay width. |
| Model exchange view | 64 KiB per payload (32 KiB head + 32 KiB tail) | `/request` keeps both ends of the pretty-printed recorded payload before sanitization and inserts one `[omitted N bytes]` marker counting bytes of that payload. The session log keeps the full event. Long lines wrap. |
| Config settings editor | 512 bytes | One config-file value; longer input is refused, values use the startup validation rules, and `web_fetch_key` is masked and never rendered. |
| Image file / dimensions | 8 MiB / 4096×4096 | Larger files are rejected with a placeholder. `image.DecodeConfig` runs first. |
| Decoded image pixels | 16 Mi (4096×4096 max) | Larger images are rejected before full decode. |
| Images per transcript | 32 | Oldest displayed thumbnails are dropped; the transcript item remains. |
| Image thumbnail | 60×20 cells or available width | Half-block glyph preview; Kitty only in the viewer. |
| Tool output | 4 KiB or 40 lines expanded | Collapsed shows the last seven rendered rows of the full result; expanded output never exceeds the logged tool-result cap. |
| Transcript fold | 7 rendered rows | Thinking and tool bodies are collapsed by default to the header, an elision row with the hidden rendered-row count, and the tail. `F3` expands all folds; the mode is sticky and display-only for this process, and `/settings` can persist the initial mode as `ui_expand_all`. |
| Todo widget | 8 rows | One header plus up to seven one-line items, each truncated to the available width. The window keeps the in-progress (or first pending) item visible and reports hidden items as a count. Hidden when the list is empty, there is no room, or the slash-command menu is open. Derived from the last successful `todo` result's `details`; not a new durable event. |
| Tool intent title | 200 bytes | Shared display-only `intent`, trimmed and cut on a rune boundary; tool blocks and the `/agent-hub` roster both use it. A missing intent falls back to the raw argument preview and never fails a call. |
| Tool arguments preview | 2 KiB or 8 lines | Full arguments remain in `model_response`/the log. |
| Input history | 100 entries | In-memory only; not persisted. |
| Minimum terminal size | 40×10 | Below shows a resize message; quit/resize keys still work. |
| Frame coalescing | 33 ms | Text deltas coalesce (~30 fps); state changes flush immediately. |
| `-ui-json` frame settle | 30 ms | The driver waits this long after a command before reading a frame, because Bubble Tea exposes no renderer barrier; reached only with `-ui-json` and with no durable effect. |
| Escape timeout | 50 ms | A lone Esc is delivered; modified sequences never interrupt. |
| Mouse wheel | 3 lines per notch | Scrolls the transcript, or the top overlay when one is open. Clicks, releases, motion, and horizontal wheel events are ignored; focus reporting stays off. Terminal text selection and middle-click paste need the terminal's `Shift` bypass; see [tui.md](tui.md). |
| Display queue | 256 events | Backpressure; text deltas are not durable. |
| Live child view | Shared 256-event display queue, 33 ms coalescing | The focused child's display events travel on the run's queue and render on the same coalescing tick as the parent transcript. A full queue applies backpressure to whichever producer sends next, parent or child; the sink does not block once the run stops, and no child sink outlives the runner. A child log snapshot at each progress or finish boundary replaces the live list; live child assistant text is capped at 64 KiB, tool arguments at 2 KiB, and a child tool body at 12 rows or 2 KiB. |
| Slash-command menu | 8 rows | Filtered by command name/alias; scrolls to the selection; shrinks to keep the header, one transcript row, composer, and status visible. |
| TUI catalog refresh | `Ctrl-R` in the model picker | Idle only; reuses the catalog timeout/body/cache caps; no `-ui-json` protocol version change. |
| Thinking effort picker | `F2`; model's normalized list | Idle only; lists `provider default` plus the model's values; no overlay when the model advertises none; next run only; no protocol version change. |
| `-ui-json` command line | 1 MiB | Longer lines are a protocol error (exit 2). |
| `-ui-json` text/paste | 64 KiB per command | Longer text is rejected with a protocol error. |
| `-ui-json` frame size | 20×5–500×200 | `-ui-size`/`resize` outside the range is a config/protocol error. |
| `-ui-json` wait timeout | 30,000 ms default; 300,000 ms max | A timeout emits `error{code:"timeout"}` and does not exit. |
| `-ui-json` frame line | 1 MiB | Over-cap emits `frame_too_large` and continues. |
| `-ui-json` style spans | 16,000 or 256 KiB encoded | Excess styles are dropped; `styles_truncated` is true and `lines` stay complete. |
| `-ui-json` snapshot | 512 KiB SVG payload | Over-cap emits `snapshot_too_large` and continues. |

## 3. No cap or no separate cap

Child turns have no separate fixed cap: the definition's `max_turns`, then the
parent's explicit `-max-turns`, then unlimited. The real bound is observation
plus `Esc`: `SubagentProgress` is forwarded through the parent display,
`/agent-hub` shows a moving clock, and a batch-created session's root grant
never offers `delegate`. Tokens are different: a child always spends inside the
parent's call, so its tokens count against `-max-total-tokens` and cannot
escape the budget.

These are intentionally bounded only by an enclosing limit, not by a dedicated
constant:

- Total session log size and total state-directory size.
- `-task`/`-task-file` read size; the transcript truncates at 64 KiB.
- Search walk time and file-tool execution time; any explicit run deadline
  applies.
- Command length, `old_string`/`new_string` length, and search pattern/glob
  length; these are indirectly bounded by the model-response cap and tool
  result caps.
- Number of tool calls per response; bounded by the response cap.
- Number of events per session; bounded by the turn, token, and time budgets
  only when those explicit flags are set.
- Thinking effort values; bounded by the model's normalized catalog list, whose
  value and byte caps are the SDK's
  ([SDK limits](https://github.com/eddiectc/aharness/blob/main/docs/limits.md)).
  No run budget is added.
- Number of concurrent sessions; one process holds one active session and one
  lock at a time. `/new` and `/resume` replace the active session while idle;
  there is no global cap.
- Child process memory and CPU; external isolation is the boundary.
- Workspace size; file tools enforce their own file-size caps.
- Provider token length; held in memory only.
- Session retention; old logs remain until the operator removes them.
