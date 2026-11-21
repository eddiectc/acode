# acode testing strategy

- **Date:** 2026-11-20
- **Status:** normative for the acode coding-agent application
- **Related:** [architecture](architecture.md),
  [coding standard](coding-standard.md), [limits](limits.md),
  [TUI](tui.md), [commands](commands.md), and the
  [aharness SDK testing strategy](https://github.com/eddiectc/aharness/blob/main/docs/testing-strategy.md)
- **Agent entry point:** [AGENTS.md](../AGENTS.md)

acode tests its application surfaces against the real aharness SDK. The SDK's
own unit, fuzz, and integration tests live in the aharness repository. Every
automated layer here is deterministic: no real network, no `time.Sleep`-based
waiting, no shared state outside `t.TempDir`, and fake provider/catalog/search
servers.

## 1. Config and unit tests

`cmd/acode` unit tests cover the layers that are pure application logic:

- flag, environment, and JSON config-file precedence for every key;
- duration/integer/boolean parsing, negative and zero semantics, and unknown
  keys;
- provider/model/catalog resolution, including the local models.dev fixture,
  base-URL policy, and inherited values on resume/fork;
- mode selection and the task matrix for TUI, `-ui-json`, batch new/resume/fork,
  and terminal actions;
- `/settings` reading, validation, atomic write, preservation of unrelated
  keys, file modes, and workspace-path refusal;
- agent overrides, model/effort selection, and session-selection helpers;
- the engine exit-code mapping for missing, locked, invalid, unsupported, and
  configuration-class session opens;
- the usage-line drift test: every `model.Usage` field is set to a unique value
  and must appear in the marshaled `-ui-json` line, so a new SDK field fails
  until the protocol is updated;
- build metadata and version output.

`internal/tui` unit tests cover display-only behavior:

- composer editing, history, submit vs steering, interrupts, and quit;
- slash-command parsing, aliases, arguments, completion, help, and per-command
  idle/active behavior;
- transcript rendering, folding, raw/rich markdown, tables, thinking, notices,
  tool blocks, todo widget, and status/details;
- model, effort, session, file, table, image, settings, agents, and child-view
  overlays;
- terminal sanitization and image path policy;
- run display lifetime: buffered events after the runner returns are drained in
  order without closing the channel, a stale SDK run ID cannot change the
  header, and a retry (`ModelStarted.Attempt`) is distinguishable from a new
  turn for the thinking block;
- headless `x/vt` cell/style/cursor projection, SVG snapshots, and the
  `-ui-json` command/event/frame encoders.

Use the fake host and Bubble Tea fake input/output for app tests. Do not assert
on real terminal cells; assert on the headless screen or decoded protocol.

## 2. Integration tests

Integration tests build the `acode` binary once per package run and execute it
against a temporary workspace and state directory. The standard harness:

- sets `ACODE_MODELS_DEV_URL` to an `httptest` models.dev fixture;
- sets `-provider-base-url` or `ACODE_PROVIDER_BASE_URL` to an `httptest`
  provider that serves Chat Completions or Responses, buffered or SSE;
- sets `ACODE_STATE_DIR`/`-state-dir` to a temporary directory;
- runs with a fake `HOME` where needed, and asserts stdout, stderr, exit code,
  session log contents, and the absence of leaked child processes.

Cover at least:

- batch new/resume/fork, streaming and buffered, tool rounds, usage/cost, and
  terminal statuses;
- both dialects and the catalog model picker/refresh actions;
- resume/fork after interruption, unpaired calls, and torn final lines;
- custom-tool refusal on the CLI, authorization, todo restoration, and
  delegation with child logs and folded accounting;
- the recorded session contract: prompt version, `appended_prompt` on new TUI
  and batch sessions, retention on resume, inheritance on fork, the acode
  identity line inside `system_prompt`, and the grant split (`tools` full,
  `root_tools` without `delegate` for a batch-created session, and a TUI resume
  of that session that cannot dispatch `delegate`);
- web search against a fake SearXNG and web fetch against fake builtin/Crawl4AI
  endpoints, including unconfigured and failure results;
- TUI and `-ui-json` end to end: startup/seed, streaming before completion,
  slash commands, settings, model/effort selection, resume/fork, steering,
  interrupt/cancel, delegation roster and child focus, and frame/snapshot caps;
- terminal safety: resize mid-stream, OSC 52/ESC/CSI injection in model and tool
  text, and clean restore on quit and signal.

The TUI tests use the fake host and `-ui-json` for deterministic automation; a
real PTY is reserved for the manual smoke.

## 3. Fuzzing

Fuzz the app trust boundaries:

- `FuzzSanitize` for terminal-safe text;
- `FuzzRenderMarkdown` for markdown/render limits;
- `FuzzDecodeCommand` for the `-ui-json` command decoder.

Fuzz targets must not panic, must keep output valid UTF-8, and must respect the
documented caps. Session, provider-stream, catalog, history, and tool fuzz
targets live in aharness.

## 4. Manual smoke

Run the manual matrix only when a change touches the actual terminal surface,
signals, images, or the real provider. Record the exact environment and any
pending checks.

- Real provider: new session, streamed tool round, steer, `Esc`/first
  `Ctrl-C`, second `Ctrl-C`, resume, fork, and a second message.
- Terminal: at least one local emulator and tmux at 80x24 and 200x60; mouse
  capture and `Shift` selection; resize during a stream; `F3` folding; `F4`
  editor; `/settings`, `/resume`, `/model`, `/effort`, `/agent-hub`, and
  `/agents`.
- `-ui-json`: drive `run`, `wait`, `resize`, and `snapshot` at both sizes; check
  frames, SVG, style truncation, and error codes.
- Images: Kitty and glyph fallbacks, oversized files, and a resumed transcript.
- Sandbox: `scripts/acode-bwrap` with the documented binds and `--shell`.
- Container: run `make dist` and then `make image`, then run the documented
  Podman and Docker invocations in [container.md](container.md), including
  `scripts/acode-docker` and `scripts/acode-podman` from two project
  directories at once with distinct container names. Check that the
  TUI starts with `-it`, that a state volume survives `--rm`, that the UID/GID
  mapping can write the workspace and the state directory, that `run_command`
  works (`/bin/sh` and git are present), and that `acode -version` reports a
  version and a VCS revision: the tag on a published image, the module version
  and the local revision on a `make dist` build.

A smoke that was not run is pending, not verified. Do not describe it
otherwise.
