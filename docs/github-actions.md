# GitHub Actions Integration

This document describes how to configure a GitHub Actions workflow to trigger kuberollouttrigger after building and pushing a container image.

## Overview

The workflow:

1. Builds and pushes a container image to a registry (e.g., GHCR)
2. Requests an OIDC token from GitHub Actions with the configured audience
3. Sends a POST request to the kuberollouttrigger webhook with the OIDC token and image payload

## Prerequisites

- kuberollouttrigger web mode deployed and accessible from GitHub Actions runners
- `GITHUB_OIDC_AUDIENCE` configured on the web mode to match the audience used in the workflow
- `GITHUB_ALLOWED_ORG` configured to match your GitHub organization
- `ALLOWED_IMAGE_PREFIX` configured to match your container registry prefix

## Example Workflow

```yaml
name: Build and Deploy

on:
  push:
    branches:
      - main

permissions:
  contents: read
  packages: write
  id-token: write  # Required for OIDC token

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

      - name: Get OIDC token
        id: oidc
        run: |
          TOKEN=$(curl -s -H "Authorization: bearer $ACTIONS_ID_TOKEN_REQUEST_TOKEN" \
            "$ACTIONS_ID_TOKEN_REQUEST_URL&audience=https://kuberollouttrigger.example.com" \
            | jq -r '.value')
          echo "::add-mask::$TOKEN"
          echo "token=$TOKEN" >> "$GITHUB_OUTPUT"

      - name: Trigger rollout
        run: |
          HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
            -X POST \
            -H "Authorization: Bearer ${{ steps.oidc.outputs.token }}" \
            -H "Content-Type: application/json" \
            -d '{"image":"ghcr.io/${{ github.repository }}","tag":"dev"}' \
            https://kuberollouttrigger.example.com/event)

          echo "HTTP Status: $HTTP_STATUS"
          if [ "$HTTP_STATUS" != "202" ]; then
            echo "::error::Rollout trigger failed with status $HTTP_STATUS"
            exit 1
          fi
```

## OIDC Token Configuration

### Audience

The `audience` parameter in the OIDC token request must exactly match the `GITHUB_OIDC_AUDIENCE` configured on the kuberollouttrigger web mode. This is a critical security control that prevents tokens issued for other services from being used.

Example:
- Webhook URL: `https://kuberollouttrigger.example.com/event`
- OIDC audience: `https://kuberollouttrigger.example.com`

### Required Permissions

The workflow must include the `id-token: write` permission to request OIDC tokens:

```yaml
permissions:
  id-token: write
```

## Payload Format

The POST request body must be a JSON object with the following fields:

| Field | Type | Required | Description |
|---|---|---|---|
| `image` | string | **Yes** | Full image name including registry and repository path, without tag |
| `tag` | string | **Yes** | Image tag (e.g., `dev`, `latest`, `v1.0.0`) |
| `digest` | string | No | Image digest in `sha256:<64 hex chars>` format |

### Example Payloads

Basic payload:

```json
{
  "image": "ghcr.io/unitvectory-labs/myservice",
  "tag": "dev"
}
```

With digest:

```json
{
  "image": "ghcr.io/unitvectory-labs/myservice",
  "tag": "dev",
  "digest": "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
}
```

### Validation Rules

- `image` must start with the configured `ALLOWED_IMAGE_PREFIX`
- `image` must contain at least one `/` (valid container image reference)
- `tag` must be non-empty
- `digest`, if present, must match `^sha256:[a-f0-9]{64}$`
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

1. **OIDC token masking**: The example workflow masks the OIDC token in logs using `::add-mask::`.
2. **Audience restriction**: Use a unique audience value for your kuberollouttrigger deployment to prevent token reuse.
3. **Organization restriction**: Only tokens from workflows in the configured GitHub organization are accepted.
4. **Image prefix restriction**: Only images under the configured prefix can be specified in payloads.
5. **TLS**: Always use HTTPS for the webhook endpoint in production to protect the OIDC token in transit.

## Troubleshooting

### 401 Unauthorized

- Verify the OIDC audience matches `GITHUB_OIDC_AUDIENCE` exactly
- Verify the repository belongs to the organization configured in `GITHUB_ALLOWED_ORG`
- Ensure `id-token: write` permission is set in the workflow
- Check web-mode logs for `OIDC token validation failed` and compare:
  - `expected_audience` vs `token_claim_audience`
  - `expected_repository_owner` vs `token_claim_repository_owner`
  - `expected_issuer` vs `token_claim_issuer`

### 403 Forbidden

- kuberollouttrigger does not emit `403` in the current implementation; authentication failures are `401`
- If you receive `403`, inspect Ingress/Gateway/WAF policy logs (request likely blocked before reaching the pod)
- Confirm whether the web pod logs contain `request completed` for the failed request
- Use the `X-Request-Id` response header to correlate proxy and pod logs

### 400 Bad Request

- Verify the image name starts with the configured `ALLOWED_IMAGE_PREFIX`
- Verify the JSON payload contains only known fields (`image`, `tag`, `digest`)
- Verify the tag is non-empty

### 502 Bad Gateway

- Check that the Valkey instance is running and accessible from the web mode pod
- Check Valkey connection credentials
