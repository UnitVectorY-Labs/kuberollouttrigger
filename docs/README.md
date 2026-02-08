---
layout: default
title: Home
nav_order: 1
permalink: /
---

# kuberollouttrigger

kuberollouttrigger is a focused bridge between CI image publishing and Kubernetes runtime rollout behavior.

It gives you a reliable way to say: "a new image was pushed, now restart only the Deployments that use it" without adding custom rollout logic to every repository or handing broad cluster credentials to CI.

## Why this exists

Most teams eventually hit the same gap:

- CI can build and push images.
- Kubernetes can run Deployments.
- The "connective tissue" between those systems is often manual, fragile, or overly privileged.

Common patterns are either:

- direct `kubectl` calls from CI with long-lived credentials,
- bespoke per-service automation,
- or broad restart jobs that touch more workloads than necessary.

kuberollouttrigger is designed to solve this with a small, auditable component that keeps trust boundaries clear.

## What you get

- OIDC-authenticated web ingress for rollout events from GitHub Actions.
- Strict, minimal JSON event contract for image updates.
- Decoupled web receiver and worker through Valkey PubSub.
- In-cluster worker that scans allowed namespaces and restarts only matching Deployments, no pre image setup required.
- A single docker image with mode-based behavior for easy deployment.

The result is predictable rollouts with minimal configuration and overhead.

## How it works

1. A GitHub Actions workflow sends an authenticated HTTP request to kuberollouttrigger `web` mode.
2. `web` validates OIDC claims and payload structure ensuring the request is legitimate.
3. The image update event is published to a Valkey PubSub channel.
4. `worker` mode subscribes to that channel.
5. `worker` finds Deployments whose pod templates reference the updated image.
6. `worker` patches each matching Deployment template to trigger a restart rollout.

This approach avoids direct CI-to-cluster mutation while still giving fast, automated deployment refreshes.

## Design priorities

- Security first: strict auth validation and least-privilege cluster interaction.
- Operational simplicity: one simple container to deploy with straightforward configuration.
- Minimal blast radius: touch only Deployments that actually reference the updated image.
- Production defaults: fail fast and explicit configuration over implicit behavior.

## Best fit

kuberollouttrigger is a fit if you want:

- GitHub Actions-driven image publishing,
- Kubernetes Deployments refreshed automatically after image updates,
- centralized rollout-triggering logic across multiple services,
- and a straightforward control-plane component that is easy to reason about.
