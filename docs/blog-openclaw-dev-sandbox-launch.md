# We Put a Personal AI Assistant on Developer Sandbox

**TL;DR:** [OpenClaw](https://github.com/openclaw/openclaw) is now available on [Red Hat Developer Sandbox](https://developers.redhat.com/developer-sandbox).
One click, one API key, and you get a personal AI assistant running in your own OpenShift project. The agent never sees your credentials and can't escape its network sandbox.

---

## What's New

If you log into [sandbox.redhat.com](https://sandbox.redhat.com) today, you'll see a new card: **OpenClaw**. Click it, paste in an LLM API key, and about a minute
later you have a working AI assistant. A free Gemini key will get you through the initial setup and a few conversations, but you'll hit rate limits quickly so a paid subscription is recommended.
No YAML, no Helm charts, no cluster-admin.

The assistant comes pre-configured with access to your workspace project, so you can ask it to deploy apps, debug crashlooping pods, explain your
NetworkPolicies, or just use it as a general-purpose coding companion. It runs [OpenClaw](https://github.com/openclaw/openclaw) under the hood, managed by a purpose-built
Kubernetes operator, [claw-operator](https://github.com/codeready-toolchain/claw-operator).

---

## The Hard Part: Security on Shared Infrastructure

Building this was less about getting OpenClaw to run (it's a Node.js app, it runs anywhere) and more about running it on Developer Sandbox, where thousands of users
share the same cluster. Each user gets their own namespace, but everything runs on shared infrastructure with shared API servers and shared networking.

The questions we had to answer:

- The agent needs Kubernetes API access to be useful. But if it can read Secrets in its own namespace, it can grab the LLM API keys, the proxy CA, and the gateway token.
- The agent needs to call LLM APIs. But if it has direct internet access, a prompt injection could exfiltrate data to an attacker-controlled endpoint.
- Users bring their own API keys. Those keys need to reach the LLM providers, but the OpenClaw process itself should never see them in plaintext.
- Namespaces provide isolation between users, but within a namespace there's no built-in way to isolate the agent from its own infrastructure.

OpenShift already gives us a solid foundation here. The restricted SCC enforces non-root UIDs, SELinux, seccomp profiles, and blocks privilege escalation
without us having to configure any of that. But pod-level security isn't enough when the problem is credential access and network boundaries. We needed
application-level isolation on top of what the platform provides.

We ended up with a design where OpenClaw is treated as untrusted by default. Here's what that looks like in practice.

### Two Namespaces, Hard Boundary

The operator deploys OpenClaw into a separate `-claw` project and only gives the agent access to your `-dev` workspace:

```
alice-dev (your workspace)              alice-claw (AI infrastructure)
├── your deployments                    ├── OpenClaw gateway
├── your services                       ├── Credential proxy
├── your apps                           ├── Secrets (API keys, CA, tokens)
│                                       ├── NetworkPolicies
│   OpenClaw has: edit access here      │   OpenClaw has: zero access here
```

OpenClaw can create Deployments and debug pods in `-dev` all day long. But it physically cannot read the secrets, proxy config, or network policies in `-claw`
because it has no RBAC there. Kubernetes doesn't let you say "access all Secrets except these three," so we split the namespaces instead.

### Credential Proxy

Your API keys live in Kubernetes Secrets (works with External Secrets Operator, Sealed Secrets, Vault, whatever you use). A dedicated proxy sits between OpenClaw
and the outside world. The proxy intercepts outbound HTTPS, looks at the destination domain, and injects the right credentials. The gateway itself only ever holds
dummy placeholder keys.

NetworkPolicies enforce this: the gateway can talk to the proxy and DNS. That's it. No direct internet access.
Even if someone found a way to make the AI send arbitrary HTTP requests,
those requests go through the proxy, which only allows explicitly configured domains.

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

For the well-known providers, you just provide a key and the operator figures out the domain, auth type, and model catalog. Custom endpoints need a
bit more config but work the same way.

---

## What Can You Do With It

The obvious: "deploy a Python app," "why is my pod failing," "write me a Dockerfile." It has `edit` access to your workspace namespace, so it can
actually do things. But it's also a general-purpose assistant — code review, architecture brainstorming, writing docs, learning new tools. The Kubernetes
access is just one of its capabilities.

On Dev Sandbox, the operator uses merge mode by default, so you can install plugins, configure messaging channels, tune agent behavior, and customize
your workspace through the OpenClaw UI — all of which survive pod restarts. The operator manages security-critical settings but leaves everything else
to you.

MCP servers are supported even on Dev Sandbox. You configure them through `spec.mcpServers` on the Claw CR, and the operator handles the
proxy routing, NetworkPolicy updates, and pod rollout. Stdio MCP servers work too. MCP setup is done externally via `oc` or the OpenShift console.

You can also use the `openclaw` CLI directly by exec-ing into the pod from your cluster. This gives you a terminal-based interface to the same
assistant — useful for scripting, piping context in, or just preferring the command line over the web UI.

Whether you're tweaking the pre-configured instance on Dev Sandbox or deploying it on your team's own cluster, you have full control over the `Claw` custom resource. The operator exposes knobs for adding custom AI providers and messaging channels (like Slack, Discord, or Telegram), configuring in-cluster network bypass, adding egress rules, and performing full config overrides via `spec.config.raw`. The security model scales from "locked-down shared platform" to "wide-open dev cluster" depending on what you configure.

Not sure how to configure a specific feature? Just ask OpenClaw! The assistant is fully aware of its own CRD and will gladly write the YAML for you.

---

## Try It

1. Sign up at [developers.redhat.com/developer-sandbox](https://developers.redhat.com/developer-sandbox) (free, no credit card)
2. Grab a free API key from [Google AI Studio](https://aistudio.google.com/apikey)
3. Click the OpenClaw card on your dashboard

You get 30 days, and the assistant idles after 12 hours of inactivity — that's a Dev Sandbox limitation (it's a free trial platform), not OpenClaw or
the operator. To bring it back, just click "Provision" again on the dashboard.

---

## Everything Is Open Source

- [OpenClaw](https://github.com/openclaw/openclaw) — the assistant itself
- [claw-operator](https://github.com/codeready-toolchain/claw-operator) — the operator that deploys and secures it
- [Developer Sandbox](https://github.com/codeready-toolchain) — the platform running it all

The operator works on any OpenShift cluster, not just Dev Sandbox. If you want the same security model on your own infrastructure, it's all there.

---

## What We're Working on Next

We're focused on hardening security further (there's no such thing as "secure enough" when you're running AI workloads on shared infrastructure)
and monitoring how the system behaves at scale with real users hitting it daily.

This is also a fresh production deployment, so expect some rough edges. We're actively watching for issues and fixing them as they come up — if
something breaks, it won't stay broken for long.

If you try it out and hit rough edges, we want to hear about it. The operator repo is at [github.com/codeready-toolchain/claw-operator](https://github.com/codeready-toolchain/claw-operator).
