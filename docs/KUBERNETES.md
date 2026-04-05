# Bakery Service — Kubernetes Deployment

## Table of Contents

1. [Overview](#overview)
2. [Multi-Stage Dockerfiles](#multi-stage-dockerfiles)
3. [Namespace and Labels](#namespace-and-labels)
4. [Secrets and ConfigMaps](#secrets-and-configmaps)
5. [Database Initialisation](#database-initialisation)
6. [Service Deployments](#service-deployments)
7. [Kubernetes Services (Networking)](#kubernetes-services-networking)
8. [Infrastructure — PostgreSQL and RabbitMQ](#infrastructure--postgresql-and-rabbitmq)
9. [Ingress](#ingress)
10. [Resource Requests and Limits](#resource-requests-and-limits)
11. [Health Probes](#health-probes)
12. [Recommended Directory Layout](#recommended-directory-layout)
13. [Deployment Checklist](#deployment-checklist)

---

## Overview

This document describes how to deploy the Bakery Service on a Kubernetes cluster. It covers:

- Multi-stage Docker builds to produce minimal production images
- Kubernetes `Secret` objects for sensitive credentials
- `ConfigMap` objects for non-sensitive configuration
- A Kubernetes `Job` for one-time database schema initialisation
- `Deployment` manifests for all five application services
- `Service` manifests for inter-pod networking
- Health probes tied to the `/healthz` and gRPC health endpoints

> **Prerequisite:** The improvements described in `docs/IMPROVEMENTS.md` — specifically health check endpoints (H-7) and graceful shutdown (H-6) — must be implemented before this deployment model is fully operational.

---

## Multi-Stage Dockerfiles

The existing Dockerfiles use a single stage, which results in large images containing the full Go toolchain. Multi-stage builds produce lean production images (~10–20 MB with Alpine).

### Pattern — Applied to All Services

```dockerfile
# syntax=docker/dockerfile:1

# ── Stage 1: Build ──────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

# Install CA certificates (needed for HTTPS calls) and build tools
RUN apk add --no-cache ca-certificates git

WORKDIR /build

# Cache dependencies first — only re-downloaded when go.mod/go.sum change
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire source tree
COPY . .

# Build the target binary; CGO disabled for a fully static binary
ARG SERVICE_PATH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /app/service ${SERVICE_PATH}

# ── Stage 2: Runtime ────────────────────────────────────────────────────────
FROM scratch

# Copy CA certs from builder (for TLS)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the compiled binary
COPY --from=builder /app/service /service

# Run as non-root user
USER 65534:65534

ENTRYPOINT ["/service"]
```

### Per-Service Dockerfiles

Each service passes a different `SERVICE_PATH` build argument:

#### `server.dockerfile`
```dockerfile
# syntax=docker/dockerfile:1
FROM golang:1.22-alpine AS builder
RUN apk add --no-cache ca-certificates git
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /app/server ./server/main.go

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/server /server
USER 65534:65534
EXPOSE 50051
ENTRYPOINT ["/server"]
```

#### `broker.dockerfile`
```dockerfile
FROM golang:1.22-alpine AS builder
RUN apk add --no-cache ca-certificates
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /app/broker ./broker/main.go

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/broker /broker
USER 65534:65534
ENTRYPOINT ["/broker"]
```

#### `makers.dockerfile`
```dockerfile
FROM golang:1.22-alpine AS builder
RUN apk add --no-cache ca-certificates
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /app/makers ./makers/main.go

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/makers /makers
USER 65534:65534
ENTRYPOINT ["/makers"]
```

#### `buyers.dockerfile`
```dockerfile
FROM golang:1.22-alpine AS builder
RUN apk add --no-cache ca-certificates
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /app/buyers ./buyers/main.go

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/buyers /buyers
USER 65534:65534
ENTRYPOINT ["/buyers"]
```

#### `frontend.dockerfile`

The Frontend requires its template files at runtime. Use a minimal Alpine base instead of `scratch` to support the file system layout:

```dockerfile
FROM golang:1.22-alpine AS builder
RUN apk add --no-cache ca-certificates
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /app/frontend ./frontend/cmd/web/main.go

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/frontend .
# Copy templates into the image
COPY frontend/templates ./templates
USER 65534:65534
EXPOSE 8080
ENTRYPOINT ["/app/frontend"]
```

> **Note:** If templates are loaded from the filesystem at runtime using a relative path, ensure the `WORKDIR` and template glob pattern align. Consider embedding templates using `//go:embed` to avoid this coupling.

---

## Namespace and Labels

All resources are deployed into a dedicated namespace:

```yaml
# k8s/namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: bakery
  labels:
    app.kubernetes.io/part-of: bakery-service
```

**Standard label set** applied to all resources:

```yaml
labels:
  app.kubernetes.io/name: <service-name>          # e.g. "server", "broker"
  app.kubernetes.io/part-of: bakery-service
  app.kubernetes.io/version: "1.0.0"
  app.kubernetes.io/managed-by: kubectl
```

---

## Secrets and ConfigMaps

### Strategy

| Type        | Use for                                                     |
|-------------|-------------------------------------------------------------|
| `Secret`    | Passwords, connection strings, JWT key, CSRF key            |
| `ConfigMap` | Service addresses, non-sensitive configuration, feature flags |

> **Production Note:** Do not commit Secret manifests with actual values to version control. Use an external secrets manager (e.g., HashiCorp Vault, AWS Secrets Manager, Sealed Secrets) and inject values at deploy time.

---

### `bakery-secrets` Secret

```yaml
# k8s/secrets/bakery-secrets.yaml
apiVersion: v1
kind: Secret
metadata:
  name: bakery-secrets
  namespace: bakery
  labels:
    app.kubernetes.io/part-of: bakery-service
type: Opaque
stringData:
  # PostgreSQL connection string
  # Format: postgres://<user>:<password>@<host>:<port>/<dbname>?sslmode=require
  DSN: "postgres://bakery_user:CHANGE_ME@postgres-svc:5432/bakery?sslmode=require"

  # RabbitMQ connection string
  RABBITMQ_SERVICE_ADDR: "amqp://bakery_user:CHANGE_ME@rabbitmq-svc:5672/"

  # JWT signing secret — generate with: openssl rand -hex 32
  JWT_SECRET: "CHANGE_ME_USE_32_BYTE_RANDOM_STRING"

  # CSRF protection key — generate with: openssl rand -hex 32
  CSRF_KEY: "CHANGE_ME_USE_32_BYTE_RANDOM_STRING"

  # PostgreSQL admin credentials (used only by the db-init Job)
  POSTGRES_PASSWORD: "CHANGE_ME"
  POSTGRES_USER: "bakery_user"
  POSTGRES_DB: "bakery"
```

---

### `bakery-config` ConfigMap

```yaml
# k8s/configmaps/bakery-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: bakery-config
  namespace: bakery
  labels:
    app.kubernetes.io/part-of: bakery-service
data:
  # gRPC server address (internal Kubernetes DNS)
  BAKERY_SERVICE_ADDR: "bakery-server-svc:50051"

  # Frontend HTTP port
  FRONTEND_PORT: "8080"

  # Log level for all services
  LOG_LEVEL: "info"

  # Low stock threshold (bread qty below which make-bread-order is published)
  LOW_STOCK_THRESHOLD: "10"

  # Replenishment quantity per bread type
  REPLENISHMENT_QTY: "20"
```

---

### Injecting Secrets and ConfigMaps into Pods

All Deployments reference these resources in their `env` sections:

```yaml
env:
  - name: DSN
    valueFrom:
      secretKeyRef:
        name: bakery-secrets
        key: DSN
  - name: RABBITMQ_SERVICE_ADDR
    valueFrom:
      secretKeyRef:
        name: bakery-secrets
        key: RABBITMQ_SERVICE_ADDR
  - name: JWT_SECRET
    valueFrom:
      secretKeyRef:
        name: bakery-secrets
        key: JWT_SECRET
  - name: BAKERY_SERVICE_ADDR
    valueFrom:
      configMapKeyRef:
        name: bakery-config
        key: BAKERY_SERVICE_ADDR
  - name: LOG_LEVEL
    valueFrom:
      configMapKeyRef:
        name: bakery-config
        key: LOG_LEVEL
```

---

## Database Initialisation

Database schema creation is performed exactly once using a Kubernetes `Job`. This job runs `bakery.sql` against the PostgreSQL instance before any application services start.

### Ordering with `initContainers`

Application service `Deployment`s use `initContainers` to wait for the schema job to complete and for PostgreSQL to be ready:

```yaml
initContainers:
  - name: wait-for-db
    image: postgres:15-alpine
    command:
      - sh
      - -c
      - |
        until pg_isready -h postgres-svc -p 5432 -U $(POSTGRES_USER); do
          echo "Waiting for PostgreSQL..."
          sleep 2
        done
    env:
      - name: POSTGRES_USER
        valueFrom:
          secretKeyRef:
            name: bakery-secrets
            key: POSTGRES_USER
```

### Schema Init Job

```yaml
# k8s/jobs/db-init-job.yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: bakery-db-init
  namespace: bakery
  labels:
    app.kubernetes.io/name: db-init
    app.kubernetes.io/part-of: bakery-service
spec:
  ttlSecondsAfterFinished: 300     # Clean up completed job after 5 minutes
  backoffLimit: 3
  template:
    metadata:
      labels:
        app.kubernetes.io/name: db-init
    spec:
      restartPolicy: OnFailure
      initContainers:
        - name: wait-for-postgres
          image: postgres:15-alpine
          command:
            - sh
            - -c
            - |
              until pg_isready -h postgres-svc -p 5432 -U $(POSTGRES_USER); do
                echo "Waiting for PostgreSQL to be ready..."
                sleep 3
              done
          env:
            - name: POSTGRES_USER
              valueFrom:
                secretKeyRef:
                  name: bakery-secrets
                  key: POSTGRES_USER
      containers:
        - name: schema-apply
          image: postgres:15-alpine
          command:
            - sh
            - -c
            - |
              echo "Applying database schema..."
              psql "$DSN" -f /schema/bakery.sql
              echo "Schema applied successfully."
          env:
            - name: DSN
              valueFrom:
                secretKeyRef:
                  name: bakery-secrets
                  key: DSN
          volumeMounts:
            - name: schema-volume
              mountPath: /schema
      volumes:
        - name: schema-volume
          configMap:
            name: bakery-schema
---
# The SQL schema is stored in a ConfigMap for the Job to consume.
# For large schemas, consider using an initContainer that clones the repo or
# baking the schema into a dedicated migration image instead.
apiVersion: v1
kind: ConfigMap
metadata:
  name: bakery-schema
  namespace: bakery
binaryData:
  bakery.sql: |
    -- Contents of bakery.sql here (base64-encoded when using binaryData,
    -- or use data: with the raw SQL if it fits within ConfigMap size limits)
```

> **Alternative:** For production, use a dedicated database migration tool such as [golang-migrate](https://github.com/golang-migrate/migrate) or [goose](https://github.com/pressly/goose) embedded in a migration image. This provides versioned, reversible migrations rather than a single idempotent schema file.

---

## Service Deployments

### Server Deployment

```yaml
# k8s/deployments/server-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: bakery-server
  namespace: bakery
  labels:
    app.kubernetes.io/name: server
    app.kubernetes.io/part-of: bakery-service
spec:
  replicas: 1                     # Single replica until H-2 race condition is fixed
  selector:
    matchLabels:
      app.kubernetes.io/name: server
  template:
    metadata:
      labels:
        app.kubernetes.io/name: server
        app.kubernetes.io/part-of: bakery-service
    spec:
      initContainers:
        - name: wait-for-db
          image: postgres:15-alpine
          command: [sh, -c, "until pg_isready -h postgres-svc -p 5432; do sleep 2; done"]
        - name: wait-for-rabbitmq
          image: busybox:1.36
          command: [sh, -c, "until nc -z rabbitmq-svc 5672; do sleep 2; done"]
      containers:
        - name: server
          image: bakery-server:latest
          imagePullPolicy: IfNotPresent
          ports:
            - name: grpc
              containerPort: 50051
              protocol: TCP
          env:
            - name: DSN
              valueFrom:
                secretKeyRef: { name: bakery-secrets, key: DSN }
            - name: RABBITMQ_SERVICE_ADDR
              valueFrom:
                secretKeyRef: { name: bakery-secrets, key: RABBITMQ_SERVICE_ADDR }
            - name: JWT_SECRET
              valueFrom:
                secretKeyRef: { name: bakery-secrets, key: JWT_SECRET }
          resources:
            requests:
              cpu: "100m"
              memory: "64Mi"
            limits:
              cpu: "500m"
              memory: "256Mi"
          livenessProbe:
            grpc:
              port: 50051
            initialDelaySeconds: 15
            periodSeconds: 20
          readinessProbe:
            grpc:
              port: 50051
            initialDelaySeconds: 5
            periodSeconds: 10
```

### Broker Deployment

```yaml
# k8s/deployments/broker-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: bakery-broker
  namespace: bakery
  labels:
    app.kubernetes.io/name: broker
    app.kubernetes.io/part-of: bakery-service
spec:
  replicas: 1                     # Single replica — concurrent consumers need coordination
  selector:
    matchLabels:
      app.kubernetes.io/name: broker
  template:
    metadata:
      labels:
        app.kubernetes.io/name: broker
    spec:
      initContainers:
        - name: wait-for-db
          image: postgres:15-alpine
          command: [sh, -c, "until pg_isready -h postgres-svc -p 5432; do sleep 2; done"]
        - name: wait-for-rabbitmq
          image: busybox:1.36
          command: [sh, -c, "until nc -z rabbitmq-svc 5672; do sleep 2; done"]
      containers:
        - name: broker
          image: bakery-broker:latest
          imagePullPolicy: IfNotPresent
          env:
            - name: DSN
              valueFrom:
                secretKeyRef: { name: bakery-secrets, key: DSN }
            - name: RABBITMQ_SERVICE_ADDR
              valueFrom:
                secretKeyRef: { name: bakery-secrets, key: RABBITMQ_SERVICE_ADDR }
          resources:
            requests:
              cpu: "50m"
              memory: "32Mi"
            limits:
              cpu: "200m"
              memory: "128Mi"
          livenessProbe:
            exec:
              command: ["/bin/sh", "-c", "pgrep broker"]
            initialDelaySeconds: 10
            periodSeconds: 30
```

### Makers Deployment

```yaml
# k8s/deployments/makers-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: bakery-makers
  namespace: bakery
  labels:
    app.kubernetes.io/name: makers
    app.kubernetes.io/part-of: bakery-service
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: makers
  template:
    metadata:
      labels:
        app.kubernetes.io/name: makers
    spec:
      initContainers:
        - name: wait-for-rabbitmq
          image: busybox:1.36
          command: [sh, -c, "until nc -z rabbitmq-svc 5672; do sleep 2; done"]
      containers:
        - name: makers
          image: bakery-makers:latest
          imagePullPolicy: IfNotPresent
          env:
            - name: DSN
              valueFrom:
                secretKeyRef: { name: bakery-secrets, key: DSN }
            - name: RABBITMQ_SERVICE_ADDR
              valueFrom:
                secretKeyRef: { name: bakery-secrets, key: RABBITMQ_SERVICE_ADDR }
          resources:
            requests:
              cpu: "50m"
              memory: "32Mi"
            limits:
              cpu: "200m"
              memory: "128Mi"
```

### Frontend Deployment

```yaml
# k8s/deployments/frontend-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: bakery-frontend
  namespace: bakery
  labels:
    app.kubernetes.io/name: frontend
    app.kubernetes.io/part-of: bakery-service
spec:
  replicas: 2                     # Frontend is stateless; multiple replicas safe
  selector:
    matchLabels:
      app.kubernetes.io/name: frontend
  template:
    metadata:
      labels:
        app.kubernetes.io/name: frontend
    spec:
      initContainers:
        - name: wait-for-server
          image: busybox:1.36
          command: [sh, -c, "until nc -z bakery-server-svc 50051; do sleep 2; done"]
      containers:
        - name: frontend
          image: bakery-frontend:latest
          imagePullPolicy: IfNotPresent
          ports:
            - name: http
              containerPort: 8080
              protocol: TCP
          env:
            - name: BAKERY_SERVICE_ADDR
              valueFrom:
                configMapKeyRef: { name: bakery-config, key: BAKERY_SERVICE_ADDR }
            - name: JWT_SECRET
              valueFrom:
                secretKeyRef: { name: bakery-secrets, key: JWT_SECRET }
            - name: CSRF_KEY
              valueFrom:
                secretKeyRef: { name: bakery-secrets, key: CSRF_KEY }
          resources:
            requests:
              cpu: "100m"
              memory: "64Mi"
            limits:
              cpu: "500m"
              memory: "256Mi"
          livenessProbe:
            httpGet:
              path: /healthz
              port: 8080
            initialDelaySeconds: 10
            periodSeconds: 15
          readinessProbe:
            httpGet:
              path: /healthz
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 10
```

### Buyers Deployment

```yaml
# k8s/deployments/buyers-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: bakery-buyers
  namespace: bakery
  labels:
    app.kubernetes.io/name: buyers
    app.kubernetes.io/part-of: bakery-service
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: buyers
  template:
    metadata:
      labels:
        app.kubernetes.io/name: buyers
    spec:
      initContainers:
        - name: wait-for-server
          image: busybox:1.36
          command: [sh, -c, "until nc -z bakery-server-svc 50051; do sleep 2; done"]
      containers:
        - name: buyers
          image: bakery-buyers:latest
          imagePullPolicy: IfNotPresent
          env:
            - name: BAKERY_SERVICE_ADDR
              valueFrom:
                configMapKeyRef: { name: bakery-config, key: BAKERY_SERVICE_ADDR }
          resources:
            requests:
              cpu: "50m"
              memory: "32Mi"
            limits:
              cpu: "200m"
              memory: "64Mi"
```

---

## Kubernetes Services (Networking)

```yaml
# k8s/services/services.yaml
---
# gRPC Server — internal ClusterIP only
apiVersion: v1
kind: Service
metadata:
  name: bakery-server-svc
  namespace: bakery
  labels:
    app.kubernetes.io/name: server
    app.kubernetes.io/part-of: bakery-service
spec:
  type: ClusterIP
  selector:
    app.kubernetes.io/name: server
  ports:
    - name: grpc
      port: 50051
      targetPort: 50051
      protocol: TCP
---
# Frontend — exposed via Ingress
apiVersion: v1
kind: Service
metadata:
  name: bakery-frontend-svc
  namespace: bakery
  labels:
    app.kubernetes.io/name: frontend
    app.kubernetes.io/part-of: bakery-service
spec:
  type: ClusterIP
  selector:
    app.kubernetes.io/name: frontend
  ports:
    - name: http
      port: 80
      targetPort: 8080
      protocol: TCP
---
# PostgreSQL — internal only
apiVersion: v1
kind: Service
metadata:
  name: postgres-svc
  namespace: bakery
spec:
  type: ClusterIP
  selector:
    app.kubernetes.io/name: postgres
  ports:
    - port: 5432
      targetPort: 5432
---
# RabbitMQ — internal only
apiVersion: v1
kind: Service
metadata:
  name: rabbitmq-svc
  namespace: bakery
spec:
  type: ClusterIP
  selector:
    app.kubernetes.io/name: rabbitmq
  ports:
    - name: amqp
      port: 5672
      targetPort: 5672
    - name: management
      port: 15672
      targetPort: 15672
```

---

## Infrastructure — PostgreSQL and RabbitMQ

For production, use managed services (e.g., AWS RDS, CloudAMQP) and reference them via Secrets. For development or staging clusters, the following StatefulSets provide in-cluster instances.

### PostgreSQL StatefulSet

```yaml
# k8s/infrastructure/postgres.yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: postgres
  namespace: bakery
  labels:
    app.kubernetes.io/name: postgres
    app.kubernetes.io/part-of: bakery-service
spec:
  serviceName: postgres-svc
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: postgres
  template:
    metadata:
      labels:
        app.kubernetes.io/name: postgres
    spec:
      containers:
        - name: postgres
          image: postgres:15-alpine
          ports:
            - containerPort: 5432
          env:
            - name: POSTGRES_DB
              valueFrom:
                secretKeyRef: { name: bakery-secrets, key: POSTGRES_DB }
            - name: POSTGRES_USER
              valueFrom:
                secretKeyRef: { name: bakery-secrets, key: POSTGRES_USER }
            - name: POSTGRES_PASSWORD
              valueFrom:
                secretKeyRef: { name: bakery-secrets, key: POSTGRES_PASSWORD }
            - name: PGDATA
              value: /var/lib/postgresql/data/pgdata
          volumeMounts:
            - name: postgres-data
              mountPath: /var/lib/postgresql/data
          resources:
            requests:
              cpu: "250m"
              memory: "256Mi"
            limits:
              cpu: "1000m"
              memory: "1Gi"
          livenessProbe:
            exec:
              command: [pg_isready, -U, $(POSTGRES_USER)]
            initialDelaySeconds: 30
            periodSeconds: 10
  volumeClaimTemplates:
    - metadata:
        name: postgres-data
      spec:
        accessModes: [ReadWriteOnce]
        resources:
          requests:
            storage: 5Gi
```

### RabbitMQ StatefulSet

```yaml
# k8s/infrastructure/rabbitmq.yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: rabbitmq
  namespace: bakery
  labels:
    app.kubernetes.io/name: rabbitmq
    app.kubernetes.io/part-of: bakery-service
spec:
  serviceName: rabbitmq-svc
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: rabbitmq
  template:
    metadata:
      labels:
        app.kubernetes.io/name: rabbitmq
    spec:
      containers:
        - name: rabbitmq
          image: rabbitmq:3-management-alpine
          ports:
            - name: amqp
              containerPort: 5672
            - name: management
              containerPort: 15672
          env:
            - name: RABBITMQ_DEFAULT_USER
              valueFrom:
                secretKeyRef: { name: bakery-secrets, key: POSTGRES_USER }
            - name: RABBITMQ_DEFAULT_PASS
              valueFrom:
                secretKeyRef: { name: bakery-secrets, key: POSTGRES_PASSWORD }
          volumeMounts:
            - name: rabbitmq-data
              mountPath: /var/lib/rabbitmq
          resources:
            requests:
              cpu: "100m"
              memory: "128Mi"
            limits:
              cpu: "500m"
              memory: "512Mi"
          readinessProbe:
            exec:
              command: [rabbitmq-diagnostics, -q, ping]
            initialDelaySeconds: 20
            periodSeconds: 10
  volumeClaimTemplates:
    - metadata:
        name: rabbitmq-data
      spec:
        accessModes: [ReadWriteOnce]
        resources:
          requests:
            storage: 2Gi
```

---

## Ingress

```yaml
# k8s/ingress/ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: bakery-ingress
  namespace: bakery
  labels:
    app.kubernetes.io/part-of: bakery-service
  annotations:
    nginx.ingress.kubernetes.io/rewrite-target: /
    nginx.ingress.kubernetes.io/ssl-redirect: "true"
    cert-manager.io/cluster-issuer: letsencrypt-prod   # Requires cert-manager
spec:
  ingressClassName: nginx
  tls:
    - hosts:
        - bakery.example.com
      secretName: bakery-tls
  rules:
    - host: bakery.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: bakery-frontend-svc
                port:
                  number: 80
```

---

## Resource Requests and Limits

| Service      | CPU Request | CPU Limit | Memory Request | Memory Limit |
|--------------|-------------|-----------|----------------|--------------|
| server       | 100m        | 500m      | 64Mi           | 256Mi        |
| broker       | 50m         | 200m      | 32Mi           | 128Mi        |
| makers       | 50m         | 200m      | 32Mi           | 128Mi        |
| buyers       | 50m         | 200m      | 32Mi           | 64Mi         |
| frontend     | 100m        | 500m      | 64Mi           | 256Mi        |
| postgres     | 250m        | 1000m     | 256Mi          | 1Gi          |
| rabbitmq     | 100m        | 500m      | 128Mi          | 512Mi        |

---

## Health Probes

Probes require the health check endpoints described in `docs/IMPROVEMENTS.md` item H-7.

| Service  | Liveness Probe                    | Readiness Probe                   |
|----------|-----------------------------------|-----------------------------------|
| server   | gRPC health (`port: 50051`)       | gRPC health (`port: 50051`)       |
| broker   | `exec: pgrep broker`              | _(none — no HTTP/gRPC port)_      |
| makers   | `exec: pgrep makers`              | _(none — no HTTP/gRPC port)_      |
| buyers   | `exec: pgrep buyers`              | _(none — simulation only)_        |
| frontend | HTTP GET `/healthz` (port 8080)   | HTTP GET `/healthz` (port 8080)   |
| postgres | `exec: pg_isready`                | `exec: pg_isready`                |
| rabbitmq | `exec: rabbitmq-diagnostics ping` | `exec: rabbitmq-diagnostics ping` |

---

## Recommended Directory Layout

```
k8s/
├── namespace.yaml
├── configmaps/
│   └── bakery-config.yaml
├── secrets/
│   ├── bakery-secrets.yaml.example      ← Template only; never commit real values
│   └── .gitignore                       ← Exclude *.yaml (real secrets)
├── infrastructure/
│   ├── postgres.yaml                    ← StatefulSet + Service
│   └── rabbitmq.yaml                    ← StatefulSet + Service
├── jobs/
│   └── db-init-job.yaml
├── deployments/
│   ├── server-deployment.yaml
│   ├── broker-deployment.yaml
│   ├── makers-deployment.yaml
│   ├── buyers-deployment.yaml
│   └── frontend-deployment.yaml
├── services/
│   └── services.yaml
└── ingress/
    └── ingress.yaml
```

---

## Deployment Checklist

Before deploying to any non-development environment, verify the following:

- [ ] All `CHANGE_ME` values in `bakery-secrets.yaml` replaced with strong, randomly generated secrets
- [ ] `JWT_SECRET` and `CSRF_KEY` are at least 32 bytes of random data
- [ ] Seed credentials (`password123`, `admin123`) changed or seed SQL excluded from production schema init
- [ ] gRPC TLS configured (improvement C-2)
- [ ] Health check endpoints implemented (improvement H-7)
- [ ] Graceful shutdown implemented (improvement H-6)
- [ ] `scratch`/`alpine` base images used (multi-stage Dockerfiles above)
- [ ] Container image tags pinned (not `latest`) in production manifests
- [ ] Resource requests and limits set for all pods
- [ ] All services running as non-root user (`USER 65534:65534`)
- [ ] `bakery-secrets.yaml` is **not** committed to version control
- [ ] Network policies added to restrict inter-pod communication to required paths only
