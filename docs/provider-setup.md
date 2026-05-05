# Provider Setup

This guide covers configuring LLM providers, external services, and messaging channels for use with Claw. Each section walks through creating the necessary Secret and Claw CR configuration.

All examples assume you have set your target namespace:

```sh
export NS=my-claw-namespace
```

## LLM Providers

For known providers (`google`, `anthropic`), the operator automatically infers the correct `domain` and `apiKey` header — you only need `name`, `type`, `secretRef`, and `provider`. You can still override any inferred field explicitly if needed (e.g., routing through a custom proxy).

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
        name: gemini-api-key
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
        name: anthropic-api-key
        key: api-key
      provider: anthropic
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
        name: vertex-sa-key
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
        name: vertex-sa-key
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
        name: vertex-sa-key
        key: sa-key.json
      gcp:
        project: "YOUR_PROJECT_ID"
        location: "us-east5"
      provider: anthropic
    - name: gemini
      type: gcp
      secretRef:
        name: vertex-sa-key
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
        name: my-kubeconfig
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
        name: multi-cluster-kubeconfig
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
        name: gemini-api-key
        key: api-key
      provider: google
    - name: k8s-workspace
      type: kubernetes
      secretRef:
        name: my-kubeconfig
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

Messaging channels (Telegram, Discord, WhatsApp) use different authentication mechanisms. Unlike LLM providers, messaging channel credentials are injected transparently by the proxy — OpenClaw is configured with placeholder tokens and never sees the real secrets.

### Telegram

The Telegram Bot API embeds the bot token in the URL path (`/bot<TOKEN>/method`). The proxy uses `pathToken` credential injection to replace the placeholder with the real token on every request.

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
      type: pathToken
      secretRef:
        name: telegram-bot-secret
        key: token
      domain: "api.telegram.org"
      pathToken:
        prefix: "/bot"
EOF
```

**4. Configure OpenClaw** with a placeholder token:

```sh
openclaw channels add --channel telegram --token placeholder
```

The proxy intercepts requests like `/botplaceholder/sendMessage` and forwards them as `/bot<REAL_TOKEN>/sendMessage`. The real token never reaches the gateway pod.

### Discord

Discord Bot API uses `Authorization: Bot <TOKEN>` header authentication. The proxy uses `apiKey` credential injection with a `Bot ` value prefix. Discord also requires passthrough domains for its WebSocket gateway and CDN.

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
      type: apiKey
      secretRef:
        name: discord-bot-secret
        key: token
      domain: "discord.com"
      apiKey:
        header: Authorization
        valuePrefix: "Bot "
    - name: discord-gateway
      type: none
      domain: "gateway.discord.gg"
    - name: discord-cdn
      type: none
      domain: "cdn.discordapp.com"
EOF
```

**4. Configure OpenClaw** with a placeholder token:

```sh
openclaw channels add --channel discord --token placeholder
```

The `discord-gateway` and `discord-cdn` entries allow Discord's WebSocket connection and media/avatar downloads without credential injection.

### WhatsApp

OpenClaw supports WhatsApp as a messaging channel via WhatsApp Web (the Baileys library). The gateway maintains a linked-device session over WebSocket to WhatsApp's servers.

All gateway egress goes through the credential-injecting proxy, which blocks unknown domains by default. To allow WhatsApp traffic, add two `type: none` credentials that allowlist the required domains:

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
        name: gemini-api-key
        key: api-key
      provider: google
    - name: whatsapp
      type: none
      domain: ".whatsapp.com"
    - name: whatsapp-net
      type: none
      domain: ".whatsapp.net"
EOF
```

The leading dot makes these suffix matches — `.whatsapp.com` covers `web.whatsapp.com` (WebSocket), `api.whatsapp.com`, and fallback hosts; `.whatsapp.net` covers `mmg.whatsapp.net` (media), `pps.whatsapp.net` (profile pictures), and CDN subdomains.

No Secret is needed. WhatsApp Web uses phone-based QR pairing, not API keys — the session state is managed by OpenClaw on the gateway pod's persistent storage. The `none` type tells the proxy to allow traffic to these domains without injecting any credentials.

After deploying, enable WhatsApp inside OpenClaw:

1. Open the OpenClaw Control UI
2. Enable the WhatsApp plugin (`openclaw plugins install @openclaw/whatsapp`)
3. Run `openclaw channels login --channel whatsapp` to get a QR code
4. Scan the QR code with your phone (WhatsApp → Linked Devices)
