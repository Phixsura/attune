# HA Worker Leases and Queue-Drain Safety

| Field | Value |
|-------|-------|
| Issue | [#155](https://github.com/Phixsura/attune/issues/155) |
| Status | Implemented |
| Started | 2026-06-25 |
| Related | — |

---

## Problem

Production Helm guidance recommends multiple replicas, but several background
workers process queues with varying degrees of ownership semantics. Without
explicit lease coordination, multi-replica deployments risk:

1. **Double-processing** — two replicas claim and process the same queue item
2. **Stuck tasks** — a crashed replica's in-flight work never recovers
3. **Silent abandonment** — shutdown drops in-flight work without reporting
4. **Blind ops** — no Console visibility into worker ownership or backlog health

### Current Worker Inventory

| Worker | Lease Mechanism | Heartbeat | Stale Recovery | Drain |
|--------|-----------------|-----------|----------------|-------|
| **OutboxWorker** | `claimed_at/claimed_by` + `FOR UPDATE SKIP LOCKED` | ✓ (3-min refresh) | ✓ (10-min on boot) | ✗ |
| **DraftWorker** | `TryClaim` (claimed_at) | ✗ | ✓ (5-min on boot) | ✗ |
| **DigestWorker** | `TryClaim` | ✗ | ✓ (5-min on boot) | ✗ |
| **EmbeddingWorker** | `TryClaim` | ✗ | ✓ (5-min on boot) | ✗ |
| **BatchJobWorker** | `Claim` + heartbeat | ✓ | ✓ (`recoverStuckJobs`) | ✗ |
| **GDPRWorker** | `ClaimNextExportJob` | ✗ (partial) | ✗ | ✗ |
| **EnrichRunner** | `TryClaim` (per-row) | ✗ | repo-level only | ✗ |
| **Pruners** (audit, idempotency, mcp) | ✗ none | ✗ | ✗ | ✗ |

**Observations:**

- OutboxWorker and BatchJobWorker have the most complete lease semantics
  (claim + heartbeat + stale recovery), but even they lack shutdown drain
- Single-claim workers (Draft, Digest, Embedding) reset stale claims on boot
  but lack heartbeat — a long-running task may be re-claimed mid-execution
- Pruners have no claim at all — multiple replicas may run identical prune
  queries concurrently (wasteful but idempotent, not double-processing)
- EnrichRunner uses per-row `TryClaim` which handles contention but has no
  global worker identity or heartbeat
- No worker reports drain status on shutdown

---

## Industry Survey: World-Class Queue Systems

### PostgreSQL-Native Queues

#### River (Go + PostgreSQL)

[River](https://github.com/riverqueue/river) is the leading Go PostgreSQL job
queue, achieving **1,000–10,000 jobs/sec** on typical hardware.

**Key architecture decisions:**

| Pattern | Implementation | Rationale |
|---------|---------------|-----------|
| Atomic claim | `FOR UPDATE SKIP LOCKED` in CTE | Single query claims + updates status |
| Batch locking | One producer locks for all executors | Reduces lock contention within process |
| Transactional enqueue | Jobs enqueued in same tx as business data | No lost jobs if tx rolls back |
| Heartbeat reaper | Background goroutine checks `heartbeat_at` | Detects crashed workers within 30s |

**Heartbeat pattern:** Workers update `heartbeat_at` every 5 seconds; a reaper
process automatically requeues jobs that haven't heartbeated within 30 seconds.

#### pg-boss (Node.js + PostgreSQL)

[pg-boss](https://github.com/timgit/pg-boss) provides exactly-once delivery via
SKIP LOCKED with these guarantees:

- Jobs created within existing transactions (ORM adapters)
- Automatic job expiration and archival
- Completion callbacks and job dependencies

#### Graphile Worker (Node.js + PostgreSQL)

[Graphile Worker](https://worker.graphile.org/) optimizes for PostgreSQL
triggers, achieving ~100-200 jobs/sec before lock contention.

### Distributed Workflow Engines

#### Temporal / Cadence (Uber)

[Temporal](https://temporal.io/) provides **durable execution** — workflows
survive crashes by replaying event history on new workers.

**Key reliability patterns:**

| Concept | Mechanism | Benefit |
|---------|-----------|---------|
| Event sourcing | Every step persisted as event | Crash recovery via replay |
| Task queues | Lightweight dynamic queues | Workers poll for tasks |
| Activity heartbeat | Periodic check-in during long ops | Fast failure detection |
| Exactly-once workflow | Server mediates all execution | No duplicate side effects |

**Heartbeat timeout:** If activity doesn't heartbeat within configured interval,
Temporal times out the activity and retries on a different worker. Last
heartbeat details are returned to the workflow for resume.

**Scale:** "Millions of timers off a single worker" — timers are durable and
resource-light.

### Redis-Based Queues

#### Sidekiq (Ruby + Redis)

[Sidekiq](https://sidekiq.org/) provides **at-least-once** delivery (not
exactly-once). Design implications:

- Jobs removed from Redis immediately on BRPOP (lost if worker crashes)
- **Sidekiq Pro's super_fetch**: uses LMOVE to keep running jobs in Redis until
  acknowledged — higher reliability at CPU/network cost
- **Idempotency required**: application must handle duplicate execution

#### BullMQ (Node.js + Redis)

[BullMQ](https://docs.bullmq.io/) uses **lock-based stall detection**:

| Config | Default | Purpose |
|--------|---------|---------|
| `lockDuration` | 30s | Lock expires if not renewed |
| `stalledInterval` | 15s | Check frequency for stalled jobs |
| `maxStalledCount` | 1 | Max stalls before permanent failure |

**Stall detection flow:**
1. Worker acquires lock on job
2. Worker renews lock every `lockDuration/2`
3. If renewal missed, job marked "stalled"
4. Another worker picks up stalled job
5. After `maxStalledCount` stalls, job fails permanently

**Graceful shutdown:** Worker stops picking new jobs, waits for current jobs to
complete (no built-in timeout — application must handle).

### Kubernetes Controller Pattern

[Kubernetes controllers](https://kubernetes.io/docs/concepts/cluster-administration/coordinated-leader-election/)
use **work queues with rate limiting**:

**Leader election via Lease API:**
- Lease object acts as distributed lock
- Optimistic concurrency via `resourceVersion`
- Only leader performs reconciliation; others standby

**Work queue pattern:**
- Event handlers add items to queue (return immediately)
- Worker goroutines process with rate limiting and retry
- Failed items re-queued with exponential backoff

**Rate limiting:** Prevents hot loops; gives transient errors time to resolve.

### Celery (Python + Redis/RabbitMQ)

[Celery](https://docs.celeryq.dev/) patterns relevant to attune:

| Setting | Default | Effect |
|---------|---------|--------|
| `task_acks_late` | False | When True, ack after execution (not before) |
| `visibility_timeout` | 1 hour (Redis) | Redelivery if not acked |
| `worker_prefetch_multiplier` | 4 | Tasks prefetched per process |

**Late acknowledgment:** With `task_acks_late=True`, if worker dies mid-task,
broker redelivers. Trade-off: possible duplicate execution.

### Delivery Semantics Comparison

| System | Guarantee | Trade-off |
|--------|-----------|-----------|
| Temporal | Exactly-once (workflow) | Higher coordination cost |
| River/pg-boss | Exactly-once (via SKIP LOCKED) | PostgreSQL-bound |
| Sidekiq | At-least-once | Requires idempotent jobs |
| BullMQ | At-least-once | Lock overhead for stall detection |
| Kafka | Exactly-once (with transactions) | Complex producer/consumer setup |

**Industry consensus:** "Exactly-once delivery is impossible in distributed
systems" — but **exactly-once processing** is achievable via idempotent
consumers + transactional writes.

### Key Patterns Summary

| Pattern | Used By | attune Status |
|---------|---------|---------------|
| `FOR UPDATE SKIP LOCKED` | River, pg-boss, Graphile | ✓ All claim-based workers |
| Heartbeat + reaper | River, Temporal, BullMQ | ✓ All workers (90s interval, 5min stale) |
| Fencing tokens (claimed_by) | Stripe, BullMQ | ✓ All workers — `WHERE claimed_by = $owner` |
| Transactional enqueue | River, Stripe | ✓ Outbox pattern |
| Late acknowledgment | Celery, Sidekiq Pro | ✓ Implicit (claim → process → mark) |
| Graceful drain | BullMQ, Temporal, K8s | ✓ 30s timeout via workerdrain pkg |
| Stale claim recovery | All | ✓ On boot + periodic RecoverStuck |
| Advisory locks (singleton) | K8s (Lease), pg patterns | ✓ Pruners use dedicated conn locks |
| Batch claiming | River | ✓ OutboxWorker (configurable batch) |
| Progress checkpoints | Temporal | ✓ BatchJobWorker (progress field) |

### Recommended Adoption

Based on this survey, attune should:

1. **Keep PostgreSQL-native approach** — River/pg-boss patterns validate our
   `FOR UPDATE SKIP LOCKED` + heartbeat strategy
2. **Add heartbeat to all claim-based workers** — following BullMQ's
   `lockDuration/2` renewal pattern
3. **Implement graceful drain** — BullMQ/Temporal pattern: stop accepting,
   wait with timeout, report status
4. **Use advisory locks for pruners** — K8s Lease pattern simplified for pg
5. **Expose queue health API** — Temporal's observability as model

---

## Deep Dive: Implementation Patterns

### Atomic Claim-Update SQL (River/pg-boss Pattern)

The canonical CTE pattern for atomic claim + status update:

```sql
-- Atomic dequeue: claim + update in single statement
WITH claimable AS (
    SELECT id
    FROM notify_outbox
    WHERE status = 'pending'
      AND (claimed_at IS NULL OR claimed_at < NOW() - INTERVAL '10 minutes')
      AND next_retry_at <= NOW()
    ORDER BY next_retry_at, id
    LIMIT 10
    FOR UPDATE SKIP LOCKED
)
UPDATE notify_outbox o
SET status = 'processing',
    claimed_at = NOW(),
    claimed_by = $1  -- worker_id
FROM claimable c
WHERE o.id = c.id
RETURNING o.*;
```

**Why this works:**

1. `FOR UPDATE SKIP LOCKED` — workers never block each other; each gets
   different rows
2. Single statement — no race between SELECT and UPDATE
3. Transaction-bound lock — crash rolls back, releases lock, row returns to
   pending
4. `claimed_at` expiry check — stale claims auto-recoverable by any worker

### Heartbeat Renewal Pattern (BullMQ/Temporal)

**Rule of thumb:** Heartbeat interval = `lockDuration / 2` to `lockDuration / 3`

```
┌─────────────────────────────────────────────────────────────────┐
│                    Heartbeat Timeline                            │
├─────────────────────────────────────────────────────────────────┤
│  0s        30s       60s       90s      120s                    │
│  │          │         │         │         │                     │
│  ├──claim───┼────hb───┼────hb───┼────hb───┼──complete           │
│  │          │         │         │         │                     │
│  │          └─────────┴─────────┴─────────┘                     │
│  │             lockDuration = 90s                               │
│  │             heartbeat every 30s (1/3 of lock)                │
│  │                                                               │
│  │  If heartbeat missed at 60s:                                 │
│  │  └── Stall detected at 90s (lockDuration expired)            │
│  │  └── Another worker can re-claim at 90s+                     │
└─────────────────────────────────────────────────────────────────┘
```

**Temporal's heartbeat detail pattern:**

```go
// Heartbeat with progress — enables resume on retry
func (w *Worker) processLargeExport(ctx context.Context, task *Task) {
    for i := task.ResumeFrom; i < len(task.Items); i++ {
        processItem(task.Items[i])
        
        // Heartbeat with checkpoint
        w.heartbeat(ctx, HeartbeatDetails{
            Progress:   i,
            LastItemID: task.Items[i].ID,
        })
    }
}

// On retry after crash, details are available:
// task.ResumeFrom = lastHeartbeatDetails.Progress
```

### Stripe's Atomic Phases and Recovery Points

Stripe's idempotency pattern tracks progress through discrete "atomic phases":

```
┌─────────────────────────────────────────────────────────────────┐
│                Stripe Atomic Phases Model                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Request with idempotency_key                                    │
│       │                                                          │
│       ▼                                                          │
│  ┌─────────────────┐                                             │
│  │ recovery_point: │                                             │
│  │   "started"     │◄──── DB record created                      │
│  └────────┬────────┘                                             │
│           │                                                      │
│           ▼  Phase 1: Local mutations (single tx)                │
│  ┌─────────────────┐                                             │
│  │ recovery_point: │                                             │
│  │   "charge_created" │◄── Commit includes recovery_point update │
│  └────────┬────────┘                                             │
│           │                                                      │
│           ▼  Foreign call: Stripe API                            │
│  ┌─────────────────┐                                             │
│  │ recovery_point: │                                             │
│  │   "charge_captured" │                                         │
│  └────────┬────────┘                                             │
│           │                                                      │
│           ▼  Phase 2: Local mutations                            │
│  ┌─────────────────┐                                             │
│  │ recovery_point: │                                             │
│  │   "finished"    │                                             │
│  └─────────────────┘                                             │
│                                                                  │
│  Completer process: finds requests stuck at non-"finished"       │
│  recovery points and resumes from that phase                     │
└─────────────────────────────────────────────────────────────────┘
```

**Transactionally-staged job drain:**

```go
// Jobs are inserted into staging table within business tx
// NOT directly into queue — prevents lost jobs if crash between commit and enqueue
func (s *Service) CreateOrder(ctx context.Context, order Order) error {
    tx, _ := s.db.Begin(ctx)
    defer tx.Rollback(ctx)
    
    // Business logic
    tx.Exec(ctx, "INSERT INTO orders ...")
    
    // Stage job in same tx — not visible to workers until commit
    tx.Exec(ctx, "INSERT INTO job_staging (payload, created_at) VALUES ($1, NOW())", jobPayload)
    
    return tx.Commit(ctx)
}

// Separate drainer moves staged jobs to queue after commit
func (d *Drainer) Run(ctx context.Context) {
    for {
        // Move committed staged jobs to real queue
        d.db.Exec(ctx, `
            WITH moved AS (
                DELETE FROM job_staging
                WHERE created_at < NOW() - INTERVAL '1 second'  -- ensure committed
                RETURNING *
            )
            INSERT INTO job_queue SELECT * FROM moved
        `)
    }
}
```

### Kubernetes Graceful Shutdown Sequence

```
┌─────────────────────────────────────────────────────────────────┐
│             Kubernetes Pod Termination Timeline                  │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  T+0s   Pod marked for deletion                                  │
│    │    ├── Removed from Service endpoints (async)               │
│    │    └── preStop hook starts (if configured)                  │
│    │                                                             │
│    ▼                                                             │
│  T+0s   SIGTERM sent to PID 1                                    │
│    │    └── preStop runs IN PARALLEL, not before!                │
│    │                                                             │
│    ▼                                                             │
│  T+Xs   preStop completes (e.g., sleep 10)                       │
│    │    └── Traffic may still be arriving during this window     │
│    │                                                             │
│    ▼                                                             │
│  T+Ys   Application drain completes                              │
│    │    └── In-flight requests finished                          │
│    │    └── Workers drained queue items                          │
│    │                                                             │
│    ▼                                                             │
│  T+30s  terminationGracePeriodSeconds expires (default)          │
│    │    └── SIGKILL sent — process forcibly killed               │
│    │                                                             │
│  ⚠️  If drain takes >30s, work is abandoned!                      │
│                                                                  │
│  Best practice formula:                                          │
│  terminationGracePeriodSeconds = preStop + maxDrainTime + buffer │
│  Example: 10s + 20s + 15s = 45s                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## Deep Dive: Production Failure Modes

### 1. Lock Contention and Hot Partitions

**Problem:** All workers contend for the same few rows at the head of the queue.

```
┌─────────────────────────────────────────────────────────────────┐
│                   Lock Contention Cascade                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Worker 1 ──┐                                                    │
│  Worker 2 ──┼──► All query: WHERE status='pending' ORDER BY id   │
│  Worker 3 ──┤        │                                           │
│  Worker 4 ──┘        ▼                                           │
│                 ┌─────────┐                                      │
│                 │ Row 1   │◄── All workers lock same row         │
│                 ├─────────┤                                      │
│                 │ Row 2   │◄── Only after Row 1 released         │
│                 ├─────────┤                                      │
│                 │ Row 3   │                                       │
│                 └─────────┘                                      │
│                                                                  │
│  Result: Convoy effect — workers serialize on lock acquisition   │
└─────────────────────────────────────────────────────────────────┘
```

**Solutions (from DBOS benchmarks achieving 30.6K workflows/sec):**

1. **Partition by tenant/queue** — distribute work across multiple queues
2. **Batch claiming** — claim 10 rows at once, not 1
3. **Partial index** — `CREATE INDEX idx_pending ON jobs (created_at) WHERE status = 'pending'`
   - 200× smaller than full index if 50K pending vs 10M total
4. **Avoid hot head** — add jitter to `ORDER BY`: `ORDER BY next_retry_at + (random() * interval '1 second')`

### 2. Connection Pool Exhaustion

**Real incident (LinkedIn):** Slow stored procedure held connections until pool
exhausted, causing 4-hour outage.

**Job queue specific pattern:**

```
┌─────────────────────────────────────────────────────────────────┐
│              Connection Pool Exhaustion Cascade                  │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Celery workers: 4 workers × 8 processes = 32 connections        │
│  Each process holds connection during:                           │
│    - Job claim (fast)                                            │
│    - Job processing (slow — external API call)   ◄── DANGER      │
│    - Job completion (fast)                                       │
│                                                                  │
│  If external API slows to 30s response:                          │
│    32 connections × 30s = all connections blocked                │
│    New job claims fail → queue backs up                          │
│    Health checks fail → cascading restarts                       │
└─────────────────────────────────────────────────────────────────┘
```

**Mitigations:**

1. **Release connection before external calls:**
   ```go
   row := claimJob(ctx)       // holds connection briefly
   conn.Release()             // release before slow work
   result := callExternalAPI() // no connection held
   conn = pool.Acquire(ctx)   // re-acquire for completion
   completeJob(ctx, row.ID, result)
   ```

2. **Pool sizing rule:** `pool_size = worker_concurrency / 2`, `max_overflow = pool_size / 4`

3. **Connection timeout:** Set `statement_timeout` to prevent runaway queries

### 3. Stalled Job False Positives

**BullMQ pattern:** Job marked stalled when lock renewal missed, but job still
running.

**Causes:**

1. **Event loop blocked** — CPU-intensive sync code prevents lock renewal
2. **GC pause** — long garbage collection freezes all goroutines
3. **Clock skew** — container clock drifts from database clock
4. **Network partition** — worker isolated but still processing

**Detection vs. reality:**

```
┌─────────────────────────────────────────────────────────────────┐
│                  Stalled Job False Positive                      │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Worker A                          Database/Reaper               │
│     │                                    │                       │
│  T+0s  Claim job, start processing       │                       │
│     │                                    │                       │
│  T+30s Event loop blocked (CPU work)     │                       │
│     │  ├── Heartbeat goroutine starved   │                       │
│     │  └── No heartbeat sent             │                       │
│     │                                    │                       │
│  T+60s Still blocked                   Reaper: "No heartbeat     │
│     │                                    for 30s, job stalled!"  │
│     │                                    │                       │
│  T+61s                                 Worker B claims job       │
│     │                                    │                       │
│  T+65s Worker A unblocks, continues      Worker B starts same job│
│     │  processing same job!              │                       │
│     │                                    │                       │
│  ⚠️  DUPLICATE PROCESSING                                         │
└─────────────────────────────────────────────────────────────────┘
```

**Mitigations:**

1. **Fencing tokens:** Include `claimed_at` timestamp in completion — reject if
   stale
   ```sql
   UPDATE jobs SET status = 'completed'
   WHERE id = $1 AND claimed_at = $2  -- fence: reject if re-claimed
   ```

2. **maxStalledCount:** Allow N stalls before permanent failure (BullMQ default: 1)

3. **Extend lock for long jobs:**
   ```go
   if estimatedDuration > lockDuration/2 {
       extendLock(ctx, job.ID, estimatedDuration*2)
   }
   ```

### 4. Idempotency Key Collisions

**Real incident:** Mobile SDK reused session IDs after app restart, causing
silent data overwrites.

**Weak idempotency keys:**

| Key Pattern | Failure Mode |
|-------------|--------------|
| `user_id + timestamp` (second precision) | Two events same second collide |
| `session_id` | Reused across app restarts |
| `hash(payload)` | Legitimate retries rejected |
| Sequential counter | Reset on restart = collisions |

**Strong idempotency key:** UUID v4 or `{entity_type}:{entity_id}:{operation}:{timestamp_ms}`

### 5. Dead Letter Queue Accumulation

**Silent failure pattern:** DLQ grows unmonitored until storage exhausted or
data too old to recover.

**Required monitoring:**

```yaml
# Prometheus alerting rules
groups:
  - name: dlq_alerts
    rules:
      - alert: DLQBacklogHigh
        expr: dlq_message_count > 100
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "DLQ has {{ $value }} messages waiting"
      
      - alert: DLQOldestMessageStale
        expr: (time() - dlq_oldest_message_timestamp) > 3600
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "DLQ has messages older than 1 hour"
```

---

## Deep Dive: Observability

### Queue Health Metrics (Prometheus)

| Metric | Type | Labels | Alert Threshold |
|--------|------|--------|-----------------|
| `attune_queue_depth` | Gauge | `queue`, `status` | depth > 1000 for 5m |
| `attune_queue_oldest_age_seconds` | Gauge | `queue` | age > 300 (5 min) |
| `attune_worker_claimed_total` | Counter | `worker` | rate < expected |
| `attune_worker_completed_total` | Counter | `worker`, `status` | error_rate > 5% |
| `attune_worker_processing_seconds` | Histogram | `worker` | p99 > SLO |
| `attune_worker_heartbeat_lag_seconds` | Gauge | `worker` | lag > heartbeat_interval |
| `attune_worker_drain_duration_seconds` | Histogram | `worker`, `status` | timeout rate > 1% |
| `attune_stale_claims_recovered_total` | Counter | `queue` | any > 0 after boot |

### SLO-Based Alerting (Google SRE Pattern)

**Multi-window, multi-burn-rate alerting:**

```yaml
# Fast burn: 14.4× error budget consumption (2% in 1 hour)
- alert: WorkerErrorBudgetFastBurn
  expr: |
    (
      job:worker_errors:ratio_rate1h > (14.4 * 0.001)
      and
      job:worker_errors:ratio_rate5m > (14.4 * 0.001)
    )
  labels:
    severity: critical

# Slow burn: 3× error budget consumption (10% in 3 days)  
- alert: WorkerErrorBudgetSlowBurn
  expr: |
    (
      job:worker_errors:ratio_rate3d > (3 * 0.001)
      and
      job:worker_errors:ratio_rate6h > (3 * 0.001)
    )
  labels:
    severity: warning
```

### Console Health API Response (Extended)

```json
{
  "workers": [
    {
      "type": "outbox",
      "instances": [
        {
          "id": "outbox-abc123",
          "hostname": "attune-7f8d9c-xj2k9",
          "last_heartbeat": "2026-06-25T10:00:00Z",
          "in_flight": 3,
          "claimed_total": 1542,
          "completed_total": 1539,
          "failed_total": 3,
          "started_at": "2026-06-25T08:00:00Z"
        }
      ],
      "queue": {
        "pending": 42,
        "processing": 5,
        "dead": 2,
        "stale_claims": 0,
        "oldest_pending_at": "2026-06-25T09:55:00Z",
        "oldest_pending_age_seconds": 300,
        "throughput_per_minute": 120
      }
    }
  ],
  "alerts": [
    {
      "worker": "outbox",
      "type": "backlog_stale",
      "severity": "warning",
      "message": "42 items pending > 5 min",
      "since": "2026-06-25T09:55:00Z"
    }
  ],
  "cluster": {
    "total_instances": 3,
    "healthy_instances": 3,
    "last_stale_recovery_at": "2026-06-25T08:00:00Z",
    "stale_recovered_count": 2
  }
}
```

---

## Deep Dive: Multi-Tenant Fairness

### Noisy Neighbor Problem

**Scenario:** Tenant A floods queue with 10,000 jobs; Tenant B's 10 jobs starve.

```
┌─────────────────────────────────────────────────────────────────┐
│                    Noisy Neighbor Starvation                     │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Queue (FIFO):                                                   │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │ A A A A A A A A A A A A A A B A A A A A B A A A A A A A ... │ │
│  └─────────────────────────────────────────────────────────────┘ │
│                                                                  │
│  Worker processing rate: 100/min                                 │
│  Tenant A jobs: 10,000                                           │
│  Tenant B jobs: 10                                               │
│                                                                  │
│  Time for Tenant B's first job: ~50 minutes (5000 A jobs ahead)  │
│                                                                  │
│  SLO violation for Tenant B despite low volume                   │
└─────────────────────────────────────────────────────────────────┘
```

### Solutions (Amazon SQS Fair Queues Pattern)

1. **Virtual queues per tenant:**
   ```sql
   -- Claim prioritizes tenants with fewer in-flight jobs
   WITH tenant_load AS (
       SELECT tenant_id, COUNT(*) as in_flight
       FROM jobs WHERE status = 'processing'
       GROUP BY tenant_id
   ),
   fair_claim AS (
       SELECT j.id
       FROM jobs j
       LEFT JOIN tenant_load t ON j.tenant_id = t.tenant_id
       WHERE j.status = 'pending'
       ORDER BY COALESCE(t.in_flight, 0), j.created_at
       LIMIT 10
       FOR UPDATE SKIP LOCKED
   )
   UPDATE jobs SET status = 'processing' ...
   ```

2. **Per-tenant concurrency limits:**
   ```go
   // Limit concurrent jobs per tenant
   if w.tenantInFlight[job.TenantID] >= w.maxPerTenant {
       return ErrTenantConcurrencyLimit
   }
   ```

3. **Weighted fair queueing:**
   - Premium tenants get 3× weight
   - Free tier tenants get 1× weight
   - Round-robin across weight classes

---

## Goals

1. **No double-processing**: two replicas running the same worker type never
   process the same queue item concurrently
2. **Automatic stale recovery**: crashed replicas' in-flight work recovers
   without manual DB intervention
3. **Observable shutdown**: drain status reported via logs and metrics; no
   silent abandonment of in-flight work
4. **Console visibility**: queue ownership, backlog depth, and stuck-queue
   alerts surfaced in the Console health API
5. **Integration test**: two-process test proves no duplicate processing

## Non-Goals

- Leader election (single-active-replica) — workers should all be active,
  coordinating via DB-level claim semantics
- Distributed locks external to PostgreSQL — we stay within pg advisory locks
  and `FOR UPDATE SKIP LOCKED`
- Worker auto-scaling — out of scope for v1.0

---

## Proposal

### 1. Standardize Worker Lease Contract

Introduce a shared lease protocol that all queue-draining workers implement:

```
┌─────────────────────────────────────────────────────────────┐
│                     Worker Lease Contract                    │
├─────────────────────────────────────────────────────────────┤
│ 1. IDENTITY: worker_id = "{worker_type}-{uuid}"             │
│ 2. CLAIM:    SET claimed_at=NOW(), claimed_by=worker_id     │
│              WHERE claimed_at IS NULL                        │
│                 OR claimed_at < NOW() - lease_window         │
│ 3. HEARTBEAT: UPDATE claimed_at=NOW() WHERE claimed_by=me   │
│ 4. RELEASE:  SET claimed_at=NULL, claimed_by=NULL           │
│ 5. STALE:    on boot, reset rows where claimed_at expired   │
└─────────────────────────────────────────────────────────────┘
```

**Lease windows** (per-worker, based on expected task duration):

| Worker | Lease Window | Heartbeat Interval | Rationale |
|--------|--------------|-------------------|-----------|
| OutboxWorker | 10 min | 3 min | Network delivery can be slow |
| DraftWorker | 5 min | 90 sec | LLM calls ~30-60s |
| DigestWorker | 5 min | 90 sec | Aggregation + delivery |
| EmbeddingWorker | 5 min | 90 sec | Embedding API calls |
| BatchJobWorker | 30 min | 5 min | Large batch exports |

**Changes required:**

- DraftWorker, DigestWorker, EmbeddingWorker: add heartbeat goroutine
  (following OutboxWorker's `heartbeatClaims` pattern)
- Add `claimed_by` column to `reply_draft_task`, `digest_run`, `embedding_task`
  if not present (migration)
- Pruners: wrap in pg advisory lock to prevent concurrent execution

### 2. Graceful Shutdown Drain

Each worker's `Run()` method will:

1. On `ctx.Done()`, stop accepting new claims
2. Wait for in-flight work to complete (with timeout)
3. Log drain status and emit metric
4. Release any held claims explicitly

```go
func (w *Worker) Run(ctx context.Context) {
    // ... existing poll loop ...
    
    select {
    case <-ctx.Done():
        w.drain(ctx)
        return
    }
}

func (w *Worker) drain(ctx context.Context) {
    const drainTimeout = 30 * time.Second
    deadline := time.Now().Add(drainTimeout)
    
    // Wait for in-flight work
    w.wg.Wait() // or similar coordination
    
    drained := w.inFlightCount.Load() == 0
    metrics.WorkerDrainStatus.WithLabelValues(w.name, strconv.FormatBool(drained)).Inc()
    
    if !drained {
        logext.Warnf(ctx, "[%s] shutdown with %d in-flight items", w.name, w.inFlightCount.Load())
    } else {
        logext.Infof(ctx, "[%s] drained cleanly", w.name)
    }
}
```

**New metrics:**

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `attune_worker_drain_total` | Counter | `worker`, `status` | Drain events (status: clean/timeout/abandoned) |
| `attune_worker_in_flight` | Gauge | `worker` | Current in-flight item count |
| `attune_worker_claim_stale_total` | Counter | `worker` | Stale claims recovered on boot |

### 3. Pruner Coordination

Pruners are idempotent but wasteful when run concurrently. Use pg advisory
locks:

```go
func runPrunerWithLock(ctx context.Context, pool *pgxpool.Pool, name string, fn func(context.Context) error) {
    lockID := hashToInt64(name) // stable per-pruner
    
    conn, _ := pool.Acquire(ctx)
    defer conn.Release()
    
    // Try to acquire advisory lock (non-blocking)
    var acquired bool
    conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", lockID).Scan(&acquired)
    if !acquired {
        logext.Infof(ctx, "[%s] skipped, another replica holds lock", name)
        return
    }
    defer conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", lockID)
    
    fn(ctx)
}
```

### 4. Console Health API

Add endpoint `GET /api/v1/admin/workers` returning:

```json
{
  "workers": [
    {
      "type": "outbox",
      "instances": [
        {"id": "outbox-abc123", "last_heartbeat": "2026-06-25T10:00:00Z", "in_flight": 3}
      ],
      "queue": {
        "pending": 42,
        "stale_claims": 0,
        "oldest_pending": "2026-06-25T09:55:00Z"
      }
    }
  ],
  "alerts": [
    {"worker": "outbox", "type": "backlog_high", "message": "42 items pending > 5 min"}
  ]
}
```

**Alert conditions:**

| Condition | Threshold | Alert Type |
|-----------|-----------|------------|
| Pending items age | > 5 min oldest | `backlog_stale` |
| Stale claims | > 0 after boot window | `stale_claims` |
| No heartbeat | > 2× heartbeat interval | `worker_unresponsive` |

### 5. Integration Test: Two-Process Safety

Add `test/integration/postgres/worker/duplicate_safety_test.go`:

```go
func TestOutboxWorker_NoDuplicateProcessing(t *testing.T) {
    // 1. Insert N outbox rows
    // 2. Start two OutboxWorker instances (same DB, different worker_id)
    // 3. Let both run until queue empty
    // 4. Assert: each row processed exactly once (check delivered_at, claimed_by history)
    // 5. Assert: no row has two different claimed_by values in audit trail
}
```

Test approach: add `processing_history` JSONB column or audit table to track
which worker_id processed each row; assert no overlaps.

---

## Alternatives Considered

### A. External Distributed Lock (Redis/etcd)

**Rejected.** Adds operational complexity and a new failure domain. PostgreSQL's
`FOR UPDATE SKIP LOCKED` and advisory locks are sufficient and already in use.

### B. Single-Active Worker (Leader Election)

**Rejected.** Wastes replica capacity. Claim-per-item semantics allow all
replicas to contribute while preventing overlap.

### C. Kafka/NATS for Queue Semantics

**Rejected.** Over-engineering for current scale. The existing outbox pattern
works; we're standardizing it, not replacing it.

---

## Risks / Tradeoffs

| Risk | Mitigation |
|------|------------|
| Heartbeat overhead | 90s–5min intervals are cheap (~1 UPDATE per interval per claimed batch) |
| Drain timeout extends shutdown | 30s cap; log warning if items abandoned |
| Advisory lock table bloat | Pruners use stable lock IDs; no growth |
| Migration adds columns | Additive; no data migration needed |

---

## Implementation Plan

### Phase 1: Heartbeat Standardization (Day 1)

- [ ] Add `claimed_by` column to `reply_draft_task`, `digest_run`, `embedding_task`
- [ ] Add heartbeat goroutine to DraftWorker, DigestWorker, EmbeddingWorker
- [ ] Update `TryClaim` queries to include `claimed_by` filter on refresh

### Phase 2: Shutdown Drain (Day 1-2)

- [ ] Add `sync.WaitGroup` or in-flight counter to each worker
- [ ] Implement drain logic in `Run()` on context cancellation
- [ ] Add `attune_worker_drain_total`, `attune_worker_in_flight` metrics
- [ ] Add drain status logging

### Phase 3: Pruner Coordination (Day 2)

- [ ] Implement `runPrunerWithLock` helper
- [ ] Wrap audit_pruner, idempotency_key_pruner, mcp_pruner

### Phase 4: Console Health API (Day 2-3)

- [ ] Add `GET /api/v1/admin/workers` endpoint
- [ ] Query each queue's pending/stale counts
- [ ] Implement alert condition evaluation
- [ ] Add proto definitions and TypeScript types

### Phase 5: Integration Test (Day 3)

- [ ] Add `test/integration/postgres/worker/` package
- [ ] Implement two-process OutboxWorker test
- [ ] Add CI job for worker integration tests

---

## Verification

1. **Unit tests**: each worker's heartbeat and drain logic
2. **Integration test**: two-worker no-duplicate-processing assertion
3. **Manual test**: `docker compose up --scale attune=2`, inject queue items,
   verify no duplicates in logs
4. **Metrics verification**: Grafana dashboard shows `attune_worker_in_flight`,
   drain events on rolling restart

---

## References

### PostgreSQL Queue Patterns
- [River: Fast, Robust Job Queue for Go + Postgres](https://brandur.org/river)
- [River GitHub Repository](https://github.com/riverqueue/river)
- [pg-boss: Queueing jobs in Postgres from Node.js](https://github.com/timgit/pg-boss)
- [Graphile Worker](https://worker.graphile.org/)
- [Postgres is the only Queue you need (until 50k jobs/sec)](https://medium.com/@harsh.vaghela.work/postgres-is-the-only-queue-you-need-until-50k-jobs-sec-5931611b551c)
- [Using FOR UPDATE SKIP LOCKED For Queue Workflows](https://www.netdata.cloud/academy/update-skip-locked/)
- [Neon: Queue System using SKIP LOCKED](https://neon.com/guides/queue-system)
- [PostgreSQL FOR UPDATE SKIP LOCKED: The One-Liner Job Queue](https://www.dbpro.app/blog/postgresql-skip-locked)
- [Solid Queue: Understanding UPDATE SKIP LOCKED](https://www.bigbinary.com/blog/solid-queue)

### PostgreSQL Performance and Contention
- [AWS: Diagnose and Mitigate Lock Manager Contention](https://aws.amazon.com/blogs/database/improve-postgresql-performance-diagnose-and-mitigate-lock-manager-contention/)
- [Postgres Indexes, Partitioning and LWLock:LockManager Scalability](https://ardentperf.com/2024/03/03/postgres-indexes-partitioning-and-lwlocklockmanager-scalability/)
- [DBOS: Benchmarking Workflow Execution Scalability on Postgres](https://dbos.dev/blog/benchmarking-workflow-execution-scalability-on-postgres)
- [Optimizing PostgreSQL with Composite and Partial Indexes](https://stormatics.tech/blogs/optimizing-postgresql-with-composite-and-partial-indexes)
- [Speeding Up PostgreSQL Queries with Partial Indexes (Heap)](https://www.heap.io/blog/speeding-up-postgresql-queries-with-partial-indexes)

### Distributed Workflow Engines
- [Temporal: Durable Execution Guide](https://www.kunalganglani.com/blog/temporal-workflow-engine-guide)
- [Temporal Platform Documentation](https://docs.temporal.io/evaluate/understanding-temporal)
- [Temporal: Detecting Activity Failures](https://docs.temporal.io/encyclopedia/detecting-activity-failures)
- [Temporal: The Four Types of Activity Timeouts](https://temporal.io/blog/activity-timeouts)
- [Temporal Graceful Worker Shutdown](https://keithtenzer.com/temporal/Temporal-Graceful-Worker-Shutdown/)
- [Temporal: Task Queue Priority and Fairness](https://temporal.io/blog/task-queue-priority-and-fairness-your-task-queue-your-way)
- [Cadence Activities](https://cadenceworkflow.io/docs/concepts/activities)
- [Cadence Timeouts](https://cadenceworkflow.io/docs/workflow-troubleshooting/timeouts)

### Redis Queue Systems
- [Sidekiq Best Practices](https://github.com/sidekiq/sidekiq/wiki/Best-Practices)
- [Sidekiq Reliability](https://sidekiq.org/wiki/Reliability)
- [Sidekiq Pro Super Fetch](https://thoughtbot.com/blog/enhancing-job-reliability-with-sidekiq-pro-s-super-fetch-strategy)
- [BullMQ Stalled Jobs](https://docs.bullmq.io/guide/jobs/stalled)
- [BullMQ Graceful Shutdown](https://docs.bullmq.io/guide/workers/graceful-shutdown)
- [BullMQ Going to Production](https://docs.bullmq.io/guide/going-to-production)
- [How to Handle Stalled Jobs in BullMQ](https://oneuptime.com/blog/post/2026-01-21-bullmq-stalled-jobs/view)
- [How to Handle Worker Crashes in BullMQ](https://oneuptime.com/blog/post/2026-01-21-bullmq-worker-crashes-recovery/view)

### Stripe Idempotency and Recovery
- [Implementing Stripe-like Idempotency Keys in Postgres](https://brandur.org/idempotency-keys)
- [Stripe: Designing robust APIs with idempotency](https://stripe.com/blog/idempotency)
- [How Stripe Prevents Double Payment Using Idempotent API](https://newsletter.systemdesign.one/p/idempotent-api)

### Kubernetes Patterns
- [Kubernetes: Terminating with Grace (Google Cloud)](https://cloud.google.com/blog/products/containers-kubernetes/kubernetes-best-practices-terminating-with-grace)
- [Kubernetes Pod Graceful Shutdown with SIGTERM & preStop Hooks](https://devopscube.com/kubernetes-pod-graceful-shutdown/)
- [Decoding the Pod Termination Lifecycle in Kubernetes (CNCF)](https://www.cncf.io/blog/2024/12/19/decoding-the-pod-termination-lifecycle-in-kubernetes-a-comprehensive-guide/)
- [Kubernetes Coordinated Leader Election](https://kubernetes.io/docs/concepts/cluster-administration/coordinated-leader-election/)
- [client-go Work Queues for Rate-Limited Event Processing](https://oneuptime.com/blog/post/2026-02-09-client-go-work-queues-rate-limited/view)
- [Kubernetes Leases for Leader Election](https://medium.com/@sehgal.mohit06/kubernetes-leases-solution-to-leader-election-optimistic-locking-ratelimiting-concurrencycontrol-bb07f53c4462)

### Celery
- [Understanding Celery Workers: Concurrency, Prefetching, and Heartbeats](https://ankurdhuriya.medium.com/understanding-celery-workers-concurrency-prefetching-and-heartbeats-85707f28c506)
- [Celery Configuration and Defaults](https://docs.celeryq.dev/en/stable/userguide/configuration.html)
- [Advanced Celery for Django: Fixing Unreliable Background Tasks](https://www.vintasoftware.com/blog/guide-django-celery-tasks)

### Delivery Semantics
- [At most once, at least once, exactly once (ByteByteGo)](https://blog.bytebytego.com/p/at-most-once-at-least-once-exactly)
- [You Cannot Have Exactly-Once Delivery](https://bravenewgeek.com/you-cannot-have-exactly-once-delivery/)
- [Exactly-Once Message Delivery](https://exactly-once.github.io/posts/exactly-once-delivery/)

### Retry, Backoff, and Dead Letter Queues
- [Retry Patterns That Work: Exponential Backoff, Jitter, and DLQs](https://dev.to/young_gao/retry-patterns-that-actually-work-exponential-backoff-jitter-and-dead-letter-queues-75)
- [Dead Letter Queues Explained: Handling Failed Messages](https://web-alert.io/blog/dead-letter-queues-explained-handling-failed-messages)
- [The Dead Letter Queue Problem: Why Your Async Systems Silently Lose Data](https://www.javacodegeeks.com/2026/05/the-dead-letter-queue-problem-why-your-async-systems-silently-lose-data.html)
- [Message Reprocessing: How to Implement the Dead Letter Queue](https://www.redpanda.com/blog/reliable-message-processing-with-dead-letter-queue)

### Multi-Tenant Fairness
- [Amazon SQS Fair Queues for Fairness in Multi-Tenant Environments](https://akkurtfurkan.medium.com/amazon-sqs-fair-queues-for-fairness-in-multi-tenant-environments-db70807a27be)
- [AWS: Building Resilient Multi-Tenant Systems with SQS Fair Queues](https://aws.amazon.com/blogs/compute/building-resilient-multi-tenant-systems-with-amazon-sqs-fair-queues/)
- [Fixing Noisy Neighbor Problems in Multi-Tenant Queueing Systems (Inngest)](https://www.inngest.com/blog/fixing-multi-tenant-queueing-concurrency-problems)

### Production Incidents and Failure Modes
- [DB Connection Pool Exhaustion: The Silent Killer](https://medium.com/@lahirukavikara/db-connection-pool-exhaustion-the-silent-killer-behind-application-slowdowns-dcaccbb8a633)
- [PostgreSQL Connection Pool Exhaustion: Lessons from a Production Outage](https://www.c-sharpcorner.com/article/postgresql-connection-pool-exhaustion-lessons-from-a-production-outage/)
- [How a Simple Race Condition Can Take Down Even the Biggest Systems](https://dev.to/georgekobaidze/how-a-simple-race-condition-can-take-down-even-the-biggest-systems-16l0)
- [Failure Modes and Edge Cases in Idempotent Pipelines](https://www.systemoverflow.com/learn/data-pipelines-orchestration/pipeline-idempotency/failure-modes-and-edge-cases-in-idempotent-pipelines)
- [Error Handling in Distributed Systems (Temporal)](https://temporal.io/blog/error-handling-in-distributed-systems)

### Observability and Monitoring
- [Google SRE: Alerting on SLOs](https://sre.google/workbook/alerting-on-slos/)
- [Monitoring with Prometheus and Grafana (RabbitMQ)](https://www.rabbitmq.com/docs/prometheus)
- [Monitor Temporal Platform Metrics](https://docs.temporal.io/self-hosted-guide/monitoring)
- [How to Configure Message Queue Monitoring with Prometheus Exporters](https://oneuptime.com/blog/post/2026-02-09-message-queue-prometheus-monitoring/view)

### PostgreSQL Fundamentals
- [PostgreSQL Advisory Locks](https://www.postgresql.org/docs/current/explicit-locking.html#ADVISORY-LOCKS)
- [FOR UPDATE SKIP LOCKED](https://www.postgresql.org/docs/current/sql-select.html#SQL-FOR-UPDATE-SHARE)
- [Transactional Outbox Pattern](https://microservices.io/patterns/data/transactional-outbox.html)
