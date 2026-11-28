# acode isolation

- **Date:** 2026-11-27
- **Status:** current for the execution boundary of the acode binary
- **Related:** [container](container.md) for the published image and the compose
  stack, [coding standard §9](coding-standard.md#9-appendix-verified-go-127-facts)
  for what `os.Root` does and does not stop, and the
  [aharness limits](https://github.com/eddiectc/aharness/blob/main/docs/limits.md)
  for the built-in tools' address and path guards.
- **Files:** [`scripts/acode-bwrap`](../scripts/acode-bwrap)

acode is not a sandbox and has no approval workflow, in batch or in the TUI. A
tool call writes the real workspace, `run_command` runs as the invoking user,
and the process reaches whatever network the host reaches. Run acode under
bubblewrap (§1), in a container (§2), or in a VM when the repository or the task
is untrusted.

The tools apply their own path, environment, and address guards
([container.md §5](container.md#5-configuration-secrets-and-network),
[coding standard §9](coding-standard.md#9-appendix-verified-go-127-facts)), but
a guard inside the process is not a boundary around it: the only real boundary
is the one you put around the process.

## 1. Bubblewrap

[`scripts/acode-bwrap`](../scripts/acode-bwrap) runs `acode` under bubblewrap
with a minimal read-only root: the project and `~/.acode` stay writable, the
rest of `$HOME` is hidden except a few read-only toolchain and config paths, and
a project that is `/`, `$HOME`, or an ancestor of `$HOME` is refused. A run
needs `acode` on `PATH` (`make install`); every invocation needs `bwrap`
installed.

```sh
cd /path/to/repo
scripts/acode-bwrap -model <model-id> -task "Fix the failing test"

# Select the project instead of using the current directory.
scripts/acode-bwrap --project /path/to/repo -model <model-id>

# Open a shell in the same sandbox to check what the agent can see.
scripts/acode-bwrap --project /path/to/repo --shell
```

The sandbox shares the host network, so it bounds filesystem and process access,
not egress: the agent still reaches the provider and anything else the host can
reach. Environment variables are inherited, which is how
`ACODE_PROVIDER_TOKEN` gets in. Credential files are not mounted: `~/.ssh`,
cloud and registry tokens, and the rest of `$HOME` stay hidden, but an
inherited environment variable is visible to the process.

The script is a sample. It binds `~/.gitconfig`, `~/.agents`, `~/go/bin`, and
`~/.local/bin` read-only when they exist, and `~/go/pkg` read-write, so an
inherited `PATH` finds a Go install. Another ecosystem's per-user toolchain
needs an entry in `ACODE_BWRAP_BIND`, a space-separated list of absolute
directory paths, each without spaces; each must exist and is bound read-write,
so the agent can modify it:

| Ecosystem | Add to `ACODE_BWRAP_BIND` |
|---|---|
| Rust via rustup | `~/.cargo ~/.rustup` |
| Node via nvm | `~/.nvm` |
| Python via pyenv, or uv-managed interpreters | `~/.pyenv ~/.local/share/uv` |
| Toolchain installed with `go install`-style user prefixes | `~/go/bin`, `~/.local/bin` (already mounted read-only) |

```sh
ACODE_BWRAP_BIND="$HOME/.cargo $HOME/.rustup" \
  scripts/acode-bwrap -model <model-id> -task "Fix the failing test"
```

Adding the ecosystem's cache (`~/.cache/go-build` for Go, `~/.cache/uv`,
`~/.npm`) avoids cold rebuilds; it is optional, and without it the tools still
work, just slower. A project-local `.venv`, `node_modules`, or `target/` needs
no entry: it lives inside the project, which is already mounted.

## 2. Containers

A container offers more control than §1: the mount set, the user the process
runs as, and the network mode. [container.md](container.md) covers the
published image, the compose stack
([§1](container.md#1-quick-start-the-compose-stack)), and the network modes
([§5](container.md#5-configuration-secrets-and-network)).
