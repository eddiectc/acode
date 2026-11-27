# acode documentation map

This directory separates current reference from dated plans. The root
[README](../README.md) is the user-facing entry point; it describes shipped
features and does not index plans.

acode is the application layer over the [aharness SDK](https://github.com/eddiectc/aharness).
SDK structure, loop, model, tool, session, and public API contracts live in the
aharness repository and are not duplicated here.

## Where a document belongs

1. **Current reference** — describes behavior that must be true on `main`
   today. Lives in `docs/`. Update it in the same change as the code.
2. **Build plan** — a dated record of one initiative: scope, contract deltas,
   milestones, decisions, deviations, and verification. Lives in `docs/plans/`.
3. **Research** — dated evidence and recommendations. acode currently has no
   app-specific research snapshot; SDK research lives in aharness.

Naming: plans use `<topic>-build.md`. A plan is `proposed`, `implemented`,
`superseded`, or a historical record. Once implemented, a plan is frozen except
for status, deviations, and verification notes.

## Precedence

- [coding-standard.md](coding-standard.md) wins on Go practice.
- [writing-standard.md](writing-standard.md) wins on prose.
- The current reference documents win on app structure, runtime contracts,
  numeric limits, and shipped surface: [architecture.md](architecture.md),
  [configuration.md](configuration.md), [limits.md](limits.md),
  [tui.md](tui.md), [commands.md](commands.md), and
  [testing-strategy.md](testing-strategy.md).
- A dated plan records intent and evidence; it never overrides the current
  reference. Where they disagree, the reference wins.
- [definition-of-done.md](definition-of-done.md) applies to every task,
  documentation change, and dated record.

## Index

### Current reference

| Document | Defines |
|---|---|
| [architecture.md](architecture.md) | App package layout, host composition, modes, exit codes, and the SDK boundary |
| [configuration.md](configuration.md) | Flag > environment > config file > default precedence, the JSON config file, and environment variables |
| [coding-standard.md](coding-standard.md) | Normative Go practice, dependencies, testing, and §8 review gates |
| [commands.md](commands.md) | Shipped slash commands: invocation, autocomplete, arguments, key equivalents, scope, and `-ui-json` mapping |
| [container.md](container.md) | The compose stack and its launcher scripts, the published image, mounts, user mapping, secrets, network modes, image builds, and the release CI |
| [definition-of-done.md](definition-of-done.md) | Normative completion checklist for every change |
| [limits.md](limits.md) | CLI, input, TUI, and `-ui-json` caps; SDK-owned caps link to aharness |
| [release.md](release.md) | Branch model, commit wording, pull-request gating, and release tagging |
| [testing-strategy.md](testing-strategy.md) | Config, integration, TUI/headless, fuzz, and manual smoke layers |
| [tui.md](tui.md) | Shipped TUI modes, keys, display behavior, `-ui-json` protocol, and verification status |
| [writing-standard.md](writing-standard.md) | Normative prose: one home per fact, line budgets, code comments, commits, issues, and pull requests |

### Build plans

| Document | Status |
|---|---|
| [plans/delegation-fidelity-build.md](plans/delegation-fidelity-build.md) | Implemented 2026-11-19 for Phases 1–2; probes R1/R2 pending real-model access |
| [plans/engine-host-build.md](plans/engine-host-build.md) | Implemented 2026-11-20 (Phases 1–4) against aharness v0.4.1; real-provider and real-terminal smokes pending |
| [plans/prompt-optimization-build.md](plans/prompt-optimization-build.md) | Partially implemented 2026-11-19 by the aharness v0.3.0 bump; v0.4.0 workspace instructions adopted by delegation-fidelity Phase 2; real-model probes pending |
| [plans/prompt-transparency-build.md](plans/prompt-transparency-build.md) | Implemented 2026-11-22 for Phases 0–3 against aharness v0.5.0; automated gates pass, real-provider and real-terminal smokes pending |
| [plans/subagent-live-view-build.md](plans/subagent-live-view-build.md) | Implemented 2026-11-25 for Phases 0–3 against aharness v0.6.0; automated gates pass, real-provider smoke pending |
| [plans/transcript-folding-build.md](plans/transcript-folding-build.md) | Implemented 2026-11-14; real-provider fold smoke pending |
| [plans/tui-build.md](plans/tui-build.md) | Implemented 2026-11-10 |

The SDK build plans (library facade, model discovery, thinking effort, subagent,
web tools, limits) remain in
[aharness](https://github.com/eddiectc/aharness/tree/main/docs/plans).

## Link check

`make docs-check` verifies that relative links and image targets in repository
Markdown files resolve. Code fences are ignored so examples are not treated as
links. `make dod` runs this check as part of the full automated
definition-of-done set.
