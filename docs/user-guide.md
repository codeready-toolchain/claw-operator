# User Guide

This guide covers configuring Claw instances: LLM providers, external services, messaging channels, MCP servers, web search, web fetch, application configuration, workspace files, skills, and custom domains. Each section walks through creating the necessary Secrets and Claw CR configuration.

All examples assume you have set your target namespace:

```sh
export NS=my-claw-namespace
```

## LLM Providers

For known providers (`google`, `anthropic`, `openai`, `xai`), the operator automatically infers defaults where possible — you only need `name`, `type`, `secretRef`, and `provider`. For `google` and `anthropic`, the `domain` and `apiKey` header are fully inferred. For `openai` and `xai`, you must provide a `domain` explicitly since they use `type: bearer`. You can still override any inferred field if needed (e.g., routing through a custom proxy).

> **Adding credentials incrementally:** Each `oc apply` of the Claw CR **replaces** the entire `credentials` list. When adding a new provider, include all existing credentials in the YAML — otherwise they will be removed. You can retrieve your current configuration with `oc get claw instance -n $NS -o yaml` and add the new entry to the list.

### Google Gemini

Uses the Gemini REST API directly with an API key.

**1. Get an API key** from [Google AI Studio](https://aistudio.google.com/apikey).

**2. Create the Secret:**

```sh
oc create secret generic gemini-api-key \
  --from-literal=api-key=YOUR_GEMINI_API_KEY \
  -n $NS
```

**3. Apply the Claw CR:**

```sh
oc apply -n $NS -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: gemini
      type: apiKey
      secretRef:
        - name: gemini-api-key
          key: api-key
      provider: google
EOF
```

### Anthropic Claude

Uses the Anthropic API directly with an API key.

**1. Get an API key** from the [Anthropic Console](https://console.anthropic.com/settings/keys).

**2. Create the Secret:**

```sh
oc create secret generic anthropic-api-key \
  --from-literal=api-key=YOUR_ANTHROPIC_API_KEY \
  -n $NS
```

**3. Apply the Claw CR:**

```sh
oc apply -n $NS -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: anthropic
      type: apiKey
      secretRef:
        - name: anthropic-api-key
          key: api-key
      provider: anthropic
EOF
```

### OpenAI

Uses the OpenAI API with a bearer token.

**1. Get an API key** from the [OpenAI Platform](https://platform.openai.com/api-keys).

**2. Create the Secret:**

```sh
oc create secret generic openai-api-key \
  --from-literal=api-key=YOUR_OPENAI_API_KEY \
  -n $NS
```

**3. Apply the Claw CR:**

```sh
oc apply -n $NS -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: openai
      type: bearer
      secretRef:
        - name: openai-api-key
          key: api-key
      provider: openai
      domain: "api.openai.com"
EOF
```

### xAI (Grok)

Uses the xAI API with a bearer token.

**1. Get an API key** from the [xAI Console](https://console.x.ai/).

**2. Create the Secret:**

```sh
oc create secret generic xai-api-key \
  --from-literal=api-key=YOUR_XAI_API_KEY \
  -n $NS
```

**3. Apply the Claw CR:**

```sh
oc apply -n $NS -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: xai
      type: bearer
      secretRef:
        - name: xai-api-key
          key: api-key
      provider: xai
      domain: "api.x.ai"
EOF
```

### Vertex AI

Vertex AI lets you access multiple model providers (Anthropic, Google, Meta, and others) through a single GCP project using IAM-based authentication instead of per-provider API keys. The `domain` defaults to `.googleapis.com` for all `gcp` credentials.

#### Prerequisites

- A GCP project with the Vertex AI API enabled
- A GCP service account with the `Vertex AI User` role

#### Create a GCP Service Account and Secret

These steps are shared across all Vertex AI providers below.

**1. Create the service account and download the JSON key:**

```sh
gcloud iam service-accounts create claw-vertex \
  --display-name="Claw Vertex AI"
gcloud projects add-iam-policy-binding YOUR_PROJECT_ID \
  --member="serviceAccount:claw-vertex@YOUR_PROJECT_ID.iam.gserviceaccount.com" \
  --role="roles/aiplatform.user"
gcloud iam service-accounts keys create sa-key.json \
  --iam-account=claw-vertex@YOUR_PROJECT_ID.iam.gserviceaccount.com
```

**2. Create the Secret:**

```sh
oc create secret generic vertex-sa-key \
  --from-file=sa-key.json=sa-key.json \
  -n $NS
```

> **For testing with your personal account:** you can skip the service account setup and use Application Default Credentials instead:
>
> ```sh
> gcloud auth application-default login
> oc create secret generic vertex-sa-key \
>   --from-file=sa-key.json=$HOME/.config/gcloud/application_default_credentials.json \
>   -n $NS
> ```
>
> The Google Cloud libraries accept both `authorized_user` and `service_account` credential types.

#### Anthropic Claude via Vertex AI

Requires Anthropic Claude models enabled in your project's [Model Garden](https://console.cloud.google.com/vertex-ai/publishers/anthropic) and a region that supports them (e.g., `us-east5`, `europe-west1` — check [Anthropic's Vertex AI docs](https://docs.anthropic.com/en/docs/build-with-claude/vertex-ai) for the latest availability).

```sh
oc apply -n $NS -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: anthropic-vertex
      type: gcp
      secretRef:
        - name: vertex-sa-key
          key: sa-key.json
      gcp:
        project: "YOUR_PROJECT_ID"
        location: "us-east5"
      provider: anthropic
EOF
```

#### Google Gemini via Vertex AI

Useful when you need IAM-based access control or when API keys aren't available.

```sh
oc apply -n $NS -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: gemini
      type: gcp
      secretRef:
        - name: vertex-sa-key
          key: sa-key.json
      gcp:
        project: "YOUR_PROJECT_ID"
        location: "us-central1"
      provider: google
EOF
```

#### Combining Multiple Vertex AI Providers

You can use multiple providers in the same Claw instance with a single service account:

```sh
oc apply -n $NS -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: anthropic-vertex
      type: gcp
      secretRef:
        - name: vertex-sa-key
          key: sa-key.json
      gcp:
        project: "YOUR_PROJECT_ID"
        location: "us-east5"
      provider: anthropic
    - name: gemini
      type: gcp
      secretRef:
        - name: vertex-sa-key
          key: sa-key.json
      gcp:
        project: "YOUR_PROJECT_ID"
        location: "us-central1"
      provider: google
EOF
```

#### How Vertex AI Routing Works

The operator uses two different routing strategies depending on the provider:

**Google Gemini via Vertex AI** (`provider: google`, `type: gcp`): Uses a gateway proxy route that forwards requests through `https://{location}-aiplatform.googleapis.com/v1/projects/{project}/locations/{location}/publishers/google/...`.

**Non-Google providers via Vertex AI** (e.g., `provider: anthropic`, `type: gcp`): Uses OpenClaw's native Vertex SDK (e.g., `@anthropic-ai/vertex-sdk`). The operator:

1. Configures OpenClaw with the `anthropic-vertex` provider, which uses the native Vertex AI SDK to construct correct API URLs
2. Provides the OpenClaw pod with a **stub ADC** (Application Default Credentials) — a dummy credentials file with no real secrets
3. The MITM proxy transparently intercepts GCP auth traffic and injects real OAuth2 tokens from the service account

This ensures **real GCP credentials stay on the proxy pod only** — the application pod never sees them.

## Kubernetes API Access

The `kubernetes` credential type lets the AI assistant interact with Kubernetes API servers through the credential-injecting proxy. You provide a standard kubeconfig file in a Secret — the operator parses it to extract server URLs, contexts, namespaces, and tokens. The assistant gets a sanitized kubeconfig (real tokens replaced with placeholders) and all API requests are transparently authenticated by the proxy.

**Requirements:**
- The kubeconfig must use **token-based authentication** (static tokens or projected service account tokens). Client certificate, exec-based, and auth provider-based auth are not supported yet.
- Each cluster server URL must map to exactly one token. If the same cluster is referenced by multiple contexts with different users/tokens, split into separate kubeconfigs.

### Single Cluster

**1. Create a ServiceAccount with RBAC:**

```sh
oc create namespace my-workspace
oc create sa claw-assistant -n my-workspace
oc create rolebinding claw-assistant-edit \
  --clusterrole=edit \
  --serviceaccount=my-workspace:claw-assistant \
  -n my-workspace
```

**2. Build a kubeconfig from your current cluster:**

This extracts the server URL and CA from your existing kubeconfig, then creates a new one with the SA token — no need to find CA files manually.

```sh
# Get the API server URL and CA data from the current context
SERVER=$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}')
CA_DATA=$(kubectl config view --raw --minify -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')

# Request a token for the ServiceAccount
SA_TOKEN=$(oc create token claw-assistant -n my-workspace --duration=8760h)

# Build the kubeconfig
kubectl config set-cluster target \
  --server="$SERVER" \
  --kubeconfig=/tmp/kubeconfig
kubectl config set clusters.target.certificate-authority-data "$CA_DATA" \
  --kubeconfig=/tmp/kubeconfig
kubectl config set-credentials claw-sa \
  --token="$SA_TOKEN" \
  --kubeconfig=/tmp/kubeconfig
kubectl config set-context workspace \
  --cluster=target \
  --user=claw-sa \
  --namespace=my-workspace \
  --kubeconfig=/tmp/kubeconfig
kubectl config use-context workspace --kubeconfig=/tmp/kubeconfig
```

> **Tip:** If your cluster uses a CA file instead of inline `certificate-authority-data`, you can embed it:
> ```sh
> CA_FILE=$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.certificate-authority}')
> kubectl config set-cluster target \
>   --server="$SERVER" \
>   --certificate-authority="$CA_FILE" \
>   --embed-certs=true \
>   --kubeconfig=/tmp/kubeconfig
> ```

**3. Create the Secret:**

```sh
oc create secret generic my-kubeconfig \
  --from-file=kubeconfig=/tmp/kubeconfig \
  -n $NS
```

**4. Apply the Claw CR:**

```sh
oc apply -n $NS -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: k8s-workspace
      type: kubernetes
      secretRef:
        - name: my-kubeconfig
          key: kubeconfig
EOF
```

### Multi-Cluster

A single kubeconfig can contain multiple clusters. The operator creates a proxy route per cluster server and the assistant can switch contexts with `kubectl config use-context`.

```sh
oc apply -n $NS -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: k8s-multi
      type: kubernetes
      secretRef:
        - name: multi-cluster-kubeconfig
          key: kubeconfig
EOF
```

The operator automatically:
- Creates proxy routes for each cluster server `hostname:port`
- Patches the proxy egress NetworkPolicy to allow non-443 ports (e.g., 6443)
- Mounts a sanitized kubeconfig on the gateway pod (tokens replaced with placeholders)
- Injects a "Kubernetes Access" section into AGENTS.md listing available contexts and namespaces

### Combining with LLM Providers

Kubernetes credentials work alongside LLM provider credentials in the same Claw instance:

```sh
oc apply -n $NS -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: gemini
      type: apiKey
      secretRef:
        - name: gemini-api-key
          key: api-key
      provider: google
    - name: k8s-workspace
      type: kubernetes
      secretRef:
        - name: my-kubeconfig
          key: kubeconfig
EOF
```

### How Kubernetes Routing Works

The `kubernetes` credential uses the proxy's existing **MITM forward proxy mode** (CONNECT tunneling). The gateway pod's `HTTP_PROXY` / `HTTPS_PROXY` env vars route all traffic through the proxy, which:

1. Matches the request `hostname:port` against cluster servers from the kubeconfig
2. TLS-terminates via MITM
3. Strips all existing auth headers
4. Injects the real `Authorization: Bearer <token>` for the matched cluster
5. Re-encrypts and forwards to the upstream API server

The gateway pod **cannot** reach any API server directly — egress is restricted to the proxy by NetworkPolicy. The assistant never sees real tokens; only the sanitized kubeconfig with placeholder values.

## Messaging Channels

For known channels (`telegram`, `discord`, `slack`, `whatsapp`), the operator automatically infers all proxy configuration — domain, credential type, companion routes, and placeholder tokens. You only need `name`, `channel`, and `secretRef`. The operator also injects the channel's config into `operator.json` so OpenClaw starts with the channel pre-configured. No manual `openclaw channels add` is needed.

> **Adding credentials incrementally:** Each `oc apply` of the Claw CR **replaces** the entire `credentials` list. When adding a new channel, include all existing credentials in the YAML — otherwise they will be removed. You can retrieve your current configuration with `oc get claw instance -n $NS -o yaml` and add the new entry to the list.

### Telegram

Uses the Telegram Bot API with path-based token injection (`/bot<TOKEN>/method`).

**1. Create a bot** via [@BotFather](https://t.me/BotFather) and copy the bot token.

**2. Create the Secret:**

```sh
oc create secret generic telegram-bot-secret \
  --from-literal=token=YOUR_BOT_TOKEN \
  -n $NS
```

**3. Apply the Claw CR:**

```sh
oc apply -n $NS -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: telegram
      channel: telegram
      secretRef:
        - name: telegram-bot-secret
          key: token
EOF
```

The operator infers `type: pathToken`, `domain: api.telegram.org`, and `pathToken.prefix: /bot`. The proxy intercepts requests like `/botplaceholder/sendMessage` and forwards them as `/bot<REAL_TOKEN>/sendMessage`. The real token never reaches the gateway pod.

By default, the operator sets `dmPolicy: "open"` so anyone who knows the bot's username can message it. This means Telegram works immediately after setup — no pairing approval needed.

To restrict who can DM the bot, override with `channelConfig`:

```yaml
    - name: telegram
      channel: telegram
      secretRef:
        - name: telegram-bot-secret
          key: token
      channelConfig:
        dmPolicy: allowlist
        allowFrom: [12345, 67890]
```

Valid `dmPolicy` values: `open` (default — anyone can message), `allowlist` (only listed user IDs), `pairing` (manual approval via CLI), `disabled` (no DMs).

### Discord

Uses the Discord Bot API with `Authorization: Bot <TOKEN>` header injection. The operator automatically creates companion routes for Discord's WebSocket gateway (`gateway.discord.gg`) and CDN (`cdn.discordapp.com`).

**1. Create a bot** in the [Discord Developer Portal](https://discord.com/developers/applications) and copy the bot token.

**2. Create the Secret:**

```sh
oc create secret generic discord-bot-secret \
  --from-literal=token=YOUR_BOT_TOKEN \
  -n $NS
```

**3. Apply the Claw CR:**

```sh
oc apply -n $NS -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: discord
      channel: discord
      secretRef:
        - name: discord-bot-secret
          key: token
EOF
```

The operator infers `type: apiKey`, `domain: discord.com`, and the `Authorization` header with `Bot ` prefix. Companion domains for WebSocket and CDN are generated automatically.

### Slack

Slack requires two tokens: an app-level token (`xapp-...`) for Socket Mode and a bot token (`xoxb-...`) for the REST API. Use the `role` field to distinguish them.

**1. Create a Slack app** at [api.slack.com/apps](https://api.slack.com/apps). Enable Socket Mode, add the required OAuth scopes, and install the app to your workspace. Copy the bot token (`xoxb-...`) and app-level token (`xapp-...`).

**2. Create the Secret:**

```sh
oc create secret generic slack-secret \
  --from-literal=app-token=YOUR_APP_TOKEN \
  --from-literal=bot-token=YOUR_BOT_TOKEN \
  -n $NS
```

**3. Apply the Claw CR:**

```sh
oc apply -n $NS -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: slack
      channel: slack
      secretRef:
        - name: slack-secret
          key: bot-token
          role: botToken
        - name: slack-secret
          key: app-token
          role: appToken
EOF
```

The operator infers `type: bearer`, `domain: slack.com`, path-based routing for the two tokens, and a companion route for WebSocket connections (`.slack.com`).

### WhatsApp

WhatsApp Web uses phone-based QR pairing — no API keys or secrets needed. The operator allowlists the required WhatsApp domains and enables the channel plugin.

**1. Apply the Claw CR:**

```sh
oc apply -n $NS -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: whatsapp
      channel: whatsapp
EOF
```

The operator infers `type: none` and creates companion routes for `.whatsapp.com`, `.whatsapp.net`, `.facebook.com`, `.facebook.net`, and `.fbcdn.net` (WhatsApp Web relies on Meta's auth and CDN infrastructure). It also enables the WhatsApp plugin entry in `operator.json`.

After applying, the OpenClaw assistant handles plugin installation (`@openclaw/whatsapp`) and QR pairing. A pod restart is required after plugin install since npm plugins load at boot:

```sh
oc delete pod -n $NS -l app=claw
```

Once the new pod is ready, the assistant completes the pairing flow. The user scans the QR code with their phone (WhatsApp → Linked Devices).

### Explicit Override

You can override any inferred field if needed (e.g., routing through a corporate proxy or using a custom domain):

```yaml
    - name: telegram
      channel: telegram
      type: pathToken
      domain: "telegram.internal.corp.com"
      pathToken:
        prefix: "/bot"
      secretRef:
        - name: telegram-bot-secret
          key: token
```

The `channel` field still enables declarative channel config injection into `operator.json` — only the proxy routing is overridden. You can override `type`, `domain`, and type-specific config (`apiKey`, `pathToken`) independently.

## External Services

### GitHub REST API

Gives OpenClaw access to GitHub's REST API for working with issues, pull requests, code search, and repositories. The proxy injects a Personal Access Token as a bearer credential — the real token never reaches the gateway pod.

> **Note:** The pre-allowed `github.com` domain covers git HTTPS clones only (for npm dependencies). `api.github.com` is a separate host and requires an explicit credential entry.

**1. Create a Personal Access Token** at [github.com/settings/tokens](https://github.com/settings/tokens). A fine-grained token scoped to specific repositories is recommended over a classic token.

**2. Create the Secret:**

```sh
oc create secret generic github-pat-secret \
  --from-literal=token=YOUR_GITHUB_PAT \
  -n $NS
```

**3. Apply the Claw CR:**

```sh
oc apply -n $NS -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: github
      type: bearer
      domain: api.github.com
      secretRef:
        - name: github-pat-secret
          key: token
EOF
```

The proxy intercepts requests to `api.github.com` and injects `Authorization: Bearer <real PAT>`. Skills that use `curl` with the GitHub REST API (like the built-in `gh-issues` skill) work through the proxy — they use a placeholder token that the proxy replaces transparently.

> **`gh` CLI vs curl:** The `gh` CLI manages its own credentials via `gh auth login` (interactive OAuth). It doesn't use the `Authorization` header in the same way as `curl`-based skills, so proxy-based credential injection is less reliable with `gh`. For best results, use skills that call the GitHub REST API with `curl` directly.

## MCP Servers

MCP (Model Context Protocol) servers extend the AI assistant's capabilities by providing structured tool access to external services (GitHub API, filesystems, databases, etc.). The operator manages MCP server configuration declaratively via `spec.mcpServers` in the Claw CR, injecting the config into `operator.json` at reconciliation time.

The operator uses a three-tier security model for MCP server credentials:

| Tier | Transport | Secret handling | Secrets on gateway? |
|------|-----------|-----------------|---------------------|
| 1. HTTP/SSE (preferred) | HTTP URL | Proxy `credentials` entry for the domain | No |
| 2. Stdio + proxy placeholder | Subprocess | Placeholder env var + proxy `credentials` entry | No |
| 3. Stdio + real secret (escape hatch) | Subprocess | `envFrom` with `secretKeyRef` on gateway | Yes |

**Tier 1** is the recommended path — traffic goes through the MITM proxy, and credentials are injected transparently. **Tier 2** keeps secrets off the gateway for stdio MCP servers whose target domain and auth type are known. **Tier 3** is an explicit escape hatch for cases where tiers 1–2 are not viable.

### Tier 1: HTTP/SSE MCP (No Auth)

The simplest case — an unauthenticated HTTP MCP server. The domain is auto-extracted from the URL and added as a passthrough route in the proxy. No secret or credential entry needed.

```sh
oc apply -n $NS -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  mcpServers:
    context7:
      url: https://mcp.context7.com/mcp
      transport: streamable-http
EOF
```

### Tier 1: HTTP/SSE MCP (With Auth)

An HTTP MCP server that requires authentication. Add both the MCP server entry and a `credentials` entry for the domain. The proxy injects the auth header — no secrets reach the gateway.

**1. Create the Secret:**

```sh
oc create secret generic mcp-api-key \
  --from-literal=api-key=YOUR_API_KEY \
  -n $NS
```

**2. Apply the Claw CR:**

```sh
oc apply -n $NS -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: my-mcp-service
      type: bearer
      domain: api.example.com
      secretRef:
        - name: mcp-api-key
          key: api-key

  mcpServers:
    my-service:
      url: https://api.example.com/mcp
      transport: streamable-http
EOF
```

The proxy intercepts requests to `api.example.com` and injects `Authorization: Bearer <real key>`. The MCP server itself never sees credentials in its configuration.

### Tier 2: Stdio MCP with Proxy Placeholder (GitHub)

For stdio MCP servers where you know which domain is called and what auth type it uses. The env var is set to a placeholder value — the proxy intercepts outbound requests and replaces the placeholder with the real credential.

**1. Create the Secret:**

```sh
oc create secret generic github-pat-secret \
  --from-literal=token=YOUR_GITHUB_PAT \
  -n $NS
```

**2. Apply the Claw CR:**

```sh
oc apply -n $NS -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
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
EOF
```

How it works: The GitHub MCP server subprocess inherits `HTTP_PROXY`/`HTTPS_PROXY` from the gateway container, so its outbound HTTPS calls to `api.github.com` go through the MITM proxy. The server sends `Authorization: Bearer placeholder` (from the env var), and the proxy strips it and injects the real PAT from the Secret. The real token never reaches the gateway.

> **Reusing credentials:** If you already have a `credentials` entry for `api.github.com` (e.g., for GitHub REST API access via skills), the same entry covers the MCP server too — no duplication needed.

### Tier 3: Stdio MCP with Real Secret (Escape Hatch)

When you don't know the MCP server's internals, or it uses the secret for non-HTTP purposes (e.g., a database password), use `envFrom` to mount the real secret as a gateway container environment variable. The subprocess inherits it at runtime.

**1. Create the Secret:**

```sh
oc create secret generic db-credentials \
  --from-literal=password=YOUR_DB_PASSWORD \
  -n $NS
```

**2. Apply the Claw CR:**

```sh
oc apply -n $NS -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: db-network
      type: none
      domain: postgres.internal

  mcpServers:
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
EOF
```

> **Security note:** Tier 3 places the real secret on the gateway container. Use this only when tiers 1–2 are not viable. The `credentials` entry with `type: none` is still needed to allowlist the domain through the proxy (even without credential injection).

### Combining MCP Servers with Providers

MCP servers work alongside LLM provider credentials and other integrations in the same Claw instance:

```sh
oc apply -n $NS -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: anthropic
      type: apiKey
      secretRef:
        - name: anthropic-api-key
          key: api-key
      provider: anthropic

    - name: github
      type: bearer
      domain: api.github.com
      secretRef:
        - name: github-pat-secret
          key: token

  mcpServers:
    context7:
      url: https://mcp.context7.com/mcp
      transport: streamable-http

    github:
      command: npx
      args: ["-y", "@modelcontextprotocol/server-github"]
      env:
        GITHUB_PERSONAL_ACCESS_TOKEN: placeholder
EOF
```

### How It Works

The operator reconciles `spec.mcpServers` into the `mcp.servers` section of `operator.json`. At pod startup, the init-config script deep-merges `operator.json` into the user's `openclaw.json`:

- **Merge mode** (default): Operator-managed MCP servers merge alongside any user-managed servers added via `openclaw mcp set` inside the pod. On collision (same server name), the operator-managed entry wins.
- **Overwrite mode**: The full config is replaced on every pod start, including MCP servers.

The operator also validates that all `envFrom`-referenced Secrets exist and contain the specified keys. If validation fails, the `McpServersConfigured` condition is set to `False` with a descriptive error message, and `Ready` is set to `False`.

## Web Search

The operator can configure a web search provider for the OpenClaw agent via `spec.webSearch`. Search API keys are injected by the MITM proxy — they never reach the gateway container.

### Brave Search

Uses the [Brave Search API](https://brave.com/search/api/) with an API key injected via the `X-Subscription-Token` header.

**1. Get an API key** from [Brave Search API](https://brave.com/search/api/).

**2. Create the Secret:**

```sh
oc create secret generic brave-search-key \
  --from-literal=api-key=YOUR_BRAVE_API_KEY \
  -n $NS
```

**3. Add to your Claw CR:**

```sh
oc apply -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
  namespace: $NS
spec:
  credentials: []  # your existing credentials
  webSearch:
    provider: brave
    secretRef:
      name: brave-search-key
      key: api-key
EOF
```

### Tavily

Uses the [Tavily API](https://tavily.com/) with a bearer token.

**1. Get an API key** from [Tavily](https://app.tavily.com/).

**2. Create the Secret:**

```sh
oc create secret generic tavily-key \
  --from-literal=api-key=YOUR_TAVILY_API_KEY \
  -n $NS
```

**3. Add to your Claw CR:**

```sh
oc apply -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
  namespace: $NS
spec:
  credentials: []  # your existing credentials
  webSearch:
    provider: tavily
    secretRef:
      name: tavily-key
      key: api-key
EOF
```

You can pass provider-specific configuration via `spec.webSearch.config`:

```yaml
webSearch:
  provider: tavily
  secretRef:
    name: tavily-key
    key: api-key
  config:
    maxResults: 10
```

### DuckDuckGo

Key-free search — no API key or Secret needed.

```sh
oc apply -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
  namespace: $NS
spec:
  credentials: []  # your existing credentials
  webSearch:
    provider: duckduckgo
EOF
```

### Gemini (Search Grounding)

Uses Google's Gemini search grounding, which sends a regular Gemini API call with `tools: [{ google_search }]`. This reuses your existing `google` provider credential — no additional Secret is needed.

**Prerequisite:** You must have a `google` provider credential in `spec.credentials`.

```sh
oc apply -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
  namespace: $NS
spec:
  credentials:
    - name: google
      provider: google
      type: apiKey
      secretRef:
        - name: gemini-api-key
          key: api-key
  webSearch:
    provider: gemini
EOF
```

### How It Works

The operator sets `tools.web.search.provider` in `operator.json` and, for API-keyed providers, adds a proxy route for the search domain with credential injection. A placeholder API key is set in the config so OpenClaw makes the HTTP call; the proxy strips it and injects the real key.

The `WebSearchConfigured` condition tracks the status:
- `True` — provider validated and config injected
- `False` — validation failed (missing secret, missing google credential for gemini, unknown provider)

## Web Fetch

The `web_fetch` tool allows the agent to fetch arbitrary URLs. Enable it via `spec.webFetch`:

```yaml
spec:
  webFetch:
    enabled: true
```

Fetched URLs are gated by the proxy allowlist. Only domains already permitted by credentials, search providers, or builtin passthroughs are reachable. To allow additional domains for fetching, add `type: none` credential entries:

```sh
oc apply -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
  namespace: $NS
spec:
  credentials:
    - name: docs-site
      type: none
      domain: docs.python.org
  webFetch:
    enabled: true
EOF
```

## Application Configuration

The Claw CR supports `spec.config` for declarative OpenClaw application settings — diagnostics, CORS origins, model preferences, agent defaults, and any other `openclaw.json` key that isn't driven by a typed CRD field.

### `spec.config.raw`

Accepts arbitrary JSON that maps directly to `openclaw.json` keys. These values are merged into `operator.json` before the enrichment pipeline runs:

```sh
oc apply -n $NS -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: gemini
      type: apiKey
      secretRef:
        - name: gemini-api-key
          key: api-key
      provider: google
  config:
    raw:
      diagnostics:
        otel:
          enabled: true
          endpoint: http://langfuse.observability.svc:3000/api/public/otel/v1/traces
          captureContent:
            inputMessages: true
            outputMessages: true
      gateway:
        controlUi:
          allowedOrigins:
            - "https://custom.example.com"
      agents:
        defaults:
          model:
            primary: google/gemini-3-flash-preview
          models:
            openrouter/qwen3-14b:
              alias: Qwen 3 14B
      plugins:
        entries:
          diagnostics-otel:
            enabled: true
EOF
```

### `spec.config.mergeMode`

Controls how `operator.json` is applied to the PVC config at pod start:

- **`merge`** (default) — deep-merges operator settings into the existing PVC config, preserving runtime changes (primary model choice, plugin installs, UI settings).
- **`overwrite`** — fully replaces the PVC config on every pod start. Useful for clean-slate deployments.

```yaml
spec:
  config:
    mergeMode: overwrite
    raw:
      # ...
```

### What can and can't be overridden

The operator enforces a three-tier model. Not all config keys are equal:

| Tier | Keys | Behavior |
|------|------|----------|
| Always-win | `gateway.mode`, `gateway.bind`, `gateway.port`, `gateway.controlUi.enabled`, `gateway.auth.*`, `models.providers`, `channels.*`, `mcp.servers`, `tools.web.*` | Operator sets unconditionally — `spec.config.raw` cannot override |
| Append/merge | `gateway.controlUi.allowedOrigins`, `gateway.trustedProxies`, `agents.defaults.models`, `agents.defaults.model.primary` | Operator provides its part, user values are merged or appended |
| User-only | `diagnostics.*`, `session.*`, `logging.*`, `plugins.*` (non-declared), `skills.*`, `ui.*`, `cron.*`, `hooks.*`, etc. | Operator never touches — full user control |

For the complete enrichment policy, see [ADR-0013](adr/0013-spec-config.md).

### `spec.config.raw` vs `openclaw config patch`

Both methods work for user-managed settings:

| | `spec.config.raw` | `openclaw config patch` |
|---|---|---|
| **Persistence** | Declarative in CR — survives CR re-apply | On PVC — survives pod restarts |
| **GitOps friendly** | Yes — part of the CR manifest | No — requires pod exec |
| **Precedence** | Wins over PVC for matching keys (except primary model) | Wins only until next reconcile |

For declarative setups managed via GitOps, prefer `spec.config.raw`. For one-off tweaks inside the pod, `openclaw config patch` is fine.

## Metrics

The operator can expose Prometheus metrics from your Claw instance using an OpenTelemetry Collector sidecar. When enabled, the gateway pushes OTLP metrics to a localhost sidecar that exposes a `/metrics` endpoint for Prometheus scraping.

### Enabling metrics

```sh
oc apply -n $NS -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: gemini
      type: apiKey
      secretRef:
        - name: gemini-api-key
          key: api-key
      provider: google
  metrics:
    enabled: true
EOF
```

This single field (`spec.metrics.enabled: true`) triggers the operator to:

1. Inject `diagnostics.otel.metrics: true` and `diagnostics.otel.endpoint: "http://localhost:4318"` into the gateway config
2. Add an OTel Collector sidecar container to the gateway pod
3. Add a `metrics` port (9464) to the Service
4. Open the NetworkPolicy for scraping from monitoring namespaces
5. Create a ServiceMonitor for Prometheus Operator auto-discovery

### How it works

The gateway pushes OTLP/HTTP metrics to `localhost:4318`. The OTel Collector sidecar receives them and exposes a Prometheus-compatible `/metrics` endpoint on port 9464. Prometheus (via the ServiceMonitor) scrapes this endpoint.

```
Gateway ──OTLP/HTTP──▶ OTel Collector sidecar ──:9464/metrics──▶ Prometheus
```

All traffic between the gateway and sidecar stays within the pod (localhost). The NetworkPolicy allows ingress on port 9464 only from namespaces labeled with `network.openshift.io/policy-group: monitoring` (OpenShift platform monitoring).

### Custom port

Override the default Prometheus exporter port (9464):

```yaml
spec:
  metrics:
    enabled: true
    port: 8888
```

### ServiceMonitor configuration

By default, the operator creates a ServiceMonitor when metrics are enabled. On OpenShift, Prometheus Operator is a platform component that automatically discovers ServiceMonitors.

Disable ServiceMonitor creation (e.g., if you configure scraping manually):

```yaml
spec:
  metrics:
    enabled: true
    serviceMonitor:
      enabled: false
```

Customize the scrape interval (default: 30s):

```yaml
spec:
  metrics:
    enabled: true
    serviceMonitor:
      interval: "15s"
```

### Verifying metrics

Once enabled, verify the metrics endpoint is working:

```sh
# Port-forward to the metrics port
oc port-forward -n $NS svc/instance 9464:9464

# In another terminal, check the endpoint
curl http://localhost:9464/metrics
```

You should see Prometheus-format metrics from the OpenClaw gateway.

### User-provided diagnostics config

If you configure `diagnostics.otel` in `spec.config.raw`, the operator will not override your settings. This allows advanced users to point OTLP to a custom endpoint or configure additional telemetry options while still using the operator-managed sidecar for Prometheus export.

## Plugins

The operator supports declarative plugin installation. List plugins in `spec.plugins` and the operator runs an init container that installs them on the PVC before the gateway starts.

### Installing plugins

```sh
oc apply -n $NS -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: gemini
      type: apiKey
      secretRef:
        - name: gemini-api-key
          key: api-key
      provider: google
  plugins:
    - "@openclaw/matrix"
    - "@openclaw/diagnostics-otel"
EOF
```

When `spec.plugins` is non-empty, the operator adds an `init-plugins` init container that runs `openclaw plugins install clawhub:<pkg>` for each entry. The init container:

- Uses the same OpenClaw image as the gateway
- Routes traffic through the MITM proxy (required by the egress NetworkPolicy)
- Installs plugins to the shared PVC so they persist across pod restarts

### How it works

The `init-plugins` container runs after the proxy is available and before the gateway starts. It downloads packages from ClawHub/npm through the MITM proxy, writing them to the PVC at `/home/node/.openclaw`.

```
init-volume → init-config → wait-for-proxy → init-plugins → gateway
```

Changing the plugin list triggers a pod rollout (the operator includes the plugin list in the config hash annotation).

### Limitations

Removing a plugin from `spec.plugins` prevents it from being installed on new pods but does **not** uninstall it from the existing PVC. To fully remove a plugin, either delete the PVC (the operator will recreate it) or manually remove the plugin files.

---

## Workspace Files

The `spec.workspace` field seeds files into the OpenClaw workspace directory on first pod start. Workspace files are **user-owned** — they are seeded once (`seedIfMissing`) and user edits via the OpenClaw UI are preserved across restarts.

### Skip Bootstrap

Set `skipBootstrap: true` to suppress the OpenClaw first-run questionnaire. This is useful for demo and onboarding setups where you pre-configure the identity and agents.

### Seeding Files

Use `spec.workspace.files` to provide an inline map of workspace-relative paths to file content. Common use cases include `IDENTITY.md` (user identity), `AGENTS.md` (agent instructions), and custom documentation.

```yaml
spec:
  workspace:
    skipBootstrap: true
    files:
      IDENTITY.md: |
        # Identity
        - Name: Demo User
        - Role: Enterprise Developer
        - Company: FantaCo
      AGENTS.md: |
        ## FantaCo Enterprise Assistant
        You are a FantaCo enterprise assistant with access to
        internal APIs and compliance guidelines.
```

### Ownership Semantics

- Files are seeded once — if the file already exists on the PVC (from a previous pod start or user edit), the operator does not overwrite it.
- If `spec.workspace.files` includes `AGENTS.md`, it overrides the operator's built-in `AGENTS.md` seed. The built-in seed becomes a no-op since the destination already exists.
- Workspace file changes in the CR trigger a pod rollout automatically (the operator includes all ConfigMap keys in the config hash).

### Path Restrictions

The following paths are rejected by the controller (the CR enters `Ready=False`):

- Empty or absolute paths
- Paths containing `..` (directory traversal)
- Paths containing `--` (reserved as the internal slash-encoding delimiter)
- Paths under `skills/platform/` or `skills/kubernetes/` (operator-managed)

---

## Skills

The `spec.skills` field injects operator-managed skills into the workspace. Unlike workspace files, skills are **always overwritten** on every pod restart (`copyAlways`). Each entry creates `workspace/skills/<name>/SKILL.md`.

```yaml
spec:
  skills:
    quote-builder: |
      ---
      name: quote-builder
      description: Build customer quotes using the pricing API
      ---
      # Quote Builder
      Connect to the pricing MCP server and generate quotes...
    compliance: |
      ---
      name: compliance
      description: Corporate compliance guidelines
      ---
      # Compliance
      Always follow FantaCo policy...
```

### Ownership Semantics

- Skills are operator-managed — the content from the CR is written to the PVC on every pod restart, overwriting any changes made inside the pod.
- This makes skills suitable for enterprise policies, shared tooling instructions, and any content that must stay in sync with the CR.

### Name Restrictions

Skill names are directory components (not paths). The following names are rejected:

- Empty names
- Names containing `/`
- Names containing `--` (reserved delimiter)
- Builtin operator skill names: `platform`, `kubernetes`

### Interaction with Other Features

| Feature | Interaction |
|---------|-------------|
| `spec.config.raw` | Independent — workspace files are PVC content, not openclaw.json config |
| `spec.plugins` | Independent — plugins are npm packages; skills are markdown files |
| Existing platform/kubernetes skills | Coexist — operator-injected skills use separate ConfigMap keys |
| Config hash / rollout | Automatic — any workspace or skill change triggers pod restart |

### Full Example

```yaml
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: demo-instance
spec:
  credentials:
    - name: gemini
      type: apiKey
      secretRef:
        - name: gemini-api-key
          key: api-key
      provider: google
  config:
    raw:
      agents:
        defaults:
          model:
            primary: google/gemini-2.5-pro
  workspace:
    skipBootstrap: true
    files:
      IDENTITY.md: |
        # Identity
        - Name: Demo User
        - Role: Enterprise Developer
        - Company: FantaCo
      AGENTS.md: |
        ## FantaCo Enterprise Assistant
        You are a FantaCo enterprise assistant with access to
        internal APIs and compliance guidelines.
  skills:
    quote-builder: |
      ---
      name: quote-builder
      description: Build customer quotes using the pricing API
      ---
      # Quote Builder
      Connect to the pricing MCP server and generate quotes...
  plugins:
    - "@openclaw/diagnostics-otel"
  metrics:
    enabled: true
```

---

## Operator Resource Limits

The operator controller itself has no resource limits set by default. On OpenShift (OLM installs), configure limits via the `Subscription` CR:

```yaml
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: claw-operator
  namespace: openshift-operators
spec:
  channel: alpha
  name: claw-operator
  source: claw-operator-catalog
  sourceNamespace: openshift-marketplace
  config:
    resources:
      limits:
        memory: 256Mi
        cpu: 500m
      requests:
        memory: 128Mi
        cpu: 50m
```
