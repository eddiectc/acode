# syntax=docker/dockerfile:1

# acode for the local compose stack: a Go toolchain, the released binary, the
# GitHub CLI, and the check tools, so the agent can build, test, lint, and push
# the workspace it edits. The published image stays toolchain-free; this one is
# a build environment. See docs/container.md section 6.
ARG ACODE_IMAGE=ghcr.io/eddiectc/acode:latest

# The tag lives in the ARG default above, which hadolint cannot see through.
# hadolint ignore=DL3006
FROM ${ACODE_IMAGE} AS release

FROM golang:1.27.4-trixie

# The image user; a host UID/GID other than 1000 builds with --build-arg, and
# rootless Podman maps the caller with compose.podman.yaml. Do not reuse a base
# image ID: GID 100 is Debian's `users` group.
ARG UID=1000
ARG GID=1000

# Selects the gh .deb for the platform being built. BuildKit defines it; the
# classic builder leaves it empty and the RUN falls back to dpkg's name for the
# host. Only amd64 and arm64 matter: those are the platforms acode publishes
# images for, and the two whose gh release assets are named linux_amd64 and
# linux_arm64.
ARG TARGETARCH
ARG GH_VERSION=2.105.0

# The check tools of docs/coding-standard.md section 8, at the versions
# .github/workflows/ci.yml installs, so `make dod` here and CI run the same
# linter and vulnerability scan. Bump both together.
ARG GOLANGCI_LINT_VERSION=v2.15.0
ARG GOVULNCHECK_VERSION=v1.9.0

# Only the file is needed, not the release image's base: buildpack-deps:scm
# already supplies /bin/sh, git, an SSH client, the CA certificates, curl, and
# dpkg.
COPY --from=release /usr/local/bin/acode /usr/local/bin/acode

# The user, its HOME, the state directory, the cache directories of
# docs/container.md section 3, and the mount targets the stack binds into. A
# golang base keeps GOPATH under root-owned /go, and run_command children get
# HOME and PATH but not GOPATH.
USER root
RUN groupadd --gid "${GID}" acode \
 && useradd --uid "${UID}" --gid "${GID}" --create-home \
      --home-dir /home/acode --shell /bin/bash acode \
 && install -d -o "${UID}" -g "${GID}" -m 0700 /home/acode/.acode \
 && install -d -o "${UID}" -g "${GID}" -m 0755 /home/acode/.cache \
 && install -d -o "${UID}" -g "${GID}" -m 0755 /home/acode/.cache/go-build \
 && install -d -o "${UID}" -g "${GID}" -m 0755 /home/acode/go/pkg/mod \
 && install -d -o "${UID}" -g "${GID}" -m 0755 /workspace \
 && install -d -o "${UID}" -g "${GID}" -m 0700 /home/acode/.config

# gh, from the release .deb rather than apt-get: trixie ships 2.46.0, which the
# CLI maintainers call broken. The checksum comes from the release's own list
# rather than a hash typed here, because it differs per architecture. /etc/gitconfig
# is root-owned, so a tool call cannot rewrite the credential helper that lets an
# https push over github.com use the same login; the blank helper first clears
# anything inherited, as `gh auth setup-git` does.
RUN set -eux; \
    arch="${TARGETARCH:-$(dpkg --print-architecture)}"; \
    base="https://github.com/cli/cli/releases/download/v${GH_VERSION}"; \
    deb="gh_${GH_VERSION}_linux_${arch}.deb"; \
    dir="$(mktemp -d)"; \
    curl -fsSLo "${dir}/${deb}" "${base}/${deb}"; \
    curl -fsSLo "${dir}/gh_checksums.txt" "${base}/gh_${GH_VERSION}_checksums.txt"; \
    grep " ${deb}\$" "${dir}/gh_checksums.txt" > "${dir}/gh.sha256"; \
    (cd "${dir}" && sha256sum -c gh.sha256); \
    dpkg -i "${dir}/${deb}"; \
    rm -rf "${dir}"; \
    gh --version; \
    git config --system credential.https://github.com.helper ''; \
    git config --system --add credential.https://github.com.helper '!gh auth git-credential'

# Run as root because /usr/local/bin is root-owned. GOTOOLCHAIN=local keeps the
# install on the base image's Go instead of fetching another toolchain, and the
# caches are dropped afterwards because nothing else uses root's: the agent's
# GOPATH is /home/acode/go, mounted as a volume.
RUN set -eux; \
    export GOTOOLCHAIN=local GOBIN=/usr/local/bin; \
    go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}"; \
    go install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"; \
    golangci-lint version; \
    govulncheck -version; \
    go clean -cache -modcache

ENV HOME=/home/acode \
    LANG=C.UTF-8 \
    PATH=/home/acode/go/bin:$PATH

USER ${UID}:${GID}
WORKDIR /workspace

STOPSIGNAL SIGTERM
ENTRYPOINT ["acode"]
