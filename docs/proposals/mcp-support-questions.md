# MCP Support — Design Questions

**Status:** All decisions resolved
**Related:** [Design document](mcp-support-design.md)

Each question has options with trade-offs and a recommendation. Go through them one by one to form the design, then update the design document.

## Q1: How should MCP server secrets be handled?

MCP servers may need credentials (API tokens, passwords). The operator's core security principle is that real secrets stay on the proxy, not the gateway. The question: how does this apply to the two MCP transport types?

### Three-tier security model (chosen)

| Tier | Transport | Secret handling | Secrets on gateway? |
|---|---|---|---|
| **1. HTTP/SSE MCP** (preferred) | HTTP URL | Proxy `credentials` entry for the URL's domain | No |
| **2. Stdio + proxy placeholder** (recommended for stdio) | Subprocess | Placeholder env var + proxy `credentials` entry for known domains | No |
| **3. Stdio + real secret** (escape hatch) | Subprocess | `envFrom` with `secretKeyRef` on the gateway container | Yes |

**Tier 1 — HTTP/SSE MCP servers:** Traffic goes through the MITM proxy. The user adds a `credentials` entry for the MCP URL's domain (e.g., `type: bearer` for `api.example.com`). The proxy injects auth headers. No secrets on the gateway.

**Tier 2 — Stdio with proxy placeholder:** Stdio MCP subprocesses inherit `HTTP_PROXY`/`HTTPS_PROXY`, so their outbound HTTPS calls go through the MITM proxy too. If the user knows which domain the MCP server talks to, they can set the env var to a **placeholder** value and add a `credentials` entry for the domain. The MCP server sends `Authorization: Bearer placeholder`, the proxy strips it and injects the real token. Example:

```yaml
spec:
  credentials:
    - name: github
      type: bearer
      domain: api.github.com
      secretRef:
        - name: github-pat-secret
          key: token
  mcpServers:
    github:
      command: npx
      args: ["-y", "@modelcontextprotocol/server-github"]
      env:
        GITHUB_PERSONAL_ACCESS_TOKEN: placeholder
```

The user needs to know: which domain the MCP server calls, and what auth type it uses. The platform skill documents this for well-known MCP servers.

**Tier 3 — Stdio with real secret (escape hatch):** When the user doesn't know the MCP server's internals, or the MCP server uses the secret for non-HTTP purposes, `envFrom` mounts the real secret as a gateway container env var. This breaks the "no secrets on gateway" principle but is the only option in these cases. Follows the same pattern as `OPENCLAW_GATEWAY_TOKEN` (already a `secretKeyRef` on the gateway).

- **Pro:** Full coverage — every MCP server can be supported at some tier
- **Pro:** Tiers 1 and 2 preserve the security model (no secrets on gateway)
- **Pro:** Tier 3 is explicit opt-in — user consciously accepts the tradeoff
- **Pro:** Tier 2 works for the most common stdio MCP servers (GitHub, Slack, etc.) whose APIs are well-documented

_Considered and rejected: No secret support for stdio (impractical: most useful stdio MCP servers need secrets). Sidecar proxy for stdio (massive complexity: MCP stdio uses stdin/stdout pipes, not sockets)._

**Decision:** Three-tier model. HTTP/SSE is the recommended path (tier 1). Stdio with proxy placeholder is the recommended stdio path (tier 2) — platform skill documents the pattern for well-known MCP servers. `envFrom` with real secrets on the gateway (tier 3) is the escape hatch for cases where tiers 1–2 don't work. Users must also add `credentials` entries to allowlist domains their stdio MCP server needs (applies to both tiers 2 and 3).

**Documentation requirement:** The three-tier model and the proxy-placeholder pattern are non-obvious. Implementation must include:
- **PLATFORM.md skill update**: Comprehensive section teaching OpenClaw how to guide users through each tier, with worked examples for well-known MCP servers (GitHub, etc.). The skill must explain which tier to recommend based on the user's situation, how to identify domains/auth types for tier 2, and when to fall back to tier 3.
- **`docs/provider-setup.md` update**: Step-by-step setup instructions for MCP servers, with examples covering all three tiers.

These doc updates are a required part of the implementation, not a follow-up.

## Q2: Should HTTP MCP auth go through the proxy or be config-injected?

**Resolved by Q1.** The three-tier model from Q1 establishes that HTTP MCP auth uses the proxy via `credentials` entries (tier 1). No separate decision needed.

**Decision:** Proxy-injected headers (Option A). User adds both an MCP server entry and a `credentials` entry for the domain. The proxy injects auth. Secrets stay off the gateway. This is consistent with the operator's security model and reuses existing credential infrastructure.

_Considered and rejected: Option B — config-injected headers (secrets in `operator.json` on a ConfigMap, breaks proxy isolation). Option C — hybrid (same outcome as A, unnecessary distinction)._

## Q3: How should proxy domain allowlisting work for MCP servers?

MCP servers need network access. Stdio MCP servers make outbound HTTP calls (through the proxy). HTTP MCP servers connect to their URL (through the proxy). Domains must be in the proxy allowlist.

### Option C: Automatic from URL only (no `allowedDomains` field)

Auto-extract domain from HTTP MCP URLs and add as a `type: none` passthrough route. Stdio MCP server domains are allowlisted via `credentials` entries (which the user already adds for tier 2/3 auth anyway per Q1).

- **Pro:** HTTP MCP "just works" — unauthenticated HTTP MCP servers (like Context7) need only the MCP entry, no separate credential
- **Pro:** Authenticated HTTP MCP servers already have a `credentials` entry (Q1 tier 1), so auto-extraction is redundant but harmless
- **Pro:** No new API surface — no `allowedDomains` field
- **Pro:** Stdio domain handling is consistent with Q1 — users add `credentials` entries for both auth and allowlisting

_Considered and rejected: Option A — auto from URL + `allowedDomains` field (adds a new domain-allowlisting concept parallel to `credentials`, unnecessary complexity). Option B — manual via `credentials` entries only (forces a `type: none` credential for every unauthenticated HTTP MCP server, verbose for the common case)._

**Decision:** Option C. HTTP MCP URLs auto-extract the domain as a passthrough. Stdio MCP domains use existing `credentials` entries. No new API surface.

Example — unauthenticated HTTP MCP (Context7) + authenticated stdio MCP (GitHub):

```yaml
spec:
  credentials:
    # Tier 2: proxy injects real PAT on api.github.com (also allowlists the domain)
    - name: github
      type: bearer
      domain: api.github.com
      secretRef:
        - name: github-pat-secret
          key: token
  mcpServers:
    # HTTP MCP — domain auto-extracted from URL, no credential needed
    context7:
      url: https://mcp.context7.com/mcp
      transport: streamable-http

    # Stdio MCP — placeholder env, proxy handles real auth on api.github.com
    github:
      command: npx
      args: ["-y", "@modelcontextprotocol/server-github"]
      env:
        GITHUB_PERSONAL_ACCESS_TOKEN: placeholder
```

## Q4: Where should MCP config live in the CRD?

The operator currently has `spec.configMode` and `spec.credentials`. MCP is a new top-level concept. Where does it go?

### Option A: New `spec.mcpServers` map

A new top-level field on `ClawSpec`, parallel to `credentials`:

```yaml
spec:
  credentials: [...]
  mcpServers:
    github:
      command: npx
      args: ["-y", "@modelcontextprotocol/server-github"]
      envFrom:
        - name: GITHUB_PERSONAL_ACCESS_TOKEN
          secretRef:
            name: github-pat-secret
            key: token
```

- **Pro:** Clean separation — MCP is not a "credential" or "channel"
- **Pro:** Map keyed by server name (natural for MCP where names matter)
- **Pro:** Room for MCP-specific fields without overloading `CredentialSpec`
- **Pro:** Maps directly to OpenClaw's `mcp.servers` config shape

_Considered and rejected: Option B — extend `CredentialSpec` (already complex with 7 types + channels + providers; MCP doesn't fit the credential model). Option C — raw config pass-through like the upstream operator (no structured validation, no secret integration, secrets as plain text in config)._

**Decision:** Option A. `spec.mcpServers` as a new top-level map.

## Q5: Should `env` and `envFrom` be separate fields or unified?

Stdio MCP servers need environment variables — some are plain values, some come from Kubernetes Secrets. How should the API express this?

### Option A: Separate `env` (plain) and `envFrom` (secret-backed)

```yaml
mcpServers:
  github:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      LOG_LEVEL: debug
    envFrom:
      - name: GITHUB_PERSONAL_ACCESS_TOKEN
        secretRef:
          name: github-pat-secret
          key: token
```

- **Pro:** Clear distinction between secret and non-secret values
- **Pro:** Plain `env` values go directly into `operator.json`; `envFrom` become `secretKeyRef` on the container and placeholders in config
- **Pro:** Follows Kubernetes naming conventions (`env` + `envFrom`)
- **Pro:** Maps cleanly to the three-tier model: `env` for tier 2 placeholders, `envFrom` for tier 3 real secrets

_Considered and rejected: Option B — unified `env` with polymorphic values (string | object is ugly in Go CRD types, CRD schema can't express it cleanly, harder to distinguish which env vars need container-level `secretKeyRef` mounting)._

**Decision:** Option A. Separate `env` and `envFrom` fields.

## Q6: Should the operator validate MCP server configurations?

The operator could validate MCP server configs (e.g., stdio must have `command`, HTTP must have `url`, not both). Or it could pass them through and let OpenClaw validate at runtime.

### Decision: Validate only what we're 100% sure about

CEL rules for things that are structurally certain:
- `command` and `url` are mutually exclusive (a server can't be both stdio and HTTP)
- At least one of `command` or `url` must be set (an empty server entry is always wrong)
- `envFrom` secret references must be well-formed (name + key required)

Skip validation for things that could change in OpenClaw:
- Don't enforce `transport` values (`sse`, `streamable-http`) — OpenClaw could add new ones
- Don't enforce that `args` requires `command` — OpenClaw might evolve
- Don't validate `env` key naming — that's the MCP server's concern

Reconciler-time validation (not CEL):
- `envFrom` referenced Secrets must exist and contain the specified key (same pattern as credential validation)

_Rationale: Tight validation that blocks valid configs is worse than loose validation that lets misconfigs through to runtime. OpenClaw gives clear errors for bad MCP config. We only gate on things that are structurally impossible to be correct._

## Q7: How should the status condition work for MCP?

The operator uses conditions to report state: `CredentialsResolved`, `ProxyConfigured`, `Ready`. Should MCP have its own condition?

### Option A: New `McpServersConfigured` condition

Separate condition, set to `True` when all MCP server secrets are validated and config is injected.

- **Pro:** Clear signal for MCP-specific issues
- **Pro:** Follows existing pattern (`CredentialsResolved`, `ProxyConfigured`)

_Considered and rejected: Option B — fold into `CredentialsResolved` (muddies the meaning, MCP isn't a "credential"). Option C — no new condition, only `Ready` (less granular, can't distinguish MCP failures from other issues)._

**Decision:** Option A. New `McpServersConfigured` condition.
