# Bounded enrichment queue with concurrency and LLM rate limiting

| Field | Value |
| --- | --- |
| **Issue** | [#80](https://github.com/Phixsura/attune/issues/80) |
| **Status** | Implemented |
| **Started** | 2026-06-17 22:50 CST |
| **Related** | [#10](https://github.com/Phixsura/attune/issues/10) (enrichment architecture audit), [#25](https://github.com/Phixsura/attune/issues/25) (embedding task outbox), [#26](https://github.com/Phixsura/attune/issues/26) (reply-draft task outbox), [#109](https://github.com/Phixsura/attune/issues/109) (managed LLM routing) |

## Problem

Issue #80 identifies two high-severity problems in the enrichment pipeline, and
the current code confirms both:

1. `service/ingest` starts inline async enrichment with an unbounded
   `go i.fireEnrich(...)` per accepted ingest row.
2. `service/enrich` background recovery is a serial poller:
   `ListPending -> for ids -> EnrichOne`.

That leaves Attune exposed to two kinds of operational failure:

- **Burst amplification.** A burst of inbound feedback can fan out into a burst
  of concurrent LLM calls with no process-local bound and no traffic shaping.
- **Sweep stall.** One slow provider call blocks the current sweep lane, so a
  batch of slow rows stretches one poll cycle into tens of seconds or minutes.

The architecture mismatch is now more visible because other async AI-adjacent
workflows in the repository already use stronger patterns:

- embedding uses a dedicated task table plus claim/retry/recovery semantics
- reply drafts use the same generic task outbox abstraction
- digest and outbox each have explicit worker loops with their own queue health
  signals

Enrichment is still split across two paths:

- immediate enqueue-by-goroutine from ingest
- periodic DB recovery from `RunBackground`

They both rely on `user_feedback` as the source of truth, but they do not share
one bounded execution model.

## Goals / Non-goals

### Goals

- Bound concurrent enrichment work inside one process.
- Bound provider call rate for enrichment-related LLM traffic.
- Keep `user_feedback` as the durable source of truth for enrichment state.
- Preserve fast ingest acknowledgement; the HTTP path must not wait on LLM work.
- Reuse existing repository patterns where they fit, especially claim/retry and
  queue-depth observability.
- Make restart and shutdown behavior explicit and testable.
- Add the configuration and metrics needed to operate the new behavior safely.

### Non-goals

- Do not introduce distributed work coordination in this first step.
- Do not replace `user_feedback` with a new durable enrichment job table yet.
- Do not redesign LLM routing, guard policy evaluation, or audit logging.
- Do not change the semantic behavior of enrichment output, only its execution
  model.
- Do not solve multi-instance SaaS queueing in the same change; reserve a clean
  seam for that later.

## Current-state reconciliation

The issue direction is correct, but the proposal must fit the current
repository, not an abstract greenfield design.

| Area | Verified current state | Proposal decision |
| --- | --- | --- |
| Ingest trigger | `IngestRow` starts `go fireEnrich(...)` after insert | Replace direct enrich execution with queue submission |
| Durable state | `user_feedback` already owns claim, retry, stale-claim, and status fields | Keep DB row state as the only durable truth |
| Recovery path | `EnrichPending` polls `ListPending` serially | Keep DB sweep as recovery, but feed a bounded processor |
| Async precedents | embedding and reply-draft already use claim/retry queue patterns | Borrow their worker semantics and queue-depth observability shape |
| LLM abstraction | `llmclient.LLMClient` is the common call surface | Apply rate limiting as an `LLMClient` wrapper |
| Config shape | `config.Enricher` currently has only `interval` and `batch` | Extend nested `enricher` config instead of inventing a sibling top-level block |

## Industry benchmarking

Reviewed 10 mature queueing and API-throttling systems with official
documentation, focusing on the exact concerns this issue raises: bounded
consumer concurrency, rate limiting, durable recovery, and backlog handling.

| System | Verified pattern | Takeaway for Attune |
| --- | --- | --- |
| Temporal | Durable task queues, worker-side concurrency control, queue-level throttling, and fairness primitives | Best long-term model for durable queue + bounded workers, but heavier than Attune needs for the first fix |
| BullMQ | Worker `concurrency` and queue `limiter` are independent controls; rate-limited jobs stay queued | Strong precedent for separating concurrency from QPS and keeping throttled work pending |
| Celery | Worker concurrency and task rate limits exist, but rate limits are effectively per worker instance unless externally coordinated | Important caution: a process-local limiter is valuable, but not a global quota system |
| Sidekiq Enterprise | Shared cross-process limiters for concurrent and rate-limited work | Good future direction if Attune later needs distributed fairness or shared provider quotas |
| AWS Lambda + SQS | Durable queue plus configurable maximum consumer concurrency; FIFO groups provide ordered partitioned concurrency | Strong precedent for queue-backed work with explicit consumer caps |
| Google Cloud Tasks | Queue-level `max dispatches/sec` and `max concurrent dispatches` are both first-class settings | Excellent confirmation that both throughput and concurrency should be tunable separately |
| RabbitMQ | Consumer prefetch provides pull-side backpressure by limiting unacked deliveries | Reminder that bounding intake into workers is as important as bounding execution |
| Kafka / Confluent quotas | Shared broker-side quotas throttle clients instead of letting them overrun the system | Good model for the eventual distributed/shared-limiter phase |
| OpenAI Cookbook | Official guidance combines bounded parallelism with RPM/TPM-aware throttling and retry/backoff | Directly relevant to Attune's LLM worker behavior under burst load |
| Anthropic API docs | Official rate-limit and acceleration-limit guidance emphasizes gradual ramp-up and explicit 429 handling | Confirms that provider protection needs both steady-state limits and burst control |

### Patterns that repeat across the benchmark

1. **Concurrency and rate are separate control planes.**
   Google Cloud Tasks and BullMQ make this explicit. A queue can dispatch only a
   few jobs at once even if its allowed QPS is high, and it can also permit many
   workers but still shape call rate. Attune should expose both knobs.

2. **Durable state and execution capacity are usually decoupled.**
   Temporal, SQS, and Cloud Tasks all keep durable task state separate from the
   number of workers currently allowed to process tasks. Attune already has the
   durable part in `user_feedback`; the missing part is the bounded executor.

3. **Throttled work is normally deferred, not failed.**
   BullMQ, Cloud Tasks, and Temporal all treat capacity pressure as a reason to
   delay or requeue work, not as an application failure. That strongly supports
   keeping pre-execution cancellation and queue pressure out of ordinary
   provider-failure semantics.

4. **Process-local controls are a valid first step, but not the final global
   story.**
   Celery's documentation is the clearest warning here: per-worker rate limits do
   not magically become cluster-wide quotas. Attune can still take the
   process-local step now, as long as the proposal states that limit clearly.

5. **Backpressure is most robust when it happens before unbounded work starts.**
   RabbitMQ prefetch and SQS max concurrency both limit work before it expands
   into too many in-flight executions. That is a direct argument against keeping
   one goroutine per ingest request.

### What Attune should borrow directly

- **Separate knobs for queue depth, worker concurrency, and provider rate.**
  Cloud Tasks and BullMQ make this the default operating model.
- **One bounded execution path shared by push and recovery sources.**
  Durable queue systems generally avoid multiple independent execution paths for
  the same unit of work.
- **Deferred retry semantics for capacity pressure and shutdown.**
  Capacity limits should leave work pending/recoverable, not pretend the
  provider itself failed.
- **A clean seam for later distributed coordination.**
  Sidekiq Enterprise, Kafka quotas, and Temporal fairness all point in the same
  direction for a future multi-instance Attune.

### What Attune should avoid

- Treating a local QPS limiter as if it solved cluster-wide quota control.
- Mixing durable task truth and process-local notification acceleration into one
  abstraction.
- Expanding execution with one goroutine per accepted request.
- Recording rate-limit wait cancellation as if it were a real provider error.

## Decision summary

Based on the current codebase and the 10-system benchmark above, this proposal
locks in the following decisions for Attune.

### Adopt now

| Decision | Why |
| --- | --- |
| Add a process-local bounded submission queue | Lowest-cost way to stop unbounded ingest fan-out while preserving DB-backed recovery |
| Add an explicit bounded processor worker | Matches the prevailing queue-worker shape used by mature systems and by Attune's own newer workers |
| Keep `user_feedback` as the durable truth | Already contains the ownership, retry, and recovery fields we need |
| Keep DB sweep as recovery/backlog refill | Preserves restart safety without introducing a new durable queue immediately |
| Add separate config for worker concurrency and LLM rate | Matches BullMQ / Cloud Tasks style operating controls |
| Treat pre-execution cancellation differently from provider failure | Matches how mature queue systems defer throttled work instead of misclassifying it as an application error |

### Defer

| Decision | Why deferred |
| --- | --- |
| Dedicated durable `enrichment_task` table | Cleaner long-term shape, but more schema and migration cost than the first P0 fix requires |
| Redis or other distributed queue backend | Not required for today's single-process runtime and adds operational weight |
| Cluster-wide shared limiter | Valuable later, but process-local protection addresses today's immediate risk first |
| Per-tenant fairness / partitioned queues | A strong future direction if tenant bursts begin starving each other, but not necessary to solve the current incident class |

### Reject for this issue

| Decision | Why rejected |
| --- | --- |
| Keep direct `go fireEnrich(...)` and only add QPS limiting | Still leaves unbounded goroutine growth and opaque shutdown behavior |
| Fail ingest when the in-memory queue is full | Breaks the durable-ack contract and turns transient executor pressure into customer-facing ingest failure |
| Treat every limiter/cancellation error as `MarkFailed` | Confuses capacity/shutdown pressure with genuine provider execution failure |

## Proposal

### 1. Introduce an in-process enrichment submission queue

Add a lightweight in-memory queue in `internal/service/enrich` that acts as a
notification accelerator, not a durable job store.

Proposed shape:

```go
type Job struct {
	ID      int64
	TraceID string
}

type Submitter interface {
	Submit(ctx context.Context, job Job) error
}
```

The default implementation is a buffered channel with non-blocking `Submit`.
When the queue is full, submit returns a bounded sentinel error and the caller
logs/records the drop. The job is not lost durably because the corresponding
`user_feedback` row remains `pending` and will be found by the DB sweeper.

This queue is intentionally process-local:

- low implementation cost
- no new dependency
- fits today's single-process deployment model
- restart recovery is already available via `ListPending`

### 2. Replace direct ingest goroutines with queue submission

`service/ingest` should stop calling `EnrichOne` from an unbounded goroutine.
After insert, it should submit `{id, traceID}` to the enrichment queue and
return immediately.

If submission fails because the queue is full or shutting down:

- do not fail the ingest request
- do not call the LLM inline as a fallback
- leave the row `pending`
- rely on the sweep path to recover it

This preserves the core product contract that ingest acknowledgement is tied to
database durability, not LLM completion.

### 3. Add a bounded enrichment processor

Add one explicit enrichment processor worker that consumes queue jobs, batches
them within a short collection window, and executes `EnrichOne` with bounded
concurrency.

Recommended runtime knobs:

- `queue_len`
- `workers`
- `batch_size`
- `batch_window`
- `sweep_interval`

Execution model:

1. collect up to `batch_size` jobs or wait up to `batch_window`
2. fan them into a worker pool capped at `workers`
3. each worker calls `EnrichOne`
4. `TryClaim` on `user_feedback` remains the deduplication gate, so duplicate
   submissions are harmless

The processor should be a first-class worker with explicit lifecycle methods,
not a loose pile of goroutines. Shutdown should:

- stop accepting new jobs
- stop batch collection
- wait for in-flight workers to finish or for parent context cancellation

### 4. Keep the DB sweeper, but downgrade it to recovery and backlog refill

The periodic DB sweep still matters, but its role changes.

Instead of being the primary serial execution path, it becomes a recovery path
that scans `ListPending` and submits rows into the same bounded processor.

That gives Attune one execution model for both sources of work:

- push path: ingest submits immediately
- pull path: sweeper refills from durable DB state

This is the same broad architecture used elsewhere in the repo: durable table
plus worker claim/recovery semantics, with process-local execution limits.

### 5. Add LLM rate limiting as an `LLMClient` wrapper

Introduce a rate-limited client that wraps another `llmclient.LLMClient` and
calls `limiter.Wait(ctx)` before delegating `Complete(...)`.

This wrapper should be inserted close to the real provider-facing client so the
limit applies to actual outbound provider calls rather than to unrelated local
middleware bookkeeping.

Configuration:

- `enricher.llm_max_qps`
- `enricher.llm_burst`

Default should remain backward-compatible:

- `llm_max_qps = 0` means unlimited

### 6. Define cancellation semantics explicitly

This is the most important behavior to settle before implementation.

When the LLM call is never attempted because the rate limiter wait exits due to
context cancellation or process shutdown, the row should remain eligible for
retry rather than being marked as a provider failure.

Therefore the implementation should distinguish:

- **execution failure after attempting work**: existing failure path, may mark
  row failed with backoff
- **pre-execution cancellation while waiting for capacity/rate slot**: do not
  convert to terminal provider failure semantics

This requires a small adjustment around the current `classify()` error path so
shutdown-related cancellation is not treated the same way as a real provider
error.

### 7. Extend observability with queue and limiter signals

Add metrics for the new moving parts:

- `attune_enrich_queue_depth`
- `attune_enrich_queue_full_total`
- `attune_enrich_batch_size`
- `attune_enrich_sweep_submitted_total`
- `attune_llm_rate_limit_wait_seconds`

These should follow the existing observability style:

- stable names
- bounded labels
- queue depth refreshed deliberately rather than through ad hoc scrape-time DB
  calls

### 8. Configuration shape

Extend the existing nested `enricher` config block rather than introducing a new
parallel top-level namespace.

Proposed shape:

```yaml
enricher:
  interval: 30s
  batch: 10
  queue_len: 1000
  workers: 3
  batch_window: 5s
  llm_max_qps: 10
  llm_burst: 10
```

Where helpful, keep the existing legacy convenience fields in `config.Config`
while the bootstrap code is still transitioning.

## Alternatives considered

### A. Keep the current design and only add an LLM rate limiter

Rejected. It would reduce provider pressure, but it would still leave:

- one goroutine per ingest
- no bounded in-process backlog
- serial background recovery

The concurrency model would remain opaque and harder to reason about during
shutdown or bursts.

### B. Introduce a new durable `enrichment_task` table immediately

Deferred. This is architecturally clean, but heavier than needed for the first
fix because `user_feedback` already contains:

- status
- attempts
- stale-claim recovery
- pending listing

The current issue is about bounded execution and rate limiting, not yet about a
cross-process enrichment queue.

### C. Reuse `internal/repo/taskoutbox` directly for enrichment

Deferred, not rejected. The shared queue abstraction is a strong precedent, but
enrichment today is anchored directly on `user_feedback` rather than a sibling
task table. Porting it cleanly likely implies a later move to a dedicated
enrichment task table, which is more change than this issue needs.

### D. Add Redis now

Deferred. Redis may become the right answer for multi-instance SaaS deployment,
but it adds dependency, operational cost, and design surface before the current
single-process bottleneck is even resolved locally.

## Risks / tradeoffs

| Risk | Tradeoff / mitigation |
| --- | --- |
| In-memory queue drops on restart | Safe because DB rows remain `pending`; sweeper refills |
| Duplicate submissions from push + sweep | Safe because `TryClaim` remains the single ownership gate |
| Added complexity in bootstrap | Keep one explicit processor worker and one sweep loop instead of ad hoc goroutines |
| Misclassified shutdown cancellations | Mitigate with explicit tests around context cancellation before provider execution |
| New knobs may be mis-set | Provide conservative defaults and observable queue/limiter metrics |

## Implementation plan

1. Add proposal-approved config fields under `config.Enricher`.
2. Add an in-memory enrichment queue and bounded processor worker.
3. Change `service/ingest` to submit jobs instead of launching inline enrich
   goroutines.
4. Change the sweep loop to submit jobs into the same processor.
5. Add the `LLMClient` rate-limit wrapper and wire it in server bootstrap.
6. Adjust enrich cancellation handling so rate-limit/shutdown cancellation is
   not recorded as an ordinary provider failure.
7. Add metrics and dashboard/rule coverage as needed.
8. Add unit tests for queue, processor, limiter, cancellation behavior, and
   duplicate submission safety.
9. Update `CHANGELOG.md` under `[Unreleased]`.

## Verification

- Queue submit is non-blocking and rejects cleanly when full.
- Ingest returns success even when queue submission fails.
- Processor with `workers > 1` completes a synthetic batch materially faster
  than serial execution.
- Duplicate submissions for one feedback row result in exactly one successful
  claim.
- Sweeper can repopulate the queue from pending DB rows after restart.
- Rate limiter bounds outbound LLM call rate under burst load.
- Shutdown waits for in-flight work and does not mis-mark never-started work as
  a provider failure.
- Metrics are registered, bounded, and covered by tests where the repo expects
  telemetry drift guards.

## References

- [Issue #80](https://github.com/Phixsura/attune/issues/80)
- [Issue #10](https://github.com/Phixsura/attune/issues/10)
- [Temporal Task Queues](https://docs.temporal.io/task-queue)
- [Temporal Task Queue Priority and Fairness](https://docs.temporal.io/develop/task-queue-priority-fairness)
- [BullMQ Worker Concurrency](https://docs.bullmq.io/guide/workers/concurrency)
- [BullMQ Rate Limiting](https://docs.bullmq.io/guide/rate-limiting)
- [Celery Workers Guide](https://docs.celeryq.dev/en/stable/userguide/workers.html)
- [Celery Tasks Guide](https://docs.celeryq.dev/en/stable/userguide/tasks.html)
- [Sidekiq Scaling](https://github.com/sidekiq/sidekiq/wiki/Scaling-Sidekiq/00d09b1b8a8ae5b4c6c10ccde16bf0552bafa666)
- [Sidekiq Enterprise Rate Limiting](https://github.com/sidekiq/sidekiq/wiki/Ent-Rate-Limiting)
- [AWS Lambda with SQS scaling](https://docs.aws.amazon.com/lambda/latest/dg/services-sqs-scaling.html)
- [AWS SQS FIFO and Lambda concurrency behavior](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/fifo-queue-lambda-behavior.html)
- [Google Cloud Tasks queue configuration](https://cloud.google.com/tasks/docs/configuring-queues)
- [RabbitMQ Consumer Prefetch](https://www.rabbitmq.com/docs/consumer-prefetch)
- [Confluent Kafka Quotas](https://docs.confluent.io/kafka/design/quotas.html)
- [OpenAI Cookbook: How to handle rate limits](https://cookbook.openai.com/examples/how_to_handle_rate_limits)
- [OpenAI Cookbook: API request parallel processor](https://github.com/openai/openai-cookbook/blob/main/examples/api_request_parallel_processor.py)
- [Anthropic API Rate Limits](https://docs.anthropic.com/en/api/rate-limits)
- [Anthropic API Errors](https://docs.anthropic.com/en/api/errors)
- [README.md](/Users/phj/Develop/attune/README.md)
- [AGENTS.md](/Users/phj/Develop/attune/AGENTS.md)
