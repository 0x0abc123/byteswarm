# Project Template — AI-assisted enterprise software lifecycle

A template repository for building software with humans + Claude Code. It
ships a pipeline of skills that take a project from blank clone to a working,
CI-green skeleton, then govern day-to-day development and architectural
change — with human sign-off gates, full traceability, and deliberate
context/token economy throughout.

> This file is the **template's** guide. When you run `/bootstrap-structure`,
> it is relocated to `docs/template-guide.md` and replaced by a README for
> the system you're building.

## Requirements

- [Claude Code](https://docs.claude.com/en/docs/claude-code) and `git`.
- Stack toolchains (Go / uv / Node) are checked — and their install hints
  provided — by the pipeline itself before anything is generated.

## Quick start

```
git clone <this-template> my-project && cd my-project
git submodule update --init          # pulls the pinned skills library into .claude/skills
claude
```

Then run the pipeline, in order, approving each stage when asked:

| Step | Skill | Produces |
|---|---|---|
| 1 | `/architecture-advisor` | interview → ADRs (docs/adr/) + architecture brief |
| 2 | `/c4-designer` | L1/L2 Mermaid diagrams + key sequence diagrams (docs/c4/) |
| 3 | `/bootstrap-structure` | component trees, CLAUDE.md files, CI/CD, README — proven green locally and in CI |

After that, run **`/plan-roadmap`** to turn the brief's feature list into an
ordered, milestone-grouped sequence of ready-to-paste `/implement-feature`
invocations (`docs/roadmap.md` — re-run it any time to mark progress and
elaborate the next milestone). Day-to-day work flows through
**`/implement-feature`**, and any change that contradicts an accepted ADR
flows through **`/refactor-architecture`**. Current pipeline position always
lives in `docs/project-state.yaml`; history in `docs/state-log/`.

## Getting the best outcomes

### Invoking `/implement-feature` well

Give it one **verifiable behavior in one component**, with acceptance
criteria and explicit non-goals. A good invocation:

> /implement-feature Add password-reset request to the backend.
> - Component: backend/
> - Behavior: `POST /auth/reset-request` takes an email and always returns
>   202. If the account exists, create a single-use token (30-min expiry,
>   stored hashed) and send it through the existing Notifier port.
> - Out of scope: the reset-confirmation endpoint and the frontend form —
>   they're the next two tasks.
> - Acceptance: unit tests for expiry and single-use; integration test with
>   a fake Notifier; no new dependencies.

What makes this good: the component is named (context loading stays small),
the behavior is testable as stated (the skill writes tests from it), scope
edges are explicit (prevents drift), and constraints are stated up front
rather than discovered in review.

**Chunking a broad feature.** "Add user accounts" is an epic, not a task.
Slice it by user-visible capability, then order chunks along the API
contract — backend endpoints first (they define the contract), clients
second (they consume it):

1. registration endpoint + credential storage (backend)
2. login + session issuance (backend)
3. login/registration forms (frontend, against the now-real API)
4. password reset request (backend) … and so on.

Right-sized ≈ one PR a human reviews in under 30 minutes: one component, a
handful of files, one behavior. Too big if it touches multiple components,
several domain packages, or needs a new ADR (that's a refactor). Very small
is fine — a rename is just a `chore`. You don't have to do this slicing by
hand: `/plan-roadmap` applies these rules to the whole feature list from the
architecture brief and hands you the invocations pre-written.

### Invoking `/refactor-architecture` well

Lead with **what changed in the world**, not with your solution — the
impact-analyzer maps the consequences; your job is the motivation and the
constraints:

> /refactor-architecture Move the backend datastore from SQLite to
> PostgreSQL.
> - Motivation: we're about to run 3 API instances behind a load balancer;
>   SQLite's single-writer model breaks that. ADR-0004's context assumed a
>   single instance — that premise no longer holds.
> - Believed scope: the store adapter, CI (integration tests need a
>   postgres service), deployment config. Domain packages should be
>   untouched — the repository ports stay as they are.
> - Constraints: no production data exists yet, so no data migration.

What makes this good: it names the broken ADR premise (the analyzer's
anchor), states believed scope as a *hypothesis* for the analyzer to verify
rather than a plan to obey, and declares constraints ("ports stay") that
become checkable claims — if the migration starts touching domain packages,
the skill halts and re-analyzes.

### Refactoring while feature branches are open

Structural change and parallel feature work don't mix silently, so the
framework serializes them explicitly:

- A refactor in flight is **visible state**: it sits in
  `docs/project-state.yaml` `refactors:` from proposal to completion, and
  `/implement-feature` checks that list before starting — new tasks on
  affected components get warned and should usually wait.
- At the refactor approval gate, the skill lists every open branch/PR and
  requires a per-branch plan before anything regenerates: **land first**
  (default for small or nearly-done work), **continue in parallel** (only
  branches on unaffected components), or **pause and rebase after**.
- After the refactor merges, each paused branch rebases onto main and
  re-runs verification before its next push. An *adapter-only* refactor
  (the impact report says which) should cost feature branches little —
  domain code and ports didn't move; if a rebase explodes anyway, that's a
  signal the refactor was misclassified — say so.
- Never run two refactors concurrently, and prefer many small refactors to
  one sweeping one — the impact report keeps each honest.

### General tips

- **Trust the gates.** The skills stop at sign-offs, blockers, and ADR
  contradictions by design. Overriding a halt is occasionally right, but do
  it knowingly — the halts exist to spend human attention where it matters.
- **Feed corrections early.** The cheapest moment to fix a decision is the
  interview playback; the second cheapest is the stage sign-off; the most
  expensive is after code exists.
- **Blockers are handoffs, not failures.** A `docs/blockers/` file is
  written so a fresh session — human or Claude — can resume cold. Resolve
  it, delete it in the fixing commit.
- **Run `/check-environment`** whenever
  verification behaves oddly on a machine — before debugging "failures"
  that are really a missing tool.

## Repository map

| Path | What it is |
|---|---|
| `.claude/skills/` | The skills library — a git **submodule** of the separately versioned `claudecode-dev-harness-skills` repo, pinned to a tag |
| `.claude/agents/` | Subagents: `verifier`, `ci-monitor`, `impact-analyzer` — context isolation for noisy/analytical work |
| `reference/` | Project-owned knowledge: design principles, security fundamentals — plus any project overrides of framework reference files (see below) |
| `docs/adr/` | Architecture Decision Records (append-only; superseded, never edited) |
| `docs/c4/` | Mermaid C4 diagrams — L1/L2 up front, L3 lazily per container |
| `docs/project-state.yaml` | Current pipeline state (small, overwritten) |
| `docs/state-log/` | Append-only event history — `ls` it for a project timeline |
| `docs/blockers/` | Structured halt reports from the 3-attempt guardrail |
| `scripts/` | `update-skills.sh` and friends |

## Updating the skills

`.claude/skills/` is a git submodule of the `claudecode-dev-harness-skills` repo,
pinned to a tag. Pull improvements deliberately:
`./scripts/update-skills.sh [tag]` (defaults to the latest tag) — it updates
the pin, writes a `skills-updated` state-log event, and stages both for your
review and commit. Never track a branch.

**Reference resolution:** framework knowledge (stack conventions, CI
providers, diagram conventions, the CLAUDE.md template) lives inside the
submodule at `.claude/skills/reference/` and updates with it. Any
`reference/<path>` a skill reads resolves **project-first**: put a copy in
this repo's `reference/` to override it for this project; delete the copy to
return to the framework default. `design-principles.md` and
`security-fundamentals.md` are always project-owned — no framework copy
exists.

## Existing codebase?

This template is the **greenfield** entry. To adopt the harness in a repo
that already contains software, don't clone this — add the skills submodule
to the existing repo and run the brownfield pipeline: `/codebase-survey`
(inventories the code, seeds the harness scaffolding) →
`/recover-architecture` (retrospective ADRs + divergence register) →
`/c4-designer` (unchanged) → `/retrofit-harness` (verification baseline
with test quarantine, CLAUDE.md wrap, CI adopt-or-generate). From there the
lifecycle is identical to greenfield.

## Design notes (why it's shaped this way)

Every skill reads its inputs from files (ADRs, state, reference/) rather
than conversation history; verification noise lives in subagents; CLAUDE.md
files are small and hierarchical; L3 diagrams are generated only when
needed. All of it serves the same goal: humans get a simple, gated
lifecycle, and Claude gets exactly the context each task needs — no more.
