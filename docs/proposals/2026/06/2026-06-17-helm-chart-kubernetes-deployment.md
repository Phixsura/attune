<!-- markdownlint-disable MD013 -->

# Helm chart for Kubernetes deployment

| Field | Value |
| --- | --- |
| **Issue** | [#42](https://github.com/Phixsura/attune/issues/42) |
| **Status** | Implemented |
| **Started** | 2026-06-17 01:00 CST |
| **Related** | #5 (Docker Compose deploy), #6 (observability overlay), #7 (private deploy docs), #23 (config-first runtime), #25 (pgvector clustering), #34 (outbound adapters), #40 (OIDC SSO) |

## Problem

Enterprise operators expect a supported Kubernetes install path. Today Attune has
a production image, Docker Compose assets, and observability dashboards, but the
Kubernetes path is "translate Compose by hand". That blocks repeatable installs,
upgrades, and enterprise deployment reviews.

Issue #42 asks for a Helm chart under `deploy/helm/attune/` with app resources,
Postgres, optional Ingress, secrets, ServiceMonitor, Grafana dashboard wiring,
docs, CI tests, and chart publication. The request is directionally right, but
several details in the issue predate the current runtime shape:

- Attune is now config-first. `config.Load()` reads one YAML file with
  `KnownFields(true)` and has no runtime environment override protocol.
- Sensitive and non-sensitive process config currently live in the same
  `config.yaml`: database URL, console session key, Tink keyset, OIDC client
  secret, and custom webhook secrets.
- LLM provider config is not YAML. Channels, abilities, routes, and provider API
  keys are DB-managed and encrypted with the Tink keyset.
- Lark is currently an outbound notification destination, not the old deleted
  Lark OAuth ingest path.
- Every server process opens Postgres and runs migrations during startup.
  Multi-replica Kubernetes installs therefore need application-level migration
  serialization and startup retry behavior, not a chart-only promise.
- Clustering uses pgvector. Embedded Postgres for Kubernetes must use a pgvector
  image, and production external databases must have `vector` available.

A chart that simply renders a Deployment, HPA, ConfigMap, and Secret would look
complete but would not be production-safe. The chart needs to encode Attune's
actual runtime contract and make the production path explicit.

## Goals

- Add a first-party chart at `deploy/helm/attune/`.
- Make `helm install` work in a fresh kind cluster.
- Support both:
  - quickstart embedded Postgres using `pgvector/pgvector:pg17`;
  - production external Postgres through an operator-owned full config Secret
    carrying the DSN.
- Mount Attune process config as a Kubernetes Secret by default, with
  `config.existingSecret` for production secret managers.
- Treat Helm-rendered config secrets as dev/CI only. Production installs should
  supply a complete `config.yaml` through `config.existingSecret` so sensitive
  values do not land in Helm release history.
- Provide explicit knobs for image, replicas, resources, probes, security
  context, service account, service exposure, Ingress, HPA, PDB, affinity,
  tolerations, topology spread constraints, annotations, labels, extra env,
  extra volumes, and extra mounts.
- Gate optional observability resources:
  - `ServiceMonitor` for Prometheus Operator;
  - Grafana dashboard ConfigMaps using existing dashboard JSON.
- Document quickstart, production install, external Postgres requirements,
  secret handling, upgrades, backup expectations, monitoring, and known
  limitations in `docs/k8s-deploy.md`.
- Add CI coverage for `helm lint`, rendered manifests, multi-replica kind
  install smoke, Service endpoint isolation, upgrade behavior, HPA renders, and
  negative validation for unsafe values.
- Keep CI path filters conservative: proto changes must exercise Go, Console,
  and contract checks; Go, Console, Dockerfile, and deploy changes must exercise
  image build plus Helm/kind checks because production ships the backend and
  SPA together.
- Support rolling release defaults and a blue/green traffic Service pattern
  without mutating Deployment selectors.
- Publish the chart through a supported registry path.

## Non-goals

- Do not make embedded Postgres the recommended production database. It is for
  kind, demos, and low-risk private installs only.
- Do not vendor a full Postgres operator, backup controller, Prometheus stack,
  Grafana instance, or ingress controller.
- Do not reintroduce LLM provider keys into process YAML.
- Do not reintroduce deleted Lark OAuth ingest config.
- Do not claim independent web/worker scaling before the binary exposes
  separate role flags; the first chart scales the combined server process.
- Do not add CRDs. Optional resources that depend on external CRDs must be
  gated and disabled by default.
- Do not solve every cloud-specific load balancer, certificate, storage, or
  service mesh policy in first-party templates. Provide escape hatches so
  operators can layer their platform policy.

## Code reconciliation

| Area | Verified reality | Chart decision |
| --- | --- | --- |
| Config source | One private YAML file selected by `--config`; no app env overrides. | Render or mount a single Secret directory and run with `/app/config/config.yaml`. Treat it as secret-bearing. |
| ConfigMap vs Secret | Issue asks for ConfigMap plus Secret, but current app cannot merge them. | Default to a Secret containing full `config.yaml`; support `existingSecret`. Consider split ConfigMap+Secret only after app-level file indirection exists. |
| External DSN secret | Kubernetes cannot splice another Secret key into a mounted config file without an init merge step or app support. | `config.existingSecret` is the production external-DB path. Literal `config.render.databaseURL` is acceptable for dev/CI. |
| Image runtime | Distroless static nonroot, no shell or curl. | Use Kubernetes HTTP probes, not exec probes. Helm test uses a separate curl image. Keep `runAsNonRoot`, drop caps, and read-only root filesystem. |
| Health | `/healthz` is process liveness only and does not check Postgres. | Add `/readyz` for DB-backed readiness; use `/healthz` for startup/liveness and `/readyz` for readiness. |
| Metrics | `/metrics` is unauthenticated. | Expose only on ClusterIP service; document network policy / internal scrape assumptions. |
| Migrations | `server` runs `database.RunMigrations` on startup and now takes a PostgreSQL advisory lock. | Multi-replica startup is safe from migration races; CI verifies two app replicas, then scales to three, against embedded pgvector Postgres. |
| Workers | Web, outbox, embedding, reply-draft, digest, audit pruning, and batch workers run in the same process. | Multi-replica deployment is supported, but independent worker scaling requires future role flags. |
| Postgres | pgvector is required when clustering is enabled; CI uses `pgvector/pgvector:pg17`. | Embedded Postgres image is `pgvector/pgvector:pg17`. External DB docs require pgvector >= 0.5.0. |
| LLM | DB-managed channels/routes/keys, encrypted with Tink. | Do not render `llm.*` into `config.yaml`. Document post-install Console/API/CLI setup; optional idempotent bootstrap is a follow-up unless a new bootstrap command lands. |
| Lark | Current Lark support is outbound webhook/card delivery. | No `lark.oauth` values. Notification targets remain runtime data unless an idempotent bootstrap surface lands. |
| OIDC | OIDC is process config and includes a client secret. | Include OIDC fields in generated `config.yaml` or `existingSecret`; keep disabled by default. |

## Industry benchmarking

Benchmarked mature Kubernetes-adjacent projects for the chart shape Attune
should emulate. The repeated pattern is not "one values tree can encode every
production topology"; it is "small runnable core, production external
dependencies, explicit profiles, and broad escape hatches".

| Project | Relevant practice | Lesson for Attune |
| --- | --- | --- |
| Kubernetes | Deployments, HTTP probes, security contexts, and PodDisruptionBudgets are separate primitives. | Model rollout, health, security, and disruption explicitly instead of hiding them behind one `production: true` flag. |
| GitLab chart | Supports bundled services for evaluation, but production docs steer operators through external service migration. | Embedded dependencies are an on-ramp, not the production story. |
| Harbor chart | HA mode depends on external database, Redis, and shared storage. | Do not claim HA if the data plane is single-pod or local-PVC only. |
| Apache Airflow chart | Production guide emphasizes external DB, secrets, custom env, and executor-specific settings. | Values need escape hatches for platform policy, not just first-party fields. |
| Argo CD chart | HA is a profile with multiple components, anti-affinity, and scaling-specific guidance. | `replicaCount: 3` is not enough; migrations, queues, and disruption policy matter. |
| kube-prometheus-stack | CRD-dependent resources are powerful but highly conditional. | `ServiceMonitor` and dashboards must be gated and safe when CRDs are absent. |
| Grafana chart | Allows existing secrets, extra mounts, sidecars, dashboards, and service account annotations. | Production secrets and cloud identity must be operator-owned. |
| Istio charts | Uses profiles and separate charts/components for complex installs. | Profiles should set baselines while leaving fine-grained overrides available. |
| cert-manager chart | CRDs and cluster-scoped resources have explicit install/upgrade caveats. | Avoid surprising cluster-scoped behavior and document CRD ownership clearly. |
| HashiCorp Vault chart | Secure production requires explicit storage, unseal, TLS, resources, and HA choices. | A secure default may still be non-HA; production readiness is a documented checklist. |

## Proposal

### Chart layout

```text
deploy/helm/attune/
  Chart.yaml
  values.yaml
  templates/
    _helpers.tpl
    config-secret.yaml
    serviceaccount.yaml
    deployment.yaml
    service.yaml
    ingress.yaml
    hpa.yaml
    pdb.yaml
    postgres-secret.yaml
    postgres-service.yaml
    postgres-statefulset.yaml
    servicemonitor.yaml
    grafana-dashboard-configmap.yaml
    NOTES.txt
    tests/
      smoke-pod.yaml
  ci/
    values-kind.yaml
    values-distributed-kind.yaml
    values-external-postgres.yaml
  values.schema.json
```

Use `apiVersion: v2`. Set `appVersion` to the Attune release that contains the
same runtime contract as the chart. `image.tag` defaults to `.Chart.AppVersion`,
not the mutable `latest` tag, even though the issue says "latest ghcr release".
Operators can still opt into `latest` explicitly.

### Values model

The chart should have a runnable dev profile and an explicit production path:

```yaml
profile: dev

image:
  repository: ghcr.io/phixsura/attune
  tag: ""
  pullPolicy: IfNotPresent
  pullSecrets: []

replicaCount: 1

app:
  port: 8090

autoscaling:
  enabled: false
  minReplicas: 2
  maxReplicas: 10
  targetCPUUtilizationPercentage: 70
  targetMemoryUtilizationPercentage: 80

service:
  type: ClusterIP
  port: 8090
  annotations: {}

ingress:
  enabled: false
  className: ""
  annotations: {}
  hosts: []
  tls: []

config:
  existingSecret: ""
  existingSecretChecksum: ""
  secretKey: config.yaml
  mountPath: /app/config
  render:
    enabled: true
    port: 8090
    databaseURL: ""
    migrations:
      confirmLarkDelete: false
    console:
      baseURL: ""
      sessionKey: ""
      bootstrapAdmin:
        email: ""
        password: ""
    secrets:
      tinkKeyset: ""
      legacyInboundMasterKey: ""
    observability:
      serviceVersion: ""
      environment: production
      otlpEndpoint: ""
      otlpTracesPath: /opentelemetry/v1/traces
      otlpHeaders: {}
      otlpInsecure: false
    rateLimit:
      perMinute: 60
      burst: 300
      disabled: false
    oidc:
      enabled: false
      issuerURL: ""
      clientID: ""
      clientSecret: ""
      redirectURI: ""
      scopes: ["openid", "email", "profile"]
      userClaim: email
      groupsClaim: groups
      providerName: SSO
      roleMapping: []
      allowedGroups: []
      oidcOnly: false
      skipIssuerCheck: false
      insecureSkipVerify: false
    customWebhooks: []

postgres:
  embedded:
    enabled: true
    image:
      repository: pgvector/pgvector
      tag: pg17
      pullPolicy: IfNotPresent
    auth:
      database: attune
      username: attune
      password: ""
      existingSecret: ""
      secretKeys:
        username: username
        password: password
    persistence:
      enabled: true
      size: 10Gi
      storageClass: ""
    resources: {}
    podSecurityContext: {}
    securityContext: {}
  external:
    enabled: false
    # The DSN must still be present in the mounted config.yaml. For production,
    # put the complete config.yaml in config.existingSecret. For dev/CI, set
    # config.render.databaseURL directly.

serviceMonitor:
  enabled: false
  labels: {}
  interval: 30s
  scrapeTimeout: 10s

grafanaDashboard:
  enabled: false
  labels:
    grafana_dashboard: "1"

podAnnotations: {}
podLabels: {}
resources: {}
probes:
  liveness:
    enabled: true
    path: /healthz
  readiness:
    enabled: true
    path: /readyz
  startup:
    enabled: true
    path: /healthz
nodeSelector: {}
tolerations: []
affinity: {}
topologySpreadConstraints: []
priorityClassName: ""
runtimeClassName: ""
terminationGracePeriodSeconds: 60
revisionHistoryLimit: 10
strategy: {}

podDisruptionBudget:
  enabled: false
  minAvailable: 1

networkPolicy:
  enabled: false
  ingress: []
  egress: []

serviceAccount:
  create: true
  name: ""
  annotations: {}

securityContext:
  runAsNonRoot: true
  runAsUser: 65532
  runAsGroup: 65532
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
  capabilities:
    drop: ["ALL"]

podSecurityContext:
  fsGroup: 65532

extraEnv: []
extraEnvFrom: []
extraVolumes: []
extraVolumeMounts: []
nameOverride: ""
fullnameOverride: ""
commonLabels: {}
commonAnnotations: {}
```

The chart may use `profile: production` to flip recommendations in docs and
examples, but it should not hide important behavior. Operators must still be
able to set every underlying field directly.

### Config and secrets

Default behavior:

1. If `config.existingSecret` is set, mount that secret key under
   `/app/config/` and run Attune with `/app/config/config.yaml`.
2. Otherwise render a Kubernetes Secret containing the full Attune
   `config.yaml`. This path is for dev, CI, and disposable installs, and it
   fails fast unless the operator supplies `config.render.secrets.tinkKeyset`.
3. If embedded Postgres is enabled and no explicit `databaseURL` is supplied,
   synthesize the DSN only when the Postgres password is available as a chart
   value managed by the same release. Empty embedded Postgres passwords fail
   template rendering instead of producing a broken StatefulSet.
4. If the Postgres password or external DSN lives in an existing Kubernetes
   Secret, require `config.existingSecret` with the complete `config.yaml`, or
   require an explicit literal `config.render.databaseURL` for dev/CI.

The full generated config is a Secret because it can contain credentials. A
ConfigMap split would be nicer for diffability, but current Attune cannot merge
non-secret and secret fragments at runtime. The production recommendation is
`config.existingSecret`, created by External Secrets Operator, Sealed Secrets,
SOPS, Vault Agent, cloud secret sync, or a platform pipeline.

Do not put production database URLs, session keys, OIDC client secrets, Tink
keysets, or custom webhook secrets in Helm values. Helm stores rendered manifests
in release history, so a chart-rendered Secret is not an adequate production
secret boundary.

Helm-generated random secrets should be avoided for long-lived critical values.
In particular, do not generate a Tink keyset with Helm templating. For dev,
`NOTES.txt` can explain how to generate:

```bash
attune secrets generate-keyset
```

For production, docs should require operators to create and back up the Tink
keyset and session key before install. Losing the Tink keyset can make encrypted
provider credentials unreadable.

### Deployment

The Attune Deployment:

- runs `/app/attune --config /app/config/config.yaml server`;
- mounts config as a Secret directory at `/app/config`;
- exposes container port 8090 named `http`;
- uses HTTP startup and liveness probes against `/healthz`;
- uses HTTP readiness probes against `/readyz`, which checks Postgres after the
  server has booted;
- marks `/readyz` unhealthy on `SIGTERM`, waits a bounded drain delay, then
  calls `http.Server.Shutdown` with a bounded timeout;
- keeps `/metrics` on the same port for internal scraping;
- uses secure defaults matching the distroless image:
  - `runAsNonRoot: true`;
  - `runAsUser: 65532`;
  - `allowPrivilegeEscalation: false`;
  - `readOnlyRootFilesystem: true`;
  - `capabilities.drop: ["ALL"]`;
- supports `resources`, `nodeSelector`, `affinity`, `tolerations`,
  `topologySpreadConstraints`, `priorityClassName`, pod annotations, labels,
  service account annotations, lifecycle hooks, extra init containers, sidecars,
  and extra mounts.

Because background workers run inside the same server process today, the first
chart should not split worker Deployments. A future chart can split web and
worker roles only after the binary has explicit role flags.

### Migration safety and HA

Production multi-replica correctness requires application-level migration
serialization because every server process checks migrations on startup. This
change uses a PostgreSQL advisory lock around `database.RunMigrations`, which
protects Compose, bare-metal, and Kubernetes deployments with one app-level
mechanism.

With migration locking in place:

- `replicaCount` still defaults to 1 for the smallest dev install;
- production examples can use `replicaCount: 2` or `autoscaling.enabled: true`;
- CI kind smoke installs two app replicas, scales the release to three app
  replicas, and verifies that the Attune Service has exactly the app endpoints
  while the embedded Postgres Service only has the Postgres endpoint;
- CI also renders blue/green app/router values and switches a stable traffic
  Service from blue endpoints to green endpoints in kind;
- PDB and topology spread examples are documented for multi-zone clusters.
- Helm validation rejects production values without external Postgres,
  production single-replica values, rendered config without a database URL,
  invalid HPA bounds, HPA without resource requests, PDBs that block every
  voluntary eviction, rendered config port mismatches, and empty NetworkPolicy
  rule sets.

Independent worker scaling remains a follow-up. The current binary starts web
traffic and background workers in the same process, so HPA scales both together.

### Rolling and blue/green release

Rolling release uses the normal Deployment with readiness-gated pods,
`maxUnavailable: 0`, `maxSurge: 1`, `minReadySeconds`, and
`progressDeadlineSeconds`. Operators should use `helm upgrade --atomic --wait`
for normal backwards-compatible releases.

Blue/green release is modeled as two workload releases plus one
traffic-service-only router release. Each workload release sets
`rollout.color` and keeps its normal per-release Service for smoke tests. The
router release renders only a stable Service whose selector is
`app.kubernetes.io/name`, `app.kubernetes.io/component=app`, and
`attune.phixsura.dev/color=<activeColor>`. Switching traffic means upgrading
only the router release's `rollout.trafficService.activeColor`; Deployment
selectors stay immutable and untouched.
Ingress and ServiceMonitor can be rendered from the router release and target
that stable traffic Service, while each app release keeps a per-release Service
for smoke tests and debugging.

This does not make database migrations magically blue/green safe. App releases
running at the same time must be compatible with the same schema. Destructive
schema changes require an expand/migrate/contract plan before blue/green is
used.

### Postgres

Embedded Postgres:

- is a `StatefulSet`;
- uses `pgvector/pgvector:pg17`;
- has a headless or ClusterIP service named through helpers;
- stores data on a PVC;
- is explicitly documented as non-HA and not a backup solution.

External Postgres:

- is the recommended production path;
- must be PostgreSQL 14+ with pgvector >= 0.5.0 when clustering is enabled;
- should use TLS where the provider requires it;
- should be supplied through `config.existingSecret` for production, or literal
  `config.render.databaseURL` for dev/CI renders.

The chart should not try to create the `vector` extension outside Attune's own
migration flow. Attune migration 025 already runs `CREATE EXTENSION IF NOT
EXISTS vector`; managed Postgres permissions must allow that or operators must
preinstall the extension.

### Networking

Render:

- a ClusterIP Service by default;
- optional `service.type: LoadBalancer`;
- optional Ingress gated by `ingress.enabled`.

The chart should not default to public exposure. Ingress examples can show:

- nginx ingress;
- cert-manager annotations;
- TLS secret wiring;
- Console `base_url` matching the public URL.

Gateway API support can be a follow-up. For now, operators can use
`extraObjects` only if we add it deliberately; otherwise they can layer Gateway
manifests outside the chart.

### Observability

ServiceMonitor:

- disabled by default;
- rendered only when `serviceMonitor.enabled=true`;
- selects the Attune service and scrapes `/metrics`;
- documents that `/metrics` is unauthenticated and should stay cluster-internal.

Grafana dashboard:

- disabled by default;
- packages existing `observability/dashboards/*.json` into ConfigMaps when
  enabled;
- labels are configurable for sidecar-based dashboard discovery.

The chart does not install Prometheus or Grafana. It only integrates with an
operator's existing observability stack.

NetworkPolicy should be disabled by default for compatibility but documented for
production. When enabled, it should allow ingress only from the operator's
ingress controller and monitoring namespace, and allow egress to Postgres, LLM
providers, OIDC issuer endpoints, and configured outbound notification targets.

### LLM, notification targets, and Lark

Attune's LLM and notification destinations are runtime resources, not process
YAML. The chart should not put provider keys in `config.yaml` and should not
model deleted Lark OAuth.

For the first chart:

- document post-install LLM setup through Console/API/CLI;
- document Lark/Slack/raw-webhook notification target setup through Console;
- provide examples for running one-shot CLI commands in Kubernetes, using the
  same mounted config secret.

If strict "configure all features through values" remains a hard requirement,
add a prerequisite or companion feature: an idempotent bootstrap command such as
`attune bootstrap apply --file /app/bootstrap.yaml`. That command would apply
LLM channels, abilities, routes, notify targets, digest subscriptions, and other
runtime resources safely. Without that command, a Helm hook Job would either be
non-idempotent or require shell/JQ tooling that the distroless image does not
contain.

### Publishing

Publish the chart as an OCI artifact in GHCR:

```bash
helm pull oci://ghcr.io/phixsura/charts/attune --version <chart-version>
helm install attune oci://ghcr.io/phixsura/charts/attune --version <chart-version>
```

The release workflow also attaches the packaged `.tgz` chart to the matching
GitHub Release.

Artifact Hub can index OCI charts with repository metadata. A `gh-pages`
`index.yaml` repository can be added later if users need classic
`helm repo add`, but GHCR OCI keeps chart and image publishing close together.

## Alternatives considered

### Split ConfigMap and Secret with an initContainer

Rejected for the first chart. It would require a shell or merge binary in an
initContainer and would create another place where config semantics can diverge
from the app. It also makes secret provenance harder to reason about. Mounting
one complete `config.yaml` Secret matches the current runtime contract.

### Helm hook migration Job

Viable, but not the first choice. Helm hooks are easy to strand during failed
upgrades, behave differently under GitOps controllers, and protect only Helm
deployments. A Postgres advisory lock in `RunMigrations` protects every
deployment mechanism.

### Depend on Bitnami PostgreSQL or another subchart

Rejected for first implementation. A subchart adds a large values surface and
supply-chain dependency. Attune needs pgvector specifically and only a small
dev/kind StatefulSet. Production should use external Postgres.

### Make embedded Postgres production-capable

Rejected. Real production needs backups, PITR, replication, restore drills,
storage class choices, upgrade procedures, and often a managed provider. The
chart can make development easy without pretending to be a database operator.

### Encode LLM and Lark as process config values

Rejected. It would violate the current config-first proposal and security
baseline. Provider keys are DB-managed write-only inputs encrypted with the
configured Tink keyset. Lark OAuth ingest was removed; current Lark is outbound
webhook delivery.

## Risks / tradeoffs

- **Combined web/worker scaling**: multi-replica installs are safe from startup
  migration races, but HPA scales web traffic and background workers together
  until explicit role flags land.
- **Secret blast radius**: a full `config.yaml` Secret contains all process
  secrets. This is accurate to today's app, but RBAC and external secret
  management must be documented.
- **Embedded database expectations**: users may mistake the embedded StatefulSet
  for production HA. Docs and values comments must be blunt.
- **pgvector permissions**: managed Postgres may block `CREATE EXTENSION`.
  Production docs need a preflight section.
- **Mutable image tags**: defaulting to `latest` is convenient but poor for
  reproducibility. Use chart `appVersion` by default and document overrides.
- **CRD-dependent observability**: ServiceMonitor fails if rendered without the
  CRD. Keep it gated and off by default.
- **Ingress diversity**: no chart can cover every ingress controller. Keep the
  base manifest simple and provide annotations/escape hatches.

## Implementation plan

### Phase 1: safe install

1. Add this proposal and get agreement on the adjusted acceptance criteria,
   especially config secrets, LLM/Lark runtime resources, and HA gating.
2. Create `deploy/helm/attune/Chart.yaml`, `values.yaml`, helpers, and template
   tests.
3. Add config Secret rendering and `existingSecret` support, with production
   docs steering operators to `existingSecret`.
4. Add embedded pgvector Postgres StatefulSet, service, PVC, and secret support.
5. Add Attune Deployment, Service, startup/liveness/readiness probes, security
   contexts, resources, service account, PDB, HPA, NetworkPolicy, and scheduling
   escape hatches.
6. Add optional Ingress, ServiceMonitor, and Grafana dashboard ConfigMaps.
7. Add `docs/k8s-deploy.md` with quickstart, production, upgrades, backups,
   observability, and troubleshooting.
8. Extend CI deploy checks:
   - path filtering that fans proto changes into Go, Console, and proto jobs,
     and fans Go, Console, Dockerfile, and deploy changes into image and Helm
     jobs;
   - backend gates: vet, build, race tests, integration tests, lint facade
     checks, raw-pointer checks, error-code checks, complexity, duplication,
     coverage regression, and vulnerability scanning;
   - frontend gates: Biome, Vite production build, TypeScript, Vitest coverage,
     dependency-cruiser architecture checks, and coverage regression;
   - contract gates: `buf lint`, breaking-change check, and generated output
     drift detection;
   - `helm lint deploy/helm/attune`;
   - values schema validation through `values.schema.json`;
   - `helm template` snapshots for kind, external-Postgres, and production HPA
     values, plus blue/green app/router values;
   - negative `helm template` validation for unsafe value combinations;
   - kubeconform validation for rendered manifests;
   - multi-replica kind install, Helm DNS/TCP/HTTP smoke test, Service endpoint
     isolation checks, upgrade, scale-out to three replicas, and post-scale-out
     tenant/API-key smoke using embedded pgvector Postgres in CI.
   - sudden-failure kind smoke that force-deletes one ready app pod after
     scale-out, probes the Service, waits for Deployment recovery, and verifies
     the killed pod is no longer a ready endpoint.
   - blue/green kind smoke that installs router, blue, and green releases, then
     verifies the stable Service switches from blue endpoints to green
     endpoints.
9. Update `CHANGELOG.md` under `[Unreleased]` / `### Added` in the
    implementation PR.

### Phase 2: production correctness

1. Add migration serialization with a Postgres advisory lock around
   `database.RunMigrations`, plus unit/integration coverage.
2. Add `/readyz` with a DB ping and wire the chart readiness probe to it.
3. Add upgrade smoke preserving Postgres data. For the first chart release,
   upgrade the same chart from one values profile to another; from the second
   release onward, test N-1 chart to candidate.
4. Add chart packaging/publishing to the release workflow.

Phase 2 items 1, 2, and 4 are implemented with this chart change. The CI upgrade
smoke now creates a tenant before upgrade and issues an API key after upgrade so
the basic data path is exercised.

### Phase 3: operational completeness

1. Add an idempotent `attune bootstrap apply` surface for LLM channels, routes,
   notify targets, digest subscriptions, and other DB-backed runtime resources.
2. Add explicit web/worker role flags so Helm can scale web and workers
   independently.
3. Promote HPA production examples after role split and capacity guidance land.

## Verification

Local:

```bash
go run ./cmd/attune secrets generate-keyset >/tmp/attune-keyset.json
helm lint deploy/helm/attune \
  -f deploy/helm/attune/ci/values-kind.yaml \
  --set-file config.render.secrets.tinkKeyset=/tmp/attune-keyset.json
helm lint deploy/helm/attune -f deploy/helm/attune/ci/values-external-postgres.yaml
helm template attune deploy/helm/attune \
  -f deploy/helm/attune/ci/values-kind.yaml \
  --set-file config.render.secrets.tinkKeyset=/tmp/attune-keyset.json \
  >/tmp/attune-kind.yaml
helm template attune deploy/helm/attune \
  -f deploy/helm/attune/ci/values-external-postgres.yaml \
  >/tmp/attune-external.yaml
docker run --rm -v /tmp:/tmp ghcr.io/yannh/kubeconform:v0.6.7 \
  -strict -ignore-missing-schemas \
  /tmp/attune-kind.yaml \
  /tmp/attune-external.yaml
```

Kind smoke:

```bash
kind create cluster --name attune-chart
helm install attune deploy/helm/attune \
  -f deploy/helm/attune/ci/values-kind.yaml \
  -f deploy/helm/attune/ci/values-distributed-kind.yaml \
  --set-file config.render.secrets.tinkKeyset=/tmp/attune-keyset.json \
  --wait --timeout 5m
kubectl rollout status deploy/attune --timeout 5m
helm test attune --namespace attune --timeout 5m
kubectl -n attune get endpointslice -l kubernetes.io/service-name=attune
helm upgrade attune deploy/helm/attune \
  --reuse-values \
  --set replicaCount=3 \
  --wait --timeout 5m
helm test attune --namespace attune --timeout 5m
kubectl -n attune get endpointslice -l kubernetes.io/service-name=attune
```

Upgrade smoke:

- install chart version N-1 or a packaged baseline;
- create a tenant and API key;
- upgrade to the candidate chart;
- verify the tenant remains by issuing an API key after upgrade;
- verify Service DNS, TCP connectivity, `/healthz`, and `/readyz` pass through
  `helm test`;
- verify EndpointSlices are isolated by component labels.

Production dry render:

- external Postgres only;
- `config.existingSecret` set;
- embedded Postgres disabled;
- ServiceMonitor enabled with CRD available;
- Ingress enabled with TLS;
- HPA enabled with external Postgres and topology spread.

Helm test:

- use dedicated curl/debug images, not the distroless Attune image;
- verify short and fully qualified Service names;
- verify TCP connectivity to Attune and embedded Postgres when enabled;
- call the in-cluster service `/healthz` and `/readyz`.

## References

- Kubernetes Deployments: <https://kubernetes.io/docs/concepts/workloads/controllers/deployment/>
- Kubernetes probes: <https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/>
- Kubernetes security context: <https://kubernetes.io/docs/tasks/configure-pod-container/security-context/>
- Kubernetes PodDisruptionBudget: <https://kubernetes.io/docs/tasks/run-application/configure-pdb/>
- Helm chart best practices: <https://helm.sh/docs/chart_best_practices/>
- Helm values best practices: <https://helm.sh/docs/chart_best_practices/values/>
- GitLab Helm chart docs: <https://docs.gitlab.com/charts/>
- Harbor HA Helm docs: <https://goharbor.io/docs/main/install-config/harbor-ha-helm/>
- Apache Airflow Helm production guide: <https://airflow.apache.org/docs/helm-chart/stable/production-guide.html>
- Argo CD HA docs: <https://argo-cd.readthedocs.io/en/release-2.5/operator-manual/high_availability/>
- kube-prometheus-stack chart: <https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack>
- Grafana Helm chart docs: <https://grafana.com/docs/grafana/latest/setup-grafana/installation/helm/>
- Istio Helm install docs: <https://istio.io/latest/docs/setup/install/helm/>
- cert-manager Helm install docs: <https://cert-manager.io/docs/installation/helm/>
- Vault Helm production docs: <https://developer.hashicorp.com/vault/docs/deploy/kubernetes/helm/run>
