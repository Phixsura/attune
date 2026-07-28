# Proposal: Cohort Sync Production-Level Completion (#233-P2)

## Context

Issue #233 cohort sync (Amplitude + Mixpanel) has a solid backend — 40 commits,
11k+ lines, 37 production-readiness findings already fixed. But the Console UI
exposes only ~40% of backend capabilities. An 8-product deep research (Braze,
LaunchDarkly, Segment, Productboard, Amplitude, Mixpanel, Customer.io, Statsig)
plus a 91-finding 5-dimension gap investigation revealed the same conclusion:
**the backend is production-grade; the product experience is MVP**.

The root cause: the UI was built as a standalone feature instead of following the
Console's established component patterns (PageHero, Card, toast, Loading,
EmptyState, useDocumentTitle, useTranslation, Table, controlled dialogs).

This proposal closes every BLOCKER, CRITICAL, and HIGH gap in one coherent pass.

## Scope: 3 BLOCKER + 12 CRITICAL + 22 HIGH = 37 gaps

### What we're NOT doing (MEDIUM/LOW — next iteration)
- Pagination/cursor on lists (MEDIUM)
- Cohort overlap/trending charts (LOW)
- Member export CSV (LOW)
- Provider icon branding (MEDIUM)
- Pipeline-stage visualization (MEDIUM — future)
- Consecutive-failure auto-pause (MEDIUM — future)
- Per-cohort feedback count decomposition (LOW — future)

---

## Phase 1: Backend API Completion (proto + handler + service)

### 1.1 New Proto RPCs + Messages

Add to `proto/attune/v1/cohort_sync.proto`:

```protobuf
// New RPCs
rpc GetCohortSource(GetCohortSourceRequest) returns (CohortSource) {
  option (google.api.http) = {get: "/fb/v1/console/cohort-sync/sources/{id}"};
}
rpc GetCohort(GetCohortRequest) returns (Cohort) {
  option (google.api.http) = {get: "/fb/v1/console/cohort-sync/cohorts/{id}"};
}
rpc ListCohortMembers(ListCohortMembersRequest) returns (ListCohortMembersResponse) {
  option (google.api.http) = {get: "/fb/v1/console/cohort-sync/cohorts/{cohort_id}/members"};
}

// New messages
message GetCohortSourceRequest { string id = 1; }
message GetCohortRequest { string id = 1; }
message CohortMembership {
  string id = 1;
  string external_user_id = 2;
  string email = 3;
  string display_name = 4;
  google.protobuf.Timestamp joined_at = 5;
  google.protobuf.Timestamp last_seen_at = 6;
}
message ListCohortMembersRequest {
  string cohort_id = 1;
  optional int32 limit = 2;
}
message ListCohortMembersResponse {
  repeated CohortMembership members = 1;
}
```

Modify existing messages:
- **`Cohort`**: add `string source_name = 13; string source_provider = 14;`
  (avoids client-side join; populated by handler from source data)
- **`CohortSource`**: add `repeated string webhook_urls = 13;` (array of
  complete webhook URLs; Amplitude gets 3, Mixpanel gets 1; handler computes
  from `config.console.public_url` + provider-specific path template)
- **`CohortSyncHealth`**: add `optional google.protobuf.Timestamp last_sync_at = 6;
  int32 syncs_last_24h = 7; int32 disabled_sources = 8;`

Run `make proto` to regenerate.

### 1.2 Backend Wiring

**Service layer** (`internal/service/cohortsync/service.go`):
- Add `GetCohort(ctx, tenantID, id)` public method (passthrough to repo)
- Add `ListMembers(ctx, tenantID, cohortID, limit)` public method
- Add `ListMembers` to `Repo` interface
- Enhance `Health()` to return `LastSyncAt`, `SyncsLast24h`, `DisabledSources`

**Handler** (`internal/handlers/console/cohortsync/handler.go`):
- Add `GetSource` handler (route `GET /sources/{id}`)
- Add `GetCohort` handler (route `GET /cohorts/{id}`)
- Add `ListMembers` handler (route `GET /cohorts/{cohort_id}/members`)
- Modify `sourceToProto()`: compute `webhook_urls` from console public URL config
  (inject via handler constructor, same pattern as `inbound/webhookBaseURL`)
- Modify `cohortToProto()`: populate `source_name`/`source_provider` (either
  via join query or by passing source data through from the service)

**Router** (`internal/handlers/console/router.go`):
- Mount `GET /sources/{id}`, `GET /cohorts/{id}`, `GET /cohorts/{cohort_id}/members`

### 1.3 Webhook URL Resolution

The handler needs the public base URL to construct complete webhook URLs. Pattern
from `inbound/adapter/webhook`: the handler receives `webhookBaseURL string` in
its constructor (injected from `config.Console.PublicURL` at `cmd/attune/setup.go`).

```go
// In NewHandler:
func NewHandler(service service, webhookBaseURL string) *Handler

// In sourceToProto:
func sourceToProto(s repo.Source, webhookBaseURL string) *attunev1.CohortSource {
    out.WebhookUrls = buildWebhookURLs(s, webhookBaseURL)
}

func buildWebhookURLs(s repo.Source, base string) []string {
    prefix := base + "/v1/cohort-sync"
    switch s.Provider {
    case "amplitude":
        return []string{
            prefix + "/amplitude/" + s.TenantID + "/" + s.ID.String() + "/create",
            prefix + "/amplitude/" + s.TenantID + "/" + s.ID.String() + "/add",
            prefix + "/amplitude/" + s.TenantID + "/" + s.ID.String() + "/remove",
        }
    case "mixpanel":
        return []string{prefix + "/mixpanel/" + s.TenantID + "/" + s.ID.String()}
    }
}
```

---

## Phase 2: Console UI Rewrite — Match Established Patterns

The entire `cohort-sync-ui.tsx` (380 lines, one monolithic file) is replaced with
a component architecture matching `external-sync` and `notification-targets`:

### 2.1 File Structure

```
console/src/features/cohort-sync/
├── api/
│   └── cohort-sync.ts          (keep — add getCohortSource, getCohort, listMembers)
├── components/
│   ├── cohort-sync-page.tsx    (rewrite — PageHero + Tabs + queries + mutations)
│   ├── sources-tab.tsx         (new — Card + Table of sources with actions)
│   ├── cohorts-tab.tsx         (new — Card + Table of cohorts with actions)
│   ├── source-form-dialog.tsx  (new — controlled Dialog for create + edit)
│   ├── delete-source-dialog.tsx(new — proper Dialog, not window.confirm)
│   ├── cohort-detail-dialog.tsx(new — Sheet showing members + run history)
│   ├── webhook-urls-display.tsx(new — labeled URL list with copy buttons)
│   └── sync-run-history.tsx    (new — Table of sync runs per cohort)
└── __tests__/
    ├── cohort-sync-page.test.tsx
    └── source-form-dialog.test.tsx
```

### 2.2 Established Patterns to Follow (from exploration)

| Pattern | Reference | How to apply |
|---------|-----------|-------------|
| **PageHero** | `external-sync-page.tsx` | `<PageHero eyebrow={t('shell.groups.integrations')} title={t('cohort_sync.title')} subtitle={...} metrics={...}>` with `<PageHeroMetric>` for Sources/Cohorts/Members |
| **Card + Table** | `notification-targets` TargetTable | Wrap sources/cohorts in `<Card>` + `<Table>` with `<TableHeader>/<TableBody>/<TableRow>` |
| **Toast (sonner)** | `notify-targets-page.tsx` lines 53-96 | `toast.success(t('common.create'))` on create, `toast.error(err.message)` on error. Import from `sonner`. |
| **Controlled Dialog** | `notification-targets` CreateTargetDialog | Parent controls `open`/`onOpenChange`/`pending` props; dialog does not manage its own trigger. |
| **Delete Dialog** | `inbound-sources` DeleteInboundSourceDialog | Proper `<Dialog>` with warning text and destructive-variant confirm button. |
| **Loading** | `@/components/loading` | `<Loading />` with Loader2 spinner + i18n text |
| **EmptyState** | `@/components/empty-state` | `<EmptyState icon={...} title={...} description={...} action={<Button>} />` |
| **useDocumentTitle** | `external-sync-page.tsx` | `useDocumentTitle(t('cohort_sync.title'))` |
| **useTranslation** | All features | Every user-visible string through `t('cohort_sync.*')` |
| **Clipboard** | `mcp-clients` `connection-workspace-card.tsx` | `navigator.clipboard.writeText(url)` with "复制"→"已复制" button state toggle |
| **Form** | All create/edit dialogs | `<form onSubmit={...}>` wrapper, `disabled={pending}` on all inputs, form reset on close |
| **Staleness** | `notification-targets` FailureBadge | `formatDistanceToNow(date, { locale: zhCN })` for relative timestamps |

### 2.3 Key Component Designs

**`cohort-sync-page.tsx`** (~200 lines):
- `useDocumentTitle(t('cohort_sync.title'))`
- `PageHero` with 3 `PageHeroMetric`: sources (active/total), cohorts, members
- `Tabs`: "来源" (Sources) and "人群" (Cohorts)
- Mutations for create/update/delete/test/sync all at page level (not in child)
- All mutations: `onSuccess: () => { invalidate(); toast.success(t(...)) }`,
  `onError: (err) => toast.error(...)`
- Create dialog controlled state: `const [createOpen, setCreateOpen] = useState(false)`

**`sources-tab.tsx`** (~180 lines):
- `<Card>` wrapping `<Table>` with columns: Name, Provider, Status, Last Sync, Actions
- Status: `<StatusBadge>` (Active/Disabled/Error) with tooltip
- Last Sync: `formatDistanceToNow()` with staleness color (green <1h, yellow <24h, red >24h)
- Actions: Test (Zap icon), Edit (Pencil icon), Enable/Disable toggle, Delete (Trash2 icon)
- Webhook URL: expandable row detail or Sheet showing all URLs with copy buttons
- EmptyState when no sources

**`source-form-dialog.tsx`** (~200 lines):
- Dual-mode: create (no initial data) / edit (pre-populated from source)
- Fields: Provider (Select, disabled in edit mode), Name, Webhook Credential,
  Pull Credential, Base URL (optional), Enabled toggle
- `<form>` wrapper, Enter-to-submit, disabled during pending
- Pull credential placeholder varies by provider (api_key:secret_key vs username:secret)
- Help links to provider documentation

**`cohorts-tab.tsx`** (~150 lines):
- `<Card>` + `<Table>`: Name, Source, Members, Last Sync, Status, Actions
- Source column shows `source_name` + provider badge (text, not icon yet)
- Actions: Sync Now (RefreshCcw icon), Edit (Pencil for name/TTL/description/enabled)
- Click row → opens cohort-detail-dialog

**`cohort-detail-dialog.tsx`** (~200 lines):
- `<Sheet>` (side panel, not overlay dialog) — pattern from existing Console
- Sections: Overview (name, source, member count, TTL), Members preview
  (first 50 via ListMembers), Sync Run History (via listCohortSyncRunsQuery)
- Sync runs table: Trigger, Status, Added, Removed, Total, Error, Duration, Timestamp
- Members table: External ID, Email, Display Name, Joined, Last Seen

**`webhook-urls-display.tsx`** (~60 lines):
- Takes `urls: string[]` and `provider: string` props
- Renders each URL in a `<code>` block with a copy button
- Labels: Amplitude shows "Create URL", "Add URL", "Remove URL"; Mixpanel shows "Webhook URL"

**`sync-run-history.tsx`** (~100 lines):
- `<Table>` of sync runs with columns: Time (relative), Trigger, Status, +Added, -Removed, Total, Error
- Status badge: succeeded (green), failed (red), skipped (gray), running (blue spinner)
- Error message in expandable row detail

### 2.4 i18n Keys

Add to `console/src/i18n/zh-CN.json` under `cohort_sync`:

```json
{
  "cohort_sync": {
    "title": "人群同步",
    "subtitle": "从 Amplitude 和 Mixpanel 导入人群，按受众成员关系筛选反馈和客户需求",
    "tabs": { "sources": "来源", "cohorts": "人群" },
    "source": {
      "create": "添加来源",
      "edit": "编辑来源",
      "delete": "删除来源",
      "delete_confirm": "确定要删除来源「{{name}}」吗？关联的所有人群和成员数据将被永久删除。",
      "test": "测试连接",
      "test_ok": "连接成功",
      "test_fail": "连接失败: {{error}}",
      "provider": "提供商",
      "name": "名称",
      "credential": "Webhook 认证密钥",
      "credential_help": "用于验证来自提供商的 webhook 推送",
      "pull_credential": "拉取凭证（可选）",
      "pull_credential_help_amplitude": "Amplitude API Key 和 Secret Key（格式：api_key:secret_key），用于测试连接和手动同步",
      "pull_credential_help_mixpanel": "Mixpanel 服务账号凭证（格式：username:secret），用于测试连接和手动同步",
      "base_url": "自定义 API 地址（可选）",
      "enabled": "启用",
      "webhook_urls": "Webhook 地址",
      "webhook_url_copy": "复制",
      "webhook_url_copied": "已复制",
      "no_sources": "尚未配置人群来源",
      "no_sources_desc": "连接 Amplitude 或 Mixpanel 开始导入人群成员",
      "status": { "active": "活跃", "disabled": "已禁用", "error": "错误" },
      "last_sync": "最后同步"
    },
    "cohort": {
      "no_cohorts": "尚未同步任何人群",
      "no_cohorts_desc": "人群将在 Amplitude 或 Mixpanel 推送成员数据后出现",
      "sync_now": "立即同步",
      "sync_ok": "同步完成: +{{added}} -{{removed}}，共 {{total}} 名成员",
      "members": "成员",
      "member_count": "{{count}} 名成员",
      "detail": "人群详情",
      "runs": "同步记录",
      "edit": "编辑人群",
      "ttl_days": "过期天数",
      "description": "描述"
    },
    "run": {
      "trigger": { "webhook": "自动推送", "manual": "手动同步" },
      "status": { "succeeded": "成功", "failed": "失败", "skipped": "已跳过", "running": "运行中" },
      "added": "+{{count}}",
      "removed": "-{{count}}",
      "error": "错误"
    },
    "health": {
      "sources": "来源",
      "cohorts": "人群",
      "members": "活跃成员",
      "last_sync": "最后同步",
      "stale_warning": "{{hours}} 小时未同步"
    }
  }
}
```

### 2.5 Feedback + Customer Request Filter Fixes

- **Feedback page**: replace hardcoded `"All cohorts"` with `t('cohort_sync.filter.all')`
- **Feedback page**: resolve cohort ID to name in the active filter chip
- **Customer request page**: add the same cohort filter dropdown (backend already supports `cohort_id`)

### 2.6 Route + A11y Mocks

- Route loader: catch individual query failures independently (no single-failure-blocks-all)
- Route: add `errorComponent` prop
- A11y mocks: update `console/e2e/accessibility/route-mocks.ts` for any new API endpoints

---

## Phase 3: Frontend Tests

Following the `notification-targets` test pattern:

**`cohort-sync-page.test.tsx`** (~300 lines):
- Mock API responses for sources, cohorts, health
- Test: create source → toast.success
- Test: delete source → confirmation dialog → toast.success
- Test: delete error → toast.error
- Test: test source → inline result (✓ OK / ✗ error)
- Test: sync now → toast.success with member counts
- Test: sync error → toast.error
- Test: edit source → dialog pre-populated → save → toast.success
- Test: enable/disable toggle → API call → toast
- Test: empty state renders EmptyState component
- Test: loading state renders Loading component

**`source-form-dialog.test.tsx`** (~100 lines):
- Test: form submission with valid data
- Test: form validation (empty name, empty credential)
- Test: form reset on close
- Test: disabled fields during pending

---

## Verification

```bash
# Backend
go build ./...
go vet ./...
go test -count=1 ./internal/service/cohortsync/... \
  ./internal/handlers/console/cohortsync/... \
  ./internal/repo/cohortsync/...
make proto  # regenerate, verify no drift

# Frontend
cd console
pnpm tsc -b --noEmit
pnpm biome check
pnpm vitest run --coverage
pnpm arch  # dependency-cruiser

# Full CI
make ci-check

# Manual verification
# 1. Open Console → Integrations → Cohort Sync
# 2. Create a source (verify webhook URLs shown with copy buttons)
# 3. Click Test (verify inline result)
# 4. Click Edit (verify pre-populated dialog)
# 5. Toggle enable/disable (verify status change)
# 6. Click a cohort → verify detail sheet with members + run history
# 7. Click Sync Now → verify toast with member counts
# 8. Delete source → verify proper dialog (not window.confirm)
# 9. Check feedback page → verify cohort filter with names (not IDs)
```

---

## Files Changed (estimated)

| Area | Files | Lines |
|------|-------|-------|
| Proto + regenerated | 5 | ~200 |
| Backend handler + service | 4 | ~250 |
| Console cohort-sync feature (rewrite) | 10 | ~1500 |
| i18n | 1 | ~80 |
| Cross-cutting fixes (feedback, customer-request) | 3 | ~40 |
| Tests (frontend) | 2 | ~400 |
| A11y mocks | 1 | ~20 |
| **Total** | **~26** | **~2490** |
