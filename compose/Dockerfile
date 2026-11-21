# syntax=docker/dockerfile:1

# acode for the local compose stack: docs/container.md section 6, second shape.
#
# The published image (../Dockerfile) deliberately carries no project
# toolchain, so acode could not build or test the workspace it edits
# (docs/container.md section 1). This image takes the project's Go toolchain as
# its base and copies the acode binary out of a published image; it does not
# compile acode, so the binary is still the one the release workflow built.
#
# Pin ACODE_IMAGE to a release tag or digest in a real project. `latest` tracks
# main and moves; after `make dist && docker build -t localhost/acode:local .`
# the stack can copy the binary from the checkout instead.
ARG ACODE_IMAGE=ghcr.io/eddiectc/acode:latest

# The tag lives in the ARG default above, which hadolint cannot see through.
# hadolint ignore=DL3006
FROM ${ACODE_IMAGE} AS release

FROM golang:1.27.4-trixie

# Match the image user to the host user. A host UID/GID other than 1000 builds
# with --build-arg UID=$(id -u) --build-arg GID=$(id -g); rootless Podman keeps
# the defaults and maps the caller onto 1000 with compose.podman.yaml. A base
# image's existing users and groups must not be reused: GID 100 is Debian's
# `users` group. See docs/container.md section 3.
ARG UID=1000
ARG GID=1000

# Only the file is needed, not the release image's base. buildpack-deps:scm,
# which the golang image derives from, already supplies /bin/sh, git, an SSH
# client, and the CA certificates that docs/container.md requires.
COPY --from=release /usr/local/bin/acode /usr/local/bin/acode

# What deriving from the release image would have supplied: the user, its HOME,
# the 0700 state directory, the two cache directories section 2 mounts, and a
# locale. A golang base keeps GOPATH and the module cache under root-owned /go,
# and the run_command environment passes HOME and PATH but not GOPATH, so the
# caches and the installed binaries belong under HOME.
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
