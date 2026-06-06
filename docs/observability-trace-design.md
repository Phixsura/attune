# Observability & trace design

> Canonical reference for attune's tracing / structured-logging design. Referenced
> from `cmd/attune/main.go`, `cmd/attune/server.go`, and
> `internal/infra/observability/otel.go`. If you change how `trace_id` / `span_id` reach
> the logs, update this file in the same change.

## Where it's wired

```
cmd/attune/main.go:50   slog.SetDefault(slog.New(observability.NewTraceIDHandler(inner)))
cmd/attune/server.go    observability.InitTracer(ctx, Options{…})  // OTLP exporter (or noop)
internal/infra/observability/
  otel.go     InitTracer — global TracerProvider + W3C propagator + OTLP/noop exporter
  idgen.go    ReadableIDGenerator — timestamp-prefixed, human-readable trace_id
  slog.go     TraceIDHandler — injects trace_id/span_id into every ctx-carrying log line
  attrs.go    reserved + business field-name constants (underscore form for SLS)
```

## Principles

1. **OTel is infrastructure, decoupled from business identity.** The OTel
   `Resource` carries only service-level constants (`service.name`,
   `service.version`, `deployment.environment`). Per-request attributes (tenant,
   user, …) are set on the active span dynamically — never baked into the
   `Resource`, never derived from login state. `InitTracer` runs once at boot.

2. **`trace_id` / `span_id` in logs are handler-injected — not OTel-log-bridged.**
   attune routes logs through the **standard-library** slog pipeline straight to
   stdout (`slog.New(TraceIDHandler(JSONHandler→os.Stdout))`). There is **no**
   `otelslog` bridge and **no** OTLP *log* exporter. The single source of
   `trace_id` / `span_id` on a log line is `TraceIDHandler`, which reads the
   active span out of the `ctx` at log time:

   ```go
   if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
       r.AddAttrs(slog.String("trace_id", …), slog.String("span_id", …))
   }
   ```

   Consequences:
   - A log call must carry `ctx` (`slog.InfoContext` / `logext.*f(ctx, …)`) for
     correlation — a non-context call silently loses it. (Enforced: lint-slog
     rule-1.)
   - These keys are a **reserved field-name contract** (`attrs.go`:
     `AttrTraceID` / `AttrSpanID`), shared across the BE / Gateway / attune fleet.
     Business code must **not** re-emit them by hand — that would duplicate the
     handler's field. (Enforced: lint-slog rule-2.)

3. **Trace IDs are always present, even without a collector.** `InitTracer` with
   an empty `Endpoint` still installs a real `TracerProvider` (AlwaysSample +
   `ReadableIDGenerator`); it just has no exporter, so spans are dropped after
   `End`. `SpanFromContext` is still valid, so logs still carry `trace_id` in
   local dev. Set `OTEL_EXPORTER_OTLP_ENDPOINT` to additionally export to a
   collector (SLS Trace, Jaeger, Tempo — any OTLP/HTTP sink).

## Readable trace IDs (`ReadableIDGenerator`)

W3C-compatible 32-hex-char trace IDs, timestamp-prefixed so an operator can read
the time straight off the ID:

```
2026 05 15 19 30 45  a1b2c3d40005f3e7a8
└── yyyyMMddHHmmss ─┘└── 72-bit random ─┘
   14 hex = 7 bytes      18 hex = 9 bytes
```

- **Dictionary order == time order**, which makes trace-listing UIs sort
  chronologically by default.
- **The frontend is the true origin.** Browser OTel SDKs generate IDs in the same
  format and propagate them via W3C `traceparent`; the server **inherits** an
  inbound trace and does not regenerate. This generator is the fallback for
  inbound requests with no `traceparent` (curl, cron, third-party webhooks).
- **Sampling caveat:** under `AlwaysSample()` the timestamp prefix is harmless. If
  this ever switches to `TraceIDRatioBased`, same-second traces would share a
  sampling decision — move the timestamp to the ID tail first (see `idgen.go`).

## Propagation & sampling

- Propagator: W3C `tracecontext` + `baggage` (composite). Baggage is available for
  deliberately propagated business fields.
- Sampler: `AlwaysSample()` — the volume is small. Revisit as
  `ParentBased(TraceIDRatioBased(…))` if export cost grows.

## Logging facade & field names

- Default handler emits **JSON** (prod; SLS field indexing). `ENV=dev` switches to
  text for readable `docker logs`.
- Structured field names live as constants in `attrs.go`, using the `_` form
  (`http_method`, `duration_ms`, …) chosen to **not** collide with the OTel
  auto-injected dotted keys. Some constants are shared fleet-wide and may be
  vestigial for attune specifically — treat `attrs.go` as the registry, not a
  per-service spec.
- Facade direction (single `logext` entry point, banning direct `slog.*` in
  business code) is tracked separately in issue #48.

## Lint enforcement (`scripts/lint-slog.sh`)

| Rule | Catches | Fix |
|---|---|---|
| rule-1 | non-context `slog.*` (loses correlation) | use `*Context` / `logext.*f(ctx, …)` |
| rule-2 | business code re-emitting a reserved key (`trace_id`, …) | drop it — the handler injects it |
| rule-3 | `&http.Client{}` without `otelhttp.NewTransport` | wrap the transport (outbound spans) |

**Facade exemption.** `internal/infra/observability/` and `internal/logext/` are exempt
from the business-field rules (1, 2): they *define and inject* the reserved keys
rather than misuse them. `TraceIDHandler` setting `trace_id` is the canonical
source, not a rule-2 violation. The linter runs `--strict` in pre-commit and CI.

## References

- Code: `internal/infra/observability/{otel,idgen,slog,attrs}.go`, `cmd/attune/main.go`.
- Issues: #9 (cleared the warnings + `--strict` + this doc), #48 (logext facade),
  #4 (lint-slog), #1 (CI gate).
