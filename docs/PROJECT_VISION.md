# OpenClaw on Dev Sandbox

## Why
git 
Personal AI assistants are coming to the enterprise. Red Hat needs to show it can help organizations deploy, secure, and manage them. Dev Sandbox is the perfect place to let people experience this firsthand — zero setup, 30-day trial, instant access.

## What

A new "Try" card on [sandbox.redhat.com](https://sandbox.redhat.com): **OpenClaw — Your Personal AI Assistant**.

Users click it, provide an LLM API key, and get a fully configured, sandboxed [OpenClaw](https://github.com/openclaw/openclaw) instance running in their own namespace. They log in via the same Red Hat SSO they already use for Dev Sandbox and land on a welcome screen with a ready-to-go assistant.

## User Journey

1. User visits Dev Sandbox, sees the OpenClaw card
2. We ask for an AI API key (with guidance on how to get one — e.g., free Gemini key)
3. Operator deploys a personal OpenClaw in the user's namespace
4. User is redirected to the OpenClaw dashboard, logs in via cluster OAuth
5. A welcome message from the assistant ("Bob") greets them with examples of what they can do

## What the Assistant Can Do

**Kube/OpenShift hat** — OpenClaw has pre-configured access to the user's namespace with the user's own permissions. Examples:
- "Deploy a Python demo app for me"
- "My app is crashing, help me debug it"
- "Explain how NetworkPolicies work and show me mine"

**Enterprise hat (rough idea)** — Demo how a personal AI assistant could integrate with corporate systems in an imaginary company (mock services, not real backends). The goal is to show the art of the possible, not to build production integrations. Exact scenarios TBD.

Other hats and use cases are open for exploration.

## High-Level Architecture

```
┌────────────────────────────────────────────────┐
│              Dev Sandbox Cluster               │
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
│  OpenClaw Operator (manages all instances)     │
└────────────────────────────────────────────────┘
```

- **One OpenClaw per user**, isolated in the user's own namespace (standard Dev Sandbox tenant model)
- **Operator** deploys and manages the full stack per user
- **Proxy** sits between OpenClaw and everything external — LLM APIs and the Kube API server alike. Injects credentials so the OpenClaw process itself never sees raw keys.
- **NetworkPolicies** ensure OpenClaw can only talk through its proxy, never directly to the internet or the API server
- **OpenShift OAuth** — users authenticate with their existing Dev Sandbox identity, no separate accounts

## Key Work Areas

| Area | Status | Notes |
|------|--------|-------|
| **Operator (bootstrapping and lifecycle management)** | In progress | Deploys OpenClaw + proxy + networking. Core reconciliation loop works. |
| **Security** | In progress | Proxy-based credential isolation and NetworkPolicies in place. More hardening needed (gateway token management, RBAC scoping, etc.) |
| **LLM Access** | Open question | BYOK (bring your own key) works today. Can we offer any zero-config experience? Free tiers exist (Gemini, Groq, Cerebras) but have rate limits and aren't meant for production. Needs research. |
| **UX / Vision Refinement** | Early | Welcome experience, assistant personas, what "hats" to ship with, onboarding flow for the API key — all need design work. |
| **Login & OAuth** | Not started | Integrate OpenClaw's auth with the cluster's OAuth/Keycloak so users log in seamlessly with their Dev Sandbox identity. |
| **Enterprise demo scenarios** | Idea stage | Integreate with Burr's demo? |

## LLM Access — The Hard Question

The weakest part of the story today. Options on the table:

- **BYOK only** — user provides their own key. Simple, but adds friction.
- **Free tiers** — Gemini (free, generous limits), Groq/Cerebras (free, open-source models). Usable for demos, not reliable for production. Rate limits vary.
- **Red Hat-provided pool** — Red Hat funds a shared API quota. Cost and abuse control are concerns.
- **Hybrid** — ship with BYOK, explore subsidized or partner-sponsored access later.

This area needs dedicated research.
