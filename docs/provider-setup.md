# Provider Setup

This guide covers configuring LLM providers and external services for use with Claw. Each section walks through creating the necessary Secret and Claw CR configuration.

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
