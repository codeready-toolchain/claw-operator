# ADR-0018: Centralized Provider Registry

**Status:** Implemented
**Date:** 2026-05-31

---

## Problem

Per-provider knowledge was scattered across four independent maps in the controller:

| Map | Purpose | File |
|-----|---------|------|
| `knownAPIKeyProviders` | Domain + header defaults for apiKey credentials | `claw_providers.go` |
| `companionProviders` | Internal provider name mappings (e.g., openai → openai-codex) | `claw_providers.go` |
| `vertexProviderAPIMapping` | Wire format API for Vertex AI SDK path | `claw_providers.go` |
| `modelCatalog` | Model names and aliases per provider | `claw_models.go` |

Adding a new first-class provider required updating up to four maps in two files — easy to miss one and produce subtle bugs. The wire format API identifier (`api` field in `models.providers`) was not tracked anywhere, leading to a bug where non-OpenAI providers (Google, Anthropic) used the wrong wire format (`openai-completions` instead of their native API).

Additionally, `injectProviders()` built provider config entries as plain maps and then relied on downstream code to mutate them with the correct `api` field — a "build then mutate" pattern that made it easy to forget the mutation step, which is exactly what caused the wire format bug.

---

## Design

### Single registry

A single `knownProviders` map of `providerDefaults` structs in `claw_providers.go` replaces all four maps. Each entry captures everything the operator needs to know about a provider:

| Field | Purpose | Example (anthropic) |
|-------|---------|---------------------|
| `Domain` | Default upstream domain for apiKey credentials | `api.anthropic.com` |
| `Header` | Default auth header name | `x-api-key` |
| `API` | OpenClaw wire format identifier | `anthropic-messages` |
| `VertexAPI` | Wire format for Vertex AI SDK path | `anthropic-messages` |
| `BasePath` | URL path appended to upstream host | (empty) |
| `Companions` | Internal provider names auto-injected alongside | (empty) |
| `VertexPlugin` | ClawHub package required for Vertex SDK path | `@openclaw/anthropic-vertex-provider` |
| `Models` | Model catalog entries (name + alias) | `[{claude-sonnet-4-6, Claude Sonnet 4.6}, ...]` |

Providers not in the registry (e.g., `openrouter`, custom self-hosted endpoints) still work — they just get no defaults, no API override (OpenClaw defaults to `openai-completions`), and no model catalog.

### Builder function

`buildProviderEntry()` constructs `models.providers` entries with the correct `api` field baked in from `knownProviders`. This replaces the "build map then mutate" pattern that caused the wire format bug — the entry is correct at construction time.

### Implicit plugins

Providers that require an external OpenClaw plugin for the Vertex AI SDK path (e.g., `@openclaw/anthropic-vertex-provider` for Anthropic via Vertex) declare the package in the `VertexPlugin` field. `effectivePlugins()` merges these implicit plugins with explicit `spec.plugins` entries, deduplicating where both are declared.

### Recreate deployment rollout guard

Unrelated to the registry consolidation but fixed in the same PR: SSA `Force: true` patches on `Recreate`-strategy Deployments increment `generation` on every apply — even when the desired state is identical — triggering unnecessary pod-killing rollouts. `isRecreateDeploymentUnchanged()` computes a SHA-256 hash of the desired `spec.template` and compares it against a `desired-template-hash` annotation on the live Deployment, skipping the apply when identical.

---

## Decisions

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | How to organize provider knowledge | Single `knownProviders` map of `providerDefaults` structs | One place to add a new provider; impossible to forget a field since the struct is self-documenting |
| 2 | How to set the wire format API | `buildProviderEntry()` reads from registry at construction time | Eliminates the "build then mutate" pattern; entry is correct from the start |
| 3 | Where to define model catalogs | `Models` field inside `providerDefaults` | Models are provider-specific knowledge; keeping them in the registry avoids a separate map that can drift |
| 4 | How to handle Vertex SDK plugins | `VertexPlugin` field + `effectivePlugins()` auto-merge | Users don't need to know about internal plugin requirements; deduplication prevents conflicts with explicit declarations |
| 5 | How to prevent Recreate rollout churn | SHA-256 template hash annotation + replicas comparison | Cheap comparison avoids expensive SSA apply; replicas check handles idle/unidle transitions |

---

## Backward Compatibility

- Provider behavior is unchanged — same defaults, same routing, same model catalogs.
- The wire format fix changes the `api` field in generated `models.providers` entries for `google`, `anthropic`, and `openai-codex`. This is a bug fix (they were using the wrong wire format before).
- The `desired-template-hash` annotation is new but inert on existing Deployments — the first reconcile stamps it, subsequent reconciles benefit from the guard.
- Plugin install script now uses manifest-tracked cleanup, preserving user-installed plugins across pod restarts.
