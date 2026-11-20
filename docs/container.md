# acode in a container

- **Date:** 2026-11-20
- **Status:** current for the published image and release binaries
- **Related:** [configuration](configuration.md), [architecture](architecture.md),
  [limits](limits.md), [definition of done](definition-of-done.md), and the
  [coding standard](coding-standard.md)
- **Files:** [`Dockerfile`](../Dockerfile), [`.dockerignore`](../.dockerignore),
  [`.github/workflows/ci.yml`](../.github/workflows/ci.yml), and
  [`.github/workflows/release.yml`](../.github/workflows/release.yml)

The image is `ghcr.io/eddiectc/acode`. It carries the `acode` binary, `/bin/sh`,
git, an SSH client, and CA certificates. The same binary is published as a
tarball for `linux` and `darwin` on `amd64` and `arm64` for every `v*` tag
([§7](#7-continuous-integration)), and the image is built from the binaries
that workflow produced. The image carries **no project toolchain**: derive an
image from it to add the compilers and test runners a project needs, or put the
binary into the project's own toolchain image (§6), or the agent cannot build
or test the workspace it is editing.

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

The image does not compile acode. It copies `dist/linux/<arch>/acode`, the
binary a Go build produced, so the image, the release tarballs, and what
`acode -version` reports all come from one build:

```sh
make dist    # dist/linux/<host-arch>/acode: GOOS=linux, CGO off, -trimpath
make image   # docker build, VERSION and REVISION taken from git
```

The image job in [release.yml](../.github/workflows/release.yml) does the same
thing with the binaries it built for both linux architectures. By hand:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath \
  -ldflags "-X main.version=vX.Y.Z" -o dist/linux/arm64/acode ./cmd/acode

docker build \
  --build-arg VERSION="$(git describe --tags --always)" \
  --build-arg REVISION="$(git rev-parse HEAD)" \
  -t acode:local .
```

| Build argument | Default | Purpose |
|---|---|---|
| `RUNTIME_IMAGE` | `debian:trixie-slim` | The base image. |
| `TARGETARCH` | set by BuildKit | Selects `dist/linux/<arch>/acode`. The classic builder leaves it empty, so the build needs BuildKit. |
| `VERSION` | `devel` | `org.opencontainers.image.version` label only. |
| `REVISION` | `unknown` | `org.opencontainers.image.revision` label only. |
| `UID`, `GID` | `1000` | The runtime user. |

`VERSION` no longer stamps the binary; the build that produced the binary did
that, and the label mirrors it. The workflow passes one version string to both
the binary build and the label, so a published image and its tarball report the
same thing. A local `make dist` passes no `-ldflags`, so a locally built image
reports the module version and the VCS revision while its label says what
`git describe` says.

`--platform linux/amd64,linux/arm64` needs both trees in the context; `make
dist` writes only the host architecture, so build the other one explicitly as
above. Nothing is compiled inside the build, so no Go cross-compile is
involved, but the `RUN` that installs packages still executes inside the target
platform's container: a multi-platform build on one machine needs binfmt/QEMU
registered (`docker/setup-qemu-action`, which the workflow uses).

The build context therefore carries `dist/` and `LICENSE` and excludes the
source; [`.dockerignore`](../.dockerignore) says so.

### Adding a toolchain

The image carries no project toolchain, so a derived image supplies one. Three
shapes work; they differ in where acode comes from:

| Base image | acode comes from | Use when |
|---|---|---|
| `ghcr.io/eddiectc/acode` | the base image | The toolchain is packaged for Debian, or you are adding one or two tools. |
| The project's own toolchain image | `COPY --from` the acode image | CI and the agent must share a base, and the toolchain is not a Debian package. |
| The project's own toolchain image | `go install` a pinned tag | You cannot fetch the image or a release tarball, and you accept building acode from source. |

Deriving is the least work: the user, `HOME`, the state directory, `/bin/sh`,
git, and the CA certificates are already right, and the added layer is only the
toolchain.

```dockerfile
FROM ghcr.io/eddiectc/acode:vX.Y.Z
USER root
RUN apt-get update \
 && apt-get install -y --no-install-recommends nodejs npm \
 && rm -rf /var/lib/apt/lists/*
USER 1000:1000
```

Pin the tag in a real project. Do not add Go this way: the distribution's
`golang-go` trails the version a project pins in `go.mod`, so `go` either
downloads the required toolchain on first use (`GOTOOLCHAIN=auto`, which needs
network) or refuses (`GOTOOLCHAIN=local`). A Go project belongs in the second
shape.

To keep your own base image, take the binary from the release tarball
(`tar -xzf acode_vX.Y.Z_linux_arm64.tar.gz -C /usr/local/bin acode`) or out of
the published image. It is built with `CGO_ENABLED=0`, so it runs in any base;
the price is that the base must supply the rest of the runtime, and you
reproduce the image's own setup:

```dockerfile
FROM ghcr.io/eddiectc/acode:vX.Y.Z AS acode
FROM <your toolchain image>

# Only the file is needed, not the acode image's base. Taking it through a FROM
# stage keeps the FROM platform rule: a multi-arch build resolves the matching
# variant of the acode image.
COPY --from=acode /usr/local/bin/acode /usr/local/bin/acode

# What deriving would have supplied: the image user (§3), its HOME, the 0700
# state directory (§2), the two cache directories §2 mounts, and a locale. The
# base may default to a non-root user, hence the explicit USER root. A
# `golang:*` base keeps GOPATH and its module cache under root-owned /go, and
# the run_command environment passes HOME and PATH but not GOPATH, so the cache
# and the installed binaries belong under HOME. Check the base does not already
# use UID or GID 1000 (§3).
USER root
RUN groupadd --gid 1000 acode \
 && useradd --uid 1000 --gid 1000 --create-home --home-dir /home/acode \
      --shell /bin/bash acode \
 && install -d -o 1000 -g 1000 -m 0700 /home/acode/.acode \
 && install -d -o 1000 -g 1000 -m 0755 /home/acode/.cache \
 && install -d -o 1000 -g 1000 -m 0755 /home/acode/go/pkg/mod \
 && install -d -o 1000 -g 1000 -m 0755 /home/acode/.cache/go-build \
 && install -d -o 1000 -g 1000 -m 0755 /workspace
ENV HOME=/home/acode \
    LANG=C.UTF-8 \
    PATH=/home/acode/go/bin:$PATH
USER 1000:1000
WORKDIR /workspace
ENTRYPOINT ["acode"]
```

The base must supply `/bin/sh`; `ca-certificates`, git, and an SSH client are
what the published runtime adds on top of `debian:trixie-slim`
([Dockerfile](../Dockerfile)), which already has a shell. Without them
`run_command`, HTTPS fetches, and the git tools fail.

An Alpine base needs `addgroup`/`adduser` instead of `groupadd`/`useradd`. A
distroless base has neither, nor `/bin/sh`, so `run_command` cannot execute
anything at all: the SDK invokes `/bin/sh -c`.

`go install` is the last option. Build it in a stage so the module and build
caches stay out of the final image, and pin the tag:

```dockerfile
FROM golang:1.27.4-trixie AS built
RUN GOBIN=/out go install github.com/eddiectc/acode/cmd/acode@vX.Y.Z
```

Drop the published-image stage and its `Only the file is needed` comment from
the runtime example, and copy from that stage instead:
`COPY --from=built /out/acode /usr/local/bin/acode`. Everything else in that
stage stays: the `USER root` and the whole `RUN` (including the two cache
directories), `ENV` with `LANG`, `USER`, `WORKDIR`, and `ENTRYPOINT`.

It is the weakest of the three: `@latest` makes the image unreproducible; the
build needs module-proxy access and a toolchain that satisfies the module's `go`
directive; the version comes from the module tag, so `acode -version` reports
`vX.Y.Z` with `vcs revision: unknown`; and the result carries no provenance
attestation. A release tarball is the same binary the image carries, with a
`SHA256SUMS` file and a build attestation ([release](release.md)), and the
image is the artifact CI built, so take one of those rather than rebuild acode.
Use `go install` when neither can be fetched.

Whichever shape you choose, the agent then runs the project's own checks
(`go test ./...`, `npm test`) against the mounted workspace through
`run_command`, which is the only verification worth trusting.

## 7. Continuous integration

| Workflow | Trigger | Result |
|---|---|---|
| [ci.yml](../.github/workflows/ci.yml) | push to `main`, pull request, manual | Runs `make dod`, the coding standard's §8 gates plus `docs-check`. |
| [release.yml](../.github/workflows/release.yml) | push to `main` or a `v*` tag, pull request, manual | Builds the binaries, attaches them to a tag's GitHub Release, and publishes `ghcr.io/eddiectc/acode` from those binaries. |

The workflow has four jobs:

| Job | Does |
|---|---|
| `version` | Resolves one version string: the tag name, or `devel-<short sha>` on a branch. |
| `binaries` | Builds it for `linux` and `darwin` on `amd64` and `arm64` with `-trimpath` and `-X main.version`, and uploads each binary as an artifact. |
| `image` | Downloads the two linux artifacts, puts them at `dist/linux/<arch>/acode`, and builds the image from them. |
| `release` | Tags only: packages the tarballs, attests them, and uploads them to the GitHub Release. |

Release assets for a `vX.Y.Z` tag:

| Asset | Contents |
|---|---|
| `acode_<version>_<os>_<arch>.tar.gz` | The `acode` binary and `LICENSE`, binary mode 0755, owned 0/0. |
| `SHA256SUMS` | One SHA-256 per tarball. |
| Attestation | Build provenance for each tarball; check it with `gh attestation verify`. |

Published tags:

| Event | Image tags | Release assets |
|---|---|---|
| Push to `main` | `latest`, `sha-<short>` | none |
| Tag `vX.Y.Z` | `vX.Y.Z`, `vX.Y`, `vX`, `sha-<short>` | tarballs, `SHA256SUMS`, attestation |
| Pull request | none; all four binaries build and the image is built for `linux/amd64` only, to prove the tree compiles | none |

A manual dispatch builds and publishes nothing, from any ref: only a push
publishes, and the push triggers are `main` and a `v*` tag. That keeps an
untested branch from reaching the registry through a manual run.

Windows is not built. `/bin/sh`, the manual PTY smoke, and `syscall.SIGTERM`
are Unix, CI is Linux-only, and nothing exercises the CLI on Windows, so a zip
would be a claim nothing checks. A Windows user builds from source.

`latest` tracks `main` HEAD, so a tagged release image can be older than
`latest`. The workflow gates on the tree compiling, not on the test suite:
`ci.yml` is the test gate, and a release is tagged after CI is green. The full
branch and tagging contract is in [release.md](release.md).

That split relies on branch protection requiring the CI check before a merge to
`main`. Because a newer push cancels an in-progress CI run, without that
protection a commit could reach `latest` with no completed test run.

## 8. Verification status

Checked for this document: the base image tags resolve; UID and GID 1000 are
free in `debian:trixie-slim`, and `useradd`, `groupadd`, `install`, `apt-get`,
and `/bin/sh` exist there; `CGO_ENABLED=0` yields a statically linked binary
(checked with `file` and `ldd`); a `-ldflags "-X main.version=..."` build
reports that version; the Dockerfile is clean under `hadolint` 2.12.0, and both
workflows parse under `actionlint`, which ran its shellcheck pass over every
`run:` block. For §6, `make dist` wrote `dist/linux/<arch>/acode`, and the
packaging loop from §7 was run on that tree: it produced
`acode_v0.0.1-test_linux_amd64.tar.gz` containing the binary at mode 0755 and
`LICENSE`, and wrote a `SHA256SUMS` whose digest matched. A `go install` of
`github.com/eddiectc/acode/cmd/acode@v0.0.1` with `GOBIN` set resolved through
the module proxy and reported `acode v0.0.1` with `vcs revision: unknown`, which
is the tag-not-stamp behaviour §6 describes.

Not checked, and required before the first published image is trusted: an
actual `docker build` and `podman build` on a machine with a working container
runtime, a TUI session against a real terminal, a real-provider run through a
gateway, and one end-to-end run of [release.yml](../.github/workflows/release.yml)
on GitHub. This environment has no working container runtime (the `docker` CLI
is a Podman shim, and Podman exits with `Error determining cgroups mode`), so
the §6 image examples were not built and their `useradd`/`install` lines and
multi-arch behaviour are unverified. The workflow's GitHub-side behaviour is
also unverified: that an uploaded artifact keeps the `dist/<goos>/<goarch>/`
path the packaging loop and the Dockerfile expect, that `merge-multiple` yields
`dist/linux/<arch>/acode` for both linux artifacts, that BuildKit resolves
`TARGETARCH` inside `COPY`, and that `actions/attest-build-provenance` and
`gh release` succeed under the job's permissions. The `go install` command
itself was run on the host; only its stage and `COPY` are untested.
