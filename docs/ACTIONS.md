---
layout: default
title: Actions
nav_order: 5
permalink: /actions
---

# GitHub Actions Integration

This document describes how to configure a GitHub Actions workflow to trigger kuberollouttrigger after building and pushing a container image.

## Overview

Use the [UnitVectorY-Labs/kuberollouttrigger-action](https://github.com/UnitVectorY-Labs/kuberollouttrigger-action) GitHub Action to automatically get the necessary OIDC token and send the webhook request to your kuberollouttrigger web mode endpoint.

The expected workflow:

1. Builds and pushes a development container image to a registry (for example GHCR)
2. Invokes `kuberollouttrigger-action`, which handles GitHub OIDC token acquisition and webhook delivery
3. kuberollouttrigger then takes care of ensuring the pod is rolled out with the latest image

## Prerequisites

- kuberollouttrigger web mode deployed and accessible from GitHub Actions runners
- `GITHUB_OIDC_AUDIENCE` configured on web mode to match the audience used by the action
- `GITHUB_ALLOWED_ORG` configured to match your GitHub organization
- `ALLOWED_IMAGE_PREFIX` configured to match your container registry prefix

## Example Workflow (Reference Implementation)

```yaml
name: Build and Deploy

on:
  push:
    branches:
      - main

permissions:
  contents: read
  packages: write
  id-token: write # Required for GitHub OIDC

jobs:
  build-and-trigger:
    runs-on: ubuntu-latest

    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Log in to GHCR
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Build and push image
        uses: docker/build-push-action@v6
        with:
          context: .
          push: true
          tags: ghcr.io/${{ github.repository }}:dev

      - name: Trigger kuberollouttrigger
        uses: UnitVectorY-Labs/kuberollouttrigger-action@main
        with:
          webhook-url: https://kuberollouttrigger.example.com/event
          audience: https://kuberollouttrigger.example.com
          image: ghcr.io/${{ github.repository }}
          tags: dev
```

For production, pin the action to a stable release tag or commit SHA instead of `@main`.

## Action Configuration Notes

- Keep `permissions.id-token: write` enabled so the action can request a GitHub OIDC token.
- Set `audience` to exactly match `GITHUB_OIDC_AUDIENCE` configured on web mode.
- Keep image/tags values aligned with what your build step publishes.
- Refer to the action README for the latest supported inputs and outputs:
  - [kuberollouttrigger-action documentation](https://github.com/UnitVectorY-Labs/kuberollouttrigger-action)

## Payload Format

The payload sent by the action is validated by kuberollouttrigger with this schema:

| Field | Type | Required | Description |
|---|---|---|---|
| `image` | string | **Yes** | Full image name including registry and repository path, without tag |
| `tags` | array of strings | **Yes** | Image tags (for example `["dev"]`, `["v1.0.0", "latest"]`) |

### Example Payload

Single tag:

```json
{
  "image": "ghcr.io/unitvectory-labs/myservice",
  "tags": ["dev"]
}
```

Multiple tags:

```json
{
  "image": "ghcr.io/unitvectory-labs/myservice",
  "tags": ["v1.0.0", "v1.0", "v1", "latest"]
}
```

### Validation Rules

- `image` must start with the configured `ALLOWED_IMAGE_PREFIX`
- `image` must contain at least one `/` (valid container image reference)
- `tags` must be a non-empty array
- Each tag in the `tags` array must be non-empty
- Unknown fields are rejected (strict schema validation)

## Response Codes

| Status Code | Meaning |
|---|---|
| `202 Accepted` | Event was validated and published to Valkey |
| `400 Bad Request` | Invalid payload (schema validation failed, wrong image prefix) |
| `401 Unauthorized` | Authentication failed (invalid token, wrong org, wrong audience) |
| `403 Forbidden` | Typically returned by an upstream Ingress/Gateway policy, not kuberollouttrigger itself |
| `405 Method Not Allowed` | Wrong HTTP method (must be POST) |
| `502 Bad Gateway` | Failed to publish to Valkey |

## Security Considerations

1. **Audience restriction**: Use a unique audience value for your kuberollouttrigger deployment to prevent token reuse.
2. **Organization restriction**: Only tokens from workflows in the configured GitHub organization are accepted.
3. **Image prefix restriction**: Only images under the configured prefix can be specified in payloads.
4. **TLS**: Always use HTTPS for the webhook endpoint in production.
5. **Immutable action pinning**: Pin the action to a release tag or commit SHA.

## Troubleshooting

### 401 Unauthorized

- Verify the `audience` input matches `GITHUB_OIDC_AUDIENCE` exactly
- Verify the repository belongs to the organization configured in `GITHUB_ALLOWED_ORG`
- Ensure `id-token: write` permission is set in the workflow
- Check web-mode logs for `OIDC token validation failed`

### 403 Forbidden

- kuberollouttrigger does not emit `403` in the current implementation; authentication failures are `401`
- If you receive `403`, inspect Ingress/Gateway/WAF policy logs (request likely blocked before reaching the pod)

### 400 Bad Request

- Verify the image name starts with the configured `ALLOWED_IMAGE_PREFIX`
- Verify the payload fields are valid (`image`, `tags`)
- Verify `tags` is a non-empty array with no empty strings

### 502 Bad Gateway

- Check that the Valkey instance is running and accessible from the web mode pod
- Check Valkey connection credentials
