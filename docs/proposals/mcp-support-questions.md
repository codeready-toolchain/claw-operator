# MCP Support — Design Questions

**Status:** Open — decisions pending
**Related:** [Design document](mcp-support-design.md)

Each question has options with trade-offs and a recommendation. Go through them one by one to form the design, then update the design document.

## Q1: How should stdio MCP server secrets be handled?

Stdio MCP servers run as subprocesses of the gateway. They inherit the gateway process environment. There is no HTTP layer to intercept — the proxy cannot inject secrets for these. The fundamental question: do we accept putting secrets on the gateway container for stdio MCP, or do we limit support to avoid it?

### Option A: Gateway env vars via `secretKeyRef` (accept the exception)

Secrets are mounted as env vars on the gateway container, referenced from Kubernetes Secrets. The MCP server config in `operator.json` uses placeholder values; the real values come from the process environment, which the subprocess inherits.

- **Pro:** Full stdio MCP support — works with all MCP servers (GitHub, filesystem, database, etc.)
- **Pro:** Follows the same pattern as `OPENCLAW_GATEWAY_TOKEN` (already a `secretKeyRef` on the gateway)
- **Pro:** Matches how the upstream operator and installer handle MCP secrets
- **Con:** Breaks the "no real secrets on the gateway" principle for these specific env vars
- **Con:** A compromised gateway process can read these env vars

### Option B: No secret support for stdio MCP — only plain env

Only allow plain-text `env` values (non-secret). Users who need secrets must manually configure them via `openclaw config patch` inside the pod or use HTTP MCP servers instead.

- **Pro:** Maintains strict secret isolation
- **Con:** Most useful stdio MCP servers need secrets (GitHub PAT, database passwords)
- **Con:** Poor UX — user has to manually inject secrets after every pod restart
- **Con:** The upstream operator doesn't have this limitation

### Option C: Sidecar proxy for stdio MCP servers

Run stdio MCP servers in a separate sidecar container with secrets, communicating with the gateway over a local socket or HTTP bridge.

- **Pro:** Secrets stay off the gateway container
- **Con:** Massive complexity — MCP stdio protocol uses stdin/stdout pipes, not sockets
- **Con:** Would require a custom bridge component that doesn't exist
- **Con:** Fundamentally changes the MCP execution model

**Recommendation:** Option A. The gateway already has `secretKeyRef`-based secrets (`OPENCLAW_GATEWAY_TOKEN`, proxy CA). Stdio MCP is architecturally incapable of proxy-based injection. This is the documented "unless it's the only way" exception. The blast radius is limited: these env vars are only visible to the gateway process and its children, not externally accessible.

## Q2: Should HTTP MCP auth go through the proxy or be config-injected?

HTTP MCP servers (`url`-based, using `streamable-http` or `sse` transport) can have auth headers. The question is whether those headers should be injected by the MITM proxy (keeping secrets off the gateway) or written into the MCP config in `operator.json`.

### Option A: Proxy-injected headers (reuse credential system)

User adds both an MCP server entry (for config) and a `credentials` entry (for auth). The credential's `domain` matches the MCP URL's host, and the proxy injects auth headers.

- **Pro:** Consistent with the operator's security model — real secrets stay on the proxy
- **Pro:** Reuses existing credential infrastructure (no new secret plumbing)
- **Pro:** HTTP MCP servers behave like any other authenticated HTTP endpoint
- **Con:** User must configure two things (MCP server + credential) for one service
- **Con:** Slightly more complex UX for simple cases

### Option B: Config-injected headers (secrets in `operator.json`)

Auth headers are written directly into the MCP server's `headers` field in `operator.json`, sourced from Kubernetes Secrets.

- **Pro:** Single configuration point — everything about the MCP server is in one place
- **Con:** Secrets end up in `operator.json` on the ConfigMap, readable by the gateway
- **Con:** Breaks the proxy-based secret isolation for HTTP traffic (where it IS possible)
- **Con:** ConfigMap data is not encrypted at rest (unlike Secrets)

### Option C: Hybrid — proxy for auth, config for non-secret headers

MCP server config has non-secret headers (like `Content-Type`). Auth goes through a credential entry. The operator documents the pattern.

- **Pro:** Best of both worlds — secrets stay secure, non-secret config stays simple
- **Con:** Same two-thing UX concern as Option A, though documented patterns mitigate this

**Recommendation:** Option A. HTTP traffic already goes through the MITM proxy. The proxy can handle auth injection for MCP URLs exactly like it does for LLM providers. Users already know the credential pattern. The PLATFORM.md skill can guide Claw to help users set up both pieces together.

## Q3: How should proxy domain allowlisting work for MCP servers?

MCP servers need network access. Stdio MCP servers make outbound HTTP calls (through the proxy). HTTP MCP servers connect to their URL (through the proxy). Domains must be in the proxy allowlist.

### Option A: Automatic from URL + explicit `allowedDomains`

For HTTP MCP servers, auto-extract the domain from `url` and add it as a passthrough route. For stdio MCP servers (and any extra domains HTTP servers need), use an `allowedDomains` field on the MCP spec.

- **Pro:** HTTP MCP "just works" — URL implies the domain
- **Pro:** `allowedDomains` covers stdio MCP servers that need specific endpoints
- **Con:** Auto-extraction adds implicit behavior (domain allowlisted without explicit credential entry)
- **Con:** `allowedDomains` is a new concept alongside `credentials`

### Option B: Manual via existing `credentials` entries

Users add `type: none` credential entries for any domains MCP servers need. No new fields on the MCP spec.

- **Pro:** Single mechanism for all domain allowlisting (existing pattern)
- **Pro:** No new API surface
- **Con:** Verbose — user must add separate credential entries for every MCP domain
- **Con:** Poor discoverability — not obvious that MCP needs credential entries for domains

### Option C: Automatic from URL only (no `allowedDomains` field)

Auto-extract domain from HTTP MCP URLs. Stdio MCP server domains must be allowlisted via `credentials` entries.

- **Pro:** Simpler API — no `allowedDomains` field
- **Pro:** HTTP MCP works automatically
- **Con:** Stdio MCP servers that need network access require separate credential entries
- **Con:** Inconsistent — HTTP auto-allowlisted, stdio not

**Recommendation:** Option C. Keep it simple for Phase 1. HTTP MCP URLs naturally imply a domain — auto-extracting it is pragmatic and low-risk (it's a `type: none` passthrough, not credential injection). Stdio MCP server domains are less predictable (they depend on what the subprocess does) and are better handled explicitly via `credentials`. This avoids a new `allowedDomains` concept. We can always add it later if the UX is painful.

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
- **Con:** Adds a new top-level spec field (API surface growth)

### Option B: Extend `CredentialSpec` with an `mcp` field

Add MCP config as another variant of `CredentialSpec`, similar to how `channel` works:

```yaml
spec:
  credentials:
    - name: github-mcp
      type: none
      mcp:
        command: npx
        args: ["-y", "@modelcontextprotocol/server-github"]
```

- **Pro:** Reuses existing credential infrastructure
- **Pro:** Secret handling via `secretRef` is already built in
- **Con:** `CredentialSpec` is already complex (7 types, channels, providers). MCP would overload it further
- **Con:** MCP servers don't fit the credential model — they're not about injecting auth on a domain
- **Con:** Forces MCP into credential validation logic that doesn't apply

### Option C: Raw config pass-through (`spec.config.raw`)

Follow the upstream operator pattern — users pass raw MCP JSON through an opaque config field:

```yaml
spec:
  config:
    raw:
      mcp:
        servers:
          github: { command: npx, args: [...] }
```

- **Pro:** Zero new types — just pass through to `openclaw.json`
- **Pro:** Matches upstream operator pattern exactly
- **Con:** No structured validation, no secret integration
- **Con:** Secrets would need to be plain text in the config (security problem)
- **Con:** No proxy domain auto-allowlisting
- **Con:** Doesn't leverage operator capabilities

**Recommendation:** Option A. MCP servers are conceptually distinct from credentials and channels. A dedicated `spec.mcpServers` map gives clean separation, structured validation, and room for proper secret handling. It's also the most natural mapping to OpenClaw's `mcp.servers` config shape.

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
- **Con:** Two fields for related concept

### Option B: Unified `env` with inline secret references

```yaml
mcpServers:
  github:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      LOG_LEVEL: debug
      GITHUB_PERSONAL_ACCESS_TOKEN:
        secretRef:
          name: github-pat-secret
          key: token
```

- **Pro:** Single field, mixed plain and secret values
- **Con:** Polymorphic value type (string | object) is ugly in Go CRD types and CEL validation
- **Con:** CRD schema can't easily express "value is either a string or an object with secretRef"
- **Con:** Harder to distinguish which env vars need container-level `secretKeyRef` mounting

**Recommendation:** Option A. The Kubernetes naming convention is well-understood. The Go type system handles it cleanly. The separation makes the reconciler logic straightforward: `env` goes to ConfigMap, `envFrom` goes to both ConfigMap (as placeholder) and Deployment (as `secretKeyRef`).

## Q6: Should the operator validate MCP server configurations?

The operator could validate MCP server configs (e.g., stdio must have `command`, HTTP must have `url`, not both). Or it could pass them through and let OpenClaw validate at runtime.

### Option A: Validate with CEL rules on the CRD

Add `XValidation` rules like: "command and url are mutually exclusive", "transport requires url", etc.

- **Pro:** Fast feedback — `oc apply` fails immediately with a clear message
- **Pro:** Consistent with how `CredentialSpec` validates (CEL rules on type/provider/channel)
- **Con:** More CEL rules to maintain
- **Con:** If OpenClaw adds new transport types, CRD validation could reject valid configs

### Option B: Minimal validation only

Only validate what the operator needs to function (e.g., `envFrom` secretRef exists). Let OpenClaw handle the rest.

- **Pro:** Forward-compatible — new MCP features in OpenClaw don't require CRD updates
- **Pro:** Less maintenance
- **Con:** Misconfigurations surface at runtime, not apply time

**Recommendation:** Option A. CEL validation is the project convention. Basic structural rules (stdio xor HTTP, required fields) catch common mistakes early. The rules can be relaxed later if needed; tightening is harder.

## Q7: How should the status condition work for MCP?

The operator uses conditions to report state: `CredentialsResolved`, `ProxyConfigured`, `Ready`. Should MCP have its own condition?

### Option A: New `McpServersConfigured` condition

Separate condition, set to `True` when all MCP server secrets are validated and config is injected.

- **Pro:** Clear signal for MCP-specific issues
- **Pro:** Follows existing pattern (`CredentialsResolved`, `ProxyConfigured`)
- **Con:** More conditions to track

### Option B: Fold into `CredentialsResolved`

MCP secret validation runs alongside credential validation; failures show on `CredentialsResolved`.

- **Pro:** Fewer conditions
- **Con:** Muddies the meaning of `CredentialsResolved` — MCP isn't a "credential"
- **Con:** Harder to diagnose MCP-specific issues

### Option C: No new condition — only `Ready`

MCP failures surface on the top-level `Ready` condition with a descriptive message.

- **Pro:** Simplest
- **Con:** Less granular debugging
- **Con:** `Ready=False` doesn't indicate whether the issue is MCP, credentials, or something else

**Recommendation:** Option A. A dedicated condition is cheap, follows the project pattern, and makes MCP issues immediately diagnosable. The operator already has 4 conditions — one more is fine.
