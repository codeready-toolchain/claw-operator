# ADR-0008: MCP Server Support

**Status:** Implemented
**Date:** 2026-05-11

## Overview

Add operator-managed MCP (Model Context Protocol) server configuration to the Claw CRD. MCP servers extend the AI assistant's capabilities by providing structured tool access to external services (GitHub API, filesystems, databases, etc.). Previously, users had to manually configure MCP servers inside the pod — there was no declarative, CR-driven way to manage them.

This feature enables users to declare MCP servers in the Claw CR with proper secret management, and the operator injects the configuration into `operator.json` at reconciliation time.

## Design Principles

1. **Security first:** Real secrets must not reach the gateway container whenever possible. The MITM proxy is the preferred credential injection path. HTTP/SSE MCP servers are recommended over stdio. For stdio, a proxy-placeholder pattern keeps secrets off the gateway in most cases. Direct gateway env var secrets (`envFrom`) are the last resort.

2. **Declarative and reconcilable:** MCP server configuration is part of the Claw CR spec, reconciled into `operator.json` the same way providers and channels are.

3. **Minimal API surface:** Follow existing patterns (channels, providers) rather than inventing new ones. Reuse `credentials` for domain allowlisting and auth injection.

4. **User config preserved:** In `merge` mode, operator-managed MCP servers merge alongside user-managed servers. Operator-managed servers win on collision (same server name).

## Decisions

| # | Question | Decision | Rationale |
|---|----------|----------|-----------|
| 1 | How should MCP server secrets be handled? | Three-tier security model (HTTP proxy, stdio proxy placeholder, stdio real secret) | Full coverage of all MCP servers while preserving the "no secrets on gateway" principle for tiers 1–2. Tier 3 is explicit opt-in for cases where proxy injection is not viable. |
| 2 | Should HTTP MCP auth go through the proxy or be config-injected? | Proxy-injected headers via `credentials` entries | Consistent with the operator's security model. Secrets stay off the gateway. Reuses existing credential infrastructure. |
| 3 | How should proxy domain allowlisting work for MCP? | Auto-extract domain from HTTP MCP URLs; stdio domains use existing `credentials` entries | HTTP MCP "just works" with no extra config. No new API surface (`allowedDomains` field rejected as unnecessary). |
| 4 | Where should MCP config live in the CRD? | New `spec.mcpServers` map, parallel to `credentials` | Clean separation — MCP is not a credential or channel. Map keyed by server name maps directly to OpenClaw's `mcp.servers` config shape. |
| 5 | Should `env` and `envFrom` be separate fields? | Separate fields | Clear secret/non-secret distinction. `env` for tier 2 placeholders in `operator.json`, `envFrom` for tier 3 container-level `secretKeyRef`. Follows Kubernetes naming conventions. |
| 6 | Should the operator validate MCP server configs? | Validate only structurally certain things | CEL for mutual exclusivity (`command` xor `url`) and required fields. Skip validation of `transport` values or `env` key naming — those are OpenClaw's concern. Reconciler validates `envFrom` secret existence. |
| 7 | How should the status condition work? | New `McpServersConfigured` condition | Follows existing pattern (`CredentialsResolved`, `ProxyConfigured`). Clear signal for MCP-specific issues without muddying other conditions. |

## Architecture

### Three-Tier Security Model

| Tier | Transport | Secret handling | Secrets on gateway? |
|---|---|---|---|
| **1. HTTP/SSE MCP** (preferred) | HTTP URL | Proxy `credentials` entry for the URL's domain | No |
| **2. Stdio + proxy placeholder** (recommended for stdio) | Subprocess | Placeholder env var + proxy `credentials` entry for known domains | No |
| **3. Stdio + real secret** (escape hatch) | Subprocess | `envFrom` with `secretKeyRef` on the gateway container | Yes |

**Tier 1** — HTTP/SSE MCP servers are the recommended path. Traffic goes through the MITM proxy. The user adds a `credentials` entry for the MCP URL's domain. The proxy injects auth headers. No secrets on the gateway.

**Tier 2** — Stdio MCP subprocesses inherit `HTTP_PROXY`/`HTTPS_PROXY`, so their outbound HTTPS calls go through the MITM proxy. The user sets the env var to a placeholder value and adds a `credentials` entry for the domain. The MCP server sends auth with the placeholder, the proxy strips and replaces it. The user needs to know which domain the MCP server calls and what auth type it uses.

**Tier 3** — When the user doesn't know the MCP server's internals, or the MCP server uses the secret for non-HTTP purposes, `envFrom` mounts the real secret on the gateway container. This is an explicit opt-in — the user accepts the security tradeoff.

### MCP Transport Types

OpenClaw supports two MCP transport types:

- **Stdio**: A child process spawned by the gateway (`command` + `args`). Environment variables are passed to the child process.
- **HTTP** (`streamable-http` or `sse`): An outbound HTTP connection to a remote URL.

### Operator Reconciliation Flow

```
Claw CR spec.mcpServers
         │
         ▼
  resolveAndApplyCredentials()
         ├─► validateMcpServerSecrets()     ◄── validate envFrom secrets exist
         │                                       set McpServersConfigured condition
         │
         ▼
  applyProxyResources()
         ├─► generateProxyConfig()          ◄── auto-extract HTTP MCP URL domains
         │                                       as passthrough routes (alongside
         │                                       credential routes and builtins)
         │
         ▼
  buildKustomizedObjects()                  ◄── load embedded Kustomize manifests
         │
         ▼
  configureDeployments()
         ├─► configureGatewayForMcpServers() ◄── env vars on gateway container
         │                                        (tier 3 envFrom secrets)
         │
         ▼
  stampMcpSecretVersionAnnotation()         ◄── stamp gateway deployment pod
         │                                       template (rollout on secret change)
         │
         ▼
  enrichConfigAndNetworkPolicy()
         ├─► injectMcpServersIntoConfigMap() ◄── operator.json { mcp.servers }
         │
         ▼
  merge.js (init-config at pod start)       ◄── PVC openclaw.json (deep-merge
                                                 preserves user MCP servers)
```

`stampSecretVersionAnnotation` (existing) stamps the **proxy** deployment for credential secrets. MCP `envFrom` secrets need a separate `stampMcpSecretVersionAnnotation` that stamps the **gateway** deployment, since that's where the env vars are mounted.

### Network Access

- **HTTP MCP servers**: Domain auto-extracted from `url` and added as a `type: none` passthrough route in the proxy config. If the user also has a `credentials` entry for the same domain (for auth), the credential takes precedence.

- **Stdio MCP servers**: Users add `credentials` entries for domains the MCP server needs. For tier 2, these credentials also handle auth injection.

## CRD Schema

New `spec.mcpServers` map on `ClawSpec`:

```yaml
spec:
  mcpServers:
    <server-name>:
      # Stdio server (mutually exclusive with url)
      command: <string>
      args: [<string>, ...]
      env:          # plain env vars (tier 2 placeholders)
        <KEY>: <value>
      envFrom:      # secret-backed env vars (tier 3)
        - name: <ENV_VAR_NAME>
          secretRef:
            name: <secret-name>
            key: <secret-key>

      # HTTP server (mutually exclusive with command)
      url: <string>
      transport: <string>  # "streamable-http" or "sse"
```

### CEL Validation

Only structurally certain rules:
- `command` and `url` are mutually exclusive
- At least one of `command` or `url` must be set

No validation of `transport` values or `env` key naming — those are OpenClaw's concern.

### Reconciler Validation

- `envFrom` referenced Secrets must exist and contain the specified key (same pattern as credential validation)
- Failures set `McpServersConfigured=False` with a descriptive message

## ConfigMap Injection

For each `McpServerSpec`, the operator builds the server config object:

- **Stdio**: `{ "command": ..., "args": ..., "env": { ... } }` — `env` includes plain values. For `envFrom` entries, the env var name is included with a placeholder value (the real value comes from the container environment at runtime).
- **HTTP**: `{ "url": ..., "transport": ... }`

The config is set at `mcp.servers.<name>` in `operator.json`.

## Gateway Deployment Modification

For `envFrom` entries (tier 3), env vars are added to the gateway container as `secretKeyRef` references. A separate `stampMcpSecretVersionAnnotation` function stamps the gateway pod template with MCP secret `ResourceVersion`s to trigger rollouts when secrets change.

## Status Condition

New `McpServersConfigured` condition type:

- `True` — all MCP server secrets validated and config injected
- `False` — secret validation failed (with descriptive message)
- Not set when `spec.mcpServers` is empty

Failures also set `Ready=False`.

## Example: Full CR

```yaml
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    # LLM provider
    - name: anthropic
      type: apiKey
      secretRef:
        - name: anthropic-api-key
          key: api-key
      provider: anthropic

    # GitHub API — enables tier 2 proxy placeholder for the GitHub MCP server
    - name: github
      type: bearer
      domain: api.github.com
      secretRef:
        - name: github-pat-secret
          key: token

  mcpServers:
    # Tier 1: HTTP MCP — domain auto-extracted, no secrets needed
    context7:
      url: https://mcp.context7.com/mcp
      transport: streamable-http

    # Tier 2: Stdio MCP — placeholder env, proxy injects real PAT
    github:
      command: npx
      args: ["-y", "@modelcontextprotocol/server-github"]
      env:
        GITHUB_PERSONAL_ACCESS_TOKEN: placeholder

    # Tier 3: Stdio MCP — real secret on gateway (escape hatch)
    custom-db:
      command: node
      args: ["db-mcp-server.js"]
      env:
        DB_HOST: postgres.internal
      envFrom:
        - name: DB_PASSWORD
          secretRef:
            name: db-credentials
            key: password
```

## Future Considerations

- Well-known MCP server presets (auto-configure command, args, and tier recommendation)
- MCP server health checking and status reporting per server
- Resource limits on stdio MCP server subprocesses
