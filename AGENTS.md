# AGENTS.md — kuberollouttrigger

kuberollouttrigger is a single Go binary with two modes:

- `web`: accepts GitHub Actions OIDC-authenticated HTTP requests and publishes the JSON payload to Valkey PubSub
- `worker`: subscribes to Valkey PubSub and triggers Kubernetes Deployment rollouts by patching the pod template

## Core goal of application

1. Receive a JSON payload from a GitHub Actions workflow via an OIDC-authenticated HTTP request.
2. That payload contains a JSON object specifying which image was updated, for example: {"image": "ghcr.io/unitvectory-labs/kuberollouttrigger-snapshot:dev"}
3. Validate the authentication of the OIDC is in the correct claims and the JSON payload is strictly in the correct format including limiting the prefix (domain and path) is allowed.
4. Publish the JSON payload to a Valkey PubSub channel.
5. A worker process subscribes to the same Valkey PubSub channel, receives the JSON
6. The worker process scans the Kubernetes namespaces it is allowed to access for Deployments with pod templates that reference the updated image.
7. For each matching Deployment, the worker triggers a rollout for that Deployment with a restart. No other changes should be made to the Deployment spec, only a restart to trigger the rollout.

## Conventions
- A single docker image is built from the same codebase and can be run in either `web` or `worker` mode based on a subcommand that must be specified
- Prefer Go stdlib; avoid extra third-party libs unless clearly necessary.
- Config is env-first with 1:1 CLI flag equivalents; flags override env.
- Fail fast on missing required config (mode-specific) with clear errors.
- Never forward auth material to Valkey; only publish the JSON event.
- Projects main README.md is kept simple with more detailed documentation contained in docs/ folder that is always updated when changes are made to the codebase to keep it in sync.
- Document the kubernetes YAML files and GitHub Actions workflow file as part of docs/ to provide a concise but practical example of how to use this application.
- Security is a top priority, the web mode should be hardened to only accept requests from GitHub Actions with the correct OIDC claims and otherwise return a minimal access denied response. The worker mode should also be secure in how it interacts with the Kubernetes API, ensuring it only has permissions to patch Deployments and nothing more.

## Local testing (recommended)

Stand up Valkey locally:

```bash
docker pull valkey/valkey:9
docker run --rm -p 6379:6379 --name valkey valkey/valkey:9
```

Then run:
	•	web publishing to 127.0.0.1:6379
	•	worker subscribing to the same channel

For development/agent sessions, keep a non-production-friendly way to exercise the full web → Valkey → worker flow even when a real GitHub OIDC token/JWKS or a live Kubernetes API is not available. Keep production defaults strict and make any dev-only behavior explicit.
