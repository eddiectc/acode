# acode in a container

- **Date:** 2026-11-21
- **Status:** current for the published image, the compose stack, the launcher
  scripts, and the release binaries
- **Related:** [configuration](configuration.md), [architecture](architecture.md),
  [limits](limits.md), [definition of done](definition-of-done.md), and the
  [coding standard](coding-standard.md)
- **Files:** [`Dockerfile`](../Dockerfile), [`Dockerfile.gh`](../Dockerfile.gh),
  [`.dockerignore`](../.dockerignore),
  [`compose.yaml`](../compose.yaml),
  [`compose.podman.yaml`](../compose.podman.yaml),
  [`compose.local.yaml.example`](../compose.local.yaml.example),
  [`.env.example`](../.env.example),
  [`scripts/acode-docker`](../scripts/acode-docker),
  [`scripts/acode-podman`](../scripts/acode-podman),
  [`.github/workflows/ci.yml`](../.github/workflows/ci.yml), and
  [`.github/workflows/release.yml`](../.github/workflows/release.yml)

The image is `ghcr.io/eddiectc/acode`. It carries the `acode` binary, `/bin/sh`,
git, an SSH client, and CA certificates, and **no project toolchain**: add the
compilers and test runners the workspace needs, or the agent cannot build or
test what it edits (§6). It carries no `gh` either;
[`Dockerfile.gh`](../Dockerfile.gh) is the same image plus the GitHub CLI, for
the opt-in login in §5. The same binary ships as a tarball for `linux` and
`darwin` on `amd64` and `arm64` for every `v*` tag, and the image is built from
the binaries CI produced (§7).

The image is **not** a sandbox, an approval workflow, or a secret store. acode
runs with the container user's privileges and can reach anything the container
can reach ([README](../README.md#isolation)); isolation comes from the mounts,
the user, and the network.

## 1. Quick start: the compose stack

[`scripts/acode-docker`](../scripts/acode-docker) and
[`scripts/acode-podman`](../scripts/acode-podman) are the recommended way to
run acode. They start the stack in [`compose.yaml`](../compose.yaml) — a
gateway and both web-tool backends already wired to the container — and run the
agent against the directory you invoke them from:

```sh
cp .env.example .env         # once, in the stack checkout: upstream key, secrets
cd ~/src/project
~/acode/scripts/acode-docker                 # Docker; edit ~/src/project
~/acode/scripts/acode-podman                 # Podman; arguments identical
~/acode/scripts/acode-docker -model <id> -task "..."   # flags go to acode
```

| Service | Image | Role | Port (env) |
|---|---|---|---|
| `acode` | built from [`compose/Dockerfile`](../compose/Dockerfile) | The agent | none |
| `litellm` | `ghcr.io/berriai/litellm:main-stable` | Credential holder, pass-through | 4000 (`LITELLM_PORT`) |
| `searxng` | `docker.io/searxng/searxng` | `web_search` backend | 8889 (`SEARXNG_PORT`) |
| `crawl4ai` | `docker.io/unclecode/crawl4ai` | `web_fetch` renderer | 11235 (`CRAWL4AI_PORT`) |

Every port is published on `127.0.0.1` only, and each variable is both the
container port and the publish — so it moves the listener, the healthcheck, and
the URL acode uses, not just the host side. Set them in `.env` to run a second
stack or to dodge a port already in use. The stack image is the Go
toolchain base of §6 with the `acode` binary copied out of `ACODE_IMAGE`, so
`go build` and `go test` work on the workspace; it is built on first run, and
`ACODE_BUILD=1` forces a rebuild.

Each invocation gets a one-off container named `acode-<8 hex>`, so instances do
not collide and `docker logs acode-<suffix>` addresses one afterwards.
`ACODE_WORKSPACE` pins a project other than the working directory;
`ACODE_TTY=0` drops the TTY for piped input; `ACODE_STACK_DIR` overrides where
the scripts look for `compose.yaml`. A `compose.local.yaml` in the stack
checkout is passed last when it exists, so a machine-local override — a
credential mount, another `ACODE_IMAGE`, an extra volume — beats the tracked
files without landing in git (§5). The scripts export the paths, print the
container name to stderr, and pass acode's exit code through.

### Workspace and state

The workspace mount and `working_dir` are `$PWD` with the host path repeated
inside the container, as §3 requires; `run -e ACODE_WORKSPACE=…` cannot change
it, because Compose resolves mounts before the container exists. State lives in
the `acode-state` volume, not a host bind (§3).

Inside the container the state directory is the image user's home, so
`~/.acode` is `/home/acode/.acode` and that path is the volume:

| What | Path inside | Where on the host |
|---|---|---|
| config | `/home/acode/.acode/config.json` | `<volume>/_data/config.json` |
| sessions | `/home/acode/.acode/sessions/*.jsonl` | `<volume>/_data/sessions/` |
| agents | `/home/acode/.acode/agents/*.md` | `<volume>/_data/agents/` |

The volume is `<project>_acode-state`, which for this stack is
`acode-stack_acode-state`. Find the host path rather than guessing it:

```sh
podman volume inspect -f '{{.Mountpoint}}' acode-stack_acode-state
```

To change a setting there are three routes, and the first one is not the file:

1. **Compose environment wins over the file.** `ACODE_MODEL`,
   `ACODE_PROVIDER_BASE_URL`, `ACODE_SEARXNG_URL`, the `ACODE_WEB_FETCH_*`
   values, and the token are set by `compose.yaml` from `.env`, so editing
   `model` in `config.json` has no effect while the stack runs. Change those in
   `.env` and re-run the launcher.
2. **`/settings` in the TUI** edits every config-file key and writes the file
   atomically. It shows which layer won (`env`, `flag`, `file`, or `current`),
   which is how you confirm route 1 is shadowing a value. Saves apply at the
   next startup, except `ui_rich` and `ui_expand_all`.
3. **Edit the file on the host** for bulk changes: the volume's `_data/config.json`.
   Files are owned by the image user, which `keep-id` maps to you, so an editor
   works; keep mode `0600`. A missing file is not an error, and `/settings`
   creates it.

`acode` also logs the resolved path at startup as `config=<path>`, which is the
quickest confirmation that a run is using the file you think it is.

### Podman and UID mapping

Rootless Podman maps the caller onto container root by default, so the image
user 1000 cannot write the workspace. The Podman launcher merges
[`compose.podman.yaml`](../compose.podman.yaml), which adds
`userns_mode: keep-id:uid=1000,gid=1000`. Do not merge that override into Docker
or rootful Podman, which have no keep-id: Docker users set `ACODE_UID` and
`ACODE_GID` and rebuild instead (§4).

That override also sets `x-podman: in_pod: false`. `podman-compose` puts every
service in one pod by default, and a pod owns the network and user namespaces,
so per-container requests are refused: the man page says `--network` with
`--pod` means the container joins the *pod's* namespace, and that a userns or
gidmap "cannot be set on the container level when in a pod". Leaving pod mode
on also makes acode's container depend on pod containers, which podman rejects
as `container dependency ... is part of a pod, but container is not`. Turning
the pod off matches Docker Compose and is required — but it is **not
sufficient**: the `conmon bytes ""` create failure also appears with the pod
off, because joining another container's network namespace cannot work under
keep-id at all. Host networking is the fix; see §1's networking subsection.

If a create still fails that way, podman is hiding the OCI runtime's error.
`oci_conmon_common.go` only reports `conmon bytes ""` when conmon wrote nothing
*and* the runtime's log could not be read, so the cause is in a file podman
failed to collect. Get it directly:

```sh
podman-compose --verbose -f compose.yaml -f compose.podman.yaml run --rm acode
podman --log-level=debug create --name acode-probe \
  --network=container:acode-stack_searxng_1 \
  --userns=keep-id:uid=1000,gid=1000 localhost/acode-stack:local
journalctl --user -b --no-pager | tail -50   # crun, SELinux, or AppArmor denial
```

A `:Z` relabel on a workspace that lives on NFS or another filesystem without
SELinux label support is a common cause on hosts where `Z` is otherwise
harmless; drop it from the workspace bind to test.

A stack created *before* `in_pod` was disabled needs one manual teardown,
because its containers are pod members and podman refuses to make one of them a
dependency of a container outside the pod, reporting
`container dependency <id> is part of a pod, but container is not`. Teardown
cannot fix it by itself: with `in_pod: false` podman-compose's pod list is
empty (`self.pods = [{"name": pod_name}] if pod_name else []`), so `down` never
removes the pod. Run:

```sh
podman-compose -f compose.yaml -f compose.podman.yaml down
podman pod rm -f pod_acode-stack
```

`down` keeps the named volumes, so sessions and the Go caches survive.
`scripts/acode-podman` checks for `pod_<project>` first and stops with these
instructions rather than letting podman fail cryptically.

### The gateway

LiteLLM here is a **credential holder** and a single egress hop, not a policy
layer. It has no `model_list`, no routing, no allowlist, and no budget. Its one
job is to keep the real provider key out of the agent container, so the key
lives in one place you can rotate instead of in the environment of a process
that runs arbitrary `run_command` calls.

That is also why the route is a lossless pass-through rather than a deployment:
a deployment would parse, resolve, and re-encode every request, which is
translation you do not want from something whose only purpose is to attach a
`Authorization` header. Pass-through forwards path and body untouched, so the
upstream behaves exactly as it does without the gateway and what you test
against is the real wire format.

```yaml
general_settings:
  master_key: os.environ/LITELLM_MASTER_KEY
  pass_through_endpoints:
    - path: "/go-native"
      target: "https://opencode.ai/zen/go"
      include_subpath: true
      forward_headers: true
      auth: true
      headers:
        Authorization: "Bearer os.environ/OPENCODE_API_KEY"
```

| Hop | URL |
|---|---|
| acode (`ACODE_PROVIDER_BASE_URL` + `/chat/completions`) | `http://litellm:${LITELLM_PORT}/go-native/v1/chat/completions` |
| LiteLLM forwards the rest of the path to `target` | `https://opencode.ai/zen/go/v1/chat/completions` |

The gateway prefix is part of `ACODE_PROVIDER_BASE_URL`; `target` is the
upstream base *before* `/v1`. Only the gateway holds `OPENCODE_API_KEY`, and
`auth: true` requires a LiteLLM key, which is the master key. It is explicit
because the YAML path hands entries over as raw dictionaries and the endpoint
check is `auth is not None and str(auth).lower() == "true"`, so the model
default does not apply.

`forward_headers: true` sends acode's user agent and provider-specific headers
such as `x-opencode-session` upstream. acode's `Authorization` cannot leak: a
client header whose name matches a configured header is dropped before the
merge, so the configured `Bearer` always wins.

`ACODE_MODEL` is forwarded verbatim as the request's `model`, so it must be
both a models.dev catalog ID — acode resolves the dialect, context window, and
limits from that entry and rejects an unknown model at startup (exit 2) — and a
model name the upstream accepts. `target` is a literal URL; LiteLLM expands
`os.environ/` in `headers`, not there.

#### What a credential holder does not buy

Be clear about the boundary, because "gateway" suggests more:

- **The master key is a spending credential for this route.** Anything that can
  reach the gateway port with it can call the upstream on your account. It
  cannot reconfigure the gateway — management endpoints need a database and
  none is configured — but it can spend. Bound that at the provider: a key with
  its own quota, or a spend cap on the account.
- **No per-model budget, rate limit, allowlist, health check, or fallback.**
  Those belong to a deployment, and there is none. Put them at the provider or
  in whatever fronts the stack.
- **It is one rotation point, not a second trust boundary.** A `run_command`
  child runs as the same user inside the same container, so it can read the
  master key from its own environment and call the route (see §5). The gateway
  keeps the *provider* key away from the agent; it does not make the agent
  container trusted.

### Networking, and why acode uses host mode

`searxng_url` allows plain HTTP only for a loopback host
([configuration §2.1](configuration.md#21-keys)), and a compose service name is
not loopback. The first design joined SearXNG's namespace
(`network_mode: "service:searxng"`), which satisfies that rule but **cannot
work with keep-id**, and therefore cannot work with rootless Podman:

- `setns()` into a network namespace requires `CAP_SYS_ADMIN` in the user
  namespace that *owns* it.
- `--userns=keep-id` puts acode in its own user namespace, a sibling of
  SearXNG's rather than the same one, so the join is refused.
- Podman validates neither the combination nor the conflict, so it surfaces as
  `container create failed (no logs from conmon): conmon bytes "": readObjectStart…`
  — conmon writes nothing and podman cannot read the OCI runtime's log.

So acode runs with `network_mode: host` and reaches each service through the
`127.0.0.1` port it already publishes:

| acode variable | Value |
|---|---|
| `ACODE_PROVIDER_BASE_URL` | `http://127.0.0.1:${LITELLM_PORT}/go-native/v1` |
| `ACODE_SEARXNG_URL` | `http://127.0.0.1:${SEARXNG_PORT}` |
| `ACODE_WEB_FETCH_URL` | `http://127.0.0.1:${CRAWL4AI_PORT}` |

Every one of them is loopback, so the strict rule is satisfied without sharing
a namespace, and keep-id still maps the caller onto the image user. The stack
keeps normal egress, which the catalog fetch, any `go mod download`, SearXNG,
and Crawl4AI all need.

The trade-off is real: a `run_command` server the agent starts binds **host**
ports, so it is reachable from the host without publishing anything, and two
instances that start a server on the same port collide. That is the price of
loopback without namespace sharing; the alternative, a service-name URL, is
refused by acode's validator for plain HTTP.

SearXNG listens on **8889**, not 8080. `SEARXNG_PORT` is the only knob: the
image's entrypoint copies it into `GRANIAN_PORT`, so it sets the listener, the
`127.0.0.1` publish, the healthcheck, `SEARXNG_BASE_URL`, and acode's
`ACODE_SEARXNG_URL` at once. 8080 is avoided because it is the usual
dev-server port.

### Tuning web_search

[`compose/searxng/settings.yml`](../compose/searxng/settings.yml) is
research-weighted: arXiv, Google Scholar, Semantic Scholar, BASE, PubMed,
GitHub, and Hacker News with raised `weight`, a hostnames plugin that floats
preprints, repositories, and code-with-paper sites above SEO aggregators, and
the open-access DOI rewrite. It merges onto the image's defaults, so it edits
those engines in place.

Four things in it are easy to get wrong, and the file follows the schema rather
than the shapes that circulate as snippets:

- `hostnames` priorities are **regexes**. A `*.arxiv.org` glob raises
  `re.error` inside the plugin's `init`, which is called without a handler, so
  the container fails to start instead of the pattern being ignored. Use
  `(.*\.)?arxiv\.org$`.
- Plugins are a **mapping keyed by import path**, and each entry carries
  exactly one field, `active`. `PluginCfg` has no other parameter, so anything
  else in the entry aborts startup with a `PluginCfg.__init__()` unexpected
  keyword argument. The hostnames configuration is therefore **top-level**,
  under the plugin's id (`hostnames:`), not nested in its entry — putting it in
  the entry is the mistake that looks right.
- `request_timeout` is under **`outgoing`**, not `search`.
- Engine `name` must match the built-in name (`semantic scholar`, not
  `semanticscholar`; `core.ac.uk`, not `core`) or the entry is appended as a
  second engine running the same module. Categories are case-sensitive: `it`,
  not `IT`.

CORE needs a free API key and ships disabled. Google Scholar bans datacenter
IPs, so expect it to be reported as unresponsive and suspended
(`search.suspended_times`); the other engines still answer. `web_search` gives
one call a fixed 15 s, so `outgoing.request_timeout` should stay under it: at
10 s a slow academic engine still cannot overrun the tool's budget. SearXNG's
own `search.formats` must include `json`, which is the one entry acode cannot
work without.

### More than one acode at a time

Run a launcher per project; the generated names keep the containers apart.
State is shared, so two instances must not open the same session (an exclusive
per-session `flock`) and concurrent `/settings` writes are last-writer-wins. The
Go caches are shared and concurrency-safe. Two instances on the same workspace
race on every edit. For separate state and its own ports, run a second stack
as its own project (`-p acode-2`, with other `SEARXNG_PORT`, `LITELLM_PORT`,
and `CRAWL4AI_PORT` values). Instances share the host network, so two that run
a server on the same port collide there rather than in a container namespace.

The healthchecks in the stack are informational: `depends_on` waits only for
the services to start, so the first model call may need a retry while LiteLLM
boots.

## 2. The published image on its own

Without the stack, run the image directly and supply the endpoints yourself.
`-it` is required for the TUI, which is selected only when stdin and stdout are
terminals and `TERM` is set
([architecture §4](architecture.md#4-modes-and-selection)); without it acode
falls back to batch mode and exits 2 unless a task is supplied.

```sh
# Podman, rootless, with the host user mapped onto the image user.
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

`--init` reaps orphaned children. acode kills a timed-out or cancelled
`run_command` process group through the SDK, but a tool that double-forks can
still leave a process behind.

## 3. Mounts

| Mount | Path | Why |
|---|---|---|
| Workspace | the project directory, read-write | The code the agent edits. Mount it at the **same absolute path** it has on the host. |
| State | `/home/acode/.acode`, a named volume | Sessions, the catalog cache, and the config file. |
| Build caches | optional, named volumes | `/home/acode/go/pkg/mod`, `/home/acode/.cache/go-build`, and equivalents avoid cold rebuilds. |
| Credentials | a read-only bind from `compose.local.yaml` | Optional and opt-in: the `gh` login, the commit identity, and `.ssh` all ship commented out (§5). |

A session records its workspace, and `-resume` and the work directory reuse that
recorded path, so an identical absolute path keeps host-created and
container-created sessions interchangeable. On an SELinux host append `:Z` to
the bind (`-v "$PWD:$PWD:Z"`) or the container user cannot write it.

### The state directory is a volume, not a host bind

`<state-dir>` is a control plane: `<state-dir>/agents/*.md` are operator
definitions that carry a **tool grant and prompt**
([configuration](configuration.md)), `config.json` carries `provider_base_url`
and the run budgets, and `sessions/*.jsonl` holds every transcript from every
project.

`run_command` runs as the container user and can write any absolute path, so a
read-write bind of a state directory the host also uses lets an injected
instruction rewrite the base URL, widen a future session's grant, or read
unrelated transcripts. Use a volume, and reset it with
`podman volume rm acode-state`. Sharing one read-write also couples the two
binaries: the session schema and cache versions are fixed per build with no
migration, and the per-session `flock` stops a host and container process from
opening the same session at once.

A volume created by a different UID leaves the 0600 state files unreadable, so
seed the ownership under the same user namespace the run uses:

```sh
podman run --rm --userns=keep-id:uid=1000,gid=1000 \
  -v acode-state:/home/acode/.acode ghcr.io/eddiectc/acode -version
```

To share host configuration, mount the **file** read-only and select it with
`-config`; `/settings` and `/agents` then report that they cannot save. A
writable `config.json` belongs in the volume: acode still *reads* a workspace
config file at startup, so keep the writable one out of the mount.

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
to UID 42 and groups up to GID 100, so `GID=100` collides with its `users` group.
A UID mismatch is the most common first-run failure, because state files are
created 0600 and directories 0700.

## 5. Configuration, secrets, and network

The three layers still apply inside the container: flag, environment, config
file ([configuration](configuration.md)). The provider token is
**environment-only** — deliberately not a config key, so it cannot be baked into
an image or written to the volume.

`ACODE_PROVIDER_TOKEN` is read once into memory and never logged. `run_command`
children get an environment allowlist (`PATH`, `LANG`, `LC_ALL`, `TERM`, `HOME`,
and a harness `TMPDIR`) and names ending in `_TOKEN`, `_SECRET`, `_KEY`, or
`_PASSWORD` are denied, so a tool call does not inherit the token by accident.

That allowlist is **not** a secrecy boundary: the child runs as the same user,
so `/proc/<pid>/environ` is readable. Treat the token as visible to anything in
the container and bound its blast radius at the provider. The same allowlist
decides what a tool call can authenticate — `SSH_AUTH_SOCK` is not passed, so a
forwarded agent does not authenticate a `git push`; give it an HTTPS remote and
a credential file under `HOME` instead of a key mounted under
`/home/acode/.ssh`. Do not copy a long-lived key into the workspace or the
image. `--env-file` keeps the token out of shell history and the inspected
environment, which `-e` does not.

A **git commit identity** does not belong in `.env` either, for a version of
the same reason: `run_command` children get an allowlist of `PATH`, `LANG`,
`LC_ALL`, `TERM`, `HOME`, and `TMPDIR` (`tools/command.go`, verified against
aharness v0.4.1), so `GIT_AUTHOR_NAME` set on the `acode` service never reaches
the `git` a tool call starts. `HOME` *does* arrive — the image sets
`ENV HOME=/home/acode` ([Dockerfile](../Dockerfile)) — and `~/.gitconfig` is
where git looks, so the identity is a file: the same local override, one more
bind, commented out in the example.

```sh
printf '[user]\n\tname = acode-bot\n\temail = 12345+acode-bot@users.noreply.github.com\n' \
  > ~/.config/acode/gitconfig
```

Mounted read-only at `/home/acode/.gitconfig`, that file is the only identity
the agent can commit under. It is opt-in, and the consequence of leaving it out
is exact: `git commit` fails with "Committer identity unknown" unless the
workspace or the repository carries its own `user.name`/`user.email`, so an
agent that only edits code and leaves committing to you needs nothing here.
Point it at a separate file rather than your own
`~/.gitconfig`: the latter would attribute the agent's commits to you. A
repo-local `git config user.name` in the workspace is the other way, and it
sticks because the workspace is a host bind — but it re-attributes *your* commits
in that repository as well. Use an address the bot account has on file, or
GitHub shows the commit unattributed; its
`<id>+<login>@users.noreply.github.com` form always is, and keeps a real address
out of the log. In the container `/home/acode` is not a volume, so a
`git config --global` run inside lasts only until `--rm` removes the container;
the read-only mount also makes it fail outright, which is the behaviour you
want. This file is also where the https credential helper belongs once the image
carries `gh` (below), unless you rely on the one in
[`Dockerfile.gh`](../Dockerfile.gh).

An **SSH host key** is the third file, and it is the reason a `git push` over
an `ssh` remote needs no flags; it is commented out in the example for the same
reason as the other two, and leaving it out means an `ssh` remote cannot verify
the host. `.ssh` is usually bound read-only so the key
cannot be replaced, and that also stops ssh creating `~/.ssh/known_hosts`, so
every run fails with "Host key verification failed". Passing
`-o UserKnownHostsFile=…` each time works around it once, which is not a fix.
Put `known_hosts` **in the directory you already bind** instead:

```sh
install -d -m 0700 ~/.config/acode/.ssh
cp ~/.ssh/id_ed25519 ~/.config/acode/.ssh/          # a copy, not your only key
curl -sS https://api.github.com/meta \
  | python3 -c 'import json,sys;[print("github.com",k) for k in json.load(sys.stdin)["ssh_keys"]]' \
  > ~/.config/acode/.ssh/known_hosts
```

One directory bind then carries both the key and the host keys, and the file
lands at the path ssh reads by default — `ssh -G github.com` prints
`userknownhostsfile /home/acode/.ssh/known_hosts`, and `~` there is the passwd
entry's home, not `$HOME`. Nothing is passed on the command line, so nothing has
to be remembered.

Read-only is the safer half of the choice, not an accident: ssh writes to
`known_hosts` only while learning an unseen host, and pre-populating removes
that write, so a writable mount buys nothing and would let an injected
instruction re-pin `github.com` to a key of its choosing — the exact attack the
file exists to prevent. The keys come from GitHub's published list over HTTPS
rather than `ssh-keyscan`, which trusts whichever host answers first and then
records it. One line per host you push to; an unknown host then fails loudly,
which is correct for a container holding a push credential. If you do want ssh
to *learn* hosts, that needs `StrictHostKeyChecking=accept-new`, and it belongs
in `core.sshCommand` in the mounted `.gitconfig` — not on each invocation, which
is the thing being avoided.

**GitHub authentication is opt-in, and the example ships it commented out.** A
credential the agent does not need only gives an injected instruction something
to use, so `cp compose.local.yaml.example compose.local.yaml` (§1) mounts no
credential until you uncomment a block. A
token cannot come from the environment instead: `git`, `curl`, and `gh` start as
`run_command` children, so an `_TOKEN` name is stripped before any of them sees
it, and the tool call is what has to authenticate.

The route is **`gh`, logged in as a bot account**, which gives a tool call
`gh api`, `gh pr`, `gh issue`, and `git push` through one login. It needs an
image carrying `gh`, which the published image does not: build
[`Dockerfile.gh`](../Dockerfile.gh) (§6), which is that image and nothing else.

**Log in on the host**, against a copy of the config directory, so the bind never
carries your own account:

```sh
install -d -m 0700 ~/.config/acode/gh
read -rs GH_PAT
printf '%s' "$GH_PAT" | env -u GH_TOKEN -u GITHUB_TOKEN \
  GH_CONFIG_DIR="$HOME/.config/acode/gh" \
  gh auth login --with-token --insecure-storage
unset GH_PAT
GH_CONFIG_DIR="$HOME/.config/acode/gh" gh auth status   # the bot, not you
```

`--insecure-storage` is not optional here: gh's default secure storage is the
host keyring, a service no bind can carry, so without the flag `hosts.yml` holds
a user name and no token and the container reads as logged out. The mount target
is fixed rather than chosen — gh reads `GH_CONFIG_DIR`, then
`XDG_CONFIG_HOME/gh`, then `HOME/.config/gh`, and a tool call gets neither of
the first two — which is why the example hard-codes `/home/acode/.config/gh`.
Read-only is the same call as the two files above: `gh auth login|switch|logout`,
`gh auth setup-git`, and `gh config set` fail inside, while the cache and state
gh writes land in `~/.cache/gh` and `~/.local/state/gh` in the container layer
and vanish with `--rm`. [`Dockerfile.gh`](../Dockerfile.gh) takes the release
`.deb` rather than a distribution package, because trixie's gh 2.46.0 is the
version the CLI maintainers call broken.

To push over an `https://github.com` remote through that login, git needs a
credential helper. [`Dockerfile.gh`](../Dockerfile.gh) writes one to
`/etc/gitconfig`, root-owned so a tool call cannot rewrite it, which is why an
`https` push authenticates even with the optional `.gitconfig` mount absent. If
you use your own image instead, the same two lines belong in that mount; the
empty `helper` first clears anything inherited:

```ini
[credential "https://github.com"]
	helper =
	helper = !gh auth git-credential
```

Bound the credential at GitHub, because the mount cannot bound it: a
**fine-grained PAT for this one repository** with `Contents: write`,
`Issues: write`, and `Pull requests: write`, a short expiry, and no
`Workflows: write` — that last permission is arbitrary code execution in the
workflow it touches. A classic PAT's `repo` scope instead covers every
repository the account can write, which is why the fine-grained kind is worth
the extra setup. `gh auth login`'s own help warns that a fine-grained token
passed to `--with-token` may behave confusingly once a command reaches past the
resources the token covers, and would rather you set `GH_TOKEN`; that is not an
option in a container whose `run_command` allowlist strips the name, so the
confinement has to come from the token's scope rather than from how gh reads it.
Then protect `main` with a required pull request, no force-push, and no bypass
for the bot, so a poisoned issue body or fetched page costs a pull request you
review rather than a commit you did not make. That is the difference from
mounting your own `~/.ssh`: a private key is readable wherever it is mounted,
it authorises every repository you can write, and nothing narrows it
afterwards. Either way the file is as visible inside the container as the
provider token above — same user, same namespace — so what holds is the scope
of the credential, not the mode on the mount.

A gateway in front of the real providers is worth it for one reason: the agent
container then holds a gateway key instead of the provider key, so the real key
lives in one place you can rotate. `provider_base_url` accepts plain `http` for
**any** host (it rejects only userinfo, a query, and a fragment, and a path
prefix is allowed), so both of these work:

```sh
-e ACODE_PROVIDER_BASE_URL=http://127.0.0.1:4000/v1            # with --network=host
-e ACODE_PROVIDER_BASE_URL=http://litellm:4000/go-native/v1    # pass-through prefix
```

The catalog rule of §1 applies either way: the model ID must exist in
models.dev, which fixes the dialect, context window, and limits, so a name that
exists only in the gateway is a startup error (exit 2). A pass-through gateway
also forwards that ID upstream verbatim, so it has to be a name the provider
accepts; a deployment-shaped gateway instead maps it to a real model in the
gateway's own configuration. Know which one you are running: only the second
enforces a model allowlist, and neither is a substitute for a spending cap at
the provider.

| Mode | Egress | Use when |
|---|---|---|
| `--network=host` | Unrestricted; a host gateway is reachable at `127.0.0.1` | The default for a trusted project. |
| A dedicated bridge network | Reaches only what is on that network | The project or its dependencies are untrusted. |
| `--internal` network plus the gateway | Reaches only the gateway | Inference is the only egress you want. |

An isolated network stops more than inference: `web_fetch`, `web_search`, and
any `go mod download`, `npm install`, or `cargo fetch` also stop working.
`web_fetch`'s private-address refusal is a policy in one tool, not a network
boundary, and `run_command` can reach anything the namespace can.
`searxng_url` and `models_dev_url` are stricter than the provider URL — HTTP
only for loopback hosts — so a SearXNG or catalog mirror must be reachable on
loopback, or be served over HTTPS. With a host network that is simply its
published loopback port:

```sh
podman run --rm -it --network=host \
  -e ACODE_SEARXNG_URL=http://127.0.0.1:8889 \
  -v "$PWD:$PWD" -w "$PWD" -v acode-state:/home/acode/.acode \
  ghcr.io/eddiectc/acode
```

Joining the backend's namespace (`--network container:searxng`) reaches the
same URL but only without a user namespace: `--userns=keep-id` and a netns join
are mutually exclusive, because `setns()` needs `CAP_SYS_ADMIN` in the user
namespace that owns the target (§1).

## 6. Building the image

The image does not compile acode. It copies `dist/linux/<arch>/acode`, the
binary a Go build produced, so the image, the release tarballs, and what
`acode -version` reports all come from one build:

```sh
make dist    # dist/linux/<host-arch>/acode: GOOS=linux, CGO off, -trimpath
make image   # docker build, VERSION and REVISION taken from git
```

[release.yml](../.github/workflows/release.yml) does the same with the binaries
it built for both linux architectures; by hand:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath \
  -ldflags "-X main.version=vX.Y.Z" -o dist/linux/arm64/acode ./cmd/acode

docker build --build-arg VERSION="$(git describe --tags --always)" \
  --build-arg REVISION="$(git rev-parse HEAD)" -t acode:local .
```

| Build argument | Default | Purpose |
|---|---|---|
| `RUNTIME_IMAGE` | `debian:trixie-slim` | The base image. |
| `TARGETARCH` | set by BuildKit | Selects `dist/linux/<arch>/acode`; the classic builder leaves it empty, so the build needs BuildKit. |
| `VERSION` | `devel` | `org.opencontainers.image.version` label only. |
| `REVISION` | `unknown` | `org.opencontainers.image.revision` label only. |
| `UID`, `GID` | `1000` | The runtime user. |

`VERSION` no longer stamps the binary: the build that produced the binary did
that, and the label mirrors it, so a published image and its tarball report the
same version. A local `make dist` passes no `-ldflags`, so a locally built image
reports the module and VCS revision while its label says what `git describe`
says. `--platform linux/amd64,linux/arm64` needs both trees in the context and
binfmt/QEMU registered; nothing is compiled in the build, but the package `RUN`
still executes per platform. The context carries `dist/` and `LICENSE` and
excludes the source ([`.dockerignore`](../.dockerignore)).

### Adding a toolchain

Derive an image for the workspace's toolchain. Three shapes work:

| Base image | acode comes from | Use when |
|---|---|---|
| `ghcr.io/eddiectc/acode` | the base image | The toolchain is packaged for Debian, or you are adding one or two tools. |
| The project's toolchain image | `COPY --from` the acode image | CI and the agent must share a base, and the toolchain is not a Debian package. |
| The project's toolchain image | `go install` a pinned tag | You cannot fetch the image or a release tarball. |

[`Dockerfile.gh`](../Dockerfile.gh) is the first shape worked out, and the image
§5's GitHub route needs: the published image, `curl` installed because the base
has no downloader, the pinned `gh` release `.deb` verified against its published
checksums, `~/.config` created for the bind target, and the
`https://github.com` credential helper in `/etc/gitconfig`.

```sh
docker build -f Dockerfile.gh -t acode:gh .        # for a direct run, §2
```

For the compose stack the launchers build the `acode` service themselves, so the
file goes in the override and the stack image is built from it:

```yaml
services:
  acode:
    build:
      dockerfile: Dockerfile.gh
```

`ACODE_IMAGE` still chooses acode's release: `compose.yaml` passes it as a build
argument, and it is `Dockerfile.gh`'s `FROM`. It cannot do this job by itself —
`ACODE_IMAGE` selects only the binary that `compose/Dockerfile` copies, so
pointing it at an image that carries `gh` would put none of it in the stack.

The cost of that override is the toolchain: the stack image becomes the published
image plus `gh`, not the `golang` base of §1, so `go build` and `go test` in the
workspace stop working. For a Go workspace, add the same three steps to
`compose/Dockerfile` in a local copy instead.

[`compose/Dockerfile`](../compose/Dockerfile) is the second shape worked out: a
`golang` base, `COPY --from` the published image, then the user, `HOME`, the
`0700` state directory, the cache directories of §3, `LANG`, `USER`, `WORKDIR`,
and `ENTRYPOINT`. Deriving is the least work — the user, `HOME`, `/bin/sh`, git,
and the CA certificates are already right:

```dockerfile
FROM ghcr.io/eddiectc/acode:vX.Y.Z
USER root
RUN apt-get update \
 && apt-get install -y --no-install-recommends nodejs npm \
 && rm -rf /var/lib/apt/lists/*
USER 1000:1000
```

Do not add Go that way: the distribution's `golang-go` trails the version in
`go.mod`, so `go` either downloads a toolchain (`GOTOOLCHAIN=auto`, needs
network) or refuses (`GOTOOLCHAIN=local`). An Alpine base needs
`addgroup`/`adduser`; a distroless base has no `/bin/sh`, and the SDK's
`run_command` is literally `/bin/sh -c`, so nothing can execute. `go install` is
the last resort — `@latest` is unreproducible, the build needs module-proxy
access, and the result carries no attestation — so pin the tag and keep the
caches out of the final image:

```dockerfile
FROM golang:1.27.4-trixie AS built
RUN GOBIN=/out go install github.com/eddiectc/acode/cmd/acode@vX.Y.Z
```

Whichever shape you choose, the agent runs the project's own checks
(`go test ./...`, `npm test`) against the mounted workspace through
`run_command`, which is the only verification worth trusting.

## 7. Continuous integration

| Workflow | Trigger | Result |
|---|---|---|
| [ci.yml](../.github/workflows/ci.yml) | push to `main`, pull request, manual | Runs `make dod`: the coding standard's §8 gates plus `docs-check`. |
| [release.yml](../.github/workflows/release.yml) | push to `main` or a `v*` tag, pull request, manual | Builds the binaries, attaches them to a tag's GitHub Release, and publishes `ghcr.io/eddiectc/acode` from those binaries. |

The release workflow has four jobs: `version` resolves one version string (the
tag name, or `devel-<short sha>` on a branch); `binaries` builds it for `linux`
and `darwin` on `amd64` and `arm64` with `-trimpath` and `-X main.version`;
`image` downloads the two linux artifacts into `dist/linux/<arch>/acode` and
builds the image from them; `release` (tags only) packages, attests, and
uploads the tarballs.

Release assets for a `vX.Y.Z` tag are `acode_<version>_<os>_<arch>.tar.gz` (the
binary and `LICENSE`, 0755, owned 0/0), `SHA256SUMS`, and a build attestation
checked with `gh attestation verify`.

| Event | Image tags | Release assets |
|---|---|---|
| Push to `main` | `latest`, `sha-<short>` | none |
| Tag `vX.Y.Z` | `vX.Y.Z`, `vX.Y`, `vX`, `sha-<short>` | tarballs, `SHA256SUMS`, attestation |
| Pull request | none; all four binaries and a `linux/amd64` image build, to prove the tree compiles | none |

A manual dispatch publishes nothing from any ref: only a push publishes, and
the push triggers are `main` and a `v*` tag, so an untested branch cannot reach
the registry. `latest` tracks `main` HEAD, so a tagged image can be older than
`latest`; the workflow gates on the tree compiling, and `ci.yml` is the test
gate. That split relies on branch protection requiring the CI check before a
merge, because a newer push cancels an in-progress run. The full branch and
tagging contract is in [release.md](release.md).

Windows is not built: `/bin/sh`, the manual PTY smoke, and `syscall.SIGTERM` are
Unix, CI is Linux-only, and a zip would be a claim nothing checks.

## 8. Verification status

Checked for this document: the base image tags resolve; UID and GID 1000 are
free in `debian:trixie-slim`, and `useradd`, `groupadd`, `install`, `apt-get`,
and `/bin/sh` exist there; `CGO_ENABLED=0` yields a statically linked binary
(checked with `file` and `ldd`); a `-ldflags "-X main.version=..."` build reports
that version; the Dockerfile is clean under `hadolint` 2.12.0, and both
workflows parse under `actionlint`, which ran its shellcheck pass over every
`run:` block. For §6, `make dist` wrote `dist/linux/<arch>/acode`, and the
packaging loop from §7 was run on that tree: it produced
`acode_v0.0.1-test_linux_amd64.tar.gz` containing the binary at mode 0755 and
`LICENSE`, and wrote a `SHA256SUMS` whose digest matched. A `go install` of
`github.com/eddiectc/acode/cmd/acode@v0.0.1` with `GOBIN` set resolved through
the module proxy and reported `acode v0.0.1` with `vcs revision: unknown`, which
is the tag-not-stamp behaviour §6 describes.

For §1, `docker-compose` 5.8.0 and `podman-compose` 1.6.0 both resolve
`compose.yaml` alone and merged with `compose.podman.yaml`; every compose and
settings YAML file parses; `ghcr.io/eddiectc/acode` resolves `latest`, `v0.1.0`,
and `sha-*` tags for anonymous pulls; `PWD` selects the mount and `working_dir`
from any directory, an explicit `ACODE_WORKSPACE` replaces it, Docker Compose
supplies `PWD` itself when the environment omits it, and `podman-compose` fails
with the named error instead of an empty path. The two launchers were exercised
against stub executables for the generated name, `--rm`, `--name`, `-T`,
`--build`, the path exports, argument passthrough, the workspace guard, and
exit-code propagation; their exact argument vectors were accepted by the real
`docker-compose` and `podman-compose` parsers, which then failed only on the
missing daemon and cgroups. Both scripts pass `bash -n`. For §5's
`compose.local.yaml`, the same stubs show it absent from the argument vector
when the file is missing and appended last when it is present, in both
launchers, and the stale-pod message names it. The allowlist that keeps
`GIT_AUTHOR_NAME` out of a tool call was read from `tools/command.go` in
aharness v0.4.1, and `HOME` is in that allowlist only because the image sets
`ENV HOME=/home/acode`, so the bind has a directory to land in; `git` with a
`user` section in a `HOME/.gitconfig` and nothing else set committed as that
identity and wrote no `user.*` into `.git/config`, while the same tree with no
gitconfig failed with "Committer identity unknown". For §5's `gh` route, a
stack was then built and run under rootless Podman, and these checks were made
from inside it — from a `run_command` child, which is the environment the claims
are about. In an image derived to carry `gh` (2.105.0, from the upstream
`.deb`), with the config directory bound read-only: `gh auth status`
resolved `account eddiectc-acode` from `/home/acode/.config/gh/hosts.yml`; the
same command with `GH_CONFIG_DIR` pointing at an empty directory reported no
login, so the credential comes from the mount and not from the image; `gh api
user` and `gh repo view` returned real data; `git credential fill` answered
through `/etc/gitconfig`'s `!gh auth git-credential` with the account name and a
40 character password; writing to `/etc/gitconfig` failed with `Permission
denied` as a non-root user; `touch` in the bound directory failed with
`Read-only file system`, as did `gh config set`; `~/.config` was owned by
`acode:acode`; and the child environment was exactly `HOME`, `LANG`, `PATH`,
`PWD`, `TERM`, and `TMPDIR`, with no `GH_CONFIG_DIR` and no `*_TOKEN` name — the
allowlist §5 describes, observed rather than read. The example ships that bind
commented out, so uncommenting it is still what a first run on a host confirms —
`podman-compose … config` shows the resolved mount source before that run.

The example's new default — every GitHub and identity bind commented out, with
`volumes: []` as the only active key — was checked by parsing it with a YAML
decoder, which yields one service key, `volumes`, holding an empty list; it was
not run through either engine's `config`, because neither is installed here.
That `volumes: []` matters rather than a bare `volumes:` is read from
podman-compose 1.6.0's `container_to_args`, which does `for volume in
cnt.get("volumes", [])`: a null value is returned by `get` and iterating it is a
`TypeError`. Docker Compose merges the empty list as a no-op, which follows from
its documented per-path merge of `volumes` but was not observed either.

The reason §5 and `Dockerfile.gh` take the upstream `.deb` instead of the
distribution package was read from two sources rather than assumed: Debian's
package tracker lists `gh` 2.46.0 as the version in `stable`, with the package
removed from `testing` in December 2025 and still carrying open security issues,
and the CLI's own install notes say the 2.45.x/2.46.x distribution package is
broken against current GitHub APIs. The 2.105.0 release assets, their
`gh_<version>_checksums.txt` file, and the `git`-only dependency were confirmed
against the release itself. For §5's SSH files this was exercised against the real service rather
than read: with `known_hosts` inside a `~/.ssh` directory bind marked read-only,
both `ssh -T git@github.com` and `git ls-remote github` succeeded with no
`UserKnownHostsFile` or `StrictHostKeyChecking` override at the default checking
level, and `ssh -G github.com` reports that same path, so the file is found
rather than passed by flag. The check is real and not TOFU: an empty
`known_hosts` and one holding only a wrong key both failed with "Host key
verification failed", while ssh falling through to a second, still-valid entry
is what an earlier misreading mistook for a pass. `HOME` is not what ssh
resolves `~` from — it uses the passwd entry — which is why the file has to sit
in the bound directory rather than at a path named on the command line.

The §1 gateway claims were read from the LiteLLM source at `main` and
`v1.92.0`, not from its documentation, which still labels pass-through `auth`
as Enterprise: `pass_through_endpoints.py` builds a subpath route as `target`
plus the rest of the path, expands `os.environ/` in `headers` only, treats
`auth` as enforced only when it is explicitly true, and registers the route
under the proxy's auth dependency; `passthrough/utils.py` drops a client header
whose name matches a configured one before merging, which is what keeps
acode's `Authorization` from replacing the upstream key. The gateway itself was
not started, so the forwarded URLs and the key substitution are read from that
source rather than observed; the two algorithms were replayed locally against
the source's own logic instead: `/go-native/v1/chat/completions` and
`/go-native/v1/responses` resolve to
`https://opencode.ai/zen/go/v1/chat/completions` and `…/v1/responses`, and a
client `Authorization` plus `user-agent`, `x-opencode-session`, and
`content-type` merge to the configured `Bearer` with the other three forwarded.

For §1's SearXNG, `compose/searxng/settings.yml` was checked against the
SearXNG source rather than its docs: top-level keys against `SCHEMA`,
`search`/`outgoing` sub-keys, engine modules against `searx/engines/`, entry
names against the built-in `settings.yml`, categories against
`CATEGORIES_AS_TABS`, plugin import paths, and `re.compile` on every hostname
pattern — all pass. That check was initially wrong about the plugin layout: it
required every top-level key to appear in `SCHEMA`, which pushed the hostnames
block inside the plugin entry and produced a real startup failure,
`PluginCfg.__init__() got an unexpected keyword argument 'hostnames'`. The
layout is now validated by mirroring `PluginStorage.load_settings` and
`hostnames.SXNGPlugin.init`: `PluginCfg` accepts only `active`, and the
hostnames patterns are read from the top-level `hostnames` key. That mirror
reproduces the reported `TypeError` from the nested form and loads the shipped
form with 12 patterns, and unknown top-level keys are confirmed to survive
`update_settings`. `SEARXNG_PORT` was confirmed to reach the listener through
the image entrypoint's `GRANIAN_PORT`. Under both engines with `config`, each of `SEARXNG_PORT`, `LITELLM_PORT`, and
`CRAWL4AI_PORT` moves its publish, listener, healthcheck, and the URL acode
uses together; a leftover `SEARXNG_HOST_PORT` from an older `.env` is inert.
`tools.ParseSearXNGURL` accepts `http://127.0.0.1:8889` and trims a trailing
slash. The two port mechanics come from the images: SearXNG's entrypoint copies
`SEARXNG_PORT` into `GRANIAN_PORT`, Crawl4AI's reads `CRAWL4AI_PORT` into
`GUNICORN_BIND`, and LiteLLM takes the port on its command line, where its own
`PORT` env var would otherwise apply.

The Podman argv was captured by standing a logging `podman` shim in front of
`podman-compose` 1.6.0 and inspecting every `create`/`run` it issued. Before
`x-podman: in_pod: false`, all four services carried `--pod=pod_acode-stack`;
after it, `--pod` is gone and the other three services keep
`--network=acode-stack_stack:alias=…`. That fixed the pod-specific failure
(`container dependency … is part of a pod, but container is not`) but not the
`conmon bytes ""` create error, which recurred with the pod gone and acode
still carrying `--network=container:…` plus `--userns keep-id:…`. Removing the
namespace join is what changes that: the emitted `run` now has
`--network=host` and keep-id, with no `--pod` and no container netns. The
`setns()`/`CAP_SYS_ADMIN` explanation is reasoned from the kernel rule and
podman's `oci_conmon_common.go` error path, not observed: no container was
created here, so whether host networking clears the failure is unconfirmed.


The stale-pod guard in `scripts/acode-podman` was exercised against a `podman`
stub: it stops with the teardown commands when `pod pod exists` succeeds, and
proceeds to `podman-compose run` with no `--pod=` in any emitted `create`/`run`
when it does not. `podman-compose` is unaffected by the extra `config` call the
guard makes to read the project name, which needs no daemon. Whether removing
the pod actually clears the dependency error is unconfirmed for the same reason.

Not checked, and required before the first published image is trusted: a
`docker build` and `docker run` (the run above was rootless Podman), a TUI
session against a real terminal, a real-provider run through a gateway, and one
end-to-end run of [release.yml](../.github/workflows/release.yml) on GitHub. The
image does now build and run under Podman, but not as
[`Dockerfile.gh`](../Dockerfile.gh) ships it: the built image was a local,
untracked variant that descended from the stack image so the same layer could
carry the Go toolchain, and its `gh` install, `~/.config` creation, and
`/etc/gitconfig` helper were the instructions now in `Dockerfile.gh`.
`Dockerfile.gh` itself has not been built, so its `FROM ghcr.io/eddiectc/acode`
base, the `USER root` / `USER 1000:1000` switch over that base, and a multi-arch
build are unverified; building it once on a host with an engine is what closes
that. It was not run through `hadolint` either, so the §6 claim about the
published `Dockerfile` does not extend to it. The §1 stack was also run under Podman, which confirms the container starts,
that `git`, `go`, and `gh` are on `PATH`, and that the workspace bind lands at
the host's absolute path; acode on host networking reaching the three published
loopback ports, the in-image `/healthz`, `/health/liveliness`, and `/health`
probes and their tools, the SearXNG `SEARXNG_SECRET`/`SEARXNG_BASE_URL` override
of a read-only `settings.yml`, whether that file loads under a real SearXNG, and
the Crawl4AI token are still unverified from inside it, as is the
native-passthrough path against a real provider. The workflow's
GitHub-side behaviour is also unverified: that an uploaded artifact keeps the
`dist/<goos>/<goarch>/` path the packaging loop and the Dockerfile expect, that
`merge-multiple` yields `dist/linux/<arch>/acode` for both linux artifacts, that
BuildKit resolves `TARGETARCH` inside `COPY`, and that
`actions/attest-build-provenance` and `gh release` succeed under the job's
permissions. The `go install` command itself was run on the host; only its stage
and `COPY` are untested.
