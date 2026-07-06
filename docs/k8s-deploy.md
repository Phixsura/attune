# Kubernetes deploy with Helm

Attune ships a first-party Helm chart at `deploy/helm/attune`.

The chart has two install paths:

- **Dev / kind**: the chart renders a Kubernetes Secret containing
  `config.yaml` and an embedded `pgvector/pgvector:pg17` StatefulSet.
- **Production**: you provide a complete `config.yaml` through
  `config.existingSecret` and use an external PostgreSQL database.

Do not put production database URLs, Tink keysets, session keys, OIDC client
secrets, or webhook secrets in Helm values. Helm stores rendered manifests in
release history, so chart-rendered Secrets are only for dev, CI, and disposable
private installs.

## Prerequisites

- Kubernetes 1.25+
- Helm 3.12+ or Helm 4+
- An Attune image matching the chart `appVersion`
- For production: PostgreSQL 14+ with pgvector 0.5.0+

Attune is a distroless nonroot image. Kubernetes probes use HTTP endpoints, not
shell commands inside the Attune container.

Released charts are published to GHCR OCI and attached to the matching GitHub
Release:

```bash
helm show chart oci://ghcr.io/phixsura/charts/attune --version 0.3.0
```

When working from a checkout, use the local chart path shown below.

There is no secretless default install. If the chart renders `config.yaml`, you
must provide a Tink keyset with `--set-file` and an embedded Postgres password.
For production, use `config.existingSecret` instead.

## What the chart guarantees

The chart is intentionally small, but it makes the core production contracts
explicit:

- App routing: the Attune Service selects only pods labeled
  `app.kubernetes.io/component=app`. Helm smoke and CI EndpointSlice checks
  verify this.
- Embedded Postgres routing: the embedded Postgres Service selects only pods
  labeled `app.kubernetes.io/component=postgres`. CI checks this separately
  from the app Service.
- Readiness: `/readyz` keeps app pods out of Service endpoints until PostgreSQL
  is reachable.
- DNS and TCP: Helm smoke checks short-name connectivity and fully qualified
  Service DNS from inside the cluster.
- Horizontal scale: CI installs two app replicas, upgrades the release, scales
  to three replicas, and checks ready EndpointSlices after each step.
- Secret handling: production installs must use `config.existingSecret`;
  rendered config Secrets are for dev/CI.

The chart does not make embedded Postgres highly available. Production installs
should use external PostgreSQL with backups and point-in-time recovery.

## Fail-fast edge-case guardrails

The chart intentionally rejects value combinations that are likely to produce a
broken production rollout:

- `profile: production` requires external Postgres and `config.existingSecret`.
- If embedded Postgres is disabled and the chart renders `config.yaml`, provide
  `config.render.databaseURL`.
- `profile: production` requires at least two app replicas, either through
  `replicaCount >= 2` or `autoscaling.minReplicas >= 2`.
- `autoscaling.minReplicas` must be less than or equal to
  `autoscaling.maxReplicas`.
- HPA CPU and memory utilization targets require matching
  `resources.requests.cpu` and `resources.requests.memory`.
- `podDisruptionBudget.minAvailable` must be lower than the fixed
  `replicaCount`, or lower than `autoscaling.minReplicas` when HPA is enabled.
  This prevents a PDB from blocking every voluntary eviction.
- When the chart renders `config.yaml`, `app.port` must match
  `config.render.port`; otherwise probes would target a different port than the
  process listens on.
- `networkPolicy.enabled=true` requires at least one ingress rule and one egress
  rule. Empty rules isolate the app from DNS, Postgres, ingress controllers, and
  monitoring.
- Traffic-service blue/green releases require
  `rollout.trafficService.activeColor`. A traffic-service-only release must set
  `workload.enabled=false`, `service.enabled=false`, `postgres.embedded.enabled=false`,
  `config.render.enabled=false`, and `helmTest.enabled=false`.

## Dev / kind install

Generate a Tink keyset from the checkout:

```bash
go run ./cmd/attune secrets generate-keyset > keyset.json
```

Install with embedded pgvector Postgres:

```bash
helm upgrade --install attune deploy/helm/attune \
  --namespace attune \
  --create-namespace \
  -f deploy/helm/attune/ci/values-kind.yaml \
  --set postgres.embedded.auth.password='replace-with-dev-password' \
  --set-file config.render.secrets.tinkKeyset=keyset.json \
  --wait \
  --timeout 5m
```

Check health:

```bash
kubectl -n attune rollout status deploy/attune --timeout 5m
kubectl -n attune port-forward svc/attune 8090:8090
curl -fsS http://127.0.0.1:8090/healthz
curl -fsS http://127.0.0.1:8090/readyz
```

Run the Helm smoke test:

```bash
helm test attune -n attune
```

The smoke test verifies in-cluster Service DNS resolution, TCP connectivity to
the Attune Service and embedded Postgres Service when enabled, and both
`/healthz` and `/readyz` over short and fully qualified Service names.
Short Service names are exercised through TCP/HTTP connections; fully qualified
Service names are also checked with DNS lookup so Kubernetes search suffix
behavior does not create false failures.

Create the first tenant and API key:

```bash
kubectl -n attune exec deploy/attune -- \
  /app/attune --config /app/config/config.yaml tenant create \
  --slug acme --name "Acme"

kubectl -n attune exec deploy/attune -- \
  /app/attune --config /app/config/config.yaml keys issue \
  --tenant acme --label main
```

## Production install

Create a private `config.yaml` with your external database DSN and secrets:

```yaml
profile: production

port: 8090

database:
  url: "postgres://attune@postgres.example.com:5432/attune?sslmode=require"

migrations:
  confirm_lark_delete: false

enricher:
  interval: "30s"
  batch: 10

audit:
  retention_days: 365
  prune_interval: "24h"

console:
  base_url: "https://attune.example.com"
  session_key: "replace-with-at-least-32-random-bytes"
  bootstrap_admin:
    email: "admin@example.com"
    password: "replace-with-temporary-password"

secrets:
  # Paste the output of `attune secrets generate-keyset`.
  tink_keyset: |
    {
      "primaryKeyId": 123456789,
      "key": []
    }

observability:
  service_version: "v0.3.0"
  environment: "prod"
  otlp_endpoint: ""
  otlp_traces_path: "/opentelemetry/v1/traces"
  otlp_headers: {}
  otlp_insecure: false

rate_limit:
  per_minute: 60
  burst: 300
  disabled: false

security:
  allow_loopback_egress: false
  allow_private_egress: false
  trusted_proxy_hops: 1

custom_webhooks: []
```

Keep the Helm chart `profile: production` in your production values file. The
chart now renders that profile into the runtime `config.yaml`, so the Go
startup path enforces the same production safety contract. If your ingress or
reverse proxy terminates TLS, make sure it also forwards
`X-Forwarded-Proto` and set `security.trusted_proxy_hops` to the number of
trusted proxy hops in front of attune.

When `config.render.observability.environment` is left empty, the chart uses the
chart `profile` for the rendered runtime `observability.environment` value.

Use the complete DSN required by your database in this private file. If that DSN
contains a password, keep it only in the Secret generated from `config.yaml`;
do not commit it to Git or pass it through Helm values.

Create the Kubernetes Secret:

```bash
kubectl create namespace attune
kubectl -n attune create secret generic attune-config \
  --from-file=config.yaml=./config.yaml
```

Create a production values file, for example `values-prod.yaml`:

```yaml
profile: production

replicaCount: 2

workload:
  minReadySeconds: 5
  progressDeadlineSeconds: 600

strategy:
  type: RollingUpdate
  rollingUpdate:
    maxUnavailable: 0
    maxSurge: 1

podDisruptionBudget:
  enabled: true
  minAvailable: 1

topologySpread:
  enabled: true
  topologyKey: topology.kubernetes.io/zone
  whenUnsatisfiable: ScheduleAnyway

postgres:
  embedded:
    enabled: false
  external:
    enabled: true

config:
  existingSecret: attune-config

resources:
  requests:
    cpu: 250m
    memory: 512Mi
```

Install with external Postgres:

```bash
helm upgrade --install attune deploy/helm/attune \
  --namespace attune \
  --create-namespace \
  -f values-prod.yaml \
  --set app.port=8090 \
  --wait \
  --timeout 5m
```

Add Ingress, ServiceMonitor, Grafana dashboard discovery, and NetworkPolicy only
after the matching ingress controller, Prometheus Operator CRDs, Grafana
sidecar, and cluster network policy are present. The checked-in files under
`deploy/helm/attune/ci/` are CI fixtures and examples, not a universal
production baseline.

Your external database must allow `CREATE EXTENSION IF NOT EXISTS vector`, or the
extension must already be installed by a database administrator.

If your `config.yaml` uses a port other than `8090`, set `app.port` to the same
value so the container port and HTTP probes match the app process.

Preflight checklist before production traffic:

- `kubectl -n attune rollout status deploy/attune --timeout 5m`
- `helm test attune -n attune --timeout 5m`
- `kubectl -n attune get endpointslice -l kubernetes.io/service-name=attune`
- `kubectl -n attune port-forward svc/attune 8090:8090`
- `curl -fsS http://127.0.0.1:8090/readyz`

## Comfortable distributed deployment

For production, run Attune with at least two app replicas, an external
PostgreSQL database, a PodDisruptionBudget, and topology spread. The startup
migration advisory lock serializes schema migrations across replicas, and
`/readyz` keeps a pod out of Service endpoints until PostgreSQL is reachable.

Example production overlay, matching the `values-prod.yaml` shape above:

```yaml
profile: production

replicaCount: 2

workload:
  minReadySeconds: 5
  progressDeadlineSeconds: 600

strategy:
  type: RollingUpdate
  rollingUpdate:
    maxUnavailable: 0
    maxSurge: 1

podDisruptionBudget:
  enabled: true
  minAvailable: 1

topologySpread:
  enabled: true
  topologyKey: topology.kubernetes.io/zone
  whenUnsatisfiable: ScheduleAnyway

postgres:
  embedded:
    enabled: false
  external:
    enabled: true

config:
  existingSecret: attune-config
```

Scale manually with Helm:

```bash
helm upgrade attune deploy/helm/attune \
  -n attune \
  --reuse-values \
  --set replicaCount=3 \
  --wait \
  --timeout 5m
```

Enable HPA only when the cluster has metrics-server or an equivalent metrics
pipeline, and set resource requests so utilization metrics are meaningful:

```yaml
autoscaling:
  enabled: true
  minReplicas: 2
  maxReplicas: 10
  targetCPUUtilizationPercentage: 70
  targetMemoryUtilizationPercentage: 80

resources:
  requests:
    cpu: 250m
    memory: 512Mi
  limits:
    memory: 1Gi
```

When `autoscaling.enabled=true`, the Deployment omits `spec.replicas` and the
HPA owns replica count. When it is `false`, `replicaCount` is the source of
truth.

If you enable NetworkPolicy, include egress to DNS, PostgreSQL, and any LLM or
notification providers Attune must call. Include ingress from your ingress
controller and monitoring namespace if those components are outside the Attune
namespace. `helm test` also reaches the app Service from a test pod, so either
allow that traffic while validating a release or run the DNS/TCP checks from an
allowed debug namespace.

The chart labels app pods with `app.kubernetes.io/component=app` and embedded
Postgres pods with `app.kubernetes.io/component=postgres`, so the main Service
and ServiceMonitor only select HTTP-serving Attune pods. CI installs a
multi-replica kind release, verifies the two initial app endpoints, scales the
release to three app replicas, then checks that the Attune Service has exactly
the app endpoints while the Postgres Service still has only the Postgres
endpoint.

## Release strategies

### Rolling release

Rolling release is the default path for normal backwards-compatible changes.
Use at least two replicas, `/readyz`, PDB, and `maxUnavailable: 0` so Kubernetes
does not remove the old ready pod set before the new pod set is ready.

Recommended rolling values:

```yaml
replicaCount: 2

workload:
  minReadySeconds: 5
  progressDeadlineSeconds: 600

strategy:
  type: RollingUpdate
  rollingUpdate:
    maxUnavailable: 0
    maxSurge: 1

podDisruptionBudget:
  enabled: true
  minAvailable: 1
```

Upgrade with an atomic wait:

```bash
helm upgrade attune deploy/helm/attune \
  -n attune \
  -f values-prod.yaml \
  --set image.tag=v0.3.1 \
  --atomic \
  --wait \
  --timeout 10m
```

Verify the rollout and endpoint set:

```bash
kubectl -n attune rollout status deploy/attune --timeout 10m
helm test attune -n attune --timeout 5m
kubectl -n attune get endpointslice -l kubernetes.io/service-name=attune
```

Rollback:

```bash
helm rollback attune -n attune
kubectl -n attune rollout status deploy/attune --timeout 10m
```

### Blue/green release

Use blue/green when you need a warmed candidate release and a fast traffic
switch. The chart supports this with two app releases plus one
traffic-service-only router release:

- `attune-router`: owns the stable Service named `attune`.
- `attune-blue`: current app release, labeled `attune.phixsura.dev/color=blue`.
- `attune-green`: candidate app release, labeled
  `attune.phixsura.dev/color=green`.

The stable traffic Service selector does not include Helm release instance. It
selects `app.kubernetes.io/name=attune`,
`app.kubernetes.io/component=app`, and the active color. That lets the router
switch between app releases without mutating either Deployment selector.

Install the router once:

```yaml
# values-router.yaml
workload:
  enabled: false

service:
  enabled: false

ingress:
  enabled: true
  hosts:
    - host: attune.example.com
      paths:
        - path: /
          pathType: Prefix

postgres:
  embedded:
    enabled: false

config:
  render:
    enabled: false

helmTest:
  enabled: false

rollout:
  trafficService:
    enabled: true
    name: attune
    activeColor: blue
```

```bash
helm upgrade --install attune-router deploy/helm/attune \
  -n attune \
  -f values-router.yaml \
  --wait
```

Install or upgrade blue:

```bash
helm upgrade --install attune-blue deploy/helm/attune \
  -n attune \
  -f values-prod.yaml \
  --set rollout.color=blue \
  --set service.enabled=true \
  --set image.tag=v0.3.0 \
  --wait \
  --timeout 10m
```

Install or upgrade green without switching traffic:

```bash
helm upgrade --install attune-green deploy/helm/attune \
  -n attune \
  -f values-prod.yaml \
  --set rollout.color=green \
  --set service.enabled=true \
  --set image.tag=v0.3.1 \
  --wait \
  --timeout 10m

helm test attune-green -n attune --timeout 5m
kubectl -n attune port-forward svc/attune-green 8091:8090
curl -fsS http://127.0.0.1:8091/readyz
```

Confirm the stable Service still targets blue:

```bash
kubectl -n attune get endpointslice \
  -l kubernetes.io/service-name=attune \
  -o jsonpath='{range .items[*].endpoints[?(@.conditions.ready==true)]}{.targetRef.name}{"\n"}{end}'
```

Switch traffic to green:

```bash
helm upgrade attune-router deploy/helm/attune \
  -n attune \
  -f values-router.yaml \
  --set rollout.trafficService.activeColor=green \
  --wait
```

Rollback is the same operation in reverse:

```bash
helm upgrade attune-router deploy/helm/attune \
  -n attune \
  -f values-router.yaml \
  --set rollout.trafficService.activeColor=blue \
  --wait
```

Keep the old color running until the new color has baked long enough for your
traffic and background-job profile. After that, uninstall the inactive app
release. Do not uninstall the router release unless you have moved the stable
Service elsewhere.

If `ingress.enabled=true` on the router release, the Ingress backend points at
the stable traffic Service, so the domain follows
`rollout.trafficService.activeColor`. Keep per-color app Services enabled for
direct smoke tests and debugging.

Database compatibility matters more than the traffic switch. Blue and green run
against the same production database, so only use blue/green for releases whose
migrations are backward-compatible with the still-running old color. Destructive
or non-backward-compatible schema changes need a staged expand/migrate/contract
plan before using blue/green.

Do not turn a single existing release from `rollout.color=blue` into
`rollout.color=green` and call that blue/green. That is a normal rolling update
with a label change. Real blue/green keeps two app releases running and switches
only the router Service selector after the candidate color is ready.

## DNS and service connectivity

For a release named `attune` in namespace `attune`, the in-cluster names are:

- Attune HTTP: short name `attune`, fully qualified name
  `attune.attune.svc.cluster.local`.
- Embedded Postgres: short name `attune-postgres`, fully qualified name
  `attune-postgres.attune.svc.cluster.local`.

The app container sets `enableServiceLinks: false` by default. Use DNS names,
not Kubernetes-injected service environment variables. This avoids large
environment blocks and stale env values when Services change.

Check app DNS, TCP, and ready endpoints from inside the cluster:

```bash
kubectl -n attune run netcheck --rm -i --restart=Never \
  --image=busybox:1.37.0 -- \
  sh -ec '
    nslookup attune.attune.svc.cluster.local
    nc -z -w 5 attune 8090
    nc -z -w 5 attune.attune.svc.cluster.local 8090
  '

kubectl -n attune get endpointslice \
  -l kubernetes.io/service-name=attune \
  -o jsonpath='{range .items[*].endpoints[?(@.conditions.ready==true)]}{.targetRef.name}{"\n"}{end}'
```

When embedded Postgres is enabled, check its Service separately:

```bash
kubectl -n attune run pg-netcheck --rm -i --restart=Never \
  --image=busybox:1.37.0 -- \
  sh -ec '
    nslookup attune-postgres.attune.svc.cluster.local
    nc -z -w 5 attune-postgres 5432
    nc -z -w 5 attune-postgres.attune.svc.cluster.local 5432
  '

kubectl -n attune get endpointslice \
  -l kubernetes.io/service-name=attune-postgres \
  -o jsonpath='{range .items[*].endpoints[?(@.conditions.ready==true)]}{.targetRef.name}{"\n"}{end}'
```

In production with external Postgres, the application uses the DSN in
`config.yaml`; the chart does not create a Postgres Service.

## Probes

- `/healthz`: process liveness only.
- `/readyz`: readiness; checks PostgreSQL with `pool.Ping`.
- `/metrics`: Prometheus metrics; unauthenticated. Keep it cluster-internal.

The chart uses `/healthz` for startup/liveness and `/readyz` for readiness.

## Graceful shutdown

Attune handles `SIGTERM` in three phases:

1. Mark the process as draining. `/readyz` returns `503`, so Kubernetes and
   ingress controllers can remove the pod from ready endpoints.
2. Wait `shutdown.drain_delay` before closing the listener. The chart renders
   `5s` by default to cover EndpointSlice and load-balancer propagation.
3. Call `http.Server.Shutdown` with `shutdown.timeout`. The default is `20s`,
   so in-flight requests can finish before the process exits.

After HTTP shutdown, Attune still runs bounded cleanup for inbound adapters and
OpenTelemetry flushes. For that reason the chart sets
`terminationGracePeriodSeconds: 60` by default.

Rendered config values:

```yaml
config:
  render:
    shutdown:
      drainDelay: 5s
      timeout: 20s

terminationGracePeriodSeconds: 60
```

When using `config.existingSecret`, include the equivalent runtime config:

```yaml
shutdown:
  drain_delay: "5s"
  timeout: "20s"
```

Tune these together. `terminationGracePeriodSeconds` must be larger than the
drain delay, HTTP shutdown timeout, inbound-adapter shutdown budget, and trace
flush budget. If an ingress or external load balancer has slow endpoint
propagation, raise `shutdown.drain_delay` instead of shortening it.

Verify a terminating pod is removed from traffic:

```bash
kubectl -n attune delete pod -l app.kubernetes.io/name=attune \
  --wait=false

kubectl -n attune get endpointslice \
  -l kubernetes.io/service-name=attune \
  -o jsonpath='{range .items[*].endpoints[?(@.conditions.ready==true)]}{.targetRef.name}{"\n"}{end}'
```

## Sudden pod failure smoke

Graceful shutdown validates planned rollouts. A sudden crash needs a harsher
test: remove one app pod without running its shutdown hooks and prove Kubernetes
keeps the Service healthy through the remaining ready replicas.

What this proves:

- the Deployment/ReplicaSet creates a replacement pod;
- the Service and EndpointSlice continue to expose only ready app pods;
- the app can survive one app-pod loss when at least two replicas are ready.

What it cannot prove:

- an in-flight request already pinned to the killed pod will finish;
- a single-replica install remains available;
- external load balancers have zero stale-connection window.

Run this against a multi-replica install:

```bash
victim="$(
  kubectl -n attune get pods \
    -l app.kubernetes.io/name=attune,app.kubernetes.io/component=app \
    --field-selector=status.phase=Running \
    -o jsonpath='{.items[0].metadata.name}'
)"

kubectl -n attune run attune-pre-chaos --rm -i --restart=Never \
  --image=curlimages/curl:8.11.1 -- \
  sh -ec 'curl -fsS --max-time 3 http://attune:8090/readyz'

kubectl -n attune delete pod "$victim" \
  --force --grace-period=0 --wait=false

kubectl -n attune run attune-post-chaos --rm -i --restart=Never \
  --image=curlimages/curl:8.11.1 -- \
  sh -ec 'for i in $(seq 1 30); do
    curl -fsS --max-time 3 http://attune:8090/readyz && exit 0
    sleep 1
  done
  exit 1'

kubectl -n attune rollout status deploy/attune --timeout 5m
kubectl -n attune get endpointslice \
  -l kubernetes.io/service-name=attune \
  -o jsonpath='{range .items[*].endpoints[?(@.conditions.ready==true)]}{.targetRef.name}{"\n"}{end}'
```

CI runs the same class of smoke after scaling the chart to three app replicas:
it force-deletes one app pod, probes the Service through Kubernetes DNS, waits
for the Deployment to recover, and verifies the deleted pod is no longer a
ready Service endpoint.

## Observability

Enable Prometheus Operator integration:

```bash
helm upgrade --install attune deploy/helm/attune \
  --reuse-values \
  --set serviceMonitor.enabled=true
```

Enable dashboard ConfigMaps for Grafana sidecar discovery:

```bash
helm upgrade --install attune deploy/helm/attune \
  --reuse-values \
  --set grafanaDashboard.enabled=true
```

The chart does not install Prometheus or Grafana.

## LLM and notification setup

LLM channels, model abilities, routes, and provider API keys are DB-managed
runtime resources. They are not process YAML and are not Helm values.

Configure LLM after install through Console or CLI:

```bash
kubectl -n attune exec deploy/attune -- \
  /app/attune --config /app/config/config.yaml llm channels create \
  --name openai --protocol openai-compat \
  --base-url https://api.openai.com/v1 --api-key "$OPENAI_API_KEY"
```

Lark, Slack, and raw webhook notification targets are also runtime resources.
Use Console for normal operation. A future idempotent bootstrap command can make
these resources declarative for Helm/GitOps workflows.

## Upgrades and backups

For embedded Postgres, data lives in the chart-managed PVC. Back it up before
upgrades and before deleting the release. Embedded Postgres is not HA and is not
recommended for production.

For production, use your database provider's backup, PITR, and restore process.
Back up the Tink keyset with the same care as database backups. Losing it can
make encrypted LLM provider credentials unreadable.

Upgrade:

```bash
helm upgrade attune deploy/helm/attune -n attune --reuse-values --wait
```

For released charts:

```bash
helm upgrade attune oci://ghcr.io/phixsura/charts/attune \
  --version 0.3.0 \
  -n attune \
  --reuse-values \
  --wait
```

When rotating an operator-owned `config.existingSecret`, either set
`config.existingSecretChecksum` to a new checksum value during `helm upgrade` or
restart the Deployment with your secret reloader / rollout process.

## Troubleshooting

Start with the rendered Kubernetes state:

```bash
kubectl -n attune get deploy,sts,svc,pod,endpointslice -o wide
kubectl -n attune describe deploy/attune
kubectl -n attune logs deploy/attune --tail=200
```

Common checks:

- Pod is running but not ready: check `/readyz`, app logs, and database
  connectivity. `/healthz` can be green while `/readyz` is red.
- Service has no endpoints: verify the app pods have
  `app.kubernetes.io/component=app` and are Ready.
- DNS resolves but HTTP fails: check the Service port, `app.port`, and readiness
  probe status.
- Short Service lookup output shows extra `NXDOMAIN` lines: prefer FQDN DNS
  checks and TCP/HTTP checks; Kubernetes search suffix attempts can print
  negative lookups before the final in-namespace answer.
- HPA does not scale: verify metrics-server, pod resource requests, and HPA
  events with `kubectl -n attune describe hpa attune`.
- ServiceMonitor does not appear: install the Prometheus Operator CRD or leave
  `serviceMonitor.enabled=false`.

## Current limitations

- The current binary runs web traffic and background workers in the same process.
  Scaling replicas also scales workers.
- Multi-replica correctness depends on the startup migration advisory lock.
- Embedded Postgres is for dev/kind and low-risk private installs only.
- `profile: production` requires `config.existingSecret` and external Postgres.
- ServiceMonitor requires the Prometheus Operator CRD.
- Grafana dashboard ConfigMaps require a Grafana sidecar or another dashboard
  discovery mechanism.
