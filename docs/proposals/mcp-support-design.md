# MCP Server Support

**Status:** Draft — pending decisions from [mcp-support-questions.md](mcp-support-questions.md)

## Overview

Add operator-managed MCP (Model Context Protocol) server configuration to the Claw CRD. MCP servers extend the AI assistant's capabilities by providing structured tool access to external services (GitHub API, filesystems, databases, etc.). Today, users must manually configure MCP servers via `openclaw config patch` inside the pod — there is no declarative, CR-driven way to manage them.

This feature enables users to declare MCP servers in the Claw CR with proper secret management, and the operator injects the configuration into `operator.json` at reconciliation time.

## Design Principles

1. **Security first:** Real secrets must not reach the gateway container whenever possible. The MITM proxy pattern is the preferred path for HTTP-based MCP servers. For stdio MCP servers that require secrets as environment variables, the gateway container receives them directly — this is the "unless it's the only way" exception, documented explicitly.

2. **Declarative and reconcilable:** MCP server configuration is part of the Claw CR spec, reconciled into `operator.json` the same way providers and channels are today.

3. **Minimal API surface:** Follow existing patterns (channels, providers) rather than inventing new ones. Reuse `secretRef` for credentials.

4. **User config preserved:** In `merge` mode, operator-managed MCP servers merge alongside user-managed servers added via `openclaw config patch` or `openclaw mcp set`. Operator-managed servers win on collision (same server name).

## Architecture

### How MCP Servers Work in OpenClaw

OpenClaw supports two MCP transport types:

- **Stdio**: A child process spawned by the gateway (`command` + `args`). Environment variables are passed to the child process. This is the most common type for local tool servers.
- **HTTP** (`streamable-http` or `sse`): An outbound HTTP connection to a remote URL. Headers can be passed. No subprocess.

OpenClaw configuration path: `mcp.servers` in `openclaw.json`:

```json
{
  "mcp": {
    "servers": {
      "github": {
        "command": "npx",
        "args": ["-y", "@modelcontextprotocol/server-github"],
        "env": {
          "GITHUB_PERSONAL_ACCESS_TOKEN": "ghp_real_token"
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

```
Claw CR spec.mcpServers
         │
         ▼
  resolveAndApplyCredentials()  ◄── validate secrets exist (stdio env)
         │
         ▼
  enrichConfigAndNetworkPolicy()
         │
         ├─► injectMcpServersIntoConfigMap()  ──► operator.json { mcp.servers }
         │
         └─► proxy domain handling (HTTP MCP URLs)
         │
         ▼
  configureGatewayForMcpServers()  ──► env vars on gateway container (stdio secrets)
         │
         ▼
  merge.js (init-config)  ──► PVC openclaw.json (deep-merge preserves user MCP servers)
```

### Security Model

> **Open question:** How should stdio MCP server secrets be handled? — see [questions document](mcp-support-questions.md), Q1.

**HTTP MCP servers** with credentials in `headers`: Can be handled by the MITM proxy (inject headers on matching domain), keeping secrets off the gateway. The proxy already supports this pattern via `defaultHeaders` on credential routes.

**Stdio MCP servers** with `env` secrets: The subprocess inherits env from the gateway process. There is no proxy interception for env vars — they must be on the gateway container. This is architecturally unavoidable for stdio MCP.

> **Open question:** Should HTTP MCP server auth go through the proxy or be injected as config? — see [questions document](mcp-support-questions.md), Q2.

### Network Access

- **Stdio MCP servers** make their own outbound connections. Their traffic goes through `HTTP_PROXY`/`HTTPS_PROXY` (the MITM proxy). Domains they need must be in the proxy allowlist — either as builtin passthroughs, existing credentials, or new `type: none` domain entries.

- **HTTP MCP servers** connect to a `url`. The gateway's HTTP client goes through the proxy. The MCP URL's domain must be allowed.

> **Open question:** How should proxy domain allowlisting work for MCP servers? — see [questions document](mcp-support-questions.md), Q3.

## Core Concepts

### CRD Schema

> **Open question:** Where should MCP servers live in the CRD? — see [questions document](mcp-support-questions.md), Q4.

A new `spec.mcpServers` map on `ClawSpec`:

```go
type ClawSpec struct {
    ConfigMode  ConfigMode       `json:"configMode,omitempty"`
    Credentials []CredentialSpec `json:"credentials,omitempty"`
    McpServers  map[string]McpServerSpec `json:"mcpServers,omitempty"`
}
```

Each `McpServerSpec` supports both stdio and HTTP transports:

```go
type McpServerSpec struct {
    // Stdio transport
    Command string   `json:"command,omitempty"`
    Args    []string `json:"args,omitempty"`

    // HTTP transport
    URL       string `json:"url,omitempty"`
    Transport string `json:"transport,omitempty"` // "streamable-http" or "sse"

    // Env vars for stdio (plain values)
    Env map[string]string `json:"env,omitempty"`

    // Secret-backed env vars for stdio
    EnvFrom []McpEnvFromSecret `json:"envFrom,omitempty"`

    // Additional domains the MCP server needs (added to proxy allowlist)
    AllowedDomains []string `json:"allowedDomains,omitempty"`
}

type McpEnvFromSecret struct {
    Name      string `json:"name"`       // env var name
    SecretRef SecretRefEntry `json:"secretRef"` // k8s secret reference
}
```

> **Open question:** Should `env` and `envFrom` be separate fields or unified? — see [questions document](mcp-support-questions.md), Q5.

### ConfigMap Injection

A new `injectMcpServersIntoConfigMap()` function (following the channels/providers pattern) that:

1. Builds `mcp.servers` map from `spec.mcpServers`
2. For `envFrom` entries, resolves to placeholder values (the real secrets go on the gateway container, not in the ConfigMap)
3. Merges into `operator.json` under `mcp.servers`

### Gateway Deployment Modification

A new `configureGatewayForMcpServers()` function (following the Vertex/Kubernetes pattern in `claw_deployment.go`) that:

1. For each `McpServerSpec` with `envFrom`, adds env vars to the gateway container sourced from `secretKeyRef`
2. The env var name matches what's in the MCP server's `env` config so the subprocess inherits it

### Proxy Configuration

For HTTP MCP servers, the URL's domain must be in the proxy allowlist. Options:

- Automatic: parse the URL, add as a `type: none` passthrough route
- Manual: user adds a `credentials` entry for the domain

> **Open question:** Should this be automatic or manual? — see [questions document](mcp-support-questions.md), Q3.

## Implementation Plan

### Phase 1: CRD and ConfigMap Injection

1. Add `McpServerSpec` types to `api/v1alpha1/claw_types.go`
2. Run `make manifests && make generate`
3. Add `injectMcpServersIntoConfigMap()` in new file `internal/controller/claw_mcp.go`
4. Wire into `enrichConfigAndNetworkPolicy()` in `claw_resource_controller.go`
5. Add unit tests in `claw_mcp_test.go`

### Phase 2: Secret Handling (stdio env)

1. Add `configureGatewayForMcpServers()` in `claw_deployment.go`
2. Add secret validation in `resolveAndApplyCredentials()` (or new MCP validation step)
3. Stamp secret versions for MCP secrets (rollout on secret change)
4. Add unit tests

### Phase 3: Proxy Allowlisting (HTTP MCP)

1. Parse HTTP MCP URLs, generate proxy routes
2. Add companion domain support via `allowedDomains`
3. Unit tests for proxy config generation with MCP domains

### Phase 4: Documentation and Platform Skill

1. Update PLATFORM.md with MCP server guidance
2. Add MCP section to `docs/provider-setup.md`
3. Add `McpServersConfigured` status condition

## Open Questions

Summary of unresolved decisions — see [mcp-support-questions.md](mcp-support-questions.md) for details:

- **Q1:** How should stdio MCP server secrets be handled?
- **Q2:** Should HTTP MCP auth go through the proxy or be config-injected?
- **Q3:** How should proxy domain allowlisting work for MCP servers?
- **Q4:** Where should MCP config live in the CRD?
- **Q5:** Should `env` and `envFrom` be separate or unified?
- **Q6:** Should the operator validate MCP server configurations?
- **Q7:** How should the status condition work for MCP?
