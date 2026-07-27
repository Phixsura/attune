# Cohort Sync Gaps — Complete Design Spec

## Phase A: "可用" (~400 lines)

### 1. UpdateCohortSource handler
- Service: `UpdateSource(ctx, UpdateSourceInput)` — get current, apply patches, re-encrypt credential if changed, validate, call repo.UpdateSource, audit
- Handler: `UpdateSource` in console handler, PATCH route at `/cohort-sync/sources/{id}`
- Pattern: follow externalsync UpdateConnection

### 2. TestCohortSource handler  
- Add `Check(ctx, Connection) (CheckResult, error)` to `cohortsync.Provider` interface
- Amplitude Check: GET /api/5/cohorts/list (list cohorts, verify auth)
- Mixpanel Check: GET /api/2.0/engage (with limit=0, verify auth)
- Service: `TestSource(ctx, tenantID, id)` — decrypt cred, lookup adapter, call Check
- Handler: `TestSource`, POST route at `/cohort-sync/sources/{id}:test`

### 3. Feedback/Request filter dropdown
- Add cohort select combobox to feedback FilterBar
- Load cohorts via listCohortsQuery, pass cohort_id to API

### 4. Console create source dialog
- CreateSourceDialog component with provider/name/auth_type/credential/enabled fields

## Phase B: "好用" (~1500 lines)

### 5. PullCohort real implementation
- Amplitude: async 3-step (request → poll → download CSV)
- Mixpanel: paginated Engage API with cohort filter

### 6. Console complete CRUD UI
- Source detail with edit/delete/test actions
- Cohort detail with sync history and member count
- Sync run history panel
- Edit source dialog
- Edit cohort dialog (name, stale_ttl_days, enabled)

### 7. Atomic transactions for Apply
- Create repo ApplyDelta/ApplyFullSnapshot methods that wrap all operations in one tx
- Pass pgx.Tx through instead of pool

### 8. Grafana dashboard + alerting
- Add cohort sync row to attune-operations.json
- Add alerting rules to attune-alerts.yml

## Phase C: "世界级" (~1200 lines)

### 9. Webhook event log + dedup
- cohort_sync_events table with dedupe_key unique constraint
- Record event before processing, dedup on replay

### 10. Cohort member listing
- ListMembers repo method + handler + frontend component

### 11. Remaining items
- Mixpanel webhook signature verification
- Control tower health badge
- Deployment documentation
- Multi-cohort filter (CohortIDs []string)
