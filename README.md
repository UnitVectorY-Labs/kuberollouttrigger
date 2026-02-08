# kuberollouttrigger

A lightweight GitHub Actions OIDC authenticated webhook and in-cluster worker that automatically restarts matching Kubernetes Deployments when a container image tag is updated.

## Overview

kuberollouttrigger provides a simple, low-ops mechanism to trigger Kubernetes Deployment rollouts for development environments when a GitHub Actions workflow publishes a new container image tag. The system is packaged as a single Go binary with two runtime modes:

- **`web`** — HTTP server that validates GitHub OIDC tokens and publishes events to Valkey PubSub
- **`worker`** — Subscribes to Valkey PubSub and triggers rollout restarts for matching Kubernetes Deployments

## Quick Start

```bash
# Web mode
kuberollouttrigger web \
  --valkey-addr valkey:6379 \
  --github-oidc-audience https://kuberollouttrigger.example.com \
  --github-allowed-org unitvectory-labs \
  --allowed-image-prefix ghcr.io/unitvectory-labs/

# Worker mode
kuberollouttrigger worker \
  --valkey-addr valkey:6379 \
  --allowed-image-prefix ghcr.io/unitvectory-labs/
```

This is then intended to be trigger by a GitHub Action using [kuberollouttrigger-action](https://github.com/UnitVectorY-Labs/kuberollouttrigger-action) so that the deployment is fully automated.

## Documentation

- [Architecture](docs/architecture.md) — System design and component overview
- [Configuration](docs/configuration.md) — Environment variables and CLI flags reference
- [Kubernetes Deployment](docs/deployment.md) — Example manifests for web, worker, RBAC, and Valkey
- [GitHub Actions Integration](docs/github-actions.md) — Workflow examples and payload format
