# We Put a Personal AI Assistant on Developer Sandbox

**TL;DR:** [OpenClaw](https://github.com/openclaw/openclaw) is now available on [Red Hat Developer Sandbox](https://developers.redhat.com/developer-sandbox). One click, one API key, and you get a personal AI assistant running in your own OpenShift project. The AI never sees your credentials and can't escape its network sandbox.

---

## What's New

If you log into [sandbox.redhat.com](https://sandbox.redhat.com) today, you'll see a new card: **OpenClaw**. Click it, paste in an LLM API key, and about a minute later you have a working AI assistant. A free Gemini key will get you through the initial setup and a few conversations, but you'll hit rate limits quickly. OpenClaw is a heavy token consumer, so a paid tier is recommended for real use. No YAML, no Helm charts, no cluster-admin.

The assistant comes pre-configured with access to your workspace project, so you can ask it to deploy apps, debug crashlooping pods, explain your NetworkPolicies, or just use it as a general-purpose coding companion. It runs [OpenClaw](https://github.com/openclaw/openclaw) under the hood, managed by a purpose-built Kubernetes operator we call [claw-operator](https://github.com/codeready-toolchain/claw-operator).

---

## The Hard Part: Security on Shared Infrastructure

Building this was less about getting OpenClaw to run (it's a Node.js app, it runs anywhere) and more about running it *safely* on Dev Sandbox, where thousands of users share the same cluster. Each user gets their own namespace, but everything runs on shared infrastructure with shared API servers and shared networking.

The questions we had to answer:

- The AI needs Kubernetes API access to be useful. But if it can read Secrets in its own namespace, it can grab the LLM API keys, the proxy CA, the gateway token... everything.
- The AI needs to call LLM APIs. But if it has direct internet access, a prompt injection could exfiltrate data to an attacker-controlled endpoint.
- Users bring their own API keys. Those keys need to reach the LLM providers, but the AI process itself should never see them in plaintext.
- Namespaces provide isolation between users, but within a namespace there's no built-in way to isolate the AI from its own infrastructure.

OpenShift already gives us a solid foundation here. The restricted SCC enforces non-root UIDs, SELinux, seccomp profiles, and blocks privilege escalation. But pod-level security isn't enough when the problem is credential access and network boundaries. We needed application-level isolation on top of what the platform provides.

We ended up with a design where the AI is treated as untrusted by default. Here's what that looks like in practice.

### Two Namespaces, Hard Boundary

The operator deploys OpenClaw into a separate `-claw` project and only gives the AI access to your `-dev` workspace:

```
alice-dev (your workspace)              alice-claw (AI infrastructure)
├── your deployments                    ├── OpenClaw gateway
├── your services                       ├── Credential proxy
├── your apps                           ├── Secrets (API keys, CA, tokens)
│                                       ├── NetworkPolicies
│   AI has: edit access here            │   AI has: zero access here
```

The AI can create Deployments and debug pods in `-dev` all day long. But it physically cannot read the secrets, proxy config, or network policies in `-claw` because it has no RBAC there. Kubernetes doesn't let you say "access all Secrets except these three," so we split the namespaces instead.

### Credential Proxy

Your API keys live in Kubernetes Secrets (works with External Secrets Operator, Sealed Secrets, Vault, whatever you use). A dedicated proxy sits between OpenClaw and the outside world. The proxy intercepts outbound HTTPS, looks at the destination domain, and injects the right credentials. The gateway itself only ever holds dummy placeholder keys.

NetworkPolicies enforce this: the gateway can talk to the proxy and DNS. That's it. No direct internet access. Even if someone found a way to make the AI send arbitrary HTTP requests, those requests go through the proxy, which only allows explicitly configured domains.

### The Proxy Is the Only Way Out

Three NetworkPolicies per instance:

- Ingress: only the OpenShift router talks to the gateway
- Gateway egress: proxy + DNS, nothing else
- Proxy egress: HTTPS (443) to configured domains only

So the threat model is: even with full code execution inside the OpenClaw container, there's no network path to exfiltrate credentials or reach unauthorized endpoints.

---

## Supported Providers

Works out of the box with:

| Provider | What You Need |
|----------|--------------|
| Google Gemini | API key from [AI Studio](https://aistudio.google.com/apikey) (free tier available) |
| OpenAI | API key from [platform.openai.com](https://platform.openai.com/api-keys) |
| Anthropic | API key from [console.anthropic.com](https://console.anthropic.com/) |
| xAI (Grok) | API key from [console.x.ai](https://console.x.ai/) |
| OpenRouter | API key from [openrouter.ai](https://openrouter.ai/) (100+ models behind one key) |
| Google Vertex AI | GCP service account |
| Self-hosted | Any OpenAI-compatible endpoint (vLLM, Ollama, LiteLLM, etc.) |

For the well-known providers, you just provide a key and the operator figures out the domain, auth type, and model catalog. Custom endpoints need a bit more config but work the same way.

---

## What Can You Do With It

The obvious stuff: "deploy a Python app," "why is my pod failing," "write me a Dockerfile." It has `edit` access to your workspace namespace, so it can actually do things, not just talk about them.

But OpenClaw is a general-purpose assistant. People use it for code review, architecture brainstorming, writing docs, learning new tools. It's not limited to Kubernetes tasks. The "Kube hat" is just one of its capabilities.

---

## Try It

1. Sign up at [developers.redhat.com/developer-sandbox](https://developers.redhat.com/developer-sandbox) (free, no credit card)
2. Grab a free API key from [Google AI Studio](https://aistudio.google.com/apikey)
3. Click the OpenClaw card on your dashboard

You get 30 days (though you can always re-sign up for free), and the assistant idles after 12 hours of continuous running — that's a Dev Sandbox limitation to free up resources, not OpenClaw or the operator. To bring it back, just click "Provision" again on the dashboard.

---

## Everything Is Open Source

- [OpenClaw](https://github.com/openclaw/openclaw) — the assistant itself
- [claw-operator](https://github.com/codeready-toolchain/claw-operator) — the operator that deploys and secures it
- [Developer Sandbox](https://github.com/codeready-toolchain) — the platform running it all

The operator works on any OpenShift cluster, not just Dev Sandbox. If you want the same security model on your own infrastructure, it's all there.

---

## What We're Working on Next

Right now our focus is on two things: hardening security further (there's no such thing as "secure enough" when you're running AI workloads on shared infrastructure) and monitoring how the system behaves at scale with real users hitting it daily.

This is also a fresh production deployment, so expect some rough edges. We're actively watching for issues and fixing them as they come up — if something breaks, it won't stay broken for long.

If you try it out and hit rough edges, we want to hear about it. The operator repo is at [github.com/codeready-toolchain/claw-operator](https://github.com/codeready-toolchain/claw-operator).