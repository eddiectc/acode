# syntax=docker/dockerfile:1

# acode container image.
#
# The image carries acode plus the runtime it needs to act on a workspace:
# /bin/sh (the SDK's run_command is literally `/bin/sh -c`), git, and CA
# certificates. It does not carry a project toolchain: derive an image from
# this one to add the compilers and test runners a project needs, otherwise the
# agent's own checks run against a workspace it cannot build. See
# docs/container.md.
#
# The image is not a sandbox. acode still runs with the privileges of the
# container user and can reach anything the container can. Isolation comes
# from the mounts, the user, and the network, not from acode.

ARG GO_IMAGE=golang:1.27.4-trixie
ARG RUNTIME_IMAGE=debian:trixie-slim

# Build on the native runner platform and cross-compile: Go needs no
# emulation to target another architecture, so a multi-arch build stays fast.
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS build

ARG TARGETOS
ARG TARGETARCH
# Stamped into the binary because the build context excludes .git and has no
# VCS metadata to derive a version from.
ARG VERSION=devel

WORKDIR /src

# Resolve modules in their own layer so source edits do not re-download them.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -buildvcs=false \
    -ldflags "-X main.version=${VERSION}" \
    -o /out/acode ./cmd/acode

FROM ${RUNTIME_IMAGE}

ARG VERSION=devel
ARG REVISION=unknown
# The container user. Rootless Podman maps the caller onto these IDs with
# --userns=keep-id, and Docker needs --user to match; a host user outside
# 1000 derives an image with different build args. See docs/container.md.
ARG UID=1000
ARG GID=1000

LABEL org.opencontainers.image.title="acode" \
      org.opencontainers.image.description="Terminal coding agent for a single workspace" \
      org.opencontainers.image.source="https://github.com/eddiectc/acode" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"

# git for the version-control tools and commits; openssh-client so an ssh
# remote can be fetched at all; ca-certificates for HTTPS. Note that the SSH
# agent socket does not reach a tool call: the SDK builds the run_command child
# environment from an allowlist that omits SSH_AUTH_SOCK. Use an HTTPS remote
# with a credential file under HOME, or a key in ~/.ssh. See docs/container.md.
# Individual Debian package versions are not pinned: the base image digest is
# the reproducible control, and pinning apt revisions breaks the build as soon
# as the archive drops one.
# hadolint ignore=DL3008
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates git openssh-client \
 && rm -rf /var/lib/apt/lists/* \
 && groupadd --gid "${GID}" acode \
 && useradd --uid "${UID}" --gid "${GID}" --create-home \
      --home-dir /home/acode --shell /bin/bash acode \
 && install -d -o "${UID}" -g "${GID}" -m 0755 /workspace \
 && install -d -o "${UID}" -g "${GID}" -m 0700 /home/acode/.acode \
 && install -d -o "${UID}" -g "${GID}" -m 0755 /home/acode/.cache \
 && install -d -o "${UID}" -g "${GID}" -m 0755 /home/acode/.cache/go-build \
 && install -d -o "${UID}" -g "${GID}" -m 0755 /home/acode/go/pkg/mod

COPY --from=build /out/acode /usr/local/bin/acode
COPY LICENSE /usr/share/doc/acode/LICENSE

# The default workspace is the mount point. Override it with -w to keep the
# host's absolute path, which resume and the recorded workspace rely on.
ENV HOME=/home/acode \
    LANG=C.UTF-8

# Numeric rather than the name: the UID is what the host maps with
# --userns=keep-id and --user, and a numeric USER needs no /etc/passwd lookup
# in a runtime that cannot resolve the image's users.
USER ${UID}:${GID}
WORKDIR /workspace

STOPSIGNAL SIGTERM

ENTRYPOINT ["acode"]
