# api-proxy Specification

## Purpose
TBD - created by archiving change include-other-manifests. Update Purpose after archive.
## Requirements
### Requirement: Operator creates proxy ConfigMap with nginx configuration
The operator SHALL create a ConfigMap named `openclaw-proxy-config` containing nginx configuration for proxying LLM API requests.

#### Scenario: Proxy ConfigMap is created
- **WHEN** the OpenClawReconciler reconciles an OpenClaw instance
- **THEN** a ConfigMap `openclaw-proxy-config` is created
- **THEN** the ConfigMap has label `app.kubernetes.io/name: openclaw`
- **THEN** the ConfigMap is read from `proxy-configmap.yaml` in the embedded filesystem

### Requirement: Operator creates proxy Deployment with credential injection
The operator SHALL create a Deployment named `openclaw-proxy` running nginx with API credentials injected from Secrets.

#### Scenario: Proxy Deployment is created with correct configuration
- **WHEN** the OpenClawReconciler reconciles an OpenClaw instance
- **THEN** a Deployment `openclaw-proxy` is created with 1 replica
- **THEN** the deployment uses image `mirror.gcr.io/library/nginx:1.27-alpine`
- **THEN** the deployment has label `app.kubernetes.io/name: openclaw`

#### Scenario: Proxy Deployment injects API credentials from Secrets
- **WHEN** the proxy Deployment is created
- **THEN** environment variables are mounted from Secret `openclaw-proxy-secrets` (ANTHROPIC_API_KEY, OPENAI_API_KEY, GEMINI_API_KEY, OPENROUTER_API_KEY, GITHUB_TOKEN, TELEGRAM_BOT_TOKEN)
- **THEN** all Secret references use `optional: true`
- **THEN** GCP credentials are mounted from Secret `openclaw-gcp-credentials` if available

#### Scenario: Proxy Deployment has security constraints
- **WHEN** the proxy Deployment is created
- **THEN** the container runs as non-root with read-only root filesystem
- **THEN** all capabilities are dropped
- **THEN** allowPrivilegeEscalation is false

### Requirement: Operator creates proxy Service
The operator SHALL create a ClusterIP Service named `openclaw-proxy` exposing the proxy on port 8080.

#### Scenario: Proxy Service is created
- **WHEN** the OpenClawReconciler reconciles an OpenClaw instance
- **THEN** a Service `openclaw-proxy` is created with type ClusterIP
- **THEN** the Service selects pods with label `app: openclaw-proxy`
- **THEN** the Service exposes port 8080 targeting container port 8080
- **THEN** the Service has label `app.kubernetes.io/name: openclaw`

