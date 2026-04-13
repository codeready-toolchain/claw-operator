## Context

The OpenClaw operator currently sets the `status.url` field to the Route HTTPS endpoint (e.g., `https://openclaw-route.example.com`). The OpenClaw web UI requires a gateway token for authentication, stored in the `openclaw-gateway-token` Secret under the `token` key. Users must manually retrieve this token via `kubectl get secret openclaw-gateway-token -o jsonpath='{.data.token}' | base64 -d` and paste it into the UI.

The controller already generates and manages the gateway token in the `applyGatewaySecret()` method during reconciliation. The token is a cryptographically secure 64-character hex string generated using `crypto/rand`.

## Goals / Non-Goals

**Goals:**
- Append the gateway token as a URL fragment (`#token=<value>`) to the status URL
- Read the token from the `openclaw-gateway-token` Secret during status updates
- Maintain backward compatibility with the existing status URL structure (scheme + host)
- Ensure the token is Base64-decoded from the Secret data before appending

**Non-Goals:**
- Changing the token generation logic or Secret structure
- Modifying how the OpenClaw UI consumes the token fragment
- Adding encryption or additional security layers to the token transmission
- Changing the status URL for vanilla Kubernetes (localhost) deployments

## Decisions

### Decision 1: Append token as URL fragment (not query parameter)
**Rationale:** URL fragments (`#token=`) are not sent to the server in HTTP requests, reducing exposure in server logs. The OpenClaw UI already expects client-side processing of authentication tokens.

**Alternatives considered:**
- Query parameter (`?token=`): Rejected because tokens would appear in server access logs and HTTP referrer headers
- Custom header: Not applicable since the URL is displayed to users, not sent programmatically

### Decision 2: Fetch token during status update
**Rationale:** The status update already happens in the `updateStatus()` method after all resources are applied, so the `openclaw-gateway-token` Secret is guaranteed to exist. This avoids duplicating Secret fetching logic.

**Alternatives considered:**
- Cache token during `applyGatewaySecret()`: Rejected because it requires passing state between reconciliation phases and could become stale
- Read token once at reconciliation start: Rejected because status update happens at the end and token might not exist yet on first reconciliation

### Decision 3: Base64-decode Secret data before appending
**Rationale:** Kubernetes Secret data is Base64-encoded. The UI expects the raw token string, not the encoded version.

**Alternatives considered:**
- Let UI decode: Rejected because the status URL should be ready-to-use without client-side processing

## Risks / Trade-offs

**Risk:** Secret read failure during status update
- **Mitigation:** Log error and continue with status update without token fragment. The Route URL alone is still functional (user can manually enter token).

**Risk:** Token exposure in kubectl output and logs
- **Mitigation:** This is acceptable because (1) Secret data is already accessible via kubectl, (2) URL fragments don't transmit over HTTP, (3) the status field requires appropriate RBAC to read.

**Trade-off:** Additional Secret read on every reconciliation
- **Impact:** Minimal - Secret reads are cheap and reconciliation already performs multiple API calls. The Secret is small (<1KB) and already exists in-cluster.
