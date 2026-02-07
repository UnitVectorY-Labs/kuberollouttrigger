# Kubernetes Deployment

This document provides example Kubernetes manifests for deploying kuberollouttrigger in a Kubernetes cluster.

## Prerequisites

- A running Valkey instance accessible from the cluster
- A GitHub Actions workflow configured with OIDC token generation
- Container images pushed to a registry accessible from the cluster

## Web Mode Deployment

### Deployment and Service

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kuberollouttrigger-web
  namespace: kuberollouttrigger
  labels:
    app: kuberollouttrigger-web
spec:
  replicas: 1
  selector:
    matchLabels:
      app: kuberollouttrigger-web
  template:
    metadata:
      labels:
        app: kuberollouttrigger-web
    spec:
      containers:
        - name: web
          image: ghcr.io/unitvectory-labs/kuberollouttrigger:latest
          args: ["web"]
          ports:
            - containerPort: 8080
              name: http
          env:
            - name: VALKEY_ADDR
              value: "valkey:6379"
            - name: VALKEY_CHANNEL
              value: "kuberollouttrigger"
            - name: GITHUB_OIDC_AUDIENCE
              value: "https://kuberollouttrigger.example.com"
            - name: GITHUB_ALLOWED_ORG
              value: "unitvectory-labs"
            - name: ALLOWED_IMAGE_PREFIX
              value: "ghcr.io/unitvectory-labs/"
            # Optional: Valkey authentication from a Secret
            # - name: VALKEY_USERNAME
            #   valueFrom:
            #     secretKeyRef:
            #       name: valkey-credentials
            #       key: username
            # - name: VALKEY_PASSWORD
            #   valueFrom:
            #     secretKeyRef:
            #       name: valkey-credentials
            #       key: password
          livenessProbe:
            httpGet:
              path: /healthz
              port: http
            initialDelaySeconds: 5
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /healthz
              port: http
            initialDelaySeconds: 5
            periodSeconds: 10
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              cpu: 200m
              memory: 128Mi
          securityContext:
            runAsNonRoot: true
            readOnlyRootFilesystem: true
            allowPrivilegeEscalation: false
            capabilities:
              drop:
                - ALL
---
apiVersion: v1
kind: Service
metadata:
  name: kuberollouttrigger-web
  namespace: kuberollouttrigger
  labels:
    app: kuberollouttrigger-web
spec:
  selector:
    app: kuberollouttrigger-web
  ports:
    - port: 80
      targetPort: http
      protocol: TCP
      name: http
  type: ClusterIP
```

### Ingress (Optional)

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: kuberollouttrigger-web
  namespace: kuberollouttrigger
  annotations:
    # Adjust annotations for your ingress controller
    nginx.ingress.kubernetes.io/ssl-redirect: "true"
spec:
  ingressClassName: nginx
  tls:
    - hosts:
        - kuberollouttrigger.example.com
      secretName: kuberollouttrigger-tls
  rules:
    - host: kuberollouttrigger.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: kuberollouttrigger-web
                port:
                  name: http
```

## Worker Mode Deployment

### ServiceAccount and RBAC

The worker requires a dedicated service account with least-privilege RBAC permissions.

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: kuberollouttrigger-worker
  namespace: kuberollouttrigger
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kuberollouttrigger-worker
rules:
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "list", "watch", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kuberollouttrigger-worker
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kuberollouttrigger-worker
subjects:
  - kind: ServiceAccount
    name: kuberollouttrigger-worker
    namespace: kuberollouttrigger
```

#### RBAC Permissions Explained

| Verb | Resource | Reason |
|---|---|---|
| `get` | deployments | Required to read individual Deployment specs |
| `list` | deployments | Required to enumerate Deployments across namespaces |
| `watch` | deployments | Required for potential future informer-based discovery |
| `patch` | deployments | Required to set the restart annotation on matching Deployments |

**Important security note:** The `patch` verb on Deployments allows the worker to modify any field in the Deployment spec, not just the restart annotation. This is a Kubernetes RBAC limitation — there is no built-in mechanism to restrict `patch` to specific fields. The kuberollouttrigger worker only patches `spec.template.metadata.annotations` to trigger rollouts, but the RBAC permissions technically allow broader modifications. This is mitigated by:

1. The worker code only performs a targeted strategic merge patch on the annotation field
2. The worker runs with a dedicated service account, isolating its permissions
3. RBAC scope can be further restricted using namespace-scoped RoleBindings instead of a ClusterRoleBinding to limit the blast radius

To restrict the worker to specific namespaces, replace the `ClusterRoleBinding` with individual `RoleBinding` resources in each target namespace:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: kuberollouttrigger-worker
  namespace: dev  # Restrict to this namespace
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kuberollouttrigger-worker
subjects:
  - kind: ServiceAccount
    name: kuberollouttrigger-worker
    namespace: kuberollouttrigger
```

### Worker Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kuberollouttrigger-worker
  namespace: kuberollouttrigger
  labels:
    app: kuberollouttrigger-worker
spec:
  replicas: 1
  selector:
    matchLabels:
      app: kuberollouttrigger-worker
  template:
    metadata:
      labels:
        app: kuberollouttrigger-worker
    spec:
      serviceAccountName: kuberollouttrigger-worker
      containers:
        - name: worker
          image: ghcr.io/unitvectory-labs/kuberollouttrigger:latest
          args: ["worker"]
          env:
            - name: VALKEY_ADDR
              value: "valkey:6379"
            - name: VALKEY_CHANNEL
              value: "kuberollouttrigger"
            - name: ALLOWED_IMAGE_PREFIX
              value: "ghcr.io/unitvectory-labs/"
            # Optional: Valkey authentication from a Secret
            # - name: VALKEY_USERNAME
            #   valueFrom:
            #     secretKeyRef:
            #       name: valkey-credentials
            #       key: username
            # - name: VALKEY_PASSWORD
            #   valueFrom:
            #     secretKeyRef:
            #       name: valkey-credentials
            #       key: password
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              cpu: 200m
              memory: 128Mi
          securityContext:
            runAsNonRoot: true
            readOnlyRootFilesystem: true
            allowPrivilegeEscalation: false
            capabilities:
              drop:
                - ALL
```

## Valkey Connection Configuration

### Using a Valkey Secret

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: valkey-credentials
  namespace: kuberollouttrigger
type: Opaque
stringData:
  username: "default"
  password: "your-valkey-password"
```

### With TLS Enabled

Add the following environment variable to the web or worker deployment:

```yaml
- name: VALKEY_TLS_ENABLED
  value: "true"
```

## Example Target Deployment

This is an example of a Deployment that kuberollouttrigger would automatically restart when a new `dev` tag is pushed:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myservice
  namespace: dev
spec:
  replicas: 2
  selector:
    matchLabels:
      app: myservice
  template:
    metadata:
      labels:
        app: myservice
    spec:
      containers:
        - name: myservice
          image: ghcr.io/unitvectory-labs/myservice:dev
          imagePullPolicy: Always
          ports:
            - containerPort: 8080
```

**Note:** `imagePullPolicy: Always` is recommended for development tags like `dev` to ensure the latest image is always pulled during a rollout restart.

## Namespace Setup

Create the namespace for kuberollouttrigger components:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: kuberollouttrigger
```
