# acode branches, commits, and releases

- **Date:** 2026-11-20
- **Status:** current for this repository
- **Related:** [definition of done](definition-of-done.md),
  [coding standard](coding-standard.md), [container](container.md), and
  [AGENTS.md](../AGENTS.md)
- **Enforced by:** [`.github/workflows/ci.yml`](../.github/workflows/ci.yml) and
  [`.github/workflows/release.yml`](../.github/workflows/release.yml)

This document is the contract between a change and a published artifact. It is
deliberately small: the repository uses a trunk-based model with short-lived
feature branches, not GitFlow.

## 1. The model

| Branch | Lifetime | Purpose |
|---|---|---|
| `main` | permanent | The only long-lived branch. Protected; always the release source. |
| `feat/<topic>`, `fix/<topic>`, `docs/<topic>` | hours to days | One change. Deleted after merge. |

There is no `develop` branch, no `release/*` branch, and no `hotfix/*` branch.
GitFlow's two long-lived branches exist to buffer a release while `develop`
moves on; this project publishes `latest` from `main` on every commit
([container §7](container.md#7-continuous-integration)), so a second long-lived
branch would either diverge from `latest` or be a second name for `main`.

A release is a **tag on a `main` commit**, not a branch. That keeps the shipped
image, the tagged source, and the version the binary reports as one identity.

## 2. Commits

History is linear: rebase onto `main` and merge without a merge commit. Do not
create merge commits from a feature branch.

Subject lines are imperative, sentence case, and describe the behavior change —
what the tree does after the commit, not what you did to it:

```text
Make engine.Host own the TUI session
Seed workspace AGENTS.md into new sessions
Forbid working around the aharness SDK
```

- No `feat:`/`fix:` prefix. This repository does not use Conventional Commits;
  the image and CI workflows do not parse commit messages, so a prefix would be
  decoration.
- No trailing period in the subject. Wrap the body at 72 columns and use it to
  record *why*, a rejected alternative, or a pending verification — the things
  a diff cannot say.
- One commit per coherent change. Keep a refactor, a dependency bump, and a
  behavior change in separate commits, and never mix a `go.mod` change with an
  unrelated edit ([definition of done §5](definition-of-done.md)).

## 3. Pull requests

`main` is protected and requires the `make dod` status check from
[`.github/workflows/ci.yml`](../.github/workflows/ci.yml). That check *is* the
automated part of the [definition of done](definition-of-done.md), so a skipped
or failing `make dod` blocks the merge.

- Open the pull request against `main` from a branch in this repository.
- The `Release` workflow also runs on a pull request: it builds all four
  binaries, builds the image for `linux/amd64` only, and publishes nothing. It
  proves the tree compiles; it is not a test gate, and the packaging and
  publish steps are skipped.
- Do not force-push a branch under review unless the reviewer asked for it.
- Do not merge with a failing or skipped `make dod`. If a check is wrong, fix
  the check in the same change and say so in the description.

Branch protection is a repository setting, not a file in this tree: nothing here
can verify that it is on. Treat the check as required regardless.

## 4. Releases

A release is three steps, in this order:

1. Merge the change to `main` with `make dod` green.
2. Tag that commit, annotated, as `vX.Y.Z`:

   ```sh
   git switch main && git pull --ff-only
   git tag -a v0.1.0 -m "v0.1.0"
   git push origin v0.1.0
   ```

3. The tag push starts [`.github/workflows/release.yml`](../.github/workflows/release.yml),
   which stamps the version from the tag, builds the `linux` and `darwin`
   binaries, attaches their tarballs and a `SHA256SUMS` file to the GitHub
   Release, and publishes the container image built from the same linux
   binaries ([container §7](container.md#7-continuous-integration)).

Version numbers follow semantic versioning for the CLI and `-ui-json` surface:
a breaking flag, config key, protocol field, or exit-code change is a major
bump; a new flag, key, or command is minor; a fix is a patch. Below `v1.0.0`
those rules still apply, so a breaking change before `v1` is a minor bump and
must be named in the annotated tag message.

Tag **after** the CI run for that commit is green. A tag on an untested commit
publishes untested artifacts, and the tag is what users pin.

### Fixing a bad release

There is no hotfix branch. Branch from `main`, fix, merge, and tag the next
patch. If the bad release is already published, do not delete the tag: publish
the next patch and mark the old one as withdrawn. Deleting a tag leaves a
published image and a GitHub Release that no longer have a matching source tag.

## 5. What is enforced, and what is convention

| Rule | Enforced by |
|---|---|
| `make dod` passes | CI check on the pull request, when branch protection requires it |
| The tree builds for `linux` and `darwin` on `amd64` and `arm64` | `Release` workflow, which must succeed to publish |
| A release tag matches its tarballs and its image | `Release` workflow, which stamps the tag into the binary and reuses that string for the image label |
| Linear history, commit wording, one change per commit | Review, not tooling |

Do not add tooling to enforce commit wording or branch names: the value is in
the review, and a linter over commit messages would be a new dependency for a
rule a maintainer already checks.
