# syntax=docker/dockerfile:1

# acode for the local compose stack: a Go toolchain with the release binary
# copied in, so the agent can build and test the workspace it edits.
# See docs/container.md section 6.
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

# Only the file is needed, not the release image's base: buildpack-deps:scm
# already supplies /bin/sh, git, an SSH client, and the CA certificates.
COPY --from=release /usr/local/bin/acode /usr/local/bin/acode

# The user, its HOME, the 0700 state directory, and the cache directories of
# docs/container.md section 3. A golang base keeps GOPATH under root-owned /go,
# and run_command children get HOME and PATH but not GOPATH.
USER root
RUN groupadd --gid "${GID}" acode \
 && useradd --uid "${UID}" --gid "${GID}" --create-home \
      --home-dir /home/acode --shell /bin/bash acode \
 && install -d -o "${UID}" -g "${GID}" -m 0700 /home/acode/.acode \
 && install -d -o "${UID}" -g "${GID}" -m 0755 /home/acode/.cache \
 && install -d -o "${UID}" -g "${GID}" -m 0755 /home/acode/.cache/go-build \
 && install -d -o "${UID}" -g "${GID}" -m 0755 /home/acode/go/pkg/mod \
 && install -d -o "${UID}" -g "${GID}" -m 0755 /workspace

ENV HOME=/home/acode \
    LANG=C.UTF-8 \
    PATH=/home/acode/go/bin:$PATH

USER ${UID}:${GID}
WORKDIR /workspace

STOPSIGNAL SIGTERM
ENTRYPOINT ["acode"]
