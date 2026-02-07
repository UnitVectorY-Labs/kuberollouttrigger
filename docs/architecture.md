# Architecture

kuberollouttrigger is a lightweight system that automatically triggers Kubernetes Deployment rollouts when a new container image is published from a GitHub Actions workflow.

## Overview

The system consists of two runtime modes packaged in a single Go binary:

1. **Web Mode** — An HTTP server that receives authenticated webhook events from GitHub Actions and publishes them to Valkey PubSub.
2. **Worker Mode** — A subscriber that listens for events on Valkey PubSub and triggers rollout restarts for matching Kubernetes Deployments.

## Architecture Diagram

```mermaid
graph LR
    GHA[GitHub Actions] -->|OIDC Token + JSON Payload| WEB[Web Mode<br/>POST /event]
    WEB -->|Validate OIDC Token| JWKS[GitHub OIDC JWKS]
    WEB -->|Publish JSON| VK[Valkey PubSub]
    VK -->|Subscribe| WORKER[Worker Mode]
    WORKER -->|List Deployments| K8S[Kubernetes API]
    WORKER -->|Patch Deployment<br/>Trigger Rollout| K8S
```

## Component Details

### Web Mode

The web mode exposes two HTTP endpoints:

- `POST /event` — Receives authenticated webhook events
- `GET /healthz` — Health check endpoint

**Request flow:**

1. GitHub Actions workflow sends a POST request with an OIDC Bearer token and a JSON payload specifying the updated image.
2. The web server validates the OIDC token:
   - Verifies the JWT signature against GitHub's JWKS endpoint
   - Validates standard claims (exp, iat, nbf)
   - Checks that the audience matches the configured value
   - Enforces that the `repository_owner` claim matches the configured allowed organization
3. The JSON payload is validated:
   - Strict schema validation (unknown fields are rejected)
   - The `image` field must start with the configured allowed prefix
   - The `tag` field must be non-empty
   - Optional `digest` field must match `sha256:<64 hex chars>` format if present
4. On success, the payload is published to the configured Valkey PubSub channel and HTTP 202 (Accepted) is returned.

**Security considerations:**

- Authentication material (OIDC tokens) is never forwarded to Valkey
- Only the validated JSON payload is published
- JWKS keys are cached with a 1-hour TTL to reduce external calls
- Request payloads are limited to 1MB

### Worker Mode

The worker mode subscribes to a Valkey PubSub channel and processes incoming events:

1. Receives a JSON message from the Valkey channel
2. Validates the message payload (same schema validation as web mode)
3. Constructs the full image reference (`image:tag`)
4. Lists all Deployments across accessible namespaces
5. Finds Deployments with containers whose image exactly matches the event image reference
6. Patches each matching Deployment's pod template annotations to trigger a rollout restart

**Matching rules:**

- The image reference is constructed as `event.image + ":" + event.tag`
- Container images must match exactly (no prefix or wildcard matching)
- Multiple Deployments across multiple namespaces can match a single event

**Restart mechanism:**

- The worker patches the Deployment's `spec.template.metadata.annotations` with `kubectl.kubernetes.io/restartedAt` set to the current UTC timestamp
- This triggers a rolling update identical to `kubectl rollout restart`

### Valkey

Valkey is used as a PubSub message broker between the web and worker components. It provides:

- Decoupling between the webhook receiver and the cluster restarter
- The ability to run multiple workers subscribing to the same channel
- Simple infrastructure with no persistence requirements

**Important:** Valkey PubSub is fire-and-forget. Messages are not persisted, so if the worker is not connected when a message is published, the message is lost. This is acceptable for development environments where occasional missed events can be handled via manual restarts.

## Data Flow

### Event Payload

The JSON payload published to Valkey matches the request payload:

```json
{
  "image": "ghcr.io/unitvectory-labs/myservice",
  "tag": "dev"
}
```

Optional fields:

```json
{
  "image": "ghcr.io/unitvectory-labs/myservice",
  "tag": "dev",
  "digest": "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
}
```

The payload format is designed for future extensibility — new optional fields can be added without breaking existing functionality.

## Security Model

1. **Authentication**: GitHub Actions OIDC tokens are validated against GitHub's JWKS endpoint with signature verification, audience matching, and organization restriction.
2. **Authorization**: Only tokens from the configured GitHub organization are accepted. The `repository_owner` claim is used to enforce this.
3. **Payload validation**: Strict JSON schema validation prevents injection of unexpected fields. Image prefixes are restricted to the configured allowed prefix.
4. **Transport**: No authentication material is passed to Valkey. Only the validated JSON event payload is published.
5. **Kubernetes RBAC**: The worker uses a dedicated service account with least-privilege permissions (get, list, watch Deployments, and patch for restart).

## Dev Mode

For local development and testing, a dev mode (`--dev-mode` flag or `DEV_MODE=true` environment variable) is available for the web mode. In dev mode:

- OIDC signature verification is disabled (tokens are parsed but not cryptographically verified)
- Organization restriction is still enforced
- All other validation (payload schema, image prefix) remains active

**Warning:** Dev mode must never be used in production environments.
