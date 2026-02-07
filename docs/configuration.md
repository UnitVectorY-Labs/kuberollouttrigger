# Configuration

kuberollouttrigger is configured through environment variables and command-line flags. Command-line flags take precedence over environment variables, which take precedence over defaults.

## Common Configuration (Both Modes)

| Environment Variable | CLI Flag | Required | Default | Description |
|---|---|---|---|---|
| `LOG_LEVEL` | `--log-level` | No | `info` | Log level (`debug`, `info`, `warn`, `error`) |
| `VALKEY_ADDR` | `--valkey-addr` | **Yes** | — | Valkey address in `host:port` format |
| `VALKEY_CHANNEL` | `--valkey-channel` | No | `kuberollouttrigger` | Valkey PubSub channel name |
| `VALKEY_USERNAME` | `--valkey-username` | No | — | Valkey authentication username |
| `VALKEY_PASSWORD` | `--valkey-password` | No | — | Valkey authentication password |
| `VALKEY_TLS_ENABLED` | `--valkey-tls` | No | `false` | Enable TLS for Valkey connection |
| `ALLOWED_IMAGE_PREFIX` | `--allowed-image-prefix` | **Yes** | — | Required prefix for image names in payloads (e.g., `ghcr.io/unitvectory-labs/`) |

## Web Mode Configuration

| Environment Variable | CLI Flag | Required | Default | Description |
|---|---|---|---|---|
| `WEB_LISTEN_ADDR` | `--listen-addr` | No | `:8080` | HTTP server listen address |
| `GITHUB_OIDC_AUDIENCE` | `--github-oidc-audience` | **Yes** | — | Required OIDC audience claim for token validation |
| `GITHUB_ALLOWED_ORG` | `--github-allowed-org` | **Yes** | — | GitHub organization that must match the token's `repository_owner` claim |
| `DEV_MODE` | `--dev-mode` | No | `false` | Disable OIDC signature verification (for development only) |

## Worker Mode Configuration

| Environment Variable | CLI Flag | Required | Default | Description |
|---|---|---|---|---|
| `KUBECONFIG` | `--kubeconfig` | No | — | Path to kubeconfig file. If empty, in-cluster configuration is used |

## Precedence

Configuration values are resolved in the following order (highest priority first):

1. **Command-line flags** — Explicitly passed flags always win
2. **Environment variables** — Used when the flag is not set
3. **Defaults** — Only for optional configuration items

## Startup Validation

Both modes validate all required configuration at startup and fail fast with a clear error message listing all missing values. For example:

```
error: missing required configuration: VALKEY_ADDR / --valkey-addr, GITHUB_OIDC_AUDIENCE / --github-oidc-audience
```

## Configuration Summary Logging

On startup, both modes log a configuration summary. Secrets (passwords) are never logged. Example:

```json
{
  "level": "INFO",
  "msg": "web mode configuration",
  "listen_addr": ":8080",
  "valkey_addr": "valkey:6379",
  "valkey_channel": "kuberollouttrigger",
  "valkey_tls": false,
  "github_oidc_audience": "https://kuberollouttrigger.example.com",
  "github_allowed_org": "unitvectory-labs",
  "allowed_image_prefix": "ghcr.io/unitvectory-labs/",
  "dev_mode": false,
  "log_level": "info"
}
```

## Request Logging (Web Mode)

Web mode emits one log entry per HTTP request with:

- `request_id` (also returned to the client as `X-Request-Id`)
- `method`, `path`, `status`, `duration_ms`
- `remote_addr`, `user_agent`

For failed token validation, web mode logs safe token diagnostics (no raw token content), including expected audience/org/issuer and unverified token claim metadata to simplify troubleshooting.

## Examples

### Web Mode with Environment Variables

```bash
export VALKEY_ADDR="valkey:6379"
export GITHUB_OIDC_AUDIENCE="https://kuberollouttrigger.example.com"
export GITHUB_ALLOWED_ORG="unitvectory-labs"
export ALLOWED_IMAGE_PREFIX="ghcr.io/unitvectory-labs/"

kuberollouttrigger web
```

### Web Mode with CLI Flags

```bash
kuberollouttrigger web \
  --valkey-addr valkey:6379 \
  --github-oidc-audience https://kuberollouttrigger.example.com \
  --github-allowed-org unitvectory-labs \
  --allowed-image-prefix ghcr.io/unitvectory-labs/
```

### Worker Mode with Environment Variables

```bash
export VALKEY_ADDR="valkey:6379"
export ALLOWED_IMAGE_PREFIX="ghcr.io/unitvectory-labs/"

kuberollouttrigger worker
```

### Worker Mode with CLI Flags (Out-of-Cluster)

```bash
kuberollouttrigger worker \
  --valkey-addr localhost:6379 \
  --allowed-image-prefix ghcr.io/unitvectory-labs/ \
  --kubeconfig ~/.kube/config
```
