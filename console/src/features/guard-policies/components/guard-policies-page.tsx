import { useQuery } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { Loader2, Pencil, Plus, RotateCcw, Sparkles, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  type GuardPolicy,
  guardPoliciesQuery,
  useCreateGuardPolicy,
  useDeleteGuardPolicy,
  usePatchGuardPolicy,
  useResolveGuardPolicy,
} from '@/features/guard-policies/api/guard-policies'
import { usePermissions } from '@/features/session/hooks/use-permissions'

const CHANNELS = ['all', 'api', 'webhook', 'email'] as const
const PURPOSES = ['enrich', 'digest', 'reply_draft', 'eval', 'outbound'] as const
const STAGES = ['llm_input', 'llm_output', 'outbound', 'tool_call'] as const
const ACTIONS = ['off', 'audit', 'redact', 'block'] as const
const ENTITIES = ['email', 'phone', 'cn_mobile', 'cn_id', 'credit_card'] as const
const DEFAULT_REPLACEMENT = '<PII:{entity}>'

type GuardPolicyFormState = {
  name: string
  kind: string
  enabled: boolean
  priority: string
  channel: string
  sourceIds: string
  sourceTags: string
  purpose: string
  stage: string
  environment: string
  entities: string[]
  action: string
  replacement: string
}

export function GuardPoliciesPage() {
  const { t } = useTranslation()
  const { can } = usePermissions()
  const canEdit = can('settings:guard_policies:edit')
  const list = useQuery(guardPoliciesQuery())
  const resolve = useResolveGuardPolicy()
  const create = useCreateGuardPolicy()
  const patch = usePatchGuardPolicy()
  const del = useDeleteGuardPolicy()
  const [channel, setChannel] = useState('api')
  const [purpose, setPurpose] = useState('enrich')
  const [sourceId, setSourceId] = useState('')
  const [sourceTags, setSourceTags] = useState('')
  const [createOpen, setCreateOpen] = useState(false)
  const [editing, setEditing] = useState<GuardPolicy | null>(null)
  const [deleting, setDeleting] = useState<GuardPolicy | null>(null)

  const policies = list.data ?? []
  const writePending = create.isPending || patch.isPending || del.isPending

  const preview = () => {
    resolve.mutate(
      {
        channel,
        sourceId: sourceId.trim(),
        sourceTags: splitCSV(sourceTags),
        purpose,
        environment: '',
      },
      {
        onError: (err) => toast.error(messageOf(err)),
      },
    )
  }

  const createPolicy = async (policy: GuardPolicy) => {
    try {
      await create.mutateAsync(policy)
      toast.success(t('guard_policies.created'))
      setCreateOpen(false)
    } catch (err) {
      toast.error(messageOf(err))
      throw err
    }
  }

  const updatePolicy = async (policy: GuardPolicy) => {
    if (!editing?.id) return
    try {
      await patch.mutateAsync({ id: editing.id, policy })
      toast.success(t('guard_policies.updated'))
      setEditing(null)
    } catch (err) {
      toast.error(messageOf(err))
      throw err
    }
  }

  const deletePolicy = async () => {
    if (!deleting?.id) return
    try {
      await del.mutateAsync(deleting.id)
      toast.success(t('guard_policies.deleted'))
      setDeleting(null)
    } catch (err) {
      toast.error(messageOf(err))
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t('nav.guard_policies')}</h1>
          <p className="mt-1 max-w-3xl text-sm text-muted-foreground">
            {t('guard_policies.subtitle')}
          </p>
        </div>
        <div className="flex shrink-0 gap-2">
          <Button type="button" variant="outline" onClick={() => list.refetch()}>
            <RotateCcw className="h-4 w-4" />
            {t('guard_policies.refresh')}
          </Button>
          {canEdit && (
            <Button type="button" onClick={() => setCreateOpen(true)}>
              <Plus className="h-4 w-4" />
              {t('guard_policies.create')}
            </Button>
          )}
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>{t('guard_policies.effective_title')}</CardTitle>
          <CardDescription>{t('guard_policies.effective_help')}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-4 md:grid-cols-4">
            <Field label={t('guard_policies.channel')} htmlFor="guard-channel">
              <Select value={channel} onValueChange={setChannel}>
                <SelectTrigger id="guard-channel" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {CHANNELS.filter((value) => value !== 'all').map((value) => (
                    <SelectItem key={value} value={value}>
                      {formatEnum(t, 'channels', value)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field label={t('guard_policies.purpose')} htmlFor="guard-purpose">
              <Select value={purpose} onValueChange={setPurpose}>
                <SelectTrigger id="guard-purpose" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {PURPOSES.map((value) => (
                    <SelectItem key={value} value={value}>
                      {formatEnum(t, 'purposes', value)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field label={t('guard_policies.source_id')} htmlFor="guard-source-id">
              <Input
                id="guard-source-id"
                value={sourceId}
                placeholder="source UUID"
                onChange={(e) => setSourceId(e.target.value)}
              />
            </Field>
            <Field label={t('guard_policies.source_tags')} htmlFor="guard-tags">
              <Input
                id="guard-tags"
                value={sourceTags}
                placeholder="regulated, vip"
                onChange={(e) => setSourceTags(e.target.value)}
              />
            </Field>
          </div>
          <Button type="button" onClick={preview} disabled={resolve.isPending}>
            {resolve.isPending ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <Sparkles className="h-4 w-4" />
            )}
            {t('guard_policies.preview')}
          </Button>
          {resolve.data ? <RulesTable rules={resolve.data.rules} /> : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('guard_policies.policies_title')}</CardTitle>
          <CardDescription>{t('guard_policies.policies_help')}</CardDescription>
        </CardHeader>
        <CardContent>
          {list.isPending ? (
            <div className="flex items-center justify-center py-8 text-muted-foreground">
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              {t('app.loading')}
            </div>
          ) : (
            <PolicyList
              policies={policies}
              onEdit={canEdit ? setEditing : undefined}
              onDelete={canEdit ? setDeleting : undefined}
              writePending={writePending}
            />
          )}
        </CardContent>
      </Card>

      <PolicyDialog
        mode="create"
        open={createOpen}
        policy={null}
        onClose={() => setCreateOpen(false)}
        onSubmit={createPolicy}
        pending={create.isPending}
      />
      <PolicyDialog
        mode="edit"
        open={editing !== null}
        policy={editing}
        onClose={() => setEditing(null)}
        onSubmit={updatePolicy}
        pending={patch.isPending}
      />
      <DeletePolicyDialog
        policy={deleting}
        onCancel={() => setDeleting(null)}
        onConfirm={deletePolicy}
        pending={del.isPending}
      />
    </div>
  )
}

function PolicyList({
  policies,
  onEdit,
  onDelete,
  writePending,
}: {
  policies: GuardPolicy[]
  onEdit?: (policy: GuardPolicy) => void
  onDelete?: (policy: GuardPolicy) => void
  writePending: boolean
}) {
  const { t } = useTranslation()
  if (policies.length === 0) {
    return (
      <p className="py-8 text-center text-sm text-muted-foreground">{t('guard_policies.empty')}</p>
    )
  }
  return (
    <div className="space-y-3">
      {policies.map((policy) => {
        const tenantPolicy = policy.scope !== 'system'
        return (
          <div key={policy.id || policy.name} className="rounded-md border border-border p-4">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <div className="break-words font-medium">{policy.name}</div>
                  <span className="rounded-md border border-border px-2 py-0.5 text-xs text-muted-foreground">
                    {formatEnum(t, 'scopes', policy.scope)}
                  </span>
                  <span
                    className={
                      policy.enabled ? 'text-sm text-green-600' : 'text-sm text-muted-foreground'
                    }
                  >
                    {policy.enabled ? t('guard_policies.enabled') : t('guard_policies.disabled')}
                  </span>
                </div>
                <div className="mt-1 text-xs text-muted-foreground">
                  {formatEnum(t, 'kinds', policy.kind)} ·{' '}
                  {t('guard_policies.priority_value', { value: policy.priority })}
                </div>
              </div>
              {(onEdit || onDelete) && (
                <div className="flex shrink-0 gap-1">
                  {onEdit && (
                    <Button
                      type="button"
                      variant="outline"
                      size="icon-sm"
                      disabled={!tenantPolicy || writePending}
                      title={
                        tenantPolicy
                          ? t('guard_policies.edit')
                          : t('guard_policies.system_readonly')
                      }
                      onClick={() => onEdit(policy)}
                    >
                      <Pencil className="h-4 w-4" />
                    </Button>
                  )}
                  {onDelete && (
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      disabled={!tenantPolicy || writePending}
                      title={
                        tenantPolicy
                          ? t('guard_policies.delete')
                          : t('guard_policies.system_readonly')
                      }
                      onClick={() => onDelete(policy)}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  )}
                </div>
              )}
            </div>
            <div className="mt-3 grid gap-3 text-xs md:grid-cols-[1fr_2fr]">
              <pre className="min-h-16 whitespace-pre-wrap rounded-md bg-muted/40 p-3 font-sans text-muted-foreground">
                {formatTarget(policy, t)}
              </pre>
              <RulesTable rules={policy.rules} compact />
            </div>
          </div>
        )
      })}
    </div>
  )
}

function PolicyDialog({
  mode,
  open,
  policy,
  onClose,
  onSubmit,
  pending,
}: {
  mode: 'create' | 'edit'
  open: boolean
  policy: GuardPolicy | null
  onClose: () => void
  onSubmit: (policy: GuardPolicy) => Promise<unknown>
  pending: boolean
}) {
  const { t } = useTranslation()
  const [values, setValues] = useState<GuardPolicyFormState>(() => formFromPolicy(null))

  useEffect(() => {
    if (open) setValues(formFromPolicy(policy))
  }, [open, policy])

  const setField = <K extends keyof GuardPolicyFormState>(
    field: K,
    value: GuardPolicyFormState[K],
  ) => setValues((prev) => ({ ...prev, [field]: value }))

  const toggleEntity = (entity: string, checked: boolean) => {
    setValues((prev) => ({
      ...prev,
      entities: checked
        ? Array.from(new Set([...prev.entities, entity]))
        : prev.entities.filter((value) => value !== entity),
    }))
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    const next = policyFromForm(values, policy)
    if (!next.name.trim() || next.rules[0]?.entities.length === 0) return
    void onSubmit(next)
  }

  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="sm:max-w-3xl">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>
              {mode === 'create'
                ? t('guard_policies.create_title')
                : t('guard_policies.edit_title')}
            </DialogTitle>
            <DialogDescription>{t('guard_policies.dialog_help')}</DialogDescription>
          </DialogHeader>

          <div className="grid gap-4 py-4 md:grid-cols-2">
            <Field label={t('guard_policies.name')} htmlFor="gp-name">
              <Input
                id="gp-name"
                value={values.name}
                onChange={(e) => setField('name', e.target.value)}
                disabled={pending}
                required
                maxLength={120}
              />
            </Field>
            <div className="grid grid-cols-[1fr_120px] gap-3">
              <Field label={t('guard_policies.kind')} htmlFor="gp-kind">
                <Select
                  value={values.kind}
                  onValueChange={(value) => setField('kind', value)}
                  disabled={pending}
                >
                  <SelectTrigger id="gp-kind" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="default">{formatEnum(t, 'kinds', 'default')}</SelectItem>
                    <SelectItem value="override">{formatEnum(t, 'kinds', 'override')}</SelectItem>
                    <SelectItem value="baseline">{formatEnum(t, 'kinds', 'baseline')}</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
              <Field label={t('guard_policies.priority')} htmlFor="gp-priority">
                <Input
                  id="gp-priority"
                  type="number"
                  min={0}
                  max={10000}
                  value={values.priority}
                  onChange={(e) => setField('priority', e.target.value)}
                  disabled={pending}
                />
              </Field>
            </div>

            <Field label={t('guard_policies.channel')} htmlFor="gp-channel">
              <Select
                value={values.channel}
                onValueChange={(value) => setField('channel', value)}
                disabled={pending}
              >
                <SelectTrigger id="gp-channel" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {CHANNELS.map((value) => (
                    <SelectItem key={value} value={value}>
                      {formatEnum(t, 'channels', value)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field label={t('guard_policies.purpose')} htmlFor="gp-purpose">
              <Select
                value={values.purpose}
                onValueChange={(value) => setField('purpose', value)}
                disabled={pending}
              >
                <SelectTrigger id="gp-purpose" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {PURPOSES.map((value) => (
                    <SelectItem key={value} value={value}>
                      {formatEnum(t, 'purposes', value)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>

            <Field label={t('guard_policies.source_id')} htmlFor="gp-source-ids">
              <Input
                id="gp-source-ids"
                value={values.sourceIds}
                placeholder="uuid-1, uuid-2"
                onChange={(e) => setField('sourceIds', e.target.value)}
                disabled={pending}
              />
            </Field>
            <Field label={t('guard_policies.source_tags')} htmlFor="gp-source-tags">
              <Input
                id="gp-source-tags"
                value={values.sourceTags}
                placeholder="regulated, vip"
                onChange={(e) => setField('sourceTags', e.target.value)}
                disabled={pending}
              />
            </Field>

            <Field label={t('guard_policies.stage')} htmlFor="gp-stage">
              <Select
                value={values.stage}
                onValueChange={(value) => setField('stage', value)}
                disabled={pending}
              >
                <SelectTrigger id="gp-stage" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {STAGES.map((value) => (
                    <SelectItem key={value} value={value}>
                      {formatEnum(t, 'stages', value)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field label={t('guard_policies.environment')} htmlFor="gp-environment">
              <Input
                id="gp-environment"
                value={values.environment}
                placeholder="prod, staging"
                onChange={(e) => setField('environment', e.target.value)}
                disabled={pending}
              />
            </Field>

            <div className="space-y-2 md:col-span-2">
              <Label>{t('guard_policies.entities')}</Label>
              <div className="grid gap-2 sm:grid-cols-2 md:grid-cols-5">
                {ENTITIES.map((entity) => (
                  <label
                    key={entity}
                    className="flex items-center gap-2 rounded-md border border-border px-3 py-2 text-sm"
                  >
                    <input
                      type="checkbox"
                      checked={values.entities.includes(entity)}
                      onChange={(e) => toggleEntity(entity, e.target.checked)}
                      disabled={pending}
                    />
                    <span>{entity}</span>
                  </label>
                ))}
              </div>
            </div>

            <Field label={t('guard_policies.action')} htmlFor="gp-action">
              <Select
                value={values.action}
                onValueChange={(value) => setField('action', value)}
                disabled={pending}
              >
                <SelectTrigger id="gp-action" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {ACTIONS.map((value) => (
                    <SelectItem key={value} value={value}>
                      {formatEnum(t, 'actions', value)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field label={t('guard_policies.replacement')} htmlFor="gp-replacement">
              <Input
                id="gp-replacement"
                value={values.replacement}
                onChange={(e) => setField('replacement', e.target.value)}
                disabled={pending || values.action !== 'redact'}
                maxLength={64}
              />
            </Field>

            <label className="flex items-center gap-2 text-sm md:col-span-2">
              <input
                type="checkbox"
                checked={values.enabled}
                onChange={(e) => setField('enabled', e.target.checked)}
                disabled={pending}
              />
              {t('guard_policies.enabled')}
            </label>
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose} disabled={pending}>
              {t('common.cancel')}
            </Button>
            <Button
              type="submit"
              disabled={pending || !values.name.trim() || values.entities.length === 0}
            >
              {pending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              {mode === 'create' ? t('common.create') : t('common.save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function DeletePolicyDialog({
  policy,
  onCancel,
  onConfirm,
  pending,
}: {
  policy: GuardPolicy | null
  onCancel: () => void
  onConfirm: () => void
  pending: boolean
}) {
  const { t } = useTranslation()
  return (
    <Dialog open={policy !== null} onOpenChange={(v) => !v && onCancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('guard_policies.delete_title')}</DialogTitle>
          <DialogDescription>{t('guard_policies.delete_help')}</DialogDescription>
        </DialogHeader>
        {policy ? (
          <p className="my-2 break-all font-mono text-xs text-muted-foreground">{policy.name}</p>
        ) : null}
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onCancel} disabled={pending}>
            {t('common.cancel')}
          </Button>
          <Button type="button" variant="destructive" onClick={onConfirm} disabled={pending}>
            {pending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
            {t('guard_policies.delete_confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function RulesTable({
  rules,
  compact = false,
}: {
  rules: GuardPolicy['rules']
  compact?: boolean
}) {
  const { t } = useTranslation()
  if (rules.length === 0) {
    return <p className="text-sm text-muted-foreground">{t('guard_policies.no_rules')}</p>
  }
  return (
    <div className="overflow-x-auto rounded-md border border-border">
      <table className="w-full text-left text-sm">
        <thead className="bg-muted/40 text-xs text-muted-foreground">
          <tr>
            <th className="px-3 py-2 font-medium">{t('guard_policies.rule_guard')}</th>
            <th className="px-3 py-2 font-medium">{t('guard_policies.rule_stage')}</th>
            <th className="px-3 py-2 font-medium">{t('guard_policies.rule_entities')}</th>
            <th className="px-3 py-2 font-medium">{t('guard_policies.rule_action')}</th>
          </tr>
        </thead>
        <tbody>
          {rules.map((rule) => (
            <tr key={`${rule.guard}:${rule.stage}:${rule.entities.join(',')}:${rule.action}`}>
              <td className="px-3 py-2">{rule.guard}</td>
              <td className="px-3 py-2">{formatEnum(t, 'stages', rule.stage)}</td>
              <td className="px-3 py-2 font-mono text-xs">{rule.entities.join(', ')}</td>
              <td className="px-3 py-2">
                <span
                  className={rule.action === 'off' ? 'text-muted-foreground' : 'text-foreground'}
                >
                  {formatEnum(t, 'actions', rule.action)}
                </span>
                {!compact && rule.replacement ? (
                  <span className="ml-2 font-mono text-xs text-muted-foreground">
                    {rule.replacement}
                  </span>
                ) : null}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function Field({
  label,
  htmlFor,
  children,
}: {
  label: string
  htmlFor: string
  children: React.ReactNode
}) {
  return (
    <div className="space-y-2">
      <Label htmlFor={htmlFor}>{label}</Label>
      {children}
    </div>
  )
}

function formFromPolicy(policy: GuardPolicy | null): GuardPolicyFormState {
  const rule = policy?.rules[0]
  const target = policy?.target
  return {
    name: policy?.name ?? '',
    kind: policy?.kind || 'default',
    enabled: policy?.enabled ?? true,
    priority: String(policy?.priority || 100),
    channel: firstKnown(target?.channels, CHANNELS, 'all'),
    sourceIds: target?.sourceIds?.join(', ') ?? '',
    sourceTags: target?.sourceTags?.join(', ') ?? '',
    purpose: firstKnown(target?.purposes, PURPOSES, 'enrich'),
    stage: firstKnown(target?.stages ?? (rule?.stage ? [rule.stage] : []), STAGES, 'llm_input'),
    environment: target?.environments?.join(', ') ?? '',
    entities: rule?.entities?.length ? rule.entities : [...ENTITIES],
    action: ACTIONS.includes(rule?.action as (typeof ACTIONS)[number])
      ? rule?.action || 'redact'
      : 'redact',
    replacement: rule?.replacement || DEFAULT_REPLACEMENT,
  }
}

function policyFromForm(values: GuardPolicyFormState, original: GuardPolicy | null): GuardPolicy {
  const stage = values.stage || 'llm_input'
  const action = values.action || 'redact'
  return {
    id: original?.id ?? '',
    tenantId: undefined,
    name: values.name.trim(),
    kind: values.kind,
    enabled: values.enabled,
    priority: Number(values.priority) || 100,
    scope: 'tenant',
    target: {
      channels: values.channel === 'all' ? [] : [values.channel],
      sourceIds: splitCSV(values.sourceIds),
      sourceTags: splitCSV(values.sourceTags),
      purposes: values.purpose ? [values.purpose] : [],
      stages: [stage],
      environments: splitCSV(values.environment),
    },
    rules: [
      {
        guard: 'pii',
        stage,
        entities: values.entities,
        action,
        replacement: action === 'redact' ? values.replacement.trim() || DEFAULT_REPLACEMENT : '',
      },
    ],
  }
}

function splitCSV(raw: string): string[] {
  return raw
    .split(',')
    .map((value) => value.trim())
    .filter(Boolean)
}

function firstKnown<T extends string>(
  values: readonly string[] | undefined,
  allowed: readonly T[],
  fallback: T,
): T {
  const first = values?.[0]
  return allowed.includes(first as T) ? (first as T) : fallback
}

function formatTarget(policy: GuardPolicy, t: TFunction): string {
  const target = policy.target
  if (!target) return t('guard_policies.all_targets')
  const parts = [
    {
      label: t('guard_policies.target_labels.channels'),
      values: target.channels,
      group: 'channels',
    },
    { label: t('guard_policies.target_labels.source_ids'), values: target.sourceIds },
    { label: t('guard_policies.target_labels.source_tags'), values: target.sourceTags },
    {
      label: t('guard_policies.target_labels.purposes'),
      values: target.purposes,
      group: 'purposes',
    },
    { label: t('guard_policies.target_labels.stages'), values: target.stages, group: 'stages' },
    { label: t('guard_policies.target_labels.env'), values: target.environments },
  ]
    .filter((part) => Array.isArray(part.values) && part.values.length > 0)
    .map((part) => {
      const values = part.group
        ? part.values.map((value) => formatEnum(t, part.group, value))
        : part.values
      return `${part.label}: ${values.join(', ')}`
    })
  return parts.length > 0 ? parts.join('\n') : t('guard_policies.all_targets')
}

function formatEnum(t: TFunction, group: string, value: string): string {
  return t(`guard_policies.${group}.${value}`, { defaultValue: value })
}

function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : 'failed'
}

export const guardPolicyPageTestables = {
  firstKnown,
  formFromPolicy,
  formatTarget,
  policyFromForm,
  splitCSV,
}
