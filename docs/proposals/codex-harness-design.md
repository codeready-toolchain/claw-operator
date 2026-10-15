# Codex App-Server Harness — Design Proposal

**Status:** Early draft — problem statement and high-level direction only

**Date:** 2026-06-03

**Depends on:** [codex-oauth-design.md](codex-oauth-design.md) (Codex OAuth credential type, prerequisite)

---

## Problem Statement

The `codexOAuth` credential type (implemented in the companion design) gives users access to Codex models (GPT-5.5, GPT-5.4-mini, etc.) through OpenClaw's own agent runtime. OpenClaw sends prompts via the `openai-chatgpt-responses` wire format, receives tool call requests, and executes them using its own tool infrastructure (MCP servers, terminal, file editing, etc.).

However, OpenClaw also supports a **native Codex agent harness** — the `codex` plugin spawns a Codex app-server binary as a child process that provides its own agent runtime with sandbox execution, native tool handling, approval flows, and subagent orchestration. Some users may want this full Codex agent experience running inside OpenClaw rather than using OpenClaw's own tool execution layer.

The current `codexOAuth` design intentionally excludes the native harness because:

1. **Security boundary.** The Codex app-server runs inside the gateway pod and needs real OAuth tokens for authentication. Our security model keeps real credentials on the proxy, never the gateway.

2. **Binary dependency.** The Codex app-server is a separate binary that must be installed in the container. The operator's gateway image does not include it today.

3. **Scope.** The operator's purpose is to deploy and secure OpenClaw instances. The `codexOAuth` credential gives users Codex model access, which is the primary ask. The native harness is an enhancement.

---

## High-Level Approach

Support the Codex app-server harness through a new CRD section (e.g., `spec.codexHarness`), following the pattern used for Kubernetes support today. Key concerns to address in the detailed design:

### Binary installation

Install the Codex app-server binary via an init container or OpenClaw's managed binary system. Options include a dedicated sidecar image, a shared volume init container, or letting OpenClaw's plugin system download it at startup.

### Authentication

The app-server authenticates with `chatgpt.com` using OAuth tokens from `auth-profiles.json`. This requires real tokens available to the gateway process — a relaxation of the proxy-only security boundary. The detailed design must define what exactly is exposed (access token only vs. refresh token), whether the proxy can mediate refresh, and what the blast radius is if the gateway is compromised.

### Plugin and model configuration

Enable the `codex` plugin in `openclaw.json`, set `agentRuntime: { id: "codex" }` on configured models, and write `auth-profiles.json` with the appropriate credential. This is mostly configuration wiring — the companion design's `openai-chatgpt-responses` provider config would be replaced or supplemented by the native harness config.

### Coexistence with `codexOAuth`

Define whether users can use both simultaneously (e.g., some models through the proxy path, others through the native harness) or whether the harness mode replaces the proxy path entirely when enabled.

### Container security

The Codex app-server may need a writable filesystem, network access, and potentially elevated capabilities for sandbox execution. Document the impact on pod security context and whether this is compatible with `readOnlyRootFilesystem` and the restricted seccomp profile.

---

## Open Questions

These will be resolved during the detailed design phase:

1. How do we install the Codex app-server binary? Init container, managed download, or bundled image?
2. What is the minimum credential exposure needed for the app-server? Can we limit it to short-lived access tokens with proxy-mediated refresh?
3. Should this be a separate CRD section (`spec.codexHarness`) or an option on the existing `codexOAuth` credential?
4. What pod security changes are required? Is the Codex app-server compatible with our restricted security context?
5. How does this interact with network policies? Does the app-server need direct egress to `chatgpt.com` or can it go through the proxy?
6. What is the upgrade path for users currently using `codexOAuth` who want to switch to the native harness?

---

## References

- [Codex OAuth design](codex-oauth-design.md) — prerequisite credential type
- OpenClaw `extensions/codex/` — plugin source, agent harness, app-server client
- OpenClaw `extensions/codex/src/app-server/` — app-server lifecycle, auth bridge, binary management
