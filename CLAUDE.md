<!-- Template CLAUDE.md — serves sessions BEFORE bootstrap. At bootstrap,
     the generate-claude-md skill replaces this with the project's own. -->

# Project template (pre-bootstrap)

This repo is a freshly cloned project template. No system code exists yet.
The pipeline position lives in `docs/project-state.yaml` — read it before
doing pipeline work.

## Pipeline (strict order, human sign-off between stages)

1. `/architecture-advisor` → ADRs + architecture brief
2. `/c4-designer` → L1/L2 Mermaid diagrams + sequences
3. `/bootstrap-structure` → structure, CLAUDE.md files, CI/CD, README (exit:
   CI green)

After bootstrap: `/plan-roadmap` to sequence the feature build-out;
`/implement-feature` for all code changes; `/refactor-architecture` for
anything contradicting an accepted ADR.

## Hard constraints

- Never skip a stage gate or mark a stage approved without an explicit human
  approval and name; state-change commits are separate:
  `chore(state): ... [approved-by: <Name>]`.
- Never commit directly to main; never self-merge a PR.
- Dispatch the `verifier` subagent for any test/lint run; `ci-monitor` for
  CI status — raw logs stay out of this context.
- Follow `reference/design-principles.md` and
  `reference/security-fundamentals.md`.
- Do not edit generated artifacts by hand (diagrams, CLAUDE.md, pipelines) —
  regenerate via skills. `reference/` and `docs/adr/` bodies are human-owned.

## Where things live

`.claude/skills/` (the skills library — a pinned submodule; run
`git submodule update --init` if it's empty) · `.claude/agents/` (subagents) ·
`reference/` (project-owned: design principles, security fundamentals,
overrides) · `docs/` (state, ADRs, C4, blockers). Any `reference/<path>` a
skill reads resolves project-first, then falls back to
`.claude/skills/reference/<path>`. Full map: README.md.
