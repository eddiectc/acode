# acode in a container

- **Date:** 2026-11-22
- **Status:** current for the published image, the compose stack, the launcher
  scripts, and the release binaries
- **Related:** [configuration](configuration.md), [architecture](architecture.md),
  [limits](limits.md), [definition of done](definition-of-done.md), and the
  [coding standard](coding-standard.md)
- **Files:** [`compose.yaml`](../compose.yaml),
  [`compose.podman.yaml`](../compose.podman.yaml),
  [`compose.local.yaml.example`](../compose.local.yaml.example),
  [`.env.example`](../.env.example),
  [`scripts/acode-docker`](../scripts/acode-docker),
  [`scripts/acode-podman`](../scripts/acode-podman),
  [`Dockerfile`](../Dockerfile), and
  [`compose/Dockerfile`](../compose/Dockerfile)

The image is `ghcr.io/eddiectc/acode`: the `acode` binary, `/bin/sh`, git, an SSH
client, and CA certificates, and **no project toolchain** — a run artifact, not a
build one. A compiler is yours to add (§6). It is not a sandbox, an approval
workflow, or a secret store: acode runs with the container user's privileges and
can reach anything the container can reach ([README](../README.md#isolation)).

## 1. Quick start: the compose stack

The launcher scripts start acode beside a LiteLLM gateway, SearXNG, and
Crawl4AI, already wired to the agent, and run it against the directory you
invoke them from:

```sh
cp .env.example .env         # once, in the stack checkout; set OPENCODE_API_KEY
cd ~/src/project
~/acode/scripts/acode-docker                 # Docker; edit ~/src/project
~/acode/scripts/acode-podman                 # Podman; arguments identical
~/acode/scripts/acode-docker -model <id> -task "..."   # flags go to acode
```

| Service | Role | Port (env) |
|---|---|---|
| `acode` | The agent, built from [`compose/Dockerfile`](../compose/Dockerfile) | none |
| `litellm` | Credential holder, pass-through | 4000 (`LITELLM_PORT`) |
| `searxng` | `web_search` backend | 8889 (`SEARXNG_PORT`) |
| `crawl4ai` | `web_fetch` renderer | 11235 (`CRAWL4AI_PORT`) |

Every port is published on `127.0.0.1` only, and each port variable moves the
listener, the healthcheck, and the URL acode uses together. The stack image adds
the Go toolchain, gh, and `golangci-lint` and `govulncheck` at the versions
[ci.yml](../.github/workflows/ci.yml) pins, so `go build`, `go test`, `make dod`,
and a push work on the workspace; it is built on first run. It also mounts the
credentials of §5, always and read-only, and a machine without them still runs.

| Variable | Set in | Effect |
|---|---|---|
| `ACODE_WORKSPACE` | the shell | Edit a project other than the working directory. |
| `ACODE_TTY` | the shell | `0` drops the TTY, for piped input. |
| `ACODE_BUILD` | the shell | `1` forces a rebuild of the stack image. |
| `ACODE_STACK_DIR` | the shell | Where the scripts look for `compose.yaml`. |
| `ACODE_IMAGE` | `.env` | The released image `compose/Dockerfile` copies acode out of. |
| `ACODE_UID`, `ACODE_GID` | `.env` | The image user, when it is not 1000 (§4). |

The shell column matters: the launchers export their own `ACODE_WORKSPACE` and
`ACODE_STACK_DIR` before calling Compose, and a shell variable beats `.env`, so
a value set only in `.env` is ignored there.

Each invocation gets a one-off container named `acode-<8 hex>`, so instances do
not collide and `docker logs acode-<suffix>` addresses one afterwards. The
scripts print that name to stderr and pass acode's exit code through.

### Workspace, state, and settings

The workspace is `$PWD`, mounted at the same absolute path inside the container,
because a session records its workspace and `-resume` reuses that path. State is
the `acode-state` volume, not a host bind: `config.json`, `sessions/`, and
`agents/` live under `<volume>/_data/` on the host.

```sh
podman volume inspect -f '{{.Mountpoint}}' acode-stack_acode-state
```

To change a setting, edit `.env` and re-run the launcher: the compose environment
(`ACODE_MODEL`, the URLs, the token) wins over the config file, so editing
`model` there has no effect while the stack runs. `/settings` in the TUI edits
the config-file keys and shows which layer won (`env`, `flag`, `file`, or
`current`); the file itself is `_data/config.json`, owned by the image user.

### Podman

The Podman launcher merges [`compose.podman.yaml`](../compose.podman.yaml), which
adds `keep-id` so the image user can write the workspace, and `in_pod: false` so
each service gets its own namespaces. Do not merge it into Docker or rootful
Podman, which have no keep-id (§4).

A stack created before `in_pod` was disabled needs one manual teardown, because
podman will not make a pod container a dependency of one outside the pod and
`down` cannot remove the pod it no longer lists:

```sh
podman-compose -f compose.yaml -f compose.podman.yaml down
podman pod rm -f pod_acode-stack
```

`down` keeps the named volumes, so sessions survive. The
launcher checks for `pod_<project>` first and stops with these instructions.

### More than one acode at a time

Run a launcher per project; the generated names keep the containers apart. State
is shared, so two instances must not open the same session (an exclusive
per-session `flock`) and concurrent `/settings` writes are last-writer-wins; two
on the same workspace race on every edit. For separate state and ports, run a
second stack as its own project (`-p acode-2`, with other `SEARXNG_PORT`,
`LITELLM_PORT`, and `CRAWL4AI_PORT` values).

The healthchecks are informational: `depends_on` waits only for the services to
start, so the first model call may need a retry while LiteLLM boots.

## 2. The published image on its own

Without the stack, run the image directly and supply the endpoints yourself.
`-it` is required for the TUI, which is selected only when stdin and stdout are
terminals and `TERM` is set and not `dumb`
([architecture §4](architecture.md#4-modes-and-selection)); without it acode
falls back to batch mode and exits 2 unless a task is supplied.

```sh
podman run --rm -it --init \
  --userns=keep-id:uid=1000,gid=1000 \
  -v "$PWD:$PWD" -w "$PWD" -v acode-state:/home/acode/.acode \
  --network=host \
  -e ACODE_MODEL -e ACODE_PROVIDER_BASE_URL -e ACODE_PROVIDER_TOKEN \
  ghcr.io/eddiectc/acode

# Docker: --user "$(id -u):$(id -g)" instead of --userns, otherwise identical.

# No terminal, so batch mode needs a task.
podman run --rm -v "$PWD:$PWD" -w "$PWD" -v acode-state:/home/acode/.acode \
  --network=host -e ACODE_PROVIDER_TOKEN \
  ghcr.io/eddiectc/acode -model <model-id> -batch -task "Fix the failing test"
```

`--init` reaps orphaned children: acode kills a timed-out or cancelled
`run_command` process group through the SDK, but a tool that double-forks can
still leave a process behind.

## 3. Mounts

| Mount | Path | Why |
|---|---|---|
| Workspace | the project directory, read-write | The code the agent edits. Mount it at the **same absolute path** it has on the host. |
| State | `/home/acode/.acode`, a named volume | Sessions, the catalog cache, and the config file. |
| Build caches | named volumes | `/home/acode/go/pkg/mod` and `/home/acode/.cache/go-build`, so a rebuild is not cold. |
| Credentials | three read-only binds from `compose.yaml` | Always mounted: the `gh` login, the commit identity, and `.ssh` (§5). An absent one is simply absent. |

A session records its workspace and `-resume` reuses that recorded path, so an
identical absolute path keeps host-created and container-created sessions
interchangeable. On an SELinux host append `:Z` to the bind or the container user
cannot write it.

Keep the state directory in a volume rather than a read-write host bind: it is a
control plane, since `agents/*.md` carry tool grants, `config.json` carries the
base URL and run budgets, and `sessions/*.jsonl` holds every transcript. A tool
call runs as the container user and can write any absolute path, so a shared
bind lets an injected instruction rewrite those. Reset it with
`podman volume rm acode-state`.

A volume created by a different UID leaves the 0600 state files unreadable, so
seed the ownership under the same user namespace the run uses:

```sh
podman run --rm --userns=keep-id:uid=1000,gid=1000 \
  -v acode-state:/home/acode/.acode ghcr.io/eddiectc/acode -version
```

To share host configuration, mount the **file** read-only and select it with
`-config`; a save from `/settings` or `/agents` then fails and is reported in
the footer.

```sh
-v "$HOME/.acode/config.json:/etc/acode/config.json:ro" \
  -e ACODE_CONFIG=/etc/acode/config.json
```

## 4. User, UID, and GID

The image builds a non-root user with UID/GID **1000** and runs with a numeric
`USER`, so no name lookup is needed. Match it to the host user:

| Runtime | Invocation |
|---|---|
| Podman, rootless | `--userns=keep-id:uid=1000,gid=1000` |
| Docker | `--user "$(id -u):$(id -g)"` |
| Either, host UID is not 1000 | Rebuild with `--build-arg UID=... --build-arg GID=...` |

Pick IDs the base image does not already use: `debian:trixie-slim` has users up
to UID 42 and groups up to GID 100, so `GID=100` collides with its `users`
group. A UID mismatch is the most common first-run failure, because state files
are created 0600 and directories 0700. In the stack, set `ACODE_UID` and
`ACODE_GID` in `.env` instead: they are build arguments to
`compose/Dockerfile`, which creates the user itself.

## 5. Configuration, secrets, and network

The three layers still apply inside the container: flag, environment, config
file ([configuration](configuration.md)). The provider token is
**environment-only** — not a config key, so it cannot be baked into an image or
written to the volume — and `--env-file` keeps it off the command line and out of
shell history, which `-e` does not. It still reaches the container's `Env`, which
`inspect` prints.

`run_command` children get an environment allowlist (`PATH`, `LANG`, `LC_ALL`,
`TERM`, `HOME`, and a harness `TMPDIR`), and names ending in `_TOKEN`, `_SECRET`,
`_KEY`, or `_PASSWORD` are denied, so a tool call does not inherit the token by
accident. That allowlist is **not** a secrecy boundary — the child runs as the
same user, so `/proc/<pid>/environ` is readable — so bound the token at the
provider. `SSH_AUTH_SOCK` is not passed either; give the agent an HTTPS remote
and a credential file under `HOME`.

### Credentials for the agent

A credential the agent itself uses is a **file**, because an `_TOKEN` name is
stripped before `git`, `curl`, or `gh` sees it and the tool call is what has to
authenticate. The stack mounts the three paths below always and read-only, so
populating one is all it takes; a machine that has none still runs, because the
launchers create each missing path first as the invoking user:

```sh
install -d -m 0700 ~/.config/acode/gh ~/.config/acode/.ssh

# Commit identity, so `git commit` does not fail with "Committer identity unknown".
# The email must belong to the bot account, or GitHub shows it unattributed;
# <numeric-id>+<login>@users.noreply.github.com always works and hides yours.
printf '[user]\n\tname = acode-bot\n\temail = 12345+acode-bot@users.noreply.github.com\n' \
  > ~/.config/acode/gitconfig

# GitHub login for the bot account. --insecure-storage is required: the default
# keyring cannot cross a bind, and without it gh reads as logged out.
read -rs GH_PAT
printf '%s' "$GH_PAT" | env -u GH_TOKEN -u GITHUB_TOKEN \
  GH_CONFIG_DIR="$HOME/.config/acode/gh" \
  gh auth login --with-token --insecure-storage
unset GH_PAT

# An SSH key copy and GitHub's published host keys, in one directory so ssh
# finds known_hosts by default.
cp ~/.ssh/id_ed25519 ~/.config/acode/.ssh/          # a copy, not your only key
curl -sS https://api.github.com/meta \
  | python3 -c 'import json,sys;[print("github.com",k) for k in json.load(sys.stdin)["ssh_keys"]]' \
  > ~/.config/acode/.ssh/known_hosts
```

All three mounts are
read-only, so an injected instruction cannot re-pin a host key or rewrite the
identity, and they are plain binds rather than Compose `secrets:` entries, which
the two engines implement differently. The `gh` target is fixed because a tool
call gets `HOME` but neither `GH_CONFIG_DIR` nor `XDG_CONFIG_HOME`. `gh` is in
the stack image ([`compose/Dockerfile`](../compose/Dockerfile), §6).

A missing bind source does not stop the container. The engine would create it
itself, but as root under Docker and as a **directory** even where the config
mounts a file — and a directory at `~/.gitconfig` makes every git call fail with
`Is a directory`. The launchers therefore create the three paths first, as the
invoking user: directories for `gh` and `.ssh`, an empty file for `gitconfig`. So
a machine with no `~/.config/acode` runs with no login, no key, and no commit
identity, which is exactly what is absent.

Bound the credential where it is issued, because the mount cannot: a
fine-grained PAT for the one repository with `Contents: write`,
`Issues: write`, and `Pull requests: write`, a short expiry, and no
`Workflows: write`; protect `main` with a required pull request and no bypass.

### Network modes

A gateway is worth it for one reason: the agent container holds a gateway key
instead of the provider key (§1). `provider_base_url` accepts plain `http` for
**any** host, and a path prefix is allowed, so both of these work:

```sh
-e ACODE_PROVIDER_BASE_URL=http://127.0.0.1:4000/v1            # with --network=host
-e ACODE_PROVIDER_BASE_URL=http://litellm:4000/go-native/v1    # pass-through prefix
```

The model ID still has to exist in models.dev, which fixes the dialect, context
window, and limits, so a name that exists only in the gateway is a startup error
(exit 2). A pass-through gateway forwards that ID upstream verbatim; a
deployment-shaped gateway maps it in its own configuration, and only that kind
can enforce a model allowlist. Neither replaces a spending cap at the provider.

| Mode | Egress | Use when |
|---|---|---|
| `--network=host` | Unrestricted; a host gateway is reachable at `127.0.0.1` | The default for a trusted project. |
| A dedicated bridge network | Reaches only what is on that network | The project or its dependencies are untrusted. |
| `--internal` network plus the gateway | Reaches only the gateway | Inference is the only egress you want. |

An isolated network also stops `web_fetch`, `web_search`, and any
`go mod download`; `web_fetch`'s private-address refusal is a policy in one tool,
not a network boundary. `searxng_url` and `models_dev_url` are stricter than the
provider URL — HTTP only for loopback hosts — so a mirror must be reachable on
loopback or served over HTTPS.

The stack itself uses `network_mode: host` for that reason: a compose service
name is not loopback, and joining SearXNG's network namespace cannot work with
keep-id, because `setns()` needs `CAP_SYS_ADMIN` in the user namespace that owns
it. The cost is that a server a tool call starts binds host ports directly.

## 6. Building the image

The image does not compile acode. It copies `dist/linux/<arch>/acode`, the
binary a Go build produced, so the image, the release tarballs, and what
`acode -version` reports all come from one build:

```sh
make dist    # dist/linux/<host-arch>/acode: GOOS=linux, CGO off, -trimpath
make image   # docker build, VERSION and REVISION taken from git
```

| Build argument | Default | Purpose |
|---|---|---|
| `RUNTIME_IMAGE` | `debian:trixie-slim` | The base image. |
| `TARGETARCH` | set by BuildKit | Selects `dist/linux/<arch>/acode`; the classic builder leaves it empty, so the build needs BuildKit. |
| `VERSION` | `devel` | `org.opencontainers.image.version` label only. |
| `REVISION` | `unknown` | `org.opencontainers.image.revision` label only. |
| `UID`, `GID` | `1000` | The runtime user. |

`VERSION` labels the image only; the binary carries the version its own build
stamped. `--platform` builds need both trees in the context and binfmt/QEMU
registered. A manual cross-build is `CGO_ENABLED=0 GOOS=linux GOARCH=<arch> go
build -trimpath -ldflags "-X main.version=vX.Y.Z" -o dist/linux/<arch>/acode
./cmd/acode`.

### Adding a toolchain

The stack image, [`compose/Dockerfile`](../compose/Dockerfile), is a `golang`
base with the released binary copied in, gh, `golangci-lint`, and `govulncheck`
added, so the agent can build, test, `make dod`, and push the workspace. Both
check tools are pinned by build argument to the versions `ci.yml` installs. The
published image stays toolchain-free: it is what runs elsewhere, and one
language's compiler is dead weight there.

Another language copies that file and changes the base — `rust:1.100-trixie`,
`node:24-trixie`, anything with `/bin/sh` — and selects it per machine in
[`compose.local.yaml`](../compose.local.yaml.example), out of git:

```yaml
services:
  acode:
    build:
      # ACODE_STACK_DIR is exported by the launchers, so this resolves from
      # wherever they are invoked.
      context: ${ACODE_STACK_DIR:-.}
      dockerfile: Dockerfile.toolchain
    # A tag of its own: the tracked image: would otherwise be replaced by it.
    image: localhost/acode-stack:toolchain
```

What the copy has to keep is the acode binary at `/usr/local/bin/acode`, the user
and `HOME` from §4, `ENTRYPOINT ["acode"]`, and the cache volumes of §3. Pin the
base tag: the agent runs whatever the Dockerfile fetched.

Do not add a toolchain to the published `Dockerfile`: the distribution's
`golang-go` trails `go.mod`, a distroless base has no `/bin/sh`, which is what
`run_command` runs, and it would be dead weight for every other workspace.

## 7. Continuous integration

| Workflow | Trigger | Result |
|---|---|---|
| [ci.yml](../.github/workflows/ci.yml) | push to `main`, pull request, manual | Runs `make dod`: the coding standard's §8 gates plus `docs-check`. |
| [release.yml](../.github/workflows/release.yml) | push to `main` or a `v*` tag, pull request, manual | Builds the binaries, attaches them to a tag's GitHub Release, and publishes `ghcr.io/eddiectc/acode` from those binaries. |

| Event | Image tags | Release assets |
|---|---|---|
| Push to `main` | `latest`, `sha-<short>` | none |
| Tag `vX.Y.Z` | `vX.Y.Z`, `vX.Y`, `vX`, `sha-<short>` | tarballs, `SHA256SUMS`, attestation |
| Pull request | none; all four binaries and a `linux/amd64` image build | none |

A manual dispatch publishes nothing, and `latest` tracks `main` HEAD, so pin a
tag or digest. Windows is not built: `/bin/sh`, the manual PTY smoke, and
`syscall.SIGTERM` are Unix. The branch and tagging contract is in
[release.md](release.md).

## 8. Verification status

Checked: the base image tags resolve; UID and GID 1000 are free in
`debian:trixie-slim`; `CGO_ENABLED=0` yields a static binary; the Dockerfile is
clean under `hadolint` 2.12.0 and both workflows parse under `actionlint`;
`make dist` plus the §7 packaging loop produced a matching `SHA256SUMS`. The
`golangci-lint` and `govulncheck` pins install and run under the stack image's Go
1.27.4, and `make dod` passes with them.

For §1, both engines resolve `compose.yaml` alone and merged with
`compose.podman.yaml`, the launchers' argv was accepted by the real parsers, and
the stale-pod guard stops with the teardown commands. The stack was built and run
under rootless Podman, and from a `run_command` child — the environment the
claims are about — the bound `gh` config resolved the bot account,
`git credential fill` answered through `/etc/gitconfig`, the bound directories
were not writable, and the child environment was exactly `HOME`, `LANG`, `PATH`,
`PWD`, `TERM`, and `TMPDIR` with no `*_TOKEN` name. A mounted `~/.gitconfig`
identity committed; without it, `git commit` failed as documented. With
`known_hosts` in a read-only `~/.ssh` bind, `ssh -T git@github.com` succeeded at
the default checking level, while an empty or wrong `known_hosts` failed.
`settings.yml` was checked against the SearXNG source, and each port variable
moves its publish, listener, healthcheck, and acode's URL together. The gateway
claims were read from the LiteLLM source; the gateway was not started.

Not verified: a `docker build` and `docker run` (the run above was rootless
Podman); a TUI session against a real terminal; a real-provider run through the
gateway; one end-to-end `release.yml` run on GitHub. The current
`compose/Dockerfile` has not been built — its `FROM`, its `USER` switch, the gh
`.deb` fetch and checksum, the `go install` of the check tools, and a multi-arch
build are open, and `hadolint` 2.12.0 reports one DL3003 warning on the gh step's
`cd`. The launchers create the credential paths and read the `image:` and whether
a `build:` exists from the merged config, which was checked against recorded
`compose config` output rather than a live engine. The `setns()`/`CAP_SYS_ADMIN`
explanation is reasoned, not observed, and so is whether host networking clears
the `conmon bytes ""` create failure. From inside the stack, acode reaching the
three loopback ports, the healthcheck probes, SearXNG's env override of the
read-only `settings.yml`, the Crawl4AI token, the native pass-through path, and
the GitHub-side workflow behaviour are unverified.
