# Kubernetes Credential Type — Design Questions

**Status:** Final — all decisions resolved
**Related:** [Design document](kubernetes-credential-design.md)

Each question has options with trade-offs and a recommendation. Go through them one by one to form the design, then update the design document.

---

## Q1: Should the operator mint tokens via TokenRequest, or only accept pre-provisioned Secrets?

The `kubernetes` credential needs a bearer token for the API server. The question is who creates this token: the operator (actively minting short-lived tokens) or the platform/user (providing a Secret that already contains one).

### Option A: User-provided Secret only

The operator treats the `kubernetes` type like `bearer` — it mounts a Secret the user (or platform) provides. The operator does not call the TokenRequest API, does not manage ServiceAccounts, and does not refresh tokens. If the platform wants short-lived tokens, it manages the lifecycle externally (e.g., a sidecar, a separate controller, or projected volumes).

- **Pro:** Simplest implementation — no new RBAC for the operator, no token refresh logic, no cross-namespace concerns
- **Pro:** Platform-agnostic — works the same whether the deployer is Dev Sandbox, a bare OpenShift cluster, or vanilla Kubernetes
- **Pro:** Consistent with the operator's role: it mounts and injects, it doesn't provision
- **Con:** Long-lived tokens if the platform doesn't actively rotate them
- **Con:** No built-in token refresh — if the token expires, the proxy starts returning 401s until the Secret is updated externally

**Decision:** Option A — the operator mounts and injects, it doesn't provision. Token lifecycle is the platform's concern. If automatic refresh is needed later, it can be added as a backward-compatible enhancement without breaking the Secret-based path.

_Considered and rejected: Option B — Operator-managed TokenRequest (adds cross-namespace RBAC, token lifecycle complexity, blurs operator's role boundary). Option C — Hybrid with optional SA reference (two code paths, CRD complexity for no immediate benefit)._

---

## Q2: What is the credential input format — raw token or kubeconfig?

The fundamental question: does the user provide a single bearer token (like other credential types), or a standard kubeconfig file that may contain multiple clusters, contexts, and namespaces?

### Option B: Kubeconfig file as credential input

The user provides a standard kubeconfig file in a Secret. The operator parses it and derives everything it needs:

```yaml
- name: k8s-workspace
  type: kubernetes
  secretRef:
    name: my-kubeconfig
    key: kubeconfig           # a standard kubeconfig YAML file
```

The operator:
1. **Parses the kubeconfig** using `client-go/tools/clientcmd` to extract cluster server URLs, contexts, and default namespaces
2. **Generates proxy routes** — one per cluster/server, each configured for credential injection matching that server's hostname
3. **Generates a sanitized kubeconfig** — same clusters, contexts, namespaces, but all auth credentials (tokens, client certs, client keys) replaced with dummies. Mounted into the gateway pod via `KUBECONFIG` env var
4. **Mounts the original kubeconfig** into the proxy pod. The proxy's `kubernetes` injector parses it, matches each request's target API server to the right user/context, and injects the real credential

Multi-cluster kubeconfig example:

```yaml
apiVersion: v1
kind: Config
clusters:
  - name: dev-cluster
    cluster:
      server: https://kubernetes.default.svc
      certificate-authority-data: ...
  - name: staging-cluster
    cluster:
      server: https://api.staging.example.com:6443
      certificate-authority-data: ...
contexts:
  - name: dev
    context:
      cluster: dev-cluster
      user: dev-sa
      namespace: alice-dev
  - name: staging
    context:
      cluster: staging-cluster
      user: staging-sa
      namespace: staging-apps
current-context: dev
users:
  - name: dev-sa
    user:
      token: <real-token-1>
  - name: staging-sa
    user:
      token: <real-token-2>
```

From this, the operator generates:
- Proxy route for `kubernetes.default.svc` → inject `<real-token-1>`
- Proxy route for `api.staging.example.com` → inject `<real-token-2>`
- Sanitized kubeconfig with dummy tokens, mounted as KUBECONFIG in the gateway pod
- kubectl/client-go in the gateway just works — HTTPS goes through the proxy, which injects real tokens transparently

- **Pro:** Kubeconfig is the standard Kubernetes credential format — users and platforms already produce them
- **Pro:** Multi-cluster for free — one credential entry, multiple clusters
- **Pro:** Namespace context built-in — kubeconfig contexts carry default namespaces
- **Pro:** kubectl/client-go/any HTTPS client just works with `KUBECONFIG` env var
- **Pro:** The proxy doesn't care about internal vs external — same MITM mechanism for all clusters
- **Pro:** No separate `KubernetesConfig` struct needed — the kubeconfig IS the config
- **Con:** Operator must parse kubeconfig (but `client-go/tools/clientcmd` is the standard, battle-tested library)
- **Con:** Operator reads Secret contents to parse structure and generate sanitized version (but doesn't copy credentials to other Secrets — it generates a redacted version)
- **Con:** Auth method diversity — kubeconfigs can contain tokens, client certs, exec-based auth, OIDC. Not all can be proxied. Needs clear scoping for v1

**Decision:** Option B — kubeconfig as the credential input. The kubeconfig is the natural Kubernetes credential format and gives the operator everything it needs in one file: server URLs (→ proxy routes), auth credentials (→ proxy injection), contexts and namespaces (→ assistant configuration). Multi-cluster, external clusters, and namespace awareness all come for free. The operator parses using `client-go/tools/clientcmd`, generates a sanitized copy for the gateway, and mounts the original into the proxy.

_Considered and rejected: Option A — raw token + explicit fields (single cluster only, manual token extraction, redundant with kubeconfig info, no multi-cluster support)._

---

## Q3: Which kubeconfig auth methods should the proxy support?

Kubeconfig `users` entries can carry different auth mechanisms. The proxy needs to understand them for credential injection.

### Option A: Token-only (v1)

Support only `user.token` fields. The proxy injects `Authorization: Bearer <token>`. This covers ServiceAccount tokens (the primary use case) and any pre-minted OIDC/JWT tokens stored in the kubeconfig.

- **Pro:** Simplest implementation — just read the token string and inject as Bearer
- **Pro:** Covers the primary use case (SA tokens from TokenRequest or static Secrets)
- **Pro:** Consistent with the existing bearer injection pattern
- **Con:** No support for client certificate auth (`user.client-certificate-data` / `user.client-key-data`)
- **Con:** No support for exec-based auth (`user.exec`)

**Decision:** Option A — token-only for v1. The operator validates at reconciliation time that all kubeconfig users use token auth and rejects kubeconfigs with unsupported auth methods (clear error message). Client certificate support can be added in a future version if demand warrants it.

_Considered and rejected: Option B — token + client certificates (mTLS is architecturally different from header injection, significantly more complex). Option C — token + certs + exec (running arbitrary commands in the proxy pod is a security concern, exec plugins require CLIs not in the proxy image, fundamentally incompatible with the proxy model)._

---

## Q4: Should the proxy egress NetworkPolicy be updated for non-443 ports?

The proxy's egress currently allows port 443 to `0.0.0.0/0`. In-cluster API servers (`kubernetes.default.svc`) use 443, but external API servers commonly use port 6443.

### Option B: Dynamically add ports parsed from the kubeconfig

The operator parses the kubeconfig server URLs, extracts unique ports, and patches the proxy egress NetworkPolicy to include them.

- **Pro:** External clusters on non-443 ports work automatically
- **Pro:** The operator already knows the ports from parsing the kubeconfig
- **Con:** Dynamic NetworkPolicy patching adds complexity
- **Con:** The embedded NP manifest would need to be parameterized or patched in-memory (similar to how ConfigMap gets `OPENCLAW_ROUTE_HOST` injected)

**Decision:** Option B — dynamically add ports from the kubeconfig. The operator already parses the kubeconfig to extract server URLs. Extracting ports and patching the NetworkPolicy is a small incremental step. Any kubeconfig the user provides will work without manual NetworkPolicy tweaks. The implementation follows the same pattern as `injectRouteHostIntoConfigMap`: modify the parsed kustomize objects in-memory before applying via SSA.

_Considered and rejected: Option A — no change (poor UX for self-managed clusters on 6443, traffic silently blocked at L4). Option C — static 6443 (doesn't cover unusual ports, opens egress even when no external cluster configured)._

---

## Q5: How should the proxy read and cache the kubeconfig?

The proxy receives the original kubeconfig as a volume-mounted file. It needs to parse it and extract the right credentials for each request's target API server.

### Option A: Parse once on startup, restart on change

Parse the kubeconfig once at proxy startup. Build an in-memory map of server hostname → auth credentials. Token rotation is handled by `stampSecretVersionAnnotation`, which triggers a rolling pod restart when the kubeconfig Secret changes — the new pod re-reads the updated file on startup.

- **Pro:** Simplest — no file watching, no mutex, no periodic stat-check
- **Pro:** Consistent with all other credential types (restart-on-Secret-change)
- **Pro:** Fast — no parsing on the hot path
- **Con:** Token rotation causes a brief proxy restart (seconds) — acceptable since proxy must handle rolling updates gracefully anyway

**Decision:** Option A — parse once on startup, rely on `stampSecretVersionAnnotation` for restarts on token rotation. Simplest approach — no file watching, no mutex, consistent with all other credential types.

_Considered and rejected: Option B — parse on every request (unnecessary YAML parsing overhead for repeated requests). Option C — fixed-interval re-read (adds file-watching complexity for marginal benefit when restart-on-change already works)._

---

## Q6: How should the sanitized kubeconfig be delivered to the gateway pod?

The operator generates a sanitized kubeconfig (same clusters/contexts/namespaces, dummy credentials). The gateway pod needs this as a file at a path referenced by `KUBECONFIG`.

### Option A: Operator-managed ConfigMap

The operator creates a ConfigMap (e.g., `claw-kube-config`) containing the sanitized kubeconfig YAML. The gateway deployment mounts it and sets `KUBECONFIG` to the mount path.

- **Pro:** ConfigMap is the right primitive for non-secret configuration data
- **Pro:** The sanitized kubeconfig contains no real credentials — it's genuinely non-secret
- **Pro:** Consistent with how `claw-config` (openclaw.json) is already delivered
- **Con:** Another resource for the operator to manage (but it already manages many ConfigMaps)

**Decision:** Option A — dedicated ConfigMap. The sanitized kubeconfig contains no real credentials, just cluster URLs, contexts, namespaces, and dummy tokens. ConfigMap is the right primitive. The operator already manages multiple ConfigMaps; one more is trivial.

_Considered and rejected: Option B — embed in existing `claw-config` (mixes concerns, complicates init container). Option C — operator-managed Secret (over-classification — cluster CAs are not secret)._

---

## Q7: Should the operator update AGENTS.md with Kubernetes context?

The AGENTS.md file is a bootstrap system prompt for the AI assistant. When a `kubernetes` credential is configured, should the operator inject Kubernetes-specific context?

### Option A: Inject Kubernetes section into AGENTS.md

The operator appends a Kubernetes section to AGENTS.md when a `kubernetes` credential is present:

```markdown
## Kubernetes Access

You have access to Kubernetes clusters via `kubectl`. Your KUBECONFIG is
pre-configured. Available contexts:
- `dev` (cluster: kubernetes.default.svc, namespace: alice-dev) [current]
- `staging` (cluster: api.staging.example.com, namespace: staging-apps)

Use `kubectl` commands to manage resources. The proxy handles authentication
transparently — do not attempt to manage tokens or kubeconfig yourself.
```

- **Pro:** The assistant knows about its Kubernetes capability without the user manually editing AGENTS.md
- **Pro:** Context-specific — lists actual clusters, namespaces, and the current context from the parsed kubeconfig
- **Pro:** Can include guardrails ("do not attempt to manage tokens")
- **Con:** Operator modifies AGENTS.md content — potentially conflicts with user customizations
- **Con:** More parsing output to template

**Decision:** Option A — inject a Kubernetes section into AGENTS.md. The operator already generates AGENTS.md from the embedded ConfigMap template. The injection is additive — appended after the base prompt with actual clusters, namespaces, and the current context parsed from the kubeconfig.

_Considered and rejected: Option B — no AGENTS.md modification (assistant won't proactively know about Kubernetes capability). Option C — separate skill file (adds dependency on OpenClaw's skill discovery, unnecessary complexity for v1)._
