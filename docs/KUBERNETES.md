# Bakery Service — Kubernetes Deployment

## Table of Contents

1. [Overview](#overview)
2. [Directory Layout](#directory-layout)
3. [Secrets](#secrets)
4. [Service Deployments](#service-deployments)
5. [Infrastructure — PostgreSQL and RabbitMQ](#infrastructure--postgresql-and-rabbitmq)
6. [OpenShift Route](#openshift-route)
7. [Environment Variables](#environment-variables)
8. [Storage](#storage)

---

## Overview

This document describes the Kubernetes deployment for the Bakery Service. The manifests in `./kubernetes/` provide a complete deployment for OpenShift/Kubernetes clusters:

- `Deployment` manifests for all five application services (server, broker, makers, buyers, frontend)
- `Service` manifests for inter-pod networking
- PostgreSQL StatefulSet with embedded schema via ConfigMap
- RabbitMQ StatefulSet with Erlang cookie management
- OpenShift `Route` for external frontend access

All services pull container images from Docker Hub (`docker.io/calvarado2004/bakery-go-*`).

---

## Directory Layout

All Kubernetes manifests are located in `./kubernetes/`:

```
kubernetes/
├── bakery-secrets.yaml      # Secret with JWT_SECRET and CSRF_KEY
├── bakery-server.yaml       # Server Deployment + Service
├── bakery-broker.yaml       # Broker Deployment
├── bakery-makers.yaml       # Makers Deployment
├── bakery-buyers.yaml       # Buyers Deployment
├── frontend.yaml            # Frontend Deployment + Service
├── postgres.yaml            # PostgreSQL ConfigMap + Service + StatefulSet
├── rabbitmq.yaml            # RabbitMQ Service + StatefulSet
└── ocp-route.yaml           # OpenShift Route for frontend access
```

---

## Secrets

The `bakery-secrets` Secret contains sensitive configuration for all services:

```yaml
# kubernetes/bakery-secrets.yaml
apiVersion: v1
kind: Secret
metadata:
  name: bakery-secrets
  labels:
    app: bakery-service
type: Opaque
stringData:
  JWT_SECRET: "bakery-go-secret-key-change-in-production"
  CSRF_KEY: "bakery-go-csrf-key-change-in-production"
```

**Keys:**
- `JWT_SECRET` — Used by frontend for JWT operations
- `CSRF_KEY` — Used by frontend for CSRF protection

---

## Service Deployments

### Server (`bakery-server.yaml`)

The gRPC server service that handles core bakery operations.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: bakery-server
spec:
  replicas: 1
  selector:
    matchLabels:
      app: bakery-server
  template:
    metadata:
      labels:
        app: bakery-server
    spec:
      containers:
        - name: bakery-server
          image: docker.io/calvarado2004/bakery-go-server
          ports:
            - containerPort: 50051
          env:
            - name: BAKERY_SERVICE_ADDR
              value: "0.0.0.0:50051"
            - name: RABBITMQ_SERVICE_ADDR
              value: "amqp://guest:guest@rabbitmq-service:5672/"
            - name: DSN
              value: "host=postgres-service port=5432 user=postgres password=postgres dbname=bakery sslmode=disable timezone=UTC connect_timeout=5"
            - name: JWT_SECRET
              valueFrom:
                secretKeyRef:
                  name: bakery-secrets
                  key: JWT_SECRET
---
apiVersion: v1
kind: Service
metadata:
  name: bakery-server-service
  labels:
    app: bakery-server
spec:
  ports:
    - port: 50051
      protocol: TCP
      name: grpc
  selector:
    app: bakery-server
```

**Service:** `bakery-server-service:50051` (gRPC)

---

### Broker (`bakery-broker.yaml`)

RabbitMQ broker service for message processing.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: bakery-broker
spec:
  replicas: 1
  selector:
    matchLabels:
      app: bakery-broker
  template:
    metadata:
      labels:
        app: bakery-broker
    spec:
      containers:
        - name: bakery-broker
          image: docker.io/calvarado2004/bakery-go-broker
          env:
            - name: RABBITMQ_SERVICE_ADDR
              value: "amqp://guest:guest@rabbitmq-service:5672/"
            - name: DSN
              value: "host=postgres-service port=5432 user=postgres password=postgres dbname=bakery sslmode=disable timezone=UTC connect_timeout=5"
```

---

### Makers (`bakery-makers.yaml`)

Bread maker service that processes make orders.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: bakery-makers
spec:
  replicas: 1
  selector:
    matchLabels:
      app: bakery-makers
  template:
    metadata:
      labels:
        app: bakery-makers
    spec:
      containers:
        - name: bakery-makers
          image: docker.io/calvarado2004/bakery-go-makers
          env:
            - name: BAKERY_SERVICE_ADDR
              value: "bakery-server-service:50051"
            - name: RABBITMQ_SERVICE_ADDR
              value: "amqp://guest:guest@rabbitmq-service:5672/"
            - name: DSN
              value: "host=postgres-service port=5432 user=postgres password=postgres dbname=bakery sslmode=disable timezone=UTC connect_timeout=5"
```

---

### Buyers (`bakery-buyers.yaml`)

Buyer service that handles purchase orders.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: bakery-buyers
spec:
  replicas: 1
  selector:
    matchLabels:
      app: bakery-buyers
  template:
    metadata:
      labels:
        app: bakery-buyers
    spec:
      containers:
        - name: bakery-buyers
          image: docker.io/calvarado2004/bakery-go-buyers
          env:
            - name: BAKERY_SERVICE_ADDR
              value: "bakery-server-service:50051"
            - name: ACTIVEMQ_SERVICE_ADDR
              value: "amqp://guest:guest@rabbitmq-service:5672/"
```

> **Note:** The Buyers service uses `ACTIVEMQ_SERVICE_ADDR` as the environment variable name, though it connects to RabbitMQ.

---

### Frontend (`frontend.yaml`)

HTTP frontend service with web UI.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: bakery-frontend
spec:
  replicas: 1
  selector:
    matchLabels:
      app: bakery-frontend
  template:
    metadata:
      labels:
        app: bakery-frontend
    spec:
      containers:
        - name: bakery-frontend
          image: docker.io/calvarado2004/bakery-go-frontend
          ports:
            - containerPort: 8080
          env:
            - name: BAKERY_SERVICE_ADDR
              value: "bakery-server-service:50051"
            - name: JWT_SECRET
              valueFrom:
                secretKeyRef:
                  name: bakery-secrets
                  key: JWT_SECRET
            - name: CSRF_KEY
              valueFrom:
                secretKeyRef:
                  name: bakery-secrets
                  key: CSRF_KEY
---
apiVersion: v1
kind: Service
metadata:
  name: bakery-frontend-service
  labels:
    app: bakery-frontend
spec:
  ports:
    - port: 8080
      protocol: TCP
      name: http
  selector:
    app: bakery-frontend
```

**Service:** `bakery-frontend-service:8080` (HTTP)

---

## Infrastructure — PostgreSQL and RabbitMQ

### PostgreSQL (`postgres.yaml`)

PostgreSQL is deployed as a StatefulSet with the database schema embedded in a ConfigMap that is mounted at startup.

**ConfigMap** (`initdb`) contains the full database schema with all tables, sequences, and default data:

```yaml
kind: ConfigMap
apiVersion: v1
metadata:
  name: initdb
immutable: true
data:
  create_tables.sql: |-
    -- Full schema with tables for:
    -- - bread, bread_maker, make_order, make_order_details
    -- - customer, buy_order, order_details
    -- - orders_processed, outbox
    -- - admin_users, invoices, invoice_items
    -- Plus default data for customer, bread_maker, and admin user
```

**StatefulSet** mounts the ConfigMap at `/docker-entrypoint-initdb.d/create_tables.sql` for automatic initialization on first startup.

```yaml
kind: StatefulSet
apiVersion: apps/v1
metadata:
  name: postgres-bakery
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: postgres-bakery
  replicas: 1
  template:
    metadata:
      labels:
        app.kubernetes.io/name: postgres-bakery
    spec:
      containers:
        - name: postgres
          image: postgres:18
          ports:
            - name: postgres
              containerPort: 5432
          env:
            - name: POSTGRES_DB
              value: bakery
            - name: POSTGRES_USER
              value: postgres
            - name: POSTGRES_PASSWORD
              value: postgres
            - name: PGDATA
              value: /var/lib/postgresql/data
          volumeMounts:
            - name: postgres-pvc
              mountPath: /var/lib/postgresql
            - name: initdb
              mountPath: /docker-entrypoint-initdb.d/create_tables.sql
              subPath: create_tables.sql
              readOnly: true
  volumeClaimTemplates:
    - metadata:
        name: postgres-pvc
      spec:
        accessModes: [ReadWriteOnce]
        storageClassName: px-csi-db
        resources:
          requests:
            storage: 7Gi
```

**Service:** `postgres-service:5432`

**Connection string:** `host=postgres-service port=5432 user=postgres password=postgres dbname=bakery sslmode=disable`

---

### RabbitMQ (`rabbitmq.yaml`)

RabbitMQ is deployed as a StatefulSet with persistent storage and Erlang cookie management.

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: rabbitmq
spec:
  serviceName: "rabbitmq"
  replicas: 1
  selector:
    matchLabels:
      app: rabbitmq
  template:
    metadata:
      labels:
        app: rabbitmq
    spec:
      initContainers:
        - name: fix-erlang-cookie-perms
          image: busybox:1.36
          command:
            - sh
            - '-c'
            - |
              chown 999:999 /var/lib/rabbitmq /var/lib/rabbitmq/.erlang.cookie || true
              chmod 700 /var/lib/rabbitmq || true
              chmod 400 /var/lib/rabbitmq/.erlang.cookie || true
          env:
            - name: RABBITMQ_ERLANG_COOKIE
              value: 1WqgH8N2v1qDBDZDbNy8Bg9IkPWLEpu79m6q+0t36lQ=
      containers:
        - name: rabbitmq
          image: rabbitmq:3.9-management
          ports:
            - containerPort: 5672
              name: rabbitmq
            - containerPort: 15672
              name: rabbitmq-mgmt
          env:
            - name: RABBITMQ_ERLANG_COOKIE
              value: "1WqgH8N2v1qDBDZDbNy8Bg9IkPWLEpu79m6q+0t36lQ="
          resources:
            requests:
              cpu: 200m
              memory: 60Mi
            limits:
              cpu: 800m
              memory: 512Mi
  volumeClaimTemplates:
    - metadata:
        name: rabbitmq-data
      spec:
        accessModes: [ReadWriteOnce]
        storageClassName: px-csi-db
        resources:
          requests:
            storage: 5Gi
```

**Service:** `rabbitmq-service:5672` (AMQP), `rabbitmq-service:15672` (management UI)

**Connection string:** `amqp://guest:guest@rabbitmq-service:5672/`

> **Note:** The Erlang cookie is fixed as `1WqgH8N2v1qDBDZDbNy8Bg9IkPWLEpu79m6q+0t36lQ=` for cluster stability.

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
          image: postgres:18-alpine
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
          image: postgres:18-alpine
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

## OpenShift Route (`ocp-route.yaml`)

The frontend is exposed externally via an OpenShift Route:

```yaml
apiVersion: route.openshift.io/v1
kind: Route
metadata:
  name: bakery-go
  labels:
    app: bakery-frontend
spec:
  host: bakery-go-bakery.apps.ocp-think.levelg.io
  port:
    targetPort: http
  tls:
    termination: edge
    insecureEdgeTerminationPolicy: Allow
  to:
    kind: Service
    name: bakery-frontend-service
```

**External URL:** `bakery-go-bakery.apps.ocp-think.levelg.io`

---

## Environment Variables

### Server
- `BAKERY_SERVICE_ADDR` — `0.0.0.0:50051`
- `RABBITMQ_SERVICE_ADDR` — `amqp://guest:guest@rabbitmq-service:5672/`
- `DSN` — PostgreSQL connection string
- `JWT_SECRET` — From `bakery-secrets` Secret

### Broker
- `RABBITMQ_SERVICE_ADDR` — `amqp://guest:guest@rabbitmq-service:5672/`
- `DSN` — PostgreSQL connection string

### Makers
- `BAKERY_SERVICE_ADDR` — `bakery-server-service:50051`
- `RABBITMQ_SERVICE_ADDR` — `amqp://guest:guest@rabbitmq-service:5672/`
- `DSN` — PostgreSQL connection string

### Buyers
- `BAKERY_SERVICE_ADDR` — `bakery-server-service:50051`
- `ACTIVEMQ_SERVICE_ADDR` — `amqp://guest:guest@rabbitmq-service:5672/`

### Frontend
- `BAKERY_SERVICE_ADDR` — `bakery-server-service:50051`
- `JWT_SECRET` — From `bakery-secrets` Secret
- `CSRF_KEY` — From `bakery-secrets` Secret

---

## Storage

### PostgreSQL
- **Storage class:** `px-csi-db`
- **Size:** 7Gi
- **Access mode:** ReadWriteOnce
- **Mount path:** `/var/lib/postgresql`

### RabbitMQ
- **Storage class:** `px-csi-db`
- **Size:** 5Gi
- **Access mode:** ReadWriteOnce
- **Mount path:** `/var/lib/rabbitmq`

---

## Deployment Checklist

- [ ] Ensure `px-csi-db` StorageClass exists in cluster
- [ ] Update `bakery-secrets.yaml` with production JWT_SECRET and CSRF_KEY
- [ ] Verify RabbitMQ Erlang cookie matches across all RabbitMQ pods
- [ ] Deploy in order: Secrets → PostgreSQL → RabbitMQ → Application services → Route
