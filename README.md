# acode

acode is a terminal coding agent for a single workspace. It runs the
[aharness](https://github.com/eddiectc/aharness) SDK's model/tool loop against
the files and commands it is allowed to use, appends every event to a durable
JSONL session log, and ends a run with a final answer or a typed stop reason.
One binary carries a full-screen TUI, a one-shot batch mode, and a headless
`-ui-json` protocol over the same sessions.

acode is the application layer: it owns the CLI, TUI, configuration, signals,
and presentation. The loop, model adapters, tools, session store, and SDK
contracts live in aharness. The default state directory is `~/.acode`.

## Install

```sh
make build        # go build -trimpath -o bin/acode ./cmd/acode
make install      # install to ~/.local/bin; override with INSTALL_DIR
```

A tagged release publishes `linux` and `darwin` tarballs on `amd64` and
`arm64`, with a `SHA256SUMS` file and a build attestation, and the container
image carries the same binary, so neither path needs a Go toolchain
([releases](https://github.com/eddiectc/acode/releases),
[container.md](docs/container.md)).

## Quick start

```sh
export ACODE_PROVIDER_TOKEN=...   # omit for a local no-auth endpoint

./bin/acode -list-models          # usable catalog; -refresh-models refetches
./bin/acode -resume <session-id>  # continue a session in place
./bin/acode -fork <session-id>    # fork it into a new one

# Open the TUI in a workspace, optionally with a first message.
./bin/acode -model <model-id> -workspace /path/to/repo
./bin/acode -model <model-id> -task "Fix the failing test"
```

`-model` (or `ACODE_MODEL`) must name a usable catalog model for a new session.
In the TUI and `-ui-json` the task is optional: the session is created eagerly
and the first submitted message supplies it. A non-terminal invocation batches
automatically, and `-batch` forces it.

## Feature map

Each area links to the reference that owns its detail.

- **Terminal workbench** — rich Markdown, workspace images, foldable thinking
  and tool blocks, a todo panel, and every key and command behind `F1`
  ([tui.md](docs/tui.md), [commands.md](docs/commands.md)).
- **Models and thinking effort** — the `opencode-go` provider, a models.dev
  catalog with model and effort pickers, and a per-run reasoning effort
  ([configuration.md](docs/configuration.md), [limits.md](docs/limits.md)).
- **Sessions** — append-only v2 JSONL logs, `-resume`, `-fork`, and `/new`
  ([architecture.md §6](docs/architecture.md#6-sessions-and-state)).
- **Workspace instructions** — the workspace root `AGENTS.md` composed into a
  new session's prompt, recorded, and inherited by delegated children
  ([architecture.md §1](docs/architecture.md#1-role-and-boundaries)).
- **Delegated subagents** — up to four children per call from five shipped
  roles, with a live `/agent-hub` roster and child focus
  ([tui.md](docs/tui.md),
  [configuration.md §2.2](docs/configuration.md#22-agent-overrides)).
- **Batch and headless** — one-shot output and the versioned `-ui-json`
  protocol of frames, events, and SVG snapshots
  ([architecture.md §4](docs/architecture.md#4-modes-and-selection),
  [tui.md](docs/tui.md#-ui-json-protocol-v1)).
- **Configuration** — flag > environment > config file > default, written
  through `/settings` and `/agents`
  ([configuration.md](docs/configuration.md)).
- **Web tools** — operator-configured SearXNG search and a native HTML fetch
  ([configuration.md](docs/configuration.md), [limits.md](docs/limits.md)).
- **Isolation** — no built-in sandbox and no approvals: run acode under
  bubblewrap, a container, or a VM ([isolation.md](docs/isolation.md)).
- **Limits and exit codes** — every cap and the status-to-code mapping
  ([limits.md](docs/limits.md),
  [architecture.md §7](docs/architecture.md#7-exit-codes)).

## Documentation

The [documentation map](docs/README.md) indexes every reference document and
build plan and says where a fact belongs; SDK contracts are documented in
[aharness](https://github.com/eddiectc/aharness).

## Contributing

Follow [AGENTS.md](AGENTS.md), the [coding standard](docs/coding-standard.md),
and the [definition of done](docs/definition-of-done.md); `make dod` is the
gate. Keep SDK changes in aharness. `main` is protected: work on a short-lived
branch, keep history linear, and merge only with `make dod` green. A release is
an annotated `vX.Y.Z` tag on a green `main` commit
([release.md](docs/release.md)).

AI agents author and review much of this repository; a person is accountable for
every merge, and the rules and gates above apply unchanged.
