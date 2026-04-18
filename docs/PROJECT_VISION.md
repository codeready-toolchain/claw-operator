# Claw Operator

## What

A Kubernetes operator that deploys and manages personal [OpenClaw](https://github.com/openclaw/openclaw) AI assistant instances. Each user gets a fully isolated OpenClaw environment with a credential-injecting proxy, network isolation, and automated lifecycle management.

The operator works on any OpenShift cluster and vanilla Kubernetes (with reduced functionality — no Route, localhost fallback for CORS).

## What the Assistant Can Do

OpenClaw is a personal AI assistant that can be configured with different "hats" (capabilities):

**Kube/OpenShift hat** — manage resources in the user's namespace:
- "Deploy a Python demo app for me"
- "My app is crashing, help me debug it"
- "Explain how NetworkPolicies work and show me mine"

**LLM integration** — connect to any LLM provider via `spec.credentials[]`:
- Gemini, Anthropic, OpenAI, OpenRouter (API keys)
- Vertex AI (GCP service accounts with OAuth2)
- Custom MCP servers, enterprise APIs (OAuth2, bearer tokens)

Other hats and use cases are open for exploration.

## Architecture

```
┌────────────────────────────────────────────────┐
│              OpenShift / Kubernetes            │
│                                                │
│  ┌──────────────────────────────────────────┐  │
│  │            User's Namespace              │  │
│  │                                          │  │
│  │   ┌────────────┐    ┌───────────────┐    │  │
│  │   │  OpenClaw  │───▶│     Proxy     │────┼──┼───▶ LLM APIs (Gemini, etc.)
│  │   │ (personal) │    │               │    │  │
│  │   └────────────┘    └───────┬───────┘    │  │
│  │                             │            │  │
│  └─────────────────────────────┼────────────┘  │
│                                │               │
│                                ▼               │
│                         Kube API Server        │
│                                                │
│  Claw Operator (manages all instances)         │
└────────────────────────────────────────────────┘
```

- **One OpenClaw per user**, isolated in the user's own namespace
- **Operator** deploys and manages the full stack: gateway, proxy, networking, credentials
- **Proxy** sits between OpenClaw and everything external — LLM APIs and the Kube API server alike. Injects credentials so the OpenClaw process itself never sees raw keys.
- **NetworkPolicies** ensure OpenClaw can only talk through its proxy, never directly to the internet or the API server

## Deployments

The operator is designed to run on any OpenShift or Kubernetes cluster. Known integration targets:

- **Red Hat Developer Sandbox** — see [sandbox-claw-operator](https://github.com/sandbox-claw-operator) for Sandbox-specific integration (namespace isolation, SpaceRequest provisioning, tier templates)
- **Any OpenShift cluster** — full functionality including Route-based ingress and OpenShift OAuth
- **Vanilla Kubernetes** — works with localhost fallback (port-forward) for CORS; no Route support
