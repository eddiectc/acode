# acode prompt optimization build plan

- **Date:** 2026-11-19 (revised after the aharness v0.3.0 bump; first issued
  the same day against v0.2.0).
- **Status:** partially implemented by the v0.3.0 bump. Phase 1 landed (the
  five role definitions, now forced: the SDK ships none), Phase 2 shipped
  upstream inside v0.3.0, Phase 0 is retired, and the composed-prompt half of
  Phase 3 is superseded by the SDK's tier-1 path (§4). What remains is the
  §13 probe pass, pending upstream as well.
- **Source:** a review of every prompt surface aharness v0.2.0 shipped (the
  main-agent template, the child template, the five bundled role
  definitions, and the tool schemas), plus a 2026-11 web research pass on
  prompt best practice (§3, sources in §15). §1 records the incidents that
  motivated the plan so it stands alone — the working notes that carried
  them were temporary and are gone. The upstream answer is aharness v0.3.0,
  "the prompt-surface release", and its build plan
  (`docs/plans/prompt-surface-build.md` in the aharness repository), whose
  Appendix A is the role-content handoff this plan's §6 was waiting on.
- **Target:** Go 1.27.4, module `acode`, aharness v0.3.0.
- **Architecture:** [architecture.md](../architecture.md) defines the SDK
  boundary; the role-definition and identity ownership note landed with the
  bump. Prompt caps are in [limits.md](../limits.md).
- **Standard:** [coding-standard.md](../coding-standard.md);
  **Definition of done:** [definition-of-done.md](../definition-of-done.md);
  **Testing:** [testing-strategy.md](../testing-strategy.md).

## Decision summary

Three decisions drove the original plan. v0.3.0 answered all three — one
with a different mechanism than proposed:

1. **acode's prompt is owned — which v0.3.0 reduced to one config field.**
   The identity line is user-visible product surface, so the branding rule
   in [AGENTS.md](../../AGENTS.md) applies. The original proposal was to
   replace the whole prompt through `Config.SystemPrompt`, composed from a
   to-be-exported body. v0.3.0 instead split the layers: the SDK keeps the
   loop contract and the frozen templates; identity, prompt content, and
   role definitions belong to the host. acode takes the SDK's documented
   tier-1 path — `Identity: "acode"` plus the default body — so it owns zero
   bytes of prompt text and transcribes nothing. The one line it would have
   composed a body to fix, the delegation criterion, went upstream into
   `prompt-v3` instead (§4.5).
2. **Descriptions are routing material, so they carry triggers.** Landed.
   The five descriptions ship in `cmd/acode/agents/*.md` with their triggers
   (§6), pinned by test. The parent's entire delegation-decision surface is
   still those description strings — now acode's own product content.
3. **acode ships its own role definitions.** Landed, and no longer a
   choice: v0.3.0 removed `DefaultAgents` outright, so the SDK ships no
   role text at all and `DelegationConfig.Agents` must be supplied. The
   discovery seam is `agent.LoadAgents(base, stateDir)`: acode's five are
   the base, operator files replace them by name, and the format, parser,
   caps, catalog, spawner, and overrides stay in the SDK. The old P1.2 —
   mirror the same text upstream as bundled defaults — is dead: nothing in
   the SDK may reproduce these texts.

| Phase | Contents | Status |
|---|---|---|
| 0 | Zero-code pilot: install the §6 role definitions in `~/.acode/agents/` | **Retired** — no bundled set exists to pilot against, and the SDK's breaks are compile-time, so the pilot's evidence role vanished |
| 1 | acode ships its five role definitions; operator files still win | **Landed** with the v0.3.0 bump (§6) |
| 2 | SDK `prompt-v3` shared body + the ownership exports | **Shipped upstream** in v0.3.0 (§4.3–§4.4) |
| 3 | acode composes its prompt through `Config.SystemPrompt` | **Superseded** — tier 1 needs no `SystemPrompt`; only the identity half was acode's to set, and it is set (§4) |

## 1. Problem

This section is the motivating record, written against aharness v0.2.0; each
gap it names is closed by v0.3.0 (§4–§6), so its facts describe the release
the plan was filed against, not the current tree.

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
| Main-agent template, prompt-v3 (v0/v1/v2 frozen) | `aharness/agent/prompt.go` | Parent, rendered per session, recorded verbatim | Frozen per version; edits need a new prompt version; ≤32 KiB recorded |
| Shared body (`harnessRulesV3`; `harnessRulesFrozen` for v0–v2) | same | Parent **and** every child (`childSystemPrompt`) | Frozen with the version; the invariant subset is exported as `LoopRules()` |
| `todo` / `delegate` guidance | same | Parent only (`delegate`); both when granted (`todo`) | Frozen with the version; the v3 delegation paragraph carries the independence criterion |
| Child template + role `Prompt` | `aharness/agent/agents.go` | One child session, recorded verbatim | Body ≤16 KiB; tools enforce what a prompt would otherwise request |
| Role `Description` | same | Parent only, via `AgentCatalog.Infos()` → the rendered catalog | **≤200 bytes**, enforced in `validateAgentDefinition` |
| Agent catalog render | `prompt.go` | Parent only | ≤4 KiB for all lines combined; exported as `AgentCatalogText` |
| `delegate` tool description | `tools/schemas.go` (toolset v2) | Parent, every request | Frozen once shipped; already states self-containment — left alone |

Two structural facts shape the design:

1. **Descriptions are the router's only signal** (§1.3), and the 200-byte cap
   forces the right split: routing triggers in the description, behavior
   detail in the role body, which only loads when that agent runs.
2. **Sessions record the resolved prompt verbatim — verified.** The recorded
   `session_start.system_prompt` is authoritative for parents and children
   alike, and acode's resume path re-supplies the recorded text
   (`cmd/acode/models.go`), so a role `Prompt` or `Description` change
   reaches new sessions and new children without a prompt version bump, and
   a pre-change session replays its recorded prompt byte-identically. DEC4
   is resolved on this basis, and the bump's end-to-end test pins it.

### Host override surface

Verified against v0.3.0; acode uses the identity and the discovery seam.

| Knob | Effect | Reaches | Notes |
|---|---|---|---|
| `aharness.Config.Identity` (facade), `engine.Spec.Identity`, `agent.Config.Identity` | names the product in the parent and child identity lines | Parents and children, new/fork only — not recorded | acode sets it at both construction sites (`cmd/acode/run.go:240`, `run.go:402`); ≤64 bytes, no control characters; resume replays the recorded text |
| `aharness.Config.PromptTag`, `engine.Spec.PromptTag`, `agent.Config.PromptTag` | attributes the host's prompt template | `session_start.prompt_tag`, new sessions | Optional, ≤64 printable ASCII; resume keeps the recorded tag, fork prefers an explicit one. **Unused by acode under tier 1** (§4.3 ask 3) |
| `aharness.Config.Instructions` | appended after the rendered prompt | New/Fork parents only | Rejected on resume; a fork with instructions now appends to the recorded prompt (fixed in v0.3.0) |
| `aharness.Config.SystemPrompt` | replaces the whole prompt | New/Fork parents only | The tier-3 escape hatch; acode does not use it (§4) |
| `<state-dir>/agents/*.md` | replaces one shipped definition by name | that role, parent catalog and child | `agent.LoadAgents(defaults, stateDir)` at `cmd/acode/tui.go:765`; the 200-byte cap applies; invalid files are skipped with a warning and never replace |

Children are reached by exactly one config knob: `Identity` flows into
`childSystemPrompt`. Everything else children see comes through their
recorded prompt version — which is why the §1.4 fixes (project instructions,
escape hatch, verify/report) shipped upstream in the shared body rather than
as an app-side append.

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

## 4. Design — acode owns identity and roles, inherits the contract

Three grounds, the first already repo policy:

1. **The branding non-negotiable decides it.** [AGENTS.md](../../AGENTS.md):
   "User-visible branding is acode: the `acode` binary, the `ACODE_*`
   environment variables, `~/.acode` state, the user agent, temp names, and
   docs." The identity line is recorded in `session_start` and replayed on
   resume — it is user-visible branding. v0.3.0 supplies `Config.Identity`,
   so the fix is one field per construction site, not an owned template.
2. **aharness is a library.** It ships no `cmd/` and no binary; its frozen
   template is the default for hosts that supply none. v0.3.0 made that
   layering explicit: the SDK owns the prompt *contract* and the loop
   invariants, and identity, coding rules, role taxonomy, and coordination
   policy are replaceable host content. acode is the first-party product, so
   it is precisely not the host that needs the defaults' identity.
3. **The knobs are designed extension points.** `doc.go` documents the
   composition story with a stated preference order; acode takes the
   recommended tier. The no-shim rule in [AGENTS.md](../../AGENTS.md)
   targets compensating for missing capability; using a documented
   capability is composition.

The original failure mode — hand-copying contract lines so they drift the
first time the SDK adds one — is solved structurally now: acode composes
nothing, so there is nothing to transcribe.

### 4.1 The tier-1 path (what acode runs)

The SDK documents three composition tiers and assigns a coding agent the
first; the upstream plan names acode as the tier-1 user:

| Tier | Host supplies | Inherits | acode |
|---|---|---|---|
| 1 | `Identity` | `DefaultPromptBody`: loop invariants, coding policy, todo/delegate guidance, the catalog of the host's own definitions | **used** |
| 2 | `Identity`, `LoopRules()`, its own policy lines, `AgentCatalogText(agents)` | nothing but the invariants | held in reserve for product rules acode does not have yet |
| 3 | `SystemPrompt` wholesale | nothing | the escape hatch; unused |

Tier 1 is enough for acode because the two things the composed prompt was
for are no longer app-side problems: the identity is one field, and the
delegation criterion went upstream into the v3 body (§4.5). Retyping the
SDK's four coding-policy lines to obtain them would be the drift the plan
exists to avoid. If acode grows genuine product rules later, they enter
through tier 2 with a `PromptTag` for attribution — never by editing SDK
text.

### 4.2 Mechanics (verified in the v0.3.0 source)

- **New and Fork only.** Resume inherits the recorded text — correct, since
  resume replays the session that ran. `Identity` is not recorded; it is
  re-supplied on every construction, including `Resume` and `Fork`, the
  same pattern as the user agent. A fork of a pre-bump session therefore
  renders the fork's identity; the session it forked from keeps its
  recorded line on resume.
- **The wiring is two fields and a base catalog.** `acodeIdentity` is set
  at both construction sites (`cmd/acode/run.go:240` for the engine spec,
  `run.go:402` for the runner config, which covers TUI children through
  the spawner), and the five definitions seed `LoadAgents`
  (`cmd/acode/tui.go:765`). The `SystemPrompt` plumbing through
  `config.go:126`/`run.go:412` exists and stays unused — tier 3, held in
  reserve.
- **Recorded verbatim, 32 KiB cap.** New sessions record the v3 body with
  the acode identity line and the catalog of acode's own definitions.
- **It lives in `cmd/acode`**, which [AGENTS.md](../../AGENTS.md) assigns
  "wiring and I/O only" — setting two config fields and seeding a base
  catalog is wiring; the loop, projection, and session contracts stay in
  the SDK.

### 4.3 The SDK asks — all five answered in v0.3.0

1. **Composition primitives — three, not one.** `agent.LoopRules()` returns
   the version-independent invariants (tool calls are the only way to act,
   one result each, the privileges fact, tool errors are data, the stop
   rule) without coding policy; `agent.AgentCatalogText(agents)` renders
   the catalog under the 4 KiB cap and omission marker;
   `agent.DefaultPromptBody(version, agents)` returns the default body
   without the identity line or the recorded-prompt cap. The one-body ask
   was insufficient: a non-coding host forced into one export would have to
   inherit "run the relevant tests" or transcribe the invariants. acode
   never needs to strip text.
2. **A host-supplied identity.** `aharness.Config.Identity`,
   `engine.Spec.Identity`, and `agent.Config.Identity` (≤64 bytes, valid
   UTF-8, no control characters; empty means `aharness`).
   `RenderSystemPrompt(version, workspace, identity, agents)` gained the
   parameter — a breaking change, compiler-caught — and `childSystemPrompt`
   uses it, so a child renders `You are the acode "reviewer" agent, working
   in the workspace …`. acode sets it; the parent line is `You are acode, a
   coding harness working in the workspace %q.`
3. **A caller prompt tag.** `Config.PromptTag`/`Spec.PromptTag`/
   `agent.Config.PromptTag` record beside the SDK template version as
   optional `session_start.prompt_tag`, exposed as `State.PromptTag` (≤64
   printable ASCII, `[A-Za-z0-9._:/-]+`). Resume keeps the recorded tag and
   rejects a different explicit one; a fork prefers an explicit tag and
   inherits only when the prompt is unchanged; a delegated child never
   records its parent's tag. **acode sets none under tier 1**: it runs the
   SDK's template, so `prompt-v3` is the attribution, and a tag would
   describe a template acode does not have. The interim content-hash idea
   is retired. A tier-2 move later would set `acode-prompt-v1`.
4. **The fork edge — fixed upstream.** A fork that sets only `Instructions`
   now appends to the effective recorded prompt instead of re-rendering the
   SDK prompt, so a replaced-prompt session forked with instructions keeps
   its text. acode needed no workaround, and the "always re-supply" defense
   is moot.
5. **A host-defaults layer in agent discovery — answered stronger than
   asked.** `LoadAgents(base []AgentDefinition, stateDir string)` seeds the
   host's base first and applies `<state-dir>/agents/*.md` second, with
   `SourceDefault`/`SourceUser` exported (`"bundled"` is renamed
   `"default"`). The ask's alternatives (`LoadAgentsWith`, a merge helper,
   source-string matching) never shipped; better, `DefaultAgents` was
   removed outright, so a nil base means no definitions and the facade
   rejects an empty `DelegationConfig.Agents` loudly. acode seeds its five;
   operator files still win.

### 4.4 The `prompt-v3` shared body — shipped upstream

All six proposed changes landed, with two wording deltas:

| proposed v3 line | shipped v3 line | Why (§3) |
|---|---|---|
| "report an action as done only when a tool call performed it." | `- Tools are your only way to act on the workspace. Report an action as done only when a tool call performed it.` | Positive form of the same rule (#3); folded into one line with the act-through-a-tool fact |
| "every tool call receives exactly one result" | `- Every tool call receives exactly one result.` | `doc.go` names it an invariant; the v3 text states it |
| "Run the relevant tests … before reporting it done, and say plainly what you could not verify." | `- Run the relevant tests before reporting the change done, and say plainly what you could not verify.` | Completion must be verifiable; the unverified half must be reported (#7). The direct antidote to §1.1 |
| "If the workspace carries project instructions (AGENTS.md or similar), read them before acting on the code." | `- If the workspace carries project instructions (AGENTS.md or the file it names), read them before acting on the code.` | §1.4: nothing told a child to read repo rules; "or the file it names" is the sharper predicate |
| "If a task is infeasible as stated, or a check is wrong, say so rather than working around it." | identical | The escape hatch (#8); a child cannot ask, so stating refusal is its only channel |
| "Stop … when blocked, say what blocks you and what remains." | folded into the stop rule verbatim | A blocked stop that reports nothing is indistinguishable from done |

Deliberately unchanged, as proposed: the todo guidance (byte-identical to
v2, pinned by test), the sandbox/privileges fact (it is the *why*, #4), and
"tool errors are data". `LoopRules()` returns the invariant subset only
(lines 1, 2, the privileges fact, tool errors, the stop rule), so the
export cannot drift into policy; the other four lines are the replaceable
coding default that tier-2 hosts may swap. New sessions record `prompt-v3`;
v0/v1/v2 stay frozen, pinned upstream by exact SHA-256, and old logs replay
their recorded prompts with no migration.

### 4.5 The delegation criterion — upstream, not an acode block

The v3 delegation paragraph carries the §1.2 fix:

> `delegate` runs independent work in a child agent. Delegate a
> self-contained investigation, review, or verification when the conclusion
> must be reached without the context that produced the work, or when it
> would otherwise flood this conversation with tool output, or when
> independent read-only tasks can run in parallel. State the goal, the
> constraints, and what to report: the child starts with no conversation
> history. Do not delegate work that depends on context the child will not
> have.

The first qualifying condition is new: the value of fresh context, not a
cost filter — so a cheap, serial review of a thirty-file diff now qualifies,
and the parent can no longer cite the SDK's own sentence for not
delegating. The criterion is about the *value of independence*, not which
role to pick, which is why it could go upstream without smuggling product
routing into the SDK.

The proposed acode-only block is therefore retired:

- Its first bullet's criterion went upstream (above). The hand-off
  specifics it named — the changed files, the intent, each claimed behavior
  change — are carried by the pieces acode owns: the reviewer description
  ("a change its author has read and believes correct") and the reviewer
  body's claims-first rule (§6), plus the delegate schema's
  self-containment requirement.
- The second bullet's cascade rule was already upstream ("Do not delegate
  work that depends on context the child will not have").
- **One line has no v3 counterpart: reconciliation** ("when child reports
  disagree, reconcile them yourself before acting on them"). It is a watch
  item, not a shipped rule: if probes show a fan-out conflict mishandled, it
  enters through tier 2 with a `PromptTag` — never by editing SDK text.

acode's assembled parent prompt is now the SDK default — identity line, v3
body, catalog of acode's definitions (~2 KB against the 32 KiB cap) — and
acode owns zero bytes of it. The leanness audit holds by construction:
there is no second copy of any rule to bloat or contradict.

## 5. Child template

Shipped upstream. A child renders the host identity — `You are the acode
"explore" agent, working in the workspace …` — then the v3 shared body, then
`## Your role` and the role's recorded body. The identity flows from the
parent's `Config.Identity` through the spawner, so the ask-2 fix reached
children without a per-role edit; the project-instructions read, the escape
hatch, and the verify/report line reach every role through the shared body
the same way. The catalog stays parent-only; the `## Your role` heading is
kept.

DEC6 resolved: the project-instructions line lives in the v3 shared body, so
read-only roles see it too. Upstream kept it there deliberately — the
reviewer is read-only and is precisely the role that reviews against repo
policy, so the line cannot be limited to mutating roles without defeating
§1.4. If probes show the extra read hurting explore, acode can move the line
into the mutating role bodies only: a role's prompt is recorded per child, so
no second version bump would be needed.

## 6. Role definitions

**Landed.** These ship in acode as embedded files (`cmd/acode/agents/*.md`,
`//go:embed`), parsed at startup with the SDK's own
`agent.ParseAgentDefinition` and seeded as the `LoadAgents` base
(`cmd/acode/tui.go:765`), so operator files still win by name. The text is
the upstream handoff — Appendix A of the aharness prompt-surface plan —
verified there for the caps: the descriptions are byte-identical to the
proposals below, and the reviewer body carries one upstream reword noted in
its section. `cmd/acode/agents_test.go` pins the roster, the exact
descriptions and their byte counts, the bodies, the grants, and the
operator-file precedence; the definitions browser shows the set with source
`default` and no warnings.

| Agent | Grant |
|---|---|
| `explore` | `read_file`, `search` |
| `implementer` | `read_file`, `edit_file`, `search`, `run_command` |
| `researcher` | `read_file`, `search`, `web_search`, `web_fetch` |
| `reviewer` | `read_file`, `search` |
| `task` | every current tool except `delegate`, pinned to `tools.NamesWithout("delegate")` by test, so an SDK toolset change forces a deliberate file update |

Each entry: the shipped `Description` (byte count against the 200-byte cap)
and the shipped role `Prompt`.

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
claims is fixed, verified, or covered by tests — check each claim against
the code and the tests, and say plainly what you could not settle, because
confident wrongness is the defect a self-check cannot catch. Then look for
behavior regressions, unstated behavior changes, contract drift between the
change and its documentation, and tests that now pass vacuously. Report
defects in the assigned change, not improvement ideas for the rest of the
codebase. Say plainly when you find nothing, and end with what you covered
and what you did not look at, so an empty finding is a scoped claim rather
than a guess.
```

- **The description finally encodes a trigger** (§1.3): *when* to call
  (before finishing contract / documented-behavior / multi-file changes),
  *who it is for* (a change the author has read and believes correct — the
  why in one clause), and *what returns* (defects, not confirmation).
- **Claims-first ordering.** Both falsely-claimed-fixed findings in §1.1
  were claims the author had explicitly reported as done. The body directs
  the reviewer to verify them directly, before hunting for new defects.
- **One upstream reword, accepted.** The proposal said "confirm each claim
  directly"; the shipped body says "check each claim against the code and
  the tests, and say plainly what you could not settle." The reviewer's
  grant is read-only, so "confirm directly" invites a reported
  verification that did not happen — the exact failure class the change
  exists to prevent. The reword keeps the claims-first order and makes the
  unresolvable claim a reported fact instead of a silent one (#7).
- **The finding classes** match what a pre-commit review must catch:
  regressions, unstated behavior changes, contract drift, vacuous tests.
- **A scoped "no findings."** Silence becomes a claim about coverage, which
  is checkable.
- The old "do not propose unrelated refactors" became the positive "Report
  defects in the assigned change, not improvement ideas for the rest of the
  codebase."

DEC7 is resolved with the grant kept read-only: `reviewer` has `read_file`
and `search` only, so it cannot run `git diff` or tests — the parent hands
it the changed files, which the §4.5 hand-off pieces instruct. Keeping the
grant also keeps review fan-out concurrent (a delegate call runs in
parallel only when every task's grant is read-only) and honors the
every-reviewer-is-read-only finding in the SDK's subagent research.

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

- **No transcribed contract text.** Still none — and under tier 1 there is
  no composed text to transcribe *from*: the loop invariants are inherited,
  and `LoopRules()` is the source if a tier-2 move ever needs them. A copied
  invariant is drift waiting for the next SDK bump.
- **No `Config.Instructions`.** Reserved for genuinely host-specific
  *additions*, of which acode has none: it carries no version of its own,
  never reaches a child, and adds unattributed text beside the recorded
  prompt. Product lines, if any ever appear, go in a tier-2 body with a
  `PromptTag`.
- **No repo-local review gate in the product prompt.** The trigger *classes*
  (contracts, documented behavior, multi-file) are universal, so they live
  in the shipped `reviewer` description; whether review is *mandatory* for
  this repo is an
  [AGENTS.md](../../AGENTS.md) / definition-of-done decision, separate from
  this plan.
- **No edits to frozen text.** `prompt-v0`/`v1`/`v2` and the toolset
  schemas stay byte-identical (pinned upstream by exact SHA-256); `v3` is a
  new version, not an edit. The `delegate` tool description still states
  self-containment and the no-nesting limit, unchanged.
- **No restatement of harness mechanics the schemas already show.** Every
  tool's contract is declared in the toolset; prompt lines would duplicate
  it and rot.
- **No per-role model or effort tuning here.** That axis exists in the SDK
  (`AgentOverride`, definition fields) and is environment policy, not
  prompt text.

## 8. Milestones

| Task | Original change | Disposition |
|---|---|---|
| P0.1 | Install the five §6 definitions in `~/.acode/agents/*.md` as a zero-code pilot over the bundled set | **Retired.** v0.3.0 removed the bundled set, so there is nothing to pilot against; the SDK's breaks are compile-time, so the bump itself forced the app-side change the pilot was meant to de-risk. |
| P0.2 | Probe the pilot per §13 before landing Phase 1 | **Retired** with P0.1. The probes that mattered moved to the SDK's own release record, which cut the tag with its probes pending — see §13. |
| P1.1 | acode: ship the five §6 definitions as embedded files, merge over the bundled-sourced entries, pin the descriptions | **Landed with the bump.** `cmd/acode/agents/*.md` embedded and parsed with `agent.ParseAgentDefinition`, seeded as the `LoadAgents` base (`cmd/acode/tui.go:765`) before `ApplyAgentOverrides`. Pins in `cmd/acode/agents_test.go`: roster, exact descriptions and byte counts (193/191/178/189/145), bodies, grants, the task-grant drift pin, and operator-file precedence (a `reviewer.md` in the state dir wins; a malformed file warns without replacing). No `Source`-string interim was needed — ask 5's seam shipped. |
| P1.2 | Upstream: mirror the identical text into the bundled defaults | **Dead.** The SDK ships no role text and may not reproduce these five in any form; a runnable `Example_delegation` is the starting template for bare library hosts. |
| P2.1 | Upstream: the v3 shared body as new consts, `PromptVersionV3`, child selection by version | **Shipped upstream** (as `PromptVersion = "prompt-v3"` plus exported `PromptVersionV2`; v0/v1/v2 pinned by exact SHA-256; v2 and v3 children select their own rules, v0/v1 keep frozen). |
| P2.2 | Upstream: the exports — body, host identity, prompt tag; the fork+`Instructions` edge | **Shipped upstream** as three primitives plus `Identity` and `PromptTag`; the fork edge is fixed (append to the recorded prompt). |
| P3.1 | acode: a prompt builder — acode identity, SDK body, §4.5 policy — through `SystemPrompt` | **Superseded by tier 1** (§4.1): no builder, no `SystemPrompt`. The identity is set at `run.go:240`/`run.go:402`; the body and catalog are the SDK default; the §4.5 criterion went upstream. |
| P3.2 | Pin the composed text; add the composition note to architecture.md | **Superseded in part.** Nothing is composed, so there is no composed-text pin; the drift alarm moved to what acode does own — the definitions pins and the task-grant pin. The architecture note landed as the role/identity ownership paragraph. |
| P3.3 | Bump the SDK pin; run the DoD set; record verification | **Landed with the bump** (§13 records the result; probes remain pending). |

Remaining work: the §13 probe pass, run against the tagged SDK plus this tree,
and the two watch items (§4.5 reconciliation, §5's read-cost check for
explore). Both need a real model and fixtures, not code.

## 9. Intended behavior changes

1. **Landed.** New/fork parent sessions record the acode prompt: the
   identity line "You are acode, a coding harness working in the workspace
   %q.", the v3 SDK body with the independence criterion, and the catalog of
   acode's definitions. Resume replays the recorded text and is unchanged.
   New sessions record `prompt-v3` where they recorded `prompt-v2` before —
   the inherited version bump.
2. **Landed.** Children introduce themselves as acode agents, not aharness
   agents.
3. **Landed upstream, inherited.** Children are told to read the workspace's
   project instructions before acting on the code (one read per session).
4. **Routing surface landed; behavior pending probes.** The reviewer
   description carries its trigger, the v3 paragraph carries the
   independence criterion — whether review is actually delegated before
   done is §13's probe 1.
5. **Changed, not landed: no prompt tag.** Under tier 1 acode runs the SDK
   template, so `prompt-v3` is the whole attribution and `session_start`
   gains no field. A tag is the mechanism for a future tier-2 body.
6. **Landed.** The agent catalog lines changed (the five descriptions),
   shipped in acode's own definitions; the SDK's bundled set no longer
   exists.

No CLI flag, environment variable, config key, slash command, or exit code
changes. `-ui-json` and the transcript change only where the identity line
and the prompt version appear, and the definitions browser labels the
shipped rows `default` instead of `bundled`.

## 10. Decisions

| ID | Decision | Resolution |
|---|---|---|
| DEC1 | acode replaces the SDK prompt, or appends to it? | **Neither — inherit, under tier 1** (§4.1). v0.3.0 split ownership so that replacing the prompt would retype four coding-policy lines the product needs, the duplication the leanness audit exists to catch. Identity is one field; the body is the SDK's v3 default. Tier 2/3 remain available with `LoopRules`/`AgentCatalogText` if acode ever grows real product rules. |
| DEC2 | Where does delegation guidance live? | **Split as proposed, then further upstream.** Mechanics and self-containment stay in the SDK body; the independence criterion went upstream too (§4.5), because it is about the value of fresh context, not which role to pick. Role *selection* policy lives in acode's descriptions (§6) — the parent's only routing signal. |
| DEC3 | Transcribe the contract lines to avoid an SDK dependency? | **Never — and now moot.** Nothing is composed, so nothing is transcribed; `LoopRules()` exists for a tier-2 move. |
| DEC4 | Role text without a prompt version bump? | **Resolved: ship without a bump.** Sessions record the resolved prompt for parents and children (§2 fact 2, verified), and acode's resume re-supplies the recorded text, so pre-bump sessions replay byte-identically and only new sessions see new role text. |
| DEC5 | Pilot before landing Phase 1? | **Retired.** `DefaultAgents` is gone, so a pilot had nothing to override; the SDK's breaks are compile-time, and the bump forced the change. |
| DEC6 | Project-instructions read for read-only roles too? | **Resolved upstream: yes, keep it in the shared body** — the reviewer is read-only and is the role that reviews against repo policy. The split to mutating role bodies stays available with no version bump if probes show the read costing explore (§5). |
| DEC7 | Widen `reviewer` tools (`run_command` for `git diff`/tests)? | **Resolved: keep the grant read-only** (`read_file`, `search`), with the reworded body (§6). A read-only reviewer cannot report a verification it did not perform, keeps review fan-out concurrent, and matches the every-reviewer-is-read-only finding in the SDK's research; the parent hands it the diff. |
| DEC8 | Who owns the five role definitions? | **Landed as proposed: acode ships them** (§6) — the roster is product surface (the `/agents` browser, the routing names, the operator `agent_overrides` keyed by name), and the SDK now keeps only mechanism (format, parser, caps, catalog, spawner, overrides) with no role text at all. The bundled-defaults mirroring (P1.2) is dead. |

## 11. Risks and mitigations

| Risk | Mitigation |
|---|---|
| A replaced prompt loses an invariant the SDK later adds | Gone with the replaced prompt: tier 1 inherits the body, so a new SDK invariant arrives by version bump. If tier 2 ever happens, `LoopRules()` is the source and the SDK's own pins guard it. |
| Description rewrite misroutes (overlapping scopes) | Each description names its trigger *and* its boundary; `task` declares itself the fallback; five agents, not a growing roster — overlap is the documented routing failure. Pins make any change deliberate; probes make any misroute visible. |
| Editing frozen prompt text corrupts replay | Not applicable to acode: v3 is a new version shipped upstream, `harnessRulesFrozen` and the v0/v1/v2 templates are untouched and pinned by exact SHA-256, and old logs replay recorded text. |
| The caller prompt tag is rejected by `SupportsPrompt` | Gone: the tag records beside the version without touching `SupportsPrompt`. acode sets none under tier 1. |
| Fork with instructions resurrects the SDK prompt | Fixed upstream in v0.3.0: a fork with instructions appends to the recorded prompt. No acode defense is needed. |
| Role text ships without a bump and old sessions replay differently | Resolved (DEC4): sessions record resolved prompts and resume re-supplies them, so pre-bump sessions replay byte-identically; only new sessions see new role text. |
| A description or body edit breaks the caps or the catalog render | The pin test fails on any byte change (§6), and the catalog render is the SDK's, capped at 4 KiB with the omission marker. |
| Probe results are intuition, not evidence | §13 follows the SDK's evaluation research: real-model component probes, one tag per variant, traces read raw; the two production failures are the regression cases. The upstream release was cut with its probes pending, so this plan's probe pass is the outstanding evidence for both sides of the dependency. |

## 12. Test plan

| Layer | Coverage |
|---|---|
| SDK unit (upstream, shipped) | v3 pins for all nine rule bullets and the delegation criterion; exact SHA-256 pins for v0/v1/v2; child identity for a v3 child; the tag's resume/fork rules; the fork-with-instructions append fix; the discovery seam's base/file precedence and caps. |
| acode unit | `cmd/acode/agents_test.go`: the roster, exact descriptions and byte counts (193/191/178/189/145, cap 200), exact bodies, grants, the task-grant drift pin against `tools.NamesWithout("delegate")`, and operator-file precedence (a same-name file wins; a malformed file warns without replacing). |
| acode integration | The parent records the acode identity line, the shipped reviewer description in the catalog, and the v3 verify/report rule; the child records the acode identity line and its role grant; resume replays recorded prompts byte-identically (the pre-bump case is the same mechanism, DEC4). |
| TUI | Definitions browser renders the shipped set with source `default`, operator rows with `user` and their path, and warnings; catalog lines come from the effective definitions. |
| Probes (real model) | §13's behaviors below — the one evidence class still missing. |
| DoD | `make dod` after the bump (green, §13); real-provider and real-terminal smokes recorded as pending where credentials are unavailable. |

## 13. Verification

**Record (2026-11-19, aharness v0.3.0 bump).** `make dod` green — goimports,
`go vet`, `golangci-lint` (0 issues), `go test -race -count=1 ./...`,
`go build`, `govulncheck` (no vulnerabilities), `docs-check`. Automated
coverage as in §12: the definitions pins, the precedence test, and the
end-to-end recorded-prompt assertions (parent identity line, catalog
description, v3 rule; child identity line). Deviations from the original
plan: Phase 0 retired, P1.2 dead, P3 superseded by tier 1 (§8).

**Pending: the probe pass.** The upstream release was tagged with its
§7.2 probes unrun, and this bump inherits that gap: probes 1 and 2 below
are the outstanding evidence for the routing change, and they must run
against the tagged SDK plus this tree, holding model, effort, and fixture
constant, reading the traces raw. Record outcomes here per behavior:

1. **Review before done.** On a qualifying change with no cost-based hint in
   the user prompt, does the parent delegate an independent review before
   reporting done? (The production failure that started this; upstream's
   pending probe 1.)
2. **Verified vs. unverified claims.** Does the final report distinguish
   them? The two falsely-claimed-fixed findings in §1.1 are the regression
   cases (upstream's pending probe 2).
3. **Composition mechanics — mostly automated now.** Recorded prompts are
   asserted by test (identity lines, v3 body, catalog; resume replays).
   The residual probe is obedience, not mechanics: do children actually
   read the workspace rules, and does a fork of a pre-bump session behave
   as its recorded contract says?
4. **Read-only cost.** Does the project-instructions read cost explore more
   than it buys (DEC6's follow-up)? If yes, move the line into the mutating
   role bodies — no version bump needed (§5).

## 14. Open questions

1. ~~DEC7: widen `reviewer` tools?~~ Resolved: keep the grant read-only (§6).
2. ~~DEC6: shared project-instructions read, or mutating roles only?~~
   Resolved upstream: shared. The read-cost check for explore remains a
   probe (§13.4).
3. ~~The 200-byte description cap?~~ Kept upstream; the five shipped
   descriptions fit with room to spare, and the pin test fails on growth
   past 200.
4. ~~P2.1 forks the shared body — two living variants?~~ Resolved upstream:
   one frozen const for v0–v2 plus `harnessRulesV3`, both pinned exactly.
5. ~~The prompt-tag design?~~ Resolved upstream: `Spec.PromptTag` recorded
   as optional `session_start.prompt_tag`, never a replay key,
   `SupportsPrompt` unchanged. Unused by acode under tier 1.
6. **Does the parent's actual choice of `implementer` change** now that the
   descriptions carry routing facts? Only probes can say.
7. **The reconciliation line** (§4.5) has no v3 counterpart. Watch for a
   mishandled fan-out conflict in probe traces; if one appears, add it
   through tier 2 with a `PromptTag`, not by editing SDK text.

## 15. Sources

Principles and exclusions in §3 trace to (accessed 2026-11-19):

- aharness v0.3.0 — CHANGELOG ("the prompt-surface release") and
  `docs/plans/prompt-surface-build.md` (the upstream implementation plan;
  §1.7 fixes acode's tier, Appendix A is the role-content handoff, §12
  records its pending probes). In the aharness repository, not on the web.
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
