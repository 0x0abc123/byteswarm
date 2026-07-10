---
id: "0007"
title: "CI provider — GitHub Actions"
date: 2026-07-10
status: proposed
deciders: ["claude (advisor)"]
retrospective: false
decision_key: ci_provider
supersedes: null
superseded_by: null
tags: [ci, tooling]
---

# ADR-0007: CI provider — GitHub Actions

## Context

The repository is hosted on **GitHub** (confirmed in the interview). The CI provider
must be recorded so bootstrap can generate the pipeline.

## Decision

We will use **GitHub Actions** as the CI provider.

## Consequences

- bootstrap's `generate-cicd` emits GitHub Actions workflows; the reference
  implementation for GitHub exists today.
- Branch protection and required status checks are set via the human checklist
  bootstrap emits — they cannot be configured from files.

## Alternatives considered

- GitLab CI / Jenkins — rejected: not the chosen host; either would receive only the
  abstract pipeline until a provider implementation exists.
