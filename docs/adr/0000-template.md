---
# Machine-readable header — parsed by skills (impact-analyzer, generate-* skills).
# Filename convention: NNNN-short-kebab-title.md  (NNNN = zero-padded id, append-only)
id: "0000"
title: "<short noun phrase for the decision>"
date: YYYY-MM-DD
status: proposed            # proposed | accepted | superseded | rejected
deciders: []                # human names; may include "claude (advisor)"
retrospective: false        # true when recovered from an existing codebase
                            #   (recover-architecture) rather than decided fresh
decision_key: null          # key written into project-state.yaml active.adr_refs
                            #   e.g. backend_language, client_approach, ci_provider,
                            #   architecture_style, data_stores, deploy_targets
supersedes: null            # ADR id this replaces, if any
superseded_by: null         # filled in later if replaced; status -> superseded
tags: []                    # free-form, e.g. [backend, security]
---

# ADR-0000: <title>

## Context

<!-- 2–6 sentences. What situation, requirement, or constraint forces a
decision? Reference interview answers or invariants, not opinions. This is the
section refactor-architecture's impact analysis leans on most: when the context
stops being true, the decision is due for review. -->

## Decision

<!-- 1–3 sentences, imperative and unambiguous: "We will use X for Y."
Include the specific choice a generator skill needs (exact stack item, exact
provider) — generators read decisions, not discussions. -->

## Consequences

<!-- Bulleted. Both directions: what this enables, what it costs or rules out,
and any follow-on decisions it forces (e.g. "choosing Wails pushes the backend
decision toward Go"). -->

## Alternatives considered

<!-- One line each: alternative — why not chosen. Keep it honest and short;
this is the second-most-valuable section during a future refactor. -->

<!-- =========================================================================
RULES (apply to all ADRs; delete this block in real ADRs)
* ADRs are APPEND-ONLY. Never edit an accepted ADR's Decision; write a new ADR
  that supersedes it and update both headers (supersedes / superseded_by).
* One decision per ADR. If the advisor makes five choices, it writes five ADRs.
* Every value in project-state.yaml `active:` must trace to exactly one
  accepted ADR via `decision_key`.
* Target length: under one screen. Detail belongs in docs/, not here.
========================================================================== -->
