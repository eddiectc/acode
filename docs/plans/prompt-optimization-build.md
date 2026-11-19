# acode prompt optimization build plan

- **Date:** 2026-11-19
- **Status:** proposed. Nothing applied. Phases 0–3 below; the SDK asks in
  §4.3 are upstream items, not app edits.
- **Source:** a review of every prompt surface aharness v0.2.0 ships (the
  main-agent template, the child template, the five bundled role
  definitions, and the tool schemas), plus a 2026-11 web research pass on
  prompt best practice (§3, sources in §15). §1 records the incidents that
  motivated the plan so it stands alone — the working notes that carried
  them were temporary and are gone.
- **Target:** Go 1.27.4, module `acode`, aharness v0.2.0.
- **Architecture:** [architecture.md](../architecture.md) defines the SDK
  boundary; the composition note lands with Phase 3. Prompt caps are in
  [limits.md](../limits.md).
- **Standard:** [coding-standard.md](../coding-standard.md);
  **Definition of done:** [definition-of-done.md](../definition-of-done.md);
  **Testing:** [testing-strategy.md](../testing-strategy.md).

## Decision summary

Two decisions drive everything else:

1. **acode owns its main-agent system prompt — by composition, not
   transcription.** The identity line is user-visible product surface, so the
   branding rule in [AGENTS.md](../../AGENTS.md) applies; aharness is a
   library whose frozen template is the default for hosts that supply none.
   acode composes: its own identity line, the SDK's exported prompt body
   (loop contract, todo/delegate mechanics, agent catalog), and its own
   delegation policy. Nothing contract-shaped is hand-copied into this repo.
2. **Descriptions are routing material, so they carry triggers.** The
   parent's entire delegation-decision surface is the five description
   strings. Today's `reviewer` description is a capability label with no
   trigger, which is why review was never delegated (§1.2).

| Phase | Contents | Status |
|---|---|---|
| 0 | Zero-code pilot: install the §6 role definitions in `~/.acode/agents/` | Proposed |
| 1 | Bundled role definitions upstream (five descriptions + bodies) | Proposed |
| 2 | SDK `prompt-v3` shared body + the three ownership exports | Proposed |
| 3 | acode composes its prompt through `Config.SystemPrompt` | Proposed |

## 1. Problem

### 1.1 The incident

A 30-file change (the aharness v0.2.0 bump plus a migration and adoption
pass) was written, self-reviewed, committed in three commits, and reported as
clean. No `implementer` and no `reviewer` subagent was used. A `reviewer` and
an `explore` subagent, run *after* the commits, found defects the author had
missed — including two the author had explicitly reported as **fixed**:

| Claimed fixed | Reality |
|---|---|
| Tool-name literals replaced with `tools.Name*` | `internal/tui/app.go:1289` still had `ev.Name == "todo"` — one line from a branch that did use `tools.NameDelegate` |
| `Result.Reason` adopted | `cmd/acode/tui.go:708-711` still derived the reason from `result.Err`, so the host's last-run line could disagree with the transcript and `-ui-json` |

It also found a behavior regression the author introduced without
documenting it: the TUI's model-refresh warning went from *reported* to
**silent**, because the new `pendingWarning` write (`cmd/acode/tui.go:176-182`)
has no reader on the refresh path. The tree was green throughout; these are
correctness and coverage gaps, not build breaks.

Skipping `implementer` was correct — the work was cascading (each compile
error depended on an SDK signature read moments before, and a child starts
with no conversation history). Skipping `reviewer` was the defect: a commit
is self-contained, and re-reading your own code finds typos, not defects,
because the assumptions that produced the defect are still active. The
remaining host defects from that review are code work, tracked separately
from this plan; what matters here is *why the review never happened*.

### 1.2 Why the review never happened

The delegation guidance framed its trigger as a cost filter: "delegate when
it would flood this conversation with tool output, or when independent
read-only tasks can run in parallel." Review is neither — a diff does not
flood context, and review is not parallel work. So the trigger never fired.
Delegation criteria written as cost filters will never fire for review;
review's trigger must be expressed as *what mistake it prevents*, and the
parent must learn it from somewhere it actually reads.

### 1.3 The routing gap

The only surface the parent reads when deciding to delegate is the catalog
rendered from the agent descriptions. The bundled `reviewer` description is
"Correctness, regression, and test-gap review of a change" — what it does,
not when to call it. A parent reading that learns nothing about why
delegating beats a self-check. `implementer`'s description encodes a trigger
("scoped… one change"); `reviewer`'s does not, and the observed failure is
the predicted outcome.

### 1.4 Composition gaps

- **Children never see the workspace's rules.** `RenderSystemPrompt` receives
  only the workspace and the agent catalog; a delegated child gets its role
  body plus the harness rules, and nothing tells it to read `AGENTS.md`.
  `reviewer` has `read_file` and *could*, but no instruction does.
- **acode injects nothing.** It never sets `AppendSystemPrompt` or
  `Instructions`, so a new session gets the SDK template plus the workspace
  path and nothing else.
- **The SDK brands the product.** The identity line "You are aharness, a
  coding harness…" is recorded verbatim in `session_start`, replayed on
  resume, and visible in the transcript and child-session views — against
  this repo's own non-negotiable that user-visible branding is acode's.

## 2. Prompt surfaces and constraints

| Surface | Lives at | Reaches | Constraint |
|---|---|---|---|
| Main-agent template, prompt-v2 | `aharness/agent/prompt.go` | Parent, rendered per session, recorded verbatim | Frozen: edits need a new prompt version; ≤32 KiB recorded |
| `harnessRules` shared body | same | Parent **and** every child (`childSystemPrompt`) | Frozen with the version |
| `todo` / `delegate` guidance | same | Parent only (`delegate`); both when granted (`todo`) | Frozen with the version |
| Child template + role `Prompt` | `aharness/agent/agents.go` | One child session, recorded verbatim | Body ≤16 KiB; tools enforce what a prompt would otherwise request |
| Role `Description` | same | Parent only, via `AgentCatalog.Infos()` → `RenderSystemPrompt` | **≤200 bytes**, enforced in `validateAgentDefinition` |
| Agent catalog render | `prompt.go` | Parent only | ≤4 KiB for all lines combined |
| `delegate` tool description | `tools/schemas.go` (toolset v2) | Parent, every request | Frozen once shipped; already states self-containment — left alone |

Two structural facts shape the design:

1. **Descriptions are the router's only signal** (§1.3), and the 200-byte cap
   forces the right split: routing triggers in the description, behavior
   detail in the role body, which only loads when that agent runs.
2. **Sessions record the resolved prompt verbatim.** Resume replays the
   recorded text, so a bundled role `Prompt` or `Description` change reaches
   new sessions and new children without a prompt version bump — only the
   frozen templates need `prompt-v3`. Verify against the session replay
   contract before relying on this; if it does not hold, ship the role
   changes with the next prompt version anyway.

### Host override surface

Verified against v0.2.0; acode currently uses none of these.

| Knob | Effect | Reaches | Notes |
|---|---|---|---|
| `aharness.Config.Instructions` | appended after the rendered prompt | New/Fork parents only | Rejected on resume; lands *after* the agent catalog; carries no version of its own |
| `aharness.Config.SystemPrompt` | replaces the whole prompt | New/Fork parents only | The designed extension point this plan adopts (§4); must keep the loop invariants `doc.go` names |
| `<state-dir>/agents/*.md` | replaces one bundled definition by name | that role, parent catalog and child | `agent.LoadAgents`, already called at `cmd/acode/tui.go:758`; the 200-byte cap applies; invalid files are skipped with a warning and never replace |

Children are reached by none of the config knobs: `childSystemPrompt`
composes from the definition alone. That is why §1.4's fixes ship upstream in
the shared body rather than as an app-side append.

## 3. Evidence: current prompt best practice (2026-11, model-agnostic)

Principles applied in §4–§6, each cross-vendor confirmed:

1. **Lean beats complete.** Every token spends attention on every request;
   leaner prompts measurably outperform bloated ones (OpenAI's GPT-5-era
   guidance reports ~10–15% score gains while cutting tokens 41–66% — figure
   quoted secondhand via Simon Willison's notes on an OpenAI page since
   revised; treat the direction as solid, the number as indicative). Claude
   Code cut its system prompt ~80% for current models and reports better
   behavior.
2. **Write only what the model cannot discover from the environment.** Audit
   for contradictions, stale rules, and dead references — contradictions
   burn reasoning reconciling them (Anthropic context engineering).
3. **Say what to do, not what not to do.** Don't-lists degrade quality on
   current models; rewrites go positive, and absolute rules become
   conditional ones with their reason attached.
4. **Explain the why behind non-obvious rules.** Models generalize from the
   explanation (Anthropic).
5. **Descriptions carry trigger conditions, not capability labels.** "Reviews
   code for security issues before commits" routes; "security expert" does
   not (Claude Code subagents docs). Keep them brief; move detail into the
   role body. Overlapping scopes degrade routing.
6. **A child assumes amnesia.** The delegation prompt is the child's entire
   world: objective, output format, tools/sources, boundaries (Anthropic
   multi-agent research post).
7. **Make completion verifiable, and demand reporting of the unverified.**
   "No or incomplete verification" and unreported unknowns are measured
   top-failure categories (MAST, arXiv:2503.13657, 2025).
8. **Give an out instead of inviting a workaround.** If a task is infeasible
   or a check is wrong, say so rather than gaming it (Anthropic's canonical
   anti-test-gaming snippet; OpenAI's escape-hatch guidance).
9. **Enforce mechanically what a prompt can only request.** Read-only is a
   tool allowlist, not an instruction line (Claude Code best practices).
   aharness already does this, so role bodies state only the useful *do*
   half.
10. **Gate prompt changes on evals and traces, not intuition.** Read raw
    transcripts because the agent's self-report is not the behavior
    (Anthropic's tools post). This repo already versions prompts; §13
    applies that.

### Excluded as outdated or model-specific

| Excluded | Why |
|---|---|
| CoT incantations ("think step by step") | No benefit on reasoning models, sometimes negative; effort is the knob, not wording |
| Emphasis theater ("IMPORTANT", "CRITICAL: You MUST") | Anthropic explicitly recommends softening |
| Anti-laziness exhortation ("be thorough") | Helped older models, measurably hurt GPT-5 (the Cursor case); removal improved behavior |
| Flattery / emotional pressure / "world-class" | 2023-era evidence; a 2026 follow-up finds more accuracy but *more sycophancy* |
| Few-shot examples in system prompts | Vendor-divergent; a frozen cross-model prompt cannot tune per model |
| Vendor format prescriptions (Claude XML/prefill, OpenAI verbosity params, "think very hard") | Model-specific by construction |
| Hardcoded effort budgets ("3–10 tool calls") | Model- and task-tuned numbers; keep the principle, drop the numbers |
| Long don't-walls | A single crisp prohibition survives only where the prohibition is the clearest form |

## 4. Design — acode owns the main-agent system prompt

Three grounds, the first already repo policy:

1. **The branding non-negotiable decides it.** [AGENTS.md](../../AGENTS.md):
   "User-visible branding is acode: the `acode` binary, the `ACODE_*`
   environment variables, `~/.acode` state, the user agent, temp names, and
   docs." The identity line is recorded in `session_start` and replayed on
   resume — it is user-visible branding.
2. **aharness is a library.** It ships no `cmd/` and no binary; its frozen
   template is the default for hosts that supply none. acode is the
   first-party product, so it is precisely not that host. Ownership also
   restores the cadence: the product prompt becomes product-versioned and
   product-evaluated instead of waiting on an SDK release.
3. **The knob is a designed extension point.** `doc.go` documents
   `Config.SystemPrompt` together with the invariants a replacement must
   keep. The no-shim rule in [AGENTS.md](../../AGENTS.md) targets
   compensating for missing capability; using a documented capability is
   composition.

The obvious failure mode — hand-copying the contract lines so they drift the
first time the SDK adds one — is solved by composition, not by declining
ownership.

### 4.1 Composition, not transcription

| Block | Owner | Why |
|---|---|---|
| Identity: "You are acode, a coding agent working in the workspace %q." | acode | product surface; the branding rule |
| Loop contract, todo and delegate mechanics, agent catalog | SDK, exported as one body | invariants and mechanics the loop depends on; one source, no drift, children share them |
| Delegation *policy* (§4.5) | acode | when to use which role is product behavior, and it is parent-only either way |

### 4.2 Mechanics (verified in the v0.2.0 source)

- **New and Fork only.** Resume inherits the recorded text — correct, since
  resume replays the session that ran. acode's fork path re-supplies the
  composed prompt every time.
- **The plumbing already exists.** acode's run config carries `SystemPrompt`
  into `engine.Spec` (`cmd/acode/config.go:123-126`, `cmd/acode/run.go:410`);
  today it is filled from recorded session state only. Ownership is a
  builder plus call sites, not new plumbing.
- **Recorded verbatim, 32 KiB cap.** Resume and a default fork inherit
  acode's prompt with no re-supply.
- **It lives in `cmd/acode`**, which [AGENTS.md](../../AGENTS.md) assigns
  "wiring and I/O only" — composing a prompt is wiring; the loop,
  projection, and session contracts stay in the SDK.

### 4.3 The SDK asks (contract deltas, filed upstream)

1. **`agent.PromptBody(version, workspace, agents) string`** — the rendered
   prompt without the identity line, so hosts compose instead of transcribe.
   Today `harnessRules`, the guidance blocks, and `renderAgentCatalog` are
   unexported, so a host's only choices are the SDK-branded whole or a
   from-scratch transcription.
2. **A host-supplied identity.** `childSystemPrompt` hardcodes "You are the
   aharness %q agent", so a replaced *parent* prompt cannot reach children.
   One field — `Config.Identity`/`Spec.Identity`, the same pattern as the
   existing per-host `UserAgent` — fixes the child line and gives library
   users the parent default too.
3. **A caller prompt tag.** `openNew` requires a *supported* `PromptVersion`
   even when the prompt is replaced, so acode's prompt would record as
   `prompt-v2` (unattributable for evals) and a tag like `acode-prompt-v1`
   is rejected outright. Ask for a caller tag recorded alongside the SDK
   template version, keyed to it for replay. Interim: attribute by content
   hash of the recorded text.
4. **Fork edge, either way:** a fork that sets only `Instructions`
   re-renders the SDK prompt from the *fork's* catalog and drops the
   recorded replacement (`resolveInherited`), so a replaced-prompt session
   forked with instructions alone resurrects "You are aharness". acode
   avoids the path by always re-supplying; the SDK could instead append to
   the recorded prompt.

### 4.4 The SDK `prompt-v3` shared body (upstream; acode inherits via the export)

These improve the shared body for library users and children; acode picks
them up through `PromptBody` with no copy:

| v2 line | proposed v3 line | Why (§3) |
|---|---|---|
| "Do not claim an action you did not take through a tool call." | "report an action as done only when a tool call performed it." | Positive form of the same rule (#3) |
| (absent) | "every tool call receives exactly one result" | `doc.go` names it an invariant a replacement must keep, yet the v2 text never states it |
| "Run the relevant tests after changing code." | "…before reporting it done, and say plainly what you could not verify." | Completion must be verifiable; the unverified half must be reported (#7). The direct antidote to §1.1 |
| (absent) | "If the workspace carries project instructions (AGENTS.md or similar), read them before acting on the code." | §1.4: nothing tells a child to read repo rules. One read per session is the whole cost |
| (absent) | "If a task is infeasible as stated, or a check is wrong, say so rather than working around it." | The escape hatch (#8); a child cannot ask, so stating refusal is its only channel |
| "Stop … when the task is complete or when you are blocked." | adds "when blocked, say what blocks you and what remains." | A blocked stop that reports nothing is indistinguishable from done |

Deliberately unchanged in the shared body: the todo guidance (byte-identical
to v2, pinned by test), the sandbox/privileges fact (it is the *why*, #4),
and "tool errors are data" (already correct positive form with its reason).

### 4.5 acode's own block — delegation policy

Parent-only either way (children cannot delegate), so product behavior, not
frozen-template material:

```markdown
- Delegate when you need a conclusion rather than the process: a broad
  read-only sweep, a research question, or an independent review. A reviewer
  starts without your assumptions and catches the confident wrongness a
  self-check cannot; hand it the changed files, the intent, and each behavior
  change you believe you made.
- Keep work in this conversation when each step depends on what the previous
  one revealed. When child reports disagree, reconcile them yourself before
  acting on them.
```

The first bullet is the §1.2 fix: the review trigger states the mistake it
prevents instead of a cost filter, and names the hand-off the post-hoc review
of the v0.2.0 bump had to reconstruct by hand. The second keeps the cascade
rule in positive form and adds reconciliation — the documented failure when a
fan-out returns conflicting claims.

Nothing else is acode-specific yet. The assembled prompt is identity, the SDK
contract, and the delegation policy (~1.7 KB against the 32 KiB cap) — the
leanest possible owned prompt. When acode grows product rules that belong in
prompt material, this block is where they go; not `Config.Instructions` (no
version attribution, dropped on fork) and not the SDK template (wrong owner).

## 5. Child template

The shape stays; the identity does not. Ask 2 in §4.3 gives children "You are
the acode reviewer agent" instead of "You are the aharness reviewer agent".
Children pick up the v3 shared body through their own recorded prompt
version, so the project-instructions read, the escape hatch, and the
verify/report line reach every role with no per-role edit; the catalog stays
parent-only; the `## Your role` heading is kept.

Open consequence (DEC6): the project-instructions line lands in read-only
roles too, where Claude Code deliberately *omits* project memory to keep
Explore cheap. If probes show the extra read hurting, split the line into
the implementer/reviewer/task bodies only (roles are recorded per child, so
no second version bump is needed).

## 6. Role definitions

Each entry: the new `Description` (byte count against the 200-byte cap) and
the new role `Prompt`. Bundled-default changes in `agents.go`; per §2 fact 2,
no prompt version is required — verify the replay contract first.

### explore

```text
Read-only lookup: where something lives, how a mechanism works, which files a change touches. Call it when you need the conclusion, not the reads; it reports with file and line and cannot edit.
```
(193 bytes)

```text
Explore the assigned area and report what you found. Lead with the answer,
cite file and line for every claim, and include only the facts the caller
needs to act. When the evidence does not settle the question, say what is
missing rather than guessing. Your tools are read-only: if a change is
needed, describe it precisely instead of making it.
```

The description gains a trigger ("when you need the conclusion, not the
reads") and the report shape. The body drops the bare "Do not edit files" —
the tool grant already enforces it (#9) — and keeps the useful half; it adds
"say what is missing rather than guessing", because explore's failure mode is
the confidently wrong conclusion and unreported unknowns are a measured
category (#7).

### implementer

```text
Implementation of one self-contained change whose full spec fits in a prompt. It reads, edits, runs the tests, and reports what changed and what it could not verify. Keep cascading work here.
```
(191 bytes)

```text
Implement the assigned change exactly as specified, keeping it minimal and
focused. Read the surrounding code before editing, and run the relevant
tests after. When the spec is ambiguous or contradicts the code, follow the
most reasonable interpretation and record the choice in your report rather
than choosing silently. Report what changed, which commands you ran and
their outcomes, and anything you could not verify.
```

The description states the trigger shape ("self-contained… fits in a prompt")
plus its negative space ("Keep cascading work here") — the routing fact the
parent actually needs. The body adds the ambiguity rule: a child cannot ask,
so stating the chosen interpretation is its only channel (#6, #8).

### researcher

```text
Answers questions needing both repository and web evidence. Call it when sources must be found and weighed; claims come back cited (file and line, or URL), with inference marked.
```
(178 bytes)

```text
Research the assigned question using the workspace and the web. Cite every
claim with a file and line or a URL, and note a source's date when recency
matters. Separate what you verified from what you inferred, and say plainly
when the evidence is insufficient. Prefer current sources; name the reason
when you rely on an older one.
```

Smallest of the five — the current body already matches the evidence
(citation accuracy is graded in Anthropic's own production rubric). Added
only source-dating discipline, because "find the latest practice" tasks fail
through undated evidence, not missing evidence.

### reviewer — the one that matters

```text
Independent review of a change its author has read and believes correct. Call it before finishing contract, documented-behavior, or multi-file changes. It returns defects, not confirmation.
```
(189 bytes)

```text
Review the assigned change as a skeptic: the author has read it and believes
it is correct, so your value is disagreement, not agreement. Lead with
concrete findings, each cited to file and line. Check first what the author
claims is fixed, verified, or covered by tests — confirm each claim
directly, because confident wrongness is the defect a self-check cannot
catch. Then look for behavior regressions, unstated behavior changes,
contract drift between the change and its documentation, and tests that now
pass vacuously. Report defects in the assigned change, not improvement
ideas for the rest of the codebase. Say plainly when you find nothing, and
end with what you covered and what you did not look at, so an empty finding
is a scoped claim rather than a guess.
```

- **The description finally encodes a trigger** (§1.3): *when* to call
  (before finishing contract / documented-behavior / multi-file changes),
  *who it is for* (a change the author has read and believes correct — the
  why in one clause), and *what returns* (defects, not confirmation).
- **Claims-first ordering.** Both falsely-claimed-fixed findings in §1.1
  were claims the author had explicitly reported as done. The body directs
  the reviewer to verify them directly, before hunting for new defects.
- **The finding classes** match what a pre-commit review must catch:
  regressions, unstated behavior changes, contract drift, vacuous tests.
- **A scoped "no findings."** Silence becomes a claim about coverage, which
  is checkable.
- The old "do not propose unrelated refactors" became the positive "Report
  defects in the assigned change, not improvement ideas for the rest of the
  codebase."

Structural limitation this role keeps for now: bundled `reviewer` has
`read_file` and `search` only, so it cannot run `git diff` or tests — the
parent must hand it the changed files (§4.5 says so). See DEC7.

### task

```text
Self-contained multi-step work needing reads, edits, and commands: the fallback when no narrower role fits. It can do everything except delegate.
```
(145 bytes)

```text
Carry out the assigned task end to end. Read before editing, keep changes
minimal, and run the relevant tests. When the spec is ambiguous, follow the
most reasonable interpretation and record the choice in your report rather
than choosing silently. Report what you changed, what you verified, and
what remains.
```

The description declares the fallback and encodes preference order —
overlapping scopes degrade routing, so `task` explicitly yields to narrower
roles. The body gains the same ambiguity-reporting rule as `implementer`,
which also covers its research-flavored uses.

## 7. Non-goals

- **No transcribed contract text.** acode owns the prompt but composes it
  from the SDK export; the loop invariants are never hand-copied into this
  repo, because a copied invariant is drift waiting for the next SDK bump.
- **No `Config.Instructions`.** Reserved for genuinely host-specific
  *additions*, of which acode has none: it carries no version of its own,
  never reaches a child, and on fork drops a replaced prompt and re-renders
  the SDK's. Product lines go in the owned prompt.
- **No repo-local review gate in the product prompt.** The trigger *classes*
  (contracts, documented behavior, multi-file) are universal, so they go
  upstream; whether review is *mandatory* for this repo is an
  [AGENTS.md](../../AGENTS.md) / definition-of-done decision, separate from
  this plan.
- **No changes to frozen text.** `prompt-v0`/`v1`/`v2` and the toolset
  schemas stay byte-identical; the `delegate` tool description already
  states self-containment and the no-nesting limit.
- **No restatement of harness mechanics the schemas already show.** Every
  tool's contract is declared in the toolset; prompt lines would duplicate
  it and rot.
- **No per-role model or effort tuning here.** That axis exists in the SDK
  (`AgentOverride`, definition fields) and is environment policy, not
  prompt text.

## 8. Milestones

| Task | Change | Acceptance |
|---|---|---|
| P0.1 | Install the five §6 definitions in `~/.acode/agents/*.md` (`name:` matches the bundled name, `tools:` is the comma-separated grant, `---`, then the body). | The definitions browser shows the replaced set with no warnings; the bundled set returns when the files are removed. |
| P0.2 | Probe the pilot per §13; if it holds, file the Phase 1 change with the pilot as evidence. | The §13 probe list runs on this machine; results recorded in this plan's verification note. |
| P1.1 | Upstream: swap the five `Description`/`Prompt` strings in `bundledAgents()`; pin the new `reviewer` description text in `agents_test.go` (the bundled set has no text pins today). | All five descriptions ≤200 bytes (193/191/178/189/145); SDK tests green; a session opened before the change replays its recorded child prompts unchanged. |
| P2.1 | Upstream: add the v3 shared body per §4.4 as new consts (`harnessRules` is compile-time concatenated into the frozen v0/v1 templates and cannot be edited in place), `PromptVersionV3`, and a `renderPrompt` case; point `childSystemPrompt` at the v3 body for v3 children. | `prompt_test.go` pins v3 in the existing style; v0/v1/v2 still render frozen. |
| P2.2 | Upstream: the §4.3 exports — `agent.PromptBody`, host identity for `childSystemPrompt`, the caller prompt tag; decide the fork+`Instructions` edge. | A host can compose without transcribing; `acode-prompt-v1`-style tags record alongside the SDK version; children render the host identity. |
| P3.1 | acode: a prompt builder in `cmd/acode` — acode identity, `agent.PromptBody`, then the §4.5 policy block — through the existing `SystemPrompt` plumbing (`cmd/acode/config.go:123`, `run.go:410`). Fork re-supplies it; resume replays the recorded text. | New/fork parents record the acode prompt; resume is byte-identical to before; fork keeps acode identity even when instructions are set. |
| P3.2 | acode: pin `const acodePromptVersion` and the composed text in a test; add the composition note to [architecture.md](../architecture.md). | An SDK body change that alters the composed text fails the acode test until the pin is deliberately moved. |
| P3.3 | Bump the SDK pin; run the full DoD set; record verification (§13). | `make dod` green; probes recorded as pending where credentials are unavailable. |

Ordering rationale: Phase 0 costs nothing and de-risks Phase 1; Phase 1 is
the smallest upstream change with the largest expected effect (the routing
fix); Phase 2's exports unblock Phase 3; nothing lands app-side before the
export exists, so there is never a transcribed copy in between.

## 9. Intended behavior changes

1. **New/fork parent sessions get the acode prompt**: acode identity, the
   v3 SDK contract, the delegation policy. Resume replays the recorded text
   and is unchanged.
2. **Children introduce themselves as acode agents**, not aharness agents.
3. **Children read the workspace's project instructions** (one read per
   session) before acting on the code.
4. **Review is delegated before done** on qualifying changes — behavioral,
   verified by probe, not by test.
5. **Session records carry a caller prompt tag** beside the SDK prompt
   version, so a trajectory is attributable to the acode prompt version.
6. **The agent catalog lines change** (the five descriptions).

No CLI flag, environment variable, config key, slash command, or exit code
changes. `-ui-json` and the transcript change only where the identity line
and prompt tag appear. The `session_start` record gains one `extra` field
(the prompt tag).

## 10. Decisions

| ID | Decision | Recommendation |
|---|---|---|
| DEC1 | acode replaces the SDK prompt, or appends to it? | Replace, by composition (§4). Appending leaves the v2 originals in place beside their new forms — the duplication the leanness audit exists to catch — and `Instructions` drops a replacement on fork. |
| DEC2 | Where does delegation guidance live? | Mechanics (self-containment) upstream in the SDK body; policy (which role, when, review-before-done) in acode's block. If a second host ever wants the policy, it migrates upstream and acode shrinks back to identity alone. |
| DEC3 | Transcribe the contract lines to avoid an SDK dependency? | Never (§7): a transcribed invariant drifts the first time the SDK adds one. Block Phase 3 on the `PromptBody` export. |
| DEC4 | Role text without a prompt version bump? | Rely on sessions recording resolved text (§2 fact 2), but verify the replay contract first; if it does not hold, ship the role changes with `prompt-v3`. |
| DEC5 | Pilot before filing upstream? | Yes: Phase 0 is reversible and turns the Phase 1 ask into an evidence-backed request. Invalid pilot files are skipped silently — check the definitions browser after installing. |
| DEC6 | Project-instructions read for read-only roles too? | Open, decided by probe (§5). Default: keep it in the shared body; split to the mutating roles only if the read costs more than it buys. |
| DEC7 | Widen `reviewer` tools (`run_command` for `git diff`/tests)? | Open. Today the parent must hand it the changed files, which §4.5 already instructs. Widening changes what "read-only review" means in constrained environments; decide with Phase 1, not after. |

## 11. Risks and mitigations

| Risk | Mitigation |
|---|---|
| A replaced prompt loses an invariant the SDK later adds | Compose from `PromptBody` (DEC3); P3.2's pin fails when the SDK body changes, forcing a deliberate look. |
| Description rewrite misroutes (overlapping scopes) | Each description names its trigger *and* its boundary; `task` declares itself the fallback; five agents, not a growing roster — overlap is the documented routing failure. |
| Editing frozen prompt text corrupts replay | v3 is new consts plus a new version; `harnessRules` and the v0/v1/v2 templates are untouched; `prompt_test.go` keeps the frozen pins. |
| The caller prompt tag is rejected by `SupportsPrompt` | Ask 3 is upstream; interim attribution is a content hash of the recorded text. |
| Fork with instructions resurrects the SDK prompt | P3.1 always re-supplies the composed prompt on fork; the test asserts acode identity survives. File the edge upstream either way. |
| Role text ships without a bump and old sessions replay differently | DEC4: verify the replay contract first; the acceptance test in P1.1 pins a pre-change session's recorded child prompt. |
| Probe results are intuition, not evidence | §13 follows the SDK's evaluation research: real-model component probes, one tag per variant, traces read raw; the two production failures are the regression cases. |

## 12. Test plan

| Layer | Coverage |
|---|---|
| SDK unit (upstream) | `prompt_test.go` v3 pins; `agents_test.go` description pins and the 200-byte caps; child identity for a v3 child. |
| acode unit | The prompt builder composes identity + body + policy; `acodePromptVersion` recorded in the session; the composed-text pin. |
| acode integration | New/fork record the acode prompt; resume replays the recorded text byte-identically; fork with instructions keeps the acode identity; a pre-change session's child prompt is unchanged. |
| TUI | Definitions browser shows replaced agents and warnings; catalog lines come from the effective definitions. |
| Probes (real model) | §13's four behaviors, 10–12 runs per variant per the SDK evaluation research. |
| DoD | `make dod` after each phase; real-provider and real-terminal smokes recorded as pending if credentials are unavailable. |

## 13. Verification

Proposed. Nothing has been applied; this plan is the record of intent. When
phases land, record here: SDK version, `make dod` result, probe outcomes per
behavior below, and deviations.

1. **Review before done.** On a qualifying change with no cost-based hint in
   the user prompt, does the parent delegate an independent review before
   reporting done? (The production failure that started this.)
2. **Verified vs. unverified claims.** Does the final report distinguish
   them? The two falsely-claimed-fixed findings in §1.1 are the regression
   cases.
3. **Composition mechanics.** Does a fork keep the acode prompt (including
   when instructions are set)? Does resume replay the recorded text? Do
   children carry the acode identity and read the workspace rules?
4. **Read-only cost.** Does the project-instructions read cost explore more
   than it buys (DEC6)?

## 14. Open questions

1. DEC7: widen `reviewer` tools or keep the parent as the diff supplier?
2. DEC6: shared project-instructions read, or mutating roles only?
3. Is the 200-byte description cap still the right number now that
   descriptions must carry triggers? All five proposals fit with room to
   spare, which suggests it is.
4. P2.1 forks the shared body (`harnessRulesV3`), leaving two living
   variants. Acceptable, or should v3 restructure so each version owns its
   full frozen body (cleaner, bigger diff)?
5. The prompt-tag design: a new `Spec.PromptTag` in `extra`, or
   `SupportsPrompt` learning a `host:` namespace? Either way it must stay
   keyed to a supported SDK template version so replay support remains
   checkable.
6. After the descriptions carry routing facts, does the parent's actual
   choice of `implementer` change? Only probes can say.

## 15. Sources

Principles and exclusions in §3 trace to (accessed 2026-11-19):

- Anthropic — Claude prompting best practices: <https://platform.claude.com/docs/en/build-with-claude/prompt-engineering/claude-prompting-best-practices>
- Anthropic — Effective context engineering for AI agents (Oct 2025): <https://www.anthropic.com/research/effective-context-engineering-for-ai-agents>
- Anthropic — How we built our multi-agent research system (Jun 2025): <https://www.anthropic.com/research/multi-agent-research-system>
- Anthropic — Writing effective tools for agents (Sep 2025): <https://www.anthropic.com/research/writing-tools-for-agents>
- OpenAI — Prompt engineering / GPT-5 prompting guide (Aug 2025): <https://developers.openai.com/api/docs/guides/prompt-engineering>
- OpenAI — Function calling (tool description guidance): <https://developers.openai.com/api/docs/guides/function-calling>
- Claude Code — Best practices: <https://code.claude.com/docs/en/best-practices>
- Claude Code — Subagents: <https://code.claude.com/docs/en/sub-agents>
- claude.com — Subagents in Claude Code (Apr 2026): <https://claude.com/resources/articles/subagents-in-claude-code>
- Simon Willison — notes on the Claude Code team's system-prompt discussion (Jul 2026): <https://simonwillison.net/2026/Jul/21/cat-and-thariq/>
- MAST — Why do multi-agent LLM systems fail? (arXiv:2503.13657, 2025): <https://arxiv.org/abs/2503.13657>
- OpenCode — Agents: <https://opencode.ai/docs/agents/>
