# acode

acode is a terminal coding agent for a single workspace. It runs the
[aharness](https://github.com/eddiectc/aharness) SDK's model/tool loop against
the files and commands it is allowed to use, appends every event to a durable
JSONL session log, and ends a run with a final answer or a typed stop reason.
It ships a full-screen TUI, a one-shot batch mode, and a headless `-ui-json`
protocol over the same sessions.

acode is the application layer: it owns the CLI, TUI, configuration, signals,
and presentation. The loop, model adapters, tools, session store, and SDK
contracts live in aharness. The default state directory is `~/.acode`; this is
a new project, so there is no migration from an older aharness state
directory.

## Build and run

```sh
make build        # go build -trimpath -o bin/acode ./cmd/acode
make install      # install to ~/.local/bin; override with INSTALL_DIR

export ACODE_PROVIDER_TOKEN=...   # omit for a local no-auth endpoint

# Print the usable catalog, or force a refresh of the 24-hour cache.
./bin/acode -list-models
./bin/acode -refresh-models

# Open the TUI in a workspace, optionally with a first message.
./bin/acode -model <model-id> -workspace /path/to/repo
./bin/acode -model <model-id> -task "Fix the failing test"

# Resume a session in the TUI, or fork it into a new one.
./bin/acode -resume <session-id>
./bin/acode -fork <session-id>
```

Building requires Go 1.27.4 or newer; the module pins a tagged aharness
release. A tagged release also publishes tarballs for `linux` and `darwin` on
`amd64` and `arm64`, with a `SHA256SUMS` file and a build attestation, on the
[releases page](https://github.com/eddiectc/acode/releases), and the container
image carries the same binary, so neither of those paths needs a Go toolchain.

In the TUI and `-ui-json`, the task is
optional: a new session is created eagerly, the header shows its ID, and the
first submitted message supplies the task. `-model` (or `ACODE_MODEL`) is
required for a new session and must name a usable model in the catalog.

## Models and thinking effort

The CLI's only provider is `opencode-go`, selected by `-provider` or
`ACODE_PROVIDER`. A new session resolves its model's dialect, base URL,
context window, and input cap from [models.dev](https://models.dev), cached for
24 hours under `<state-dir>/cache/models.dev/<provider>.json`. A fresh cache
avoids the network; a stale cache refreshes conditionally, and a failed
implicit refresh falls back to stale data with a warning. `-refresh-models` and
`Ctrl-R` in the model picker force an unconditional refresh.

`-list-models` prints the usable catalog without creating a session, including
each model's advertised thinking efforts. `-thinking-effort` (or
`ACODE_THINKING_EFFORT`) sends the selected `reasoning_effort` on Chat
Completions or `reasoning.effort` on Responses; unset uses the provider/model
default, and `default` clears an inherited value. The effective effort is
recorded and inherited on resume or fork unless changed. In the TUI, `Ctrl-P`
opens the model picker and `F2` opens the effort picker for the next run.

Resume and fork resolve the recorded provider, model, dialect, base URL,
limits, and effort without a catalog fetch. An explicit model change
re-resolves through the catalog, and an explicitly unsupported effort is a
startup error.

## The terminal workbench

The transcript renders assistant text as rich Markdown with table overlays and
displays workspace-contained images; `Ctrl-R` toggles rich and raw rendering.
`Ctrl-T` opens the most recent table, `Ctrl-G` the most recent image, and
`Ctrl-O` a workspace file picker. Model reasoning appears as a display-only
`thinking` block; it is never logged, projected, replayed, or printed in batch
mode. `F3` folds or expands thinking and tool blocks.

The `todo` tool maintains a checklist: send items to replace it, omit items to
read it, or send an empty list to clear it. At most one item is `in_progress`.
The TUI shows the checklist in a persistent panel above the composer. Tool
calls show their `intent` as the block title, and delegated work gets a
foldable block per call.

The composer is multi-line: `Enter` submits, `Shift-Enter` or `Alt-Enter`
inserts a newline, and `F4` edits the draft in `$VISUAL` or `$EDITOR`. While a
run is active, `Enter` submits steering for the running agent, `Esc` interrupts
the in-flight operation, and a second `Ctrl-C` cancels the run. `Ctrl-D` quits.
The mouse wheel scrolls the transcript; because the TUI captures the mouse,
use `Shift`+drag to select text and `Shift`+middle-click or `Ctrl+Shift+V` to
paste.

| Key | Action |
|---|---|
| `Enter` / `Shift-Enter` | submit or steer / insert a newline |
| `Esc` | close an overlay; otherwise interrupt the operation or clear the composer |
| `Ctrl-C` | during a run: interrupt, then cancel; idle: clear the composer |
| `Ctrl-D` | quit |
| `Ctrl-P` / `F2` | model picker / thinking-effort picker |
| `Ctrl-O` / `Ctrl-G` / `Ctrl-T` | file picker / image viewer / table overlay |
| `Ctrl-R` / `F3` | rich or raw rendering / fold or expand blocks |
| `F1` / `Ctrl-X` | help / session and terminal details |
| `F4` | edit the composer in `$VISUAL` or `$EDITOR` |

Typing `/` opens a filtered command menu; `Tab` completes the highlighted
command and `Enter` runs it. The commands are `/help`, `/model`, `/effort`,
`/provider`, `/details`, `/settings`, `/agents`, `/agent-hub`, `/new`,
`/resume`, `/files`, `/table`, `/image`, `/rich`, `/raw`, `/repaint`,
`/refresh`, `/interrupt`, `/cancel`, and `/quit`. Unrecognized `/text` is sent
as a normal message. The full reference is in [commands.md](docs/commands.md).

## Sessions, resume, and fork

Sessions are append-only v2 JSONL logs under
`<state-dir>/sessions/<id>.jsonl`; the state directory defaults to
`~/.acode` and can be moved with `-state-dir` (or `ACODE_STATE_DIR`).
Logs live outside the workspace. A process holds one active session and an
exclusive `flock` on its log; `/new` replaces the active session and `/resume`
reopens a previous session in the same workspace while idle.

`-resume` continues a session in place. `-fork` creates a new self-contained
session with parent metadata and copies the history under the forked contract.
Both open the TUI by default; add `-batch` for one-shot output. Each run
records turns, provider usage totals and subsets, and an observational cost
estimate from the catalog price snapshot. The estimate is not a billing
authority.

## Workspace instructions

On a new session, acode reads the workspace's root `AGENTS.md` (up to 16 KiB)
and composes it into the system prompt after the built-in rules and, in the
TUI, the agent catalog. The text is recorded once in the session log as
`system_prompt` and again as `appended_prompt`, so a fresh delegated child
inherits the same workspace rules after its role body. Resume and fork replay
the recorded prompt; they never re-read the file, and a log written before
`appended_prompt` existed seeds no children. `/new` is the way to pick up a
changed file.

A missing or empty file changes nothing. A file over 16 KiB, a symlink, a
directory, or an unreadable file is skipped with a warning and the session
still starts; the model can still open a larger file with `read_file`. The
file is workspace content, not executable config: acode does not parse it or
run anything from it, and a cloned repository can therefore inject
instructions into the model's context.

## Delegated subagents

The `delegate` tool runs up to four child agents in one call. Each child is a
separate session under `<state-dir>/sessions`, linked by `parent_session` and
`parent_call_id`, with its own resolved prompt, tool grant, model, effort,
provider, usage, and pricing. A child cannot delegate further.

Five agents ship with acode: `explore`, `researcher`, `reviewer`,
`implementer`, and `task`. Files in `<state-dir>/agents/*.md` override them by
exact name; an override uses a strict `key: value` header and a Markdown prompt
body. A call runs concurrently (up to four) when every task's grant is limited
to `read_file`, `search`, `web_search`, and `web_fetch`; any write-capable task
forces the whole call to run sequentially.

The TUI renders one foldable block per delegated call. `/agent-hub` shows a
live roster of running children with agent, intent, model, effort, elapsed,
context tokens, turns, cost, and status. `Enter` on a roster row focuses that
child: its read-only transcript replaces the parent view, and composer input
becomes steering for the child; `Esc` returns to the parent without
interrupting it. `/agents` browses the definitions and edits an agent's
`model`, `thinking_effort`, `max_turns`, and `enabled` through the
`agent_overrides` config object. Tool grants and prompts stay file-only, so the
settings surface cannot widen authority.

Child input and output tokens count against `-max-total-tokens`. Child turns do
not count against `-max-turns`, but seed the child's default cap.
`-max-subagents` (default 8, `0` unlimited) bounds fresh children per run. A
session's tool grant is fixed for its lifetime, so delegation requires a
session whose grant includes the `delegate` tool. A session first created by
`-batch` records the full toolset as its session grant and narrows only the
root through `root_tools`, which omits `delegate`; it is therefore permanently
non-delegating, even if a TUI resumes it later, because an unattended run has
no way to observe or steer children. A session created by the TUI records no
root grant and stays delegating.

## Batch and headless modes

`-batch` forces the one-shot CLI: final assistant text goes to stdout,
diagnostics go to stderr, and the exit code reports the result. A new session
needs exactly one of `-task` or `-task-file`; `-fork` requires a task;
`-resume` requires a task only after a completed run. `-stream` opts into
assistant-text streaming; the TUI and `-ui-json` always stream, and deltas are
display-only.

`-ui-json` runs the same Bubble Tea program headlessly over a versioned JSONL
protocol of frames, semantic events, and deterministic SVG snapshots, with no
PTY required. `-ui-size` sets the initial frame size. Without `-ui-json`,
non-TTY invocations batch automatically.

## Configuration

Configuration comes from flags, environment variables, an optional JSON config
file, and built-in defaults, in that order. The file defaults to
`<state-dir>/config.json` and can be selected with `-config` or
`ACODE_CONFIG`; `-config ""` disables it, and unknown keys are rejected.

The common operational keys cover the model, workspace, provider base URL,
thinking effort, run turn and token budgets, context and output caps, timeouts,
`require_usage`, and `log_requests`. `searxng_url` configures `web_search`;
`web_timeout`, `web_fetch_provider`, `web_fetch_url`, and `web_fetch_key`
configure `web_fetch`. `agent_overrides` routes an agent's model, effort, turn
cap, and enable state. `ui_rich` and `ui_expand_all` set the TUI's initial
rendering and fold modes. Run budgets default to unlimited.

`/settings` lists every config-file key with its saved or effective value and
the layer that supplies it, then writes edits atomically. It masks
`web_fetch_key` and applies display preferences immediately. `/agents` writes
`agent_overrides` through the same writer. See
[configuration.md](docs/configuration.md) and [limits.md](docs/limits.md).

## Web tools

`web_search` queries an operator-configured SearXNG instance with
`format=json` and returns up to ten titles, URLs, and snippets. Set
`-searxng-url`, `ACODE_SEARXNG_URL`, or `searxng_url`; HTTP is allowed only
for loopback instances. With no endpoint, the tool stays registered and
returns an actionable `unavailable` result.

`web_fetch` retrieves one absolute http(s) URL and returns plain text. HTML is
stripped of scripts, styles, navigation, and boilerplate by a native Go
extractor; text, Markdown, JSON, XML, and CSV pass through. Results are capped
at 60 KiB with `offset` continuation, and empty or app-shell pages report
`content_unavailable` rather than empty success. The builtin client refuses
private, loopback, link-local, and metadata addresses at dial time, follows at
most five same-host redirects, and sends no credentials or cookies. For
JavaScript-heavy pages, `-web-fetch-provider crawl4ai` uses an operator-run
Crawl4AI endpoint (`-web-fetch-url`, optional `-web-fetch-key`);
`web_timeout` defaults to 30 seconds and is capped at 120.

## Isolation

acode does not sandbox itself and has no approval workflow. Tool calls write
to the real workspace and can use the host network. Run it inside bwrap,
Docker, or a VM whenever the repository or the task is untrusted. The TUI adds
no approval step or sandbox.

### Sandboxed runs with bubblewrap

[`scripts/acode-bwrap`](scripts/acode-bwrap) runs the binary under
bubblewrap with a minimal read-only root. The project and `~/.acode` stay
writable, the rest of `$HOME` is hidden, and the script refuses a project that
is `/`, `$HOME`, or an ancestor of it.

```sh
cd /path/to/repo
scripts/acode-bwrap -model <model-id> -task "Fix the failing test"

# Select the project instead of using the current directory.
scripts/acode-bwrap --project /path/to/repo -model <model-id>

# Open a shell in the same sandbox to check what the agent can see.
scripts/acode-bwrap --project /path/to/repo --shell
```

The sandbox shares the host network, so it bounds filesystem and process
access, not egress: the agent can still reach the provider and anything else
the host can reach. Environment variables are inherited, which is how
`ACODE_PROVIDER_TOKEN` gets in.

The script is a sample. It preconfigures Go (`~/go/pkg` writable, `~/go/bin` and
`~/.local/bin` on `PATH`) and mounts `~/.gitconfig` and `~/.agents` read-only.
It does not mount credentials: `~/.ssh` and cloud or registry tokens stay
hidden. Another ecosystem needs its per-user toolchain mounted, which is
everything not under `/usr`:

| Ecosystem | Add to `ACODE_BWRAP_BIND` |
|---|---|
| Rust via rustup | `~/.cargo ~/.rustup` |
| Node via nvm | `~/.nvm` |
| Python via pyenv, or uv-managed interpreters | `~/.pyenv ~/.local/share/uv` |
| Toolchain installed with `go install`-style user prefixes | `~/go/bin`, `~/.local/bin` (already mounted read-only) |

Adding the ecosystem's cache (`~/.cache/go-build` for Go, `~/.cache/uv`,
`~/.npm`) avoids cold rebuilds and re-downloads. It is optional: without it the
tools still work, just slower.

```sh
ACODE_BWRAP_BIND="$HOME/.cargo $HOME/.rustup" \
  scripts/acode-bwrap -model <model-id> -task "Fix the failing test"
```

Paths must be absolute and are bound read-write, so the agent can modify them. A
project-local `.venv`, `node_modules`, or `target/` needs no entry: it lives
inside the project, which is already mounted.

### Containers

CI publishes a container image at `ghcr.io/eddiectc/acode` for `linux/amd64`
and `linux/arm64`, built from the same binaries it attaches to a tag's release.
It carries the binary, `/bin/sh`, git, and CA certificates, but no project
toolchain: derive an image from it to add the compilers and test runners a
workspace needs, or copy the binary into the project's own toolchain image when
CI and the agent must share a base.

```sh
podman run --rm -it \
  --userns=keep-id:uid=1000,gid=1000 \
  -v "$PWD:$PWD" -w "$PWD" \
  -v acode-state:/home/acode/.acode \
  --network=host \
  -e ACODE_MODEL -e ACODE_PROVIDER_TOKEN \
  ghcr.io/eddiectc/acode
```

Mount the workspace at its host path and keep the state directory in a named
volume: `<state-dir>` holds session logs, the config file, and the agent
definitions that carry tool grants, so it is a control plane rather than
disposable data. `-it` is required for the TUI; without it acode selects batch
mode. Use `--env-file` rather than `-e` so a token does not land on the command
line or in the shell history; it is still part of the container's `Env`, which
`inspect` prints.

[container.md](docs/container.md) covers UID mapping, secrets, host versus
isolated networking, a LiteLLM-style gateway, and how the image is built from
the release binaries. A full local stack — acode, LiteLLM, SearXNG, and
Crawl4AI — is in [compose.yaml](compose.yaml) and documented in
[container.md §1](docs/container.md#1-quick-start-the-compose-stack).
[`scripts/acode-docker`](scripts/acode-docker) and
[`scripts/acode-podman`](scripts/acode-podman) launch that stack from any
project directory, each run under its own randomly named container:

```sh
cd /path/to/repo
~/acode/scripts/acode-docker                        # or scripts/acode-podman
~/acode/scripts/acode-docker -model <model-id> -task "Fix the failing test"
```

`compose.local.yaml` beside `compose.yaml` is passed last when it exists, which
is where a machine-local credential mount or image override belongs: it stays
out of git and out of the tracked stack files
([container.md §5](docs/container.md#5-configuration-secrets-and-network)).

The published image carries no GitHub CLI or compiler.
[compose/Dockerfile](compose/Dockerfile) is the stack image: a `golang` base with
the released binary copied in, `gh` installed from its release `.deb`, and a
`github.com` credential helper in `/etc/gitconfig`, so an `https` push
authenticates through the same login. Another language copies it and changes the
base ([container.md §6](docs/container.md#6-building-the-image)).

## Documentation

| Document | Defines |
|---|---|
| [architecture.md](docs/architecture.md) | App structure, host composition, modes, exit codes, and the SDK boundary |
| [configuration.md](docs/configuration.md) | Flag > environment > config file > default precedence, the JSON config file, and environment variables |
| [limits.md](docs/limits.md) | CLI, input, TUI, and `-ui-json` caps; links to the SDK's limits |
| [coding-standard.md](docs/coding-standard.md) | Normative Go practice, dependencies, testing, and §8 review gates |
| [tui.md](docs/tui.md) | TUI modes, keys, `-ui-json` protocol, and caps |
| [commands.md](docs/commands.md) | Slash-command reference: syntax, autocomplete, arguments, key equivalents, idle/run behavior, `-ui-json` mapping, and caps |
| [container.md](docs/container.md) | Container image, release binaries, mounts, UID/GID mapping, secrets, network modes, the compose stack, and the release CI |
| [testing-strategy.md](docs/testing-strategy.md) | Config, integration, TUI/headless, fuzz, and manual smoke checks |
| [definition-of-done.md](docs/definition-of-done.md) | Completion checklist for every change |
| [release.md](docs/release.md) | Branch model, commit wording, pull-request gating, and release tagging |
| [docs/README.md](docs/README.md) | Documentation map and placement rules |
| [aharness SDK](https://github.com/eddiectc/aharness) | The loop, model, tools, session, and public facade this app builds on |

## Contributing

Follow [AGENTS.md](AGENTS.md), the [coding standard](docs/coding-standard.md),
and the [definition of done](docs/definition-of-done.md). `make dod` runs
`docs-check`, formatting, `go vet`, `golangci-lint`, `go test -race`, the
`bin/acode` build, and `govulncheck`. Keep SDK changes in the aharness
repository; acode contains no loop, tool, or provider logic.

`main` is protected: work on a short-lived `feat/`, `fix/`, or `docs/` branch,
keep history linear, and merge only with `make dod` green. A release is an
annotated `vX.Y.Z` tag on a green `main` commit, which publishes the container
image. See [release.md](docs/release.md).
