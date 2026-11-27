# acode writing standard

- **Date:** 2026-11-27
- **Status:** normative for prose in this repository
- **Scope:** reference documents, plans, code comments, commit messages, issues,
  pull requests, and the README
- **Related:** [coding standard](coding-standard.md) (Go practice),
  [release](release.md) (branches, commit form, tags),
  [definition of done](definition-of-done.md) (completion), and the
  [documentation map](README.md) (where a document belongs)
- **Agent entry point:** [AGENTS.md](../AGENTS.md)

The coding standard wins on Go practice and the definition of done governs
completion; this document governs the prose they do not cover. Where prose
disagrees with the code or its tests, the code wins. An instruction file reaches
a model as an instruction and the code as tool output, and agents follow a stale
file over the code even when they notice the conflict
([arXiv 2610.30571](https://arxiv.org/abs/2610.30571)).

Normative terms are defined in the [coding standard](coding-standard.md). A MUST
here is required for merge and is checked in review.

## 1. Principles

- **Code is the record.** A fact a reader can get from a type, a test, `go doc`,
  `-help`, or a name MUST NOT be restated in prose.
- **One home per fact.** §2 names it. A second copy is deleted in the change that
  notices it.
- **Every line is paid for.** The reference set is roughly 50k tokens at four
  bytes per token, and an agent reads the workspace `AGENTS.md` at the start of
  every session. Write for the reader who has the file open, not for one who
  might.
- **A check beats a sentence.** If `go test`, `go vet`, `golangci-lint`, or
  `make docs-check` can enforce a rule, it goes there and leaves the prose.
- **Delete as you go.** The sentence a change makes false is deleted in that
  change; there is no "update the docs later".
- **Frozen means frozen.** A dated record says what was believed and when; it
  never becomes a second reference.

## 2. Where a fact lives

| Fact | Home |
|---|---|
| What a type, function, or field promises | Doc comment on the declaration, and the test that checks it |
| A key, flag, cap, command, or protocol field a user sees | One reference document ([tui.md](tui.md), [commands.md](commands.md), [configuration.md](configuration.md), [limits.md](limits.md)) plus a golden or unit test |
| Why a change happened, and what was rejected | That commit's body |
| Prospective work, with milestones and deviations | `docs/plans/<topic>-build.md` |
| How to build, test, and release | `Makefile`, CI, and [release.md](release.md); the command is the source |
| A rule for working in this repository | `AGENTS.md` when every task needs it; a nested `AGENTS.md` when one directory does |
| An SDK contract: loop, tool, session, provider | [aharness](https://github.com/eddiectc/aharness): link to the section, never restate it |
| Dependency, toolchain, and platform facts | `go.mod`, CI, and the `Dockerfile`; never restated |
| What the product does for a user | [README.md](../README.md): install, quick start, feature map |

Three consequences:

- **No second index.** The [documentation map](README.md) owns the document
  table. The root README links to it once and does not repeat the rows.
- **No generated API reference.** `go doc -all` is the reference; a checked-in
  copy is a stale copy.
- **No status ledger in an index.** Status belongs to the plan or to the
  document's own header; a table of contents states what each document is.

## 3. Budgets

A ceiling is a review rule, not a target to fill. A change that touches an
over-ceiling document MUST NOT make it longer, and SHOULD move a section to its
home in §2. The two totals are targets reached by those changes, not a sweep and
not a gate.

| Surface | Lines |
|---|---|
| `AGENTS.md` | 80 |
| `README.md` | 150 |
| One reference document | 300 |
| One plan | 400 |
| One issue or pull-request body | 60 |
| Reference set: `AGENTS.md`, `README.md`, and `docs/*.md` | 2,500 target |
| `docs/plans/` total | 1,000 target |

A document that needs more than 300 lines is two documents, or a fact that
belongs in code. A plan that needs more than 400 is a reference document
pretending to be dated. The totals keep the average reference document near 200
lines: adding a document means shrinking another.

## 4. Documents

The [documentation map](README.md) defines the document kinds and where each
belongs; the [definition of done](definition-of-done.md) §3 requires the update
in the same change. This standard adds:

- An implemented plan MAY be deleted once its still-true facts live in the
  reference or a commit body; git history is the archive. An implemented plan is
  the least-read prose in the tree.
- Prose links to a plan for history, never for behavior. A plan is not an
  authority.

## 5. Code comments

The [coding standard](coding-standard.md) owns where a doc comment is required;
this section owns what it says.

- A doc comment states the contract: what the caller gets, what it may assume,
  and how it fails. It does not narrate the implementation.
- A body comment MUST explain why, or record an invariant, a gotcha, or the
  measurement behind a constant. A comment that restates the line is deleted.
- An invariant in a comment names the test that enforces it. Without a test it is
  a warning, not a contract.
- No changelog in code: no "previously", no `TODO: implement`, no pasted error
  text. The commit body holds that.
- A comment longer than three lines SHOULD be a rename or an extraction instead.
- Delete a comment in the same change that makes it false.

## 6. Commits

[release.md §2](release.md) owns the form and the body's purpose. Two rules are
additional:

- The body explains why. A list of edited files, a tool log, or a pasted
  transcript is not an explanation.
- No AI attribution trailer. `Co-authored-by` asserts a co-author, and a model is
  not one. Disclose AI involvement once in the README, not in every commit.

## 7. Issues and pull requests

- Use two YAML issue forms, `.github/ISSUE_TEMPLATE/bug.yml` and `feature.yml`,
  plus `config.yml`. A YAML form can require a field before submission; a
  Markdown template cannot.
- Set `blank_issues_enabled: false` and add a `contact_links` entry for a private
  security advisory. `SECURITY.md` is five lines: supported versions, "open a
  private advisory", "do not file a public issue".
- The bug form requires `acode -version` output, OS and terminal, provider and
  model, the exact command or keys, expected versus actual, and a short log
  excerpt. It never asks for a token, a full environment, or a whole session log.
- The feature form requires the problem, the current workaround, and what "done"
  looks like. It does not ask for a design; the design is the pull request.
- An issue title states the observable problem, not a proposed solution. The body
  is one screen: reproduction before narrative, no pasted agent transcript, local
  absolute path, or token.
- A pull-request template asks three prose questions: what changed and why, what
  the gate did not cover, and who reviewed it. Do not paste the definition of
  done into it; `make dod` is the gate, and a copied checklist is rubber-stamped.
- An agent-authored issue or pull request states what was verified and what was
  not, in the same terms as the definition of done. "Tests pass" is not a claim;
  the named gate and the pending smoke are.

## 8. What is not checked

The ceilings are review rules, not CI gates: several documents are over them, and
a gate that fails on `main` teaches people to bypass gates. `make docs-check`
stays a relative-link check, and external links stay unchecked because they need
the network.

Do not add Vale, markdownlint, or lychee without a demonstrated class of defect
that `docs-check` cannot catch. Duplicate rules are the failure this standard
cares about most, and no linter detects them; review does.

## 9. Code as the map

A directory listing is the first document an agent reads, so names carry the
navigation load.

- One concept per file, named for the concept a document or a search would use.
  `internal/tui/` has `settings_overlay.go`, `agents_overlay.go`, and
  `transparency_overlay.go`, yet `widgets.go` still holds the help, details,
  table, file, model, effort, and session pickers. That name says nothing; split
  it along the overlays when a change touches it. Do not split to hit a line
  count; split where a reader would look for the file and not find it.
- A test name states the behavior a document would otherwise claim, so
  `go test -run` is a search over the spec and table cases carry the documented
  examples.
- The `-ui-json` reference and `internal/tui/protocol.go` encode the same
  contract. Golden frames under `internal/tui/testdata` make a field change fail
  a test as well as update [tui.md](tui.md), which stays the user-facing
  contract.

## Sources

- [AGENTS.md format](https://agents.md/)
- [Claude Code instruction-file guidance](https://code.claude.com/docs/en/memory):
  200-line target per file, imports do not reduce context cost, path-scoped rules
  load on demand
- [How to Write a Git Commit Message](https://cbea.ms/git-commit/)
- [GitHub issue form syntax](https://docs.github.com/en/communities/using-templates-to-encourage-useful-issues-and-pull-requests/syntax-for-issue-forms)
- [Go doc comments](https://go.dev/doc/comment)
- [Stale instruction files override the code](https://arxiv.org/abs/2610.30571)
- [Always-loaded context is charged every round](https://arxiv.org/abs/2610.11007)
