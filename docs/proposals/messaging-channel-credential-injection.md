# Messaging Channel Credential Injection

**Status:** TODO

## Problem

Messaging channels (Telegram, Discord, Slack) use API tokens that are currently managed by OpenClaw itself — stored in its config and included in outgoing requests. The proxy only allowlists domains (`type: none`) but doesn't manage the credentials, breaking our security model where real secrets should stay on the proxy pod.

## How OpenClaw uses messaging tokens

- **Telegram**: Bot token embedded in URL path (`/bot<TOKEN>/sendMessage`)
- **Discord**: Bot token sent as `Authorization: Bot <TOKEN>` header
- **Slack**: Bot token as `Authorization: Bearer <TOKEN>` header; app token validated in-process (must start with `xapp-`)

## How our proxy could handle them

- **Telegram**: `pathToken` type — proxy prepends `/bot<TOKEN>` to the request path
- **Discord**: `apiKey` type with `header: Authorization`, `valuePrefix: "Bot "` — proxy strips existing auth header and injects real one
- **Slack**: `bearer` type — proxy strips existing auth header and injects real one

## The gap

Our proxy's credential injection works by **stripping and replacing** auth headers (or prepending path tokens). But OpenClaw **also** constructs the full authenticated request. For this to work, OpenClaw needs to either:

1. Send requests **without** credentials and let the proxy add them, or
2. Send requests with **placeholder** credentials that the proxy recognizes and replaces

Option 1 would require OpenClaw changes. Option 2 is what NemoClaw/OpenShell does — they use `openshell:resolve:env:*` placeholder strings that the proxy scans for and rewrites.

Our proxy currently strips all auth headers unconditionally before injecting, so option 2 already works for **header-based** credentials (Discord, Slack) — OpenClaw sends a dummy token, proxy strips it, injects the real one. The open question is whether OpenClaw validates tokens locally before making network calls (Slack's Bolt library does this — it rejects tokens that don't start with `xapp-`).

For **path-based** credentials (Telegram), our `pathToken` injector **prepends** a prefix — it doesn't scan for and replace a placeholder in an existing path. This needs proxy changes.

## Tasks

- [ ] Test Discord with `apiKey` type: configure OpenClaw with a dummy bot token, verify proxy strips and replaces it
- [ ] Test Slack with `bearer` type: same approach, check if Bolt's in-process validation blocks placeholder tokens
- [ ] Extend proxy's `pathToken` injector to support replacement mode (scan for placeholder in path, replace with real token) for Telegram
- [ ] Add Telegram, Discord, Slack sections to the proxy setup skill and `docs/provider-setup.md`
- [ ] Add integration tests for each channel

## References

- NemoClaw placeholder model: `NemoClaw/agents/hermes/generate-config.ts` — uses `openshell:resolve:env:*`
- NemoClaw Slack workaround: `NemoClaw/scripts/nemoclaw-start.sh` — resolves Slack placeholders at startup before Bolt validates
- OpenShell credential rewriting: `OpenShell/architecture/sandbox-providers.md` — header, query param, and path segment rewriting
- Our pathToken injector: `internal/proxy/injector_pathtoken.go`
