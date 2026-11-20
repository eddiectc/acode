# acode in a container

- **Date:** 2026-11-20
- **Status:** current for the published container image
- **Related:** [configuration](configuration.md), [architecture](architecture.md),
  [limits](limits.md), [definition of done](definition-of-done.md), and the
  [coding standard](coding-standard.md)
- **Files:** [`Dockerfile`](../Dockerfile), [`.dockerignore`](../.dockerignore),
  [`.github/workflows/ci.yml`](../.github/workflows/ci.yml), and
  [`.github/workflows/image.yml`](../.github/workflows/image.yml)

The image is `ghcr.io/eddiectc/acode`. It carries the `acode` binary, `/bin/sh`,
git, an SSH client, and CA certificates. It carries **no project toolchain**:
derive an image from it to add the compilers and test runners a project needs,
or the agent cannot build or test the workspace it is editing.

The image is **not** a sandbox, an approval workflow, or a secret store. acode
does not sandbox itself and has no approval step ([README](../README.md#isolation));
it runs with the container user's privileges and can reach anything the
container can reach. Isolation comes from the mounts, the user, and the network.

## 1. Quick start

Mount the workspace and give the process a terminal. The TUI is selected only
when stdin and stdout are both terminals and `TERM` is set
([architecture §4](architecture.md#4-modes-and-selection)), so `-it` is part of
the invocation.

```sh
# Podman, rootless, with the host user mapped onto the image user.
podman run --rm -it --init \
  --userns=keep-id:uid=1000,gid=1000 \
  -v "$PWD:$PWD" -w "$PWD" \
  -v acode-state:/home/acode/.acode \
  --network=host \
  -e ACODE_MODEL \
  -e ACODE_PROVIDER_BASE_URL \
  -e ACODE_PROVIDER_TOKEN \
  ghcr.io/eddiectc/acode

# Docker.
docker run --rm -it --init \
  --user "$(id -u):$(id -g)" \
  -v "$PWD:$PWD" -w "$PWD" \
  -v acode-state:/home/acode/.acode \
  --network=host \
  -e ACODE_MODEL \
  -e ACODE_PROVIDER_BASE_URL \
  -e ACODE_PROVIDER_TOKEN \
  ghcr.io/eddiectc/acode
```

With no task argument the first message supplies it. Without `-it` the process
is not a terminal, so acode falls back to batch mode and exits 2 unless a task
is supplied:

```sh
podman run --rm -v "$PWD:$PWD" -w "$PWD" -v acode-state:/home/acode/.acode \
  --network=host -e ACODE_PROVIDER_TOKEN \
  ghcr.io/eddiectc/acode -model <model-id> -batch -task "Fix the failing test"
```

`--init` reaps orphaned children. acode kills a timed-out or cancelled
`run_command` process group through the SDK, but a tool that double-forks can
still leave a process behind.

## 2. Mounts

| Mount | Path | Why |
|---|---|---|
| Workspace | the project directory, read-write | The code the agent edits. Mount it at the **same absolute path** it has on the host. |
| State | `/home/acode/.acode`, a named volume | Sessions, the model catalog cache, and the config file. |
| Build caches | optional, named volumes | `/home/acode/go/pkg/mod`, `/home/acode/.cache/go-build`, and equivalents avoid cold rebuilds. |

Mount the workspace at its host path (`-v "$PWD:$PWD" -w "$PWD"`) rather than at
a fixed `/workspace`. A session records its workspace, and `-resume` and the
work directory reuse that recorded path, so an identical absolute path keeps
host-created and container-created sessions interchangeable.

On a host with SELinux enforcing, append `:Z` to the workspace bind
(`-v "$PWD:$PWD:Z"`) or the container user cannot write it.

### The state directory is a volume, not a host bind

`<state-dir>` is a control plane, not just data:

- `<state-dir>/agents/*.md` are operator definitions that carry a **tool grant
  and prompt** ([configuration](configuration.md)).
- `<state-dir>/config.json` carries `provider_base_url` and the run budgets.
- `<state-dir>/sessions/*.jsonl` holds every transcript from every project.

`run_command` runs as the container user and can write any absolute path, so a
read-write bind of a state directory the host also uses lets an injected
instruction rewrite the base URL, widen a future session's grant, or read
unrelated transcripts. Use a volume, and reset it with
`podman volume rm acode-state`.

If host and container must share configuration, mount the **file** read-only
and select it with `-config`, so the agent cannot edit it:

```sh
-v "$HOME/.acode/config.json:/etc/acode/config.json:ro" \
  -e ACODE_CONFIG=/etc/acode/config.json
```

`/settings` and `/agents` then report that they cannot save. A read-only config
still needs a writable state directory for sessions. Sharing a state directory
read-write also couples the two binaries: the session schema and cache versions
are fixed per build with no migration, and an exclusive `flock` per session log
means a host and container process cannot open the same session concurrently.

## 3. User, UID, and GID

The image builds a non-root user with UID/GID **1000**, and the process runs
with a numeric `USER`, so no name lookup is needed. Match it to the host user:

| Runtime | Invocation |
|---|---|
| Podman, rootless | `--userns=keep-id:uid=1000,gid=1000` |
| Docker | `--user "$(id -u):$(id -g)"` |
| Either, host UID is not 1000 | Build your own with `--build-arg UID=... --build-arg GID=...` |

Pick IDs that the base image does not already use: `debian:trixie-slim` has
system users up to UID 42 and groups up to GID 100, so `GID=100` collides with
its `users` group and fails the build.

A UID mismatch is the most common first-run failure: state files are created
0600 and directories 0700, so a container user that does not own the volume
cannot read or extend a session.

## 4. Configuration

The three layers still apply inside the container: flag, environment, config
file ([configuration](configuration.md)). Two rules matter here:

- The provider token is **environment-only**. It is deliberately not a config
  key, so it cannot be baked into an image or written to the volume.
- `/settings` and `/agents` refuse to write a config file that lives inside the
  workspace, so a writable `config.json` belongs in the state volume. acode
  still *reads* a workspace config file at startup, which means a repository
  can supply one; keep the writable file out of the mount.

Seed the volume with the non-secret defaults so the run needs no flags:

```sh
podman volume create acode-state >/dev/null
# Seed the volume's ownership from the image directory, under the same user
# namespace the real run uses.
podman run --rm --userns=keep-id:uid=1000,gid=1000 \
  -v acode-state:/home/acode/.acode \
  ghcr.io/eddiectc/acode -version
```

Then write `/home/acode/.acode/config.json` in the volume:

```json
{
  "model": "deepseek-v4.1-flash",
  "provider_base_url": "http://127.0.0.1:4000/v1",
  "thinking_effort": "high",
  "max_turns": 40
}
```

### Secrets

`ACODE_PROVIDER_TOKEN` is read once into memory and never logged. The SDK
gives `run_command` children an explicit environment allowlist (`PATH`,
`LANG`, `LC_ALL`, `TERM`, `HOME`, and a harness `TMPDIR`) and denies names
ending in `_TOKEN`, `_SECRET`, `_KEY`, or `_PASSWORD`, so a tool call does not
inherit the token by accident.

That allowlist is **not** a secrecy boundary. The child runs as the same user,
so `/proc/<pid>/environ` is readable and the token is in it. Treat the token as
visible to anything that runs in the container, and bound its blast radius at
the provider instead.

The same allowlist decides what a tool call can authenticate: `SSH_AUTH_SOCK`
is not passed, so a forwarded agent does not authenticate a `git push` that the
agent runs. Use an HTTPS remote with a credential file under `HOME`, or mount a
key read-only under `/home/acode/.ssh`. Do not copy a long-lived key into the
workspace or the image.

`--env-file` also keeps the value out of shell history and out of the
container's inspected environment, which `-e` does not:

```sh
install -m 0600 /dev/null acode.env
printf 'ACODE_PROVIDER_TOKEN=%s\n' "$TOKEN" > acode.env
podman run --rm -it --env-file acode.env ... ghcr.io/eddiectc/acode
```

## 5. Network

### A local gateway

A gateway such as LiteLLM in front of the real providers is the recommended
shape: the container holds only a gateway key, and budget, rate limits, and the
model allowlist are enforced where you can see them.

`provider_base_url` accepts plain `http` for **any** host — it rejects only
userinfo, a query, and a fragment, and a path prefix is allowed — so a gateway
address needs no special handling:

```sh
-e ACODE_PROVIDER_BASE_URL=http://127.0.0.1:4000/v1   # with --network=host
-e ACODE_PROVIDER_BASE_URL=http://litellm:4000/v1     # on a shared container network
```

The model ID must exist in the models.dev catalog. acode resolves the dialect,
context window, and base URL from that entry and has no dialect override, so an
alias that exists only in the gateway is a startup error naming the model
(exit 2). Name the gateway deployment after the catalog ID and keep the real
upstream in the gateway's own configuration.

### Network modes

| Mode | Egress | Use when |
|---|---|---|
| `--network=host` | Unrestricted, and a host gateway is reachable at `127.0.0.1` | The default for a trusted project. |
| A dedicated bridge network | Reaches only what is on that network | The project or its dependencies are untrusted. |
| `--internal` network plus the gateway | Reaches only the gateway | Inference is the only egress you want to allow. |

```sh
podman network create --internal acode-net
podman run -d --name litellm --network acode-net ...      # gateway
podman run --rm -it --network acode-net \
  -e ACODE_PROVIDER_BASE_URL=http://litellm:4000/v1 \
  -e ACODE_PROVIDER_TOKEN \
  -v "$PWD:$PWD" -w "$PWD" -v acode-state:/home/acode/.acode \
  ghcr.io/eddiectc/acode
```

An isolated network stops more than inference: `web_fetch`, `web_search`, and
any `go mod download`, `npm install`, or `cargo fetch` the agent runs also stop
working. `web_fetch`'s own private-address refusal is a policy in one tool, not
a network boundary; `run_command` can reach anything the namespace can.

`searxng_url` and `models_dev_url` are stricter than the provider URL: they
allow HTTP only for loopback hosts ([configuration §2.1](configuration.md#21-keys)).
A SearXNG or catalog mirror therefore has to share the namespace to be
reachable over loopback:

```sh
podman run --rm -it --network container:searxng \
  -e ACODE_SEARXNG_URL=http://127.0.0.1:8080 \
  -v "$PWD:$PWD" -w "$PWD" -v acode-state:/home/acode/.acode \
  ghcr.io/eddiectc/acode
```

Otherwise serve them over HTTPS.

## 6. Building the image

```sh
docker build \
  --build-arg VERSION="$(git describe --tags --always)" \
  --build-arg REVISION="$(git rev-parse HEAD)" \
  -t acode:local .
```

| Build argument | Default | Purpose |
|---|---|---|
| `GO_IMAGE` | `golang:1.27.4-trixie` | Builder stage. |
| `RUNTIME_IMAGE` | `debian:trixie-slim` | Runtime stage. |
| `VERSION` | `devel` | Stamped with `-X main.version`; what `acode -version` reports. |
| `REVISION` | `unknown` | `org.opencontainers.image.revision` label only. |
| `UID`, `GID` | `1000` | The runtime user. |

The build context excludes `.git`, so the image has no VCS metadata to derive a
version from: `VERSION` is the only source, and the Dockerfile default is
`devel`. The binary is built with `-trimpath` and is not stripped, per
[coding standard §8](coding-standard.md#8-tooling-and-review-gates).

The builder stage runs on the native platform and cross-compiles with
`GOOS`/`GOARCH`, so `--platform linux/amd64,linux/arm64` needs no emulation.

### Adding a toolchain

Derive from the published image rather than rebuilding acode:

```dockerfile
FROM ghcr.io/eddiectc/acode:latest
USER root
RUN apt-get update \
 && apt-get install -y --no-install-recommends golang-go nodejs npm \
 && rm -rf /var/lib/apt/lists/*
USER 1000:1000
```

Pin the tag in a real project. The agent then runs the project's own checks
(`go test ./...`, `npm test`) against the mounted workspace through
`run_command`, which is the only verification worth trusting.

## 7. Continuous integration

| Workflow | Trigger | Result |
|---|---|---|
| [ci.yml](../.github/workflows/ci.yml) | push to `main`, pull request, manual | Runs `make dod`, the coding standard's §8 gates plus `docs-check`. |
| [image.yml](../.github/workflows/image.yml) | push to `main` or a `v*` tag, pull request, manual | Publishes `ghcr.io/eddiectc/acode`. |

Published tags:

| Event | Tags |
|---|---|
| Push to `main` | `latest`, `sha-<short>` |
| Tag `vX.Y.Z` | `vX.Y.Z`, `vX.Y`, `vX`, `sha-<short>` |
| Pull request | none; `linux/amd64` build only, to prove the tree compiles |

`latest` tracks `main` HEAD, so a tagged release image can be older than
`latest`. The image job gates on the tree compiling, not on the test suite:
`ci.yml` is the test gate, and a release is tagged after CI is green. The full
branch and tagging contract is in [release.md](release.md).

That split relies on branch protection requiring the CI check before a merge to
`main`. Because a newer push cancels an in-progress CI run, without that
protection a commit could reach `latest` with no completed test run.

## 8. Verification status

Checked for this document: the base image tags resolve; UID and GID 1000 are
free in `debian:trixie-slim`, and `useradd`, `groupadd`, `install`, `apt-get`,
and `/bin/sh` exist there; the build context is sufficient to compile the
binary with the documented flags; a `-ldflags "-X main.version=..."` build
reports that version; the Dockerfile is clean under `hadolint`, and both
workflows parse under `actionlint`.

Not checked, and required before the first published image is trusted: an
actual `docker build` and `podman build` on a machine with a working container
runtime, a TUI session against a real terminal, and a real-provider run through
a gateway.
