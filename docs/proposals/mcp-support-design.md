# MCP Server Support

**Status:** Final — all decisions resolved in [mcp-support-questions.md](mcp-support-questions.md)

## Overview

Add operator-managed MCP (Model Context Protocol) server configuration to the Claw CRD. MCP servers extend the AI assistant's capabilities by providing structured tool access to external services (GitHub API, filesystems, databases, etc.). Today, users must manually configure MCP servers via `openclaw config patch` inside the pod — there is no declarative, CR-driven way to manage them.

This feature enables users to declare MCP servers in the Claw CR with proper secret management, and the operator injects the configuration into `operator.json` at reconciliation time.

## Design Principles

1. **Security first:** Real secrets must not reach the gateway container whenever possible. The MITM proxy is the preferred credential injection path. HTTP/SSE MCP servers are recommended over stdio. For stdio, a proxy-placeholder pattern keeps secrets off the gateway in most cases. Direct gateway env var secrets (`envFrom`) are the last resort.

2. **Declarative and reconcilable:** MCP server configuration is part of the Claw CR spec, reconciled into `operator.json` the same way providers and channels are today.

3. **Minimal API surface:** Follow existing patterns (channels, providers) rather than inventing new ones. Reuse `credentials` for domain allowlisting and auth injection.

4. **User config preserved:** In `merge` mode, operator-managed MCP servers merge alongside user-managed servers added via `openclaw config patch` or `openclaw mcp set`. Operator-managed servers win on collision (same server name).

## Architecture

### Three-Tier Security Model

| Tier | Transport | Secret handling | Secrets on gateway? |
|---|---|---|---|
| **1. HTTP/SSE MCP** (preferred) | HTTP URL | Proxy `credentials` entry for the URL's domain | No |
| **2. Stdio + proxy placeholder** (recommended for stdio) | Subprocess | Placeholder env var + proxy `credentials` entry for known domains | No |
| **3. Stdio + real secret** (escape hatch) | Subprocess | `envFrom` with `secretKeyRef` on the gateway container | Yes |

**Tier 1** — HTTP/SSE MCP servers are the recommended path. Traffic goes through the MITM proxy. The user adds a `credentials` entry for the MCP URL's domain. The proxy injects auth headers. No secrets on the gateway.

**Tier 2** — Stdio MCP subprocesses inherit `HTTP_PROXY`/`HTTPS_PROXY`, so their outbound HTTPS calls go through the MITM proxy. The user sets the env var to a placeholder value and adds a `credentials` entry for the domain. The MCP server sends auth with the placeholder, the proxy strips and replaces it. The user needs to know which domain the MCP server calls and what auth type it uses. The platform skill documents this for well-known MCP servers.

**Tier 3** — When the user doesn't know the MCP server's internals, or the MCP server uses the secret for non-HTTP purposes, `envFrom` mounts the real secret on the gateway container. This is an explicit opt-in — the user accepts the security tradeoff.

### How MCP Servers Work in OpenClaw

OpenClaw supports two MCP transport types:

- **Stdio**: A child process spawned by the gateway (`command` + `args`). Environment variables are passed to the child process.
- **HTTP** (`streamable-http` or `sse`): An outbound HTTP connection to a remote URL.

OpenClaw configuration path: `mcp.servers` in `openclaw.json`:

```json
{
  "mcp": {
    "servers": {
      "github": {
        "command": "npx",
        "args": ["-y", "@modelcontextprotocol/server-github"],
        "env": {
          "GITHUB_PERSONAL_ACCESS_TOKEN": "placeholder"
        }
      },
      "context7": {
        "url": "https://mcp.context7.com/mcp",
        "transport": "streamable-http"
      }
    }
  }
}
```

### Operator Flow

Mapped to the actual reconciliation order in `claw_resource_controller.go`:

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

Note: `stampSecretVersionAnnotation` (existing) stamps the **proxy** deployment for credential secrets. MCP `envFrom` secrets need a separate `stampMcpSecretVersionAnnotation` that stamps the **gateway** deployment, since that's where the env vars are mounted.

### Network Access

- **HTTP MCP servers**: Domain auto-extracted from `url` and added as a `type: none` passthrough route in the proxy config. If the user also has a `credentials` entry for the same domain (for auth), the credential takes precedence.

- **Stdio MCP servers**: Users add `credentials` entries for domains the MCP server needs. For tier 2, these credentials also handle auth injection.

## CRD Schema

New `spec.mcpServers` map on `ClawSpec`:

```go
type ClawSpec struct {
    ConfigMode  ConfigMode                  `json:"configMode,omitempty"`
    Credentials []CredentialSpec            `json:"credentials,omitempty"`
    McpServers  map[string]McpServerSpec    `json:"mcpServers,omitempty"`
}
```

Each `McpServerSpec`:

```go
// McpServerSpec defines an MCP server the operator injects into OpenClaw's config.
// +kubebuilder:validation:XValidation:rule="has(self.command) || has(self.url)",message="either command (stdio) or url (HTTP) must be set"
// +kubebuilder:validation:XValidation:rule="!has(self.command) || !has(self.url)",message="command and url are mutually exclusive"
type McpServerSpec struct {
    // Command is the executable for a stdio MCP server.
    // +optional
    Command string `json:"command,omitempty"`

    // Args are command-line arguments for the stdio server.
    // +optional
    Args []string `json:"args,omitempty"`

    // URL is the endpoint for an HTTP MCP server.
    // +optional
    URL string `json:"url,omitempty"`

    // Transport selects the HTTP transport type ("streamable-http" or "sse").
    // Only valid when url is set.
    // +optional
    Transport string `json:"transport,omitempty"`

    // Env are plain environment variables passed to the stdio server process
    // and written into the MCP server config in operator.json.
    // Use for non-secret values and tier-2 placeholder tokens.
    // +optional
    Env map[string]string `json:"env,omitempty"`

    // EnvFrom are secret-backed environment variables mounted on the gateway
    // container and inherited by the stdio server subprocess (tier 3).
    // Use only when the proxy-placeholder pattern (tier 2) is not viable.
    // +optional
    EnvFrom []McpEnvFromSecret `json:"envFrom,omitempty"`
}

// McpEnvFromSecret maps a Kubernetes Secret key to an environment variable.
type McpEnvFromSecret struct {
    // Name is the environment variable name.
    // +kubebuilder:validation:MinLength=1
    Name string `json:"name"`

    // SecretRef references a key in a Kubernetes Secret.
    SecretRef SecretRefEntry `json:"secretRef"`
}
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

New `injectMcpServersIntoConfigMap()` function (following the channels/providers pattern):

1. For each `McpServerSpec`, builds the server config object:
   - Stdio: `{ "command": ..., "args": ..., "env": { ... } }` — `env` includes plain values from `env` field. For `envFrom` entries, the env var name is included in the config with a placeholder value (the real value comes from the container environment at runtime).
   - HTTP: `{ "url": ..., "transport": ... }`
2. Sets `config["mcp"]["servers"][name] = serverConfig`
3. Marshals back into `operator.json`

## Gateway Deployment Modification

New `configureGatewayForMcpServers()` function in `claw_deployment.go`, called from `configureDeployments()` alongside the existing Vertex/Kubernetes gateway modifications:

- For each `McpServerSpec` with `envFrom` entries, adds env vars to the gateway container:
  ```json
  {
    "name": "GITHUB_PERSONAL_ACCESS_TOKEN",
    "valueFrom": {
      "secretKeyRef": {
        "name": "github-pat-secret",
        "key": "token"
      }
    }
  }
  ```

New `stampMcpSecretVersionAnnotation()` — separate from the existing `stampSecretVersionAnnotation` which targets the **proxy** deployment. MCP secrets are on the **gateway** deployment, so this function stamps the gateway pod template with MCP secret `ResourceVersion`s to trigger rollouts when secrets change.

## Proxy Configuration

HTTP MCP URL domain auto-extraction happens inside `generateProxyConfig()` (in `claw_proxy.go`), which runs during `applyProxyResources()` — before ConfigMap injection. The function receives the Claw instance's `McpServers` map (or a pre-extracted list of domains) and:

1. Parses each HTTP MCP URL to extract the hostname
2. Checks if the domain is already covered by a `credentials` entry or builtin passthrough
3. If not covered, adds a `type: none` passthrough route to the proxy config

This ensures unauthenticated HTTP MCP servers "just work" without requiring a separate `credentials` entry.

## Status Condition

New `McpServersConfigured` condition type:

- `True` — all MCP server secrets validated and config injected
- `False` — secret validation failed (with descriptive message)
- Not set when `spec.mcpServers` is empty

Failures also set `Ready=False`.

## Implementation Plan

Each phase corresponds to a single PR. Phases must be merged in order.

### Phase 1: HTTP/SSE and basic stdio MCP support (tiers 1 & 2) - DONE

Delivers working MCP for HTTP servers (auto-allowlisted) and stdio servers with static/placeholder env vars. No `envFrom` field yet — the CRD only advertises capabilities the reconciler supports.

1. Add `McpServerSpec` type to `api/v1alpha1/claw_types.go` (**without** `EnvFrom` field)
2. Add `McpServers map[string]McpServerSpec` field to `ClawSpec`
3. Add `ConditionTypeMcpServersConfigured` constant
4. Add CEL validation rules (`command` xor `url`)
5. Run `make manifests && make generate`
6. Add `injectMcpServersIntoConfigMap()` in new file `internal/controller/claw_mcp.go`
7. Add HTTP MCP URL domain extraction in `generateProxyConfig()` (in `claw_proxy.go`)
8. Wire `injectMcpServersIntoConfigMap` into `enrichConfigAndNetworkPolicy()`
9. Set `McpServersConfigured` condition (`True` on success, not set when empty)
10. Add unit tests in `claw_mcp_test.go` and `claw_proxy_test.go`

### Phase 2: Stdio MCP secret injection (tier 3 — escape hatch)

Adds the `envFrom` field and reconciler logic to mount real secrets into the gateway container for stdio MCP servers that manage their own credentials.

1. Add `McpEnvFromSecret` type and `EnvFrom []McpEnvFromSecret` field to `McpServerSpec`
2. Add CEL validation for `envFrom` entries
3. Run `make manifests && make generate`
4. Add MCP `envFrom` secret validation in `resolveAndApplyCredentials()` (or adjacent)
5. Add `configureGatewayForMcpServers()` in `claw_deployment.go`, called from `configureDeployments()`
6. Add `stampMcpSecretVersionAnnotation()` targeting the gateway deployment
7. Enhance `McpServersConfigured` condition to report secret validation failures
8. Add unit tests

### Phase 3: MCP documentation

Required part of the feature — not follow-up work.

1. Update PLATFORM.md with comprehensive MCP section (three-tier model, worked examples for well-known MCP servers, guidance on which tier to recommend)
2. Add MCP section to `docs/provider-setup.md` (step-by-step setup for all three tiers)

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
