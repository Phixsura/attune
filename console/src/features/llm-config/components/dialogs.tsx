import { Loader2, RefreshCw } from 'lucide-react'
import type * as React from 'react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
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
import type {
  CreateLLMChannelRequest,
  LLMChannel,
  LLMChannelAbility,
  LLMProviderModel,
  LLMRoute,
  TestLLMChannelRequest,
  UpdateLLMChannelRequest,
  UpsertLLMChannelAbilityRequest,
  UpsertLLMRouteRequest,
} from '@/features/llm-config/api/llm-config'

const protocols = ['openai-compat', 'openai-responses', 'anthropic', 'gemini']
const statuses = ['enabled', 'disabled', 'draining']

export function ChannelDialog({
  open,
  target,
  pending,
  onOpenChange,
  onSubmit,
}: {
  open: boolean
  target: LLMChannel | null
  pending: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (
    body: CreateLLMChannelRequest | Omit<UpdateLLMChannelRequest, 'id'>,
  ) => Promise<unknown>
}) {
  const { t } = useTranslation()
  const [form, setForm] = useState(channelForm(target))
  useEffect(() => setForm(open ? channelForm(target) : channelForm(null)), [open, target])
  const isEdit = target !== null
  const requiresNewAPIKey =
    form.authMode === 'bearer' &&
    !form.apiKey.trim() &&
    (!isEdit || target.authMode === 'none' || !target.hasApiKey)
  const submitDisabled =
    pending ||
    !form.name.trim() ||
    !form.protocol ||
    (form.protocol === 'openai-compat' && !form.baseUrl.trim()) ||
    requiresNewAPIKey

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <form
          onSubmit={(event) => {
            event.preventDefault()
            if (submitDisabled) return
            const body = {
              name: form.name.trim(),
              protocol: form.protocol,
              baseUrl: form.baseUrl.trim(),
              authMode: form.authMode,
              status: form.status,
              priority: toInt(form.priority, 0),
              weight: toInt(form.weight, 1),
              timeoutSeconds: toInt(form.timeoutSeconds, 60),
            }
            const apiKey = form.apiKey.trim()
            void onSubmit(form.authMode === 'bearer' && apiKey ? { ...body, apiKey } : body)
          }}
        >
          <DialogHeader>
            <DialogTitle>
              {isEdit ? t('llm_config.channels.edit_title') : t('llm_config.channels.create_title')}
            </DialogTitle>
            <DialogDescription>{t('llm_config.channels.dialog_description')}</DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-4 md:grid-cols-2">
            <Field label={t('llm_config.channels.fields.name')} htmlFor="llm-channel-name">
              <Input
                id="llm-channel-name"
                value={form.name}
                onChange={(event) => setForm({ ...form, name: event.target.value })}
                disabled={pending}
                required
              />
            </Field>
            <Field label={t('llm_config.channels.fields.protocol')} htmlFor="llm-channel-protocol">
              <Select
                value={form.protocol}
                onValueChange={(value) => setForm({ ...form, protocol: value })}
                disabled={pending}
              >
                <SelectTrigger id="llm-channel-protocol" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {protocols.map((protocol) => (
                    <SelectItem key={protocol} value={protocol}>
                      {protocol}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field label={t('llm_config.channels.fields.base_url')} htmlFor="llm-channel-base-url">
              <Input
                id="llm-channel-base-url"
                value={form.baseUrl}
                onChange={(event) => setForm({ ...form, baseUrl: event.target.value })}
                placeholder="https://api.example.com"
                disabled={pending}
              />
            </Field>
            <Field label={t('llm_config.channels.fields.auth')} htmlFor="llm-channel-auth">
              <Select
                value={form.authMode}
                onValueChange={(value) =>
                  setForm({ ...form, authMode: value, apiKey: value === 'none' ? '' : form.apiKey })
                }
                disabled={pending}
              >
                <SelectTrigger id="llm-channel-auth" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="bearer">bearer</SelectItem>
                  <SelectItem value="none">none</SelectItem>
                </SelectContent>
              </Select>
            </Field>
            <Field label={t('llm_config.channels.fields.api_key')} htmlFor="llm-channel-api-key">
              <Input
                id="llm-channel-api-key"
                type="password"
                value={form.apiKey}
                onChange={(event) => setForm({ ...form, apiKey: event.target.value })}
                disabled={pending || form.authMode === 'none'}
                placeholder={isEdit ? t('llm_config.channels.keep_key') : ''}
              />
            </Field>
            <Field label={t('llm_config.channels.fields.status')} htmlFor="llm-channel-status">
              <Select
                value={form.status}
                onValueChange={(value) => setForm({ ...form, status: value })}
                disabled={pending}
              >
                <SelectTrigger id="llm-channel-status" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {statuses.map((status) => (
                    <SelectItem key={status} value={status}>
                      {status}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <NumberField
              id="llm-channel-priority"
              label={t('llm_config.channels.fields.priority')}
              value={form.priority}
              pending={pending}
              onChange={(priority) => setForm({ ...form, priority })}
            />
            <NumberField
              id="llm-channel-weight"
              label={t('llm_config.channels.fields.weight')}
              min={0}
              value={form.weight}
              pending={pending}
              onChange={(weight) => setForm({ ...form, weight })}
            />
            <NumberField
              id="llm-channel-timeout"
              label={t('llm_config.channels.fields.timeout')}
              min={1}
              value={form.timeoutSeconds}
              pending={pending}
              onChange={(timeoutSeconds) => setForm({ ...form, timeoutSeconds })}
            />
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              disabled={pending}
              onClick={() => onOpenChange(false)}
            >
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={submitDisabled}>
              {pending && <Loader2 className="mr-2 size-3.5 animate-spin" />}
              {isEdit ? t('common.save') : t('common.create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export function AbilityDialog({
  open,
  target,
  models,
  modelsPending,
  modelsError,
  pending,
  onRefreshModels,
  onOpenChange,
  onSubmit,
}: {
  open: boolean
  target: LLMChannelAbility | null
  models: LLMProviderModel[]
  modelsPending: boolean
  modelsError: string
  pending: boolean
  onRefreshModels: () => void
  onOpenChange: (open: boolean) => void
  onSubmit: (body: Omit<UpsertLLMChannelAbilityRequest, 'channelId'>) => Promise<unknown>
}) {
  const { t } = useTranslation()
  const [form, setForm] = useState(abilityForm(target))
  useEffect(() => setForm(open ? abilityForm(target) : abilityForm(null)), [open, target])
  const submitDisabled = pending || !form.logicalModel.trim() || !form.providerModel.trim()
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form
          onSubmit={(event) => {
            event.preventDefault()
            if (submitDisabled) return
            void onSubmit({
              logicalModel: form.logicalModel.trim(),
              providerModel: form.providerModel.trim(),
              enabled: form.enabled === 'true',
              priority: toInt(form.priority, 0),
              weight: toInt(form.weight, 1),
            })
          }}
        >
          <DialogHeader>
            <DialogTitle>{t('llm_config.abilities.dialog_title')}</DialogTitle>
            <DialogDescription>{t('llm_config.abilities.dialog_description')}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <Field label={t('llm_config.abilities.fields.logical')} htmlFor="llm-ability-logical">
              <Input
                id="llm-ability-logical"
                value={form.logicalModel}
                onChange={(event) => setForm({ ...form, logicalModel: event.target.value })}
                disabled={pending || target !== null}
                required
              />
            </Field>
            <ProviderModelField
              id="llm-ability-provider"
              label={t('llm_config.abilities.fields.provider')}
              value={form.providerModel}
              models={models}
              modelsPending={modelsPending}
              modelsError={modelsError}
              disabled={pending}
              onChange={(providerModel) => setForm({ ...form, providerModel })}
              onRefreshModels={onRefreshModels}
            />
            <div className="grid grid-cols-3 gap-3">
              <NumberField
                id="llm-ability-priority"
                label={t('llm_config.channels.fields.priority')}
                value={form.priority}
                pending={pending}
                onChange={(priority) => setForm({ ...form, priority })}
              />
              <NumberField
                id="llm-ability-weight"
                label={t('llm_config.channels.fields.weight')}
                min={0}
                value={form.weight}
                pending={pending}
                onChange={(weight) => setForm({ ...form, weight })}
              />
              <Field label={t('llm_config.routes.table.status')} htmlFor="llm-ability-enabled">
                <Select
                  value={form.enabled}
                  onValueChange={(enabled) => setForm({ ...form, enabled })}
                  disabled={pending}
                >
                  <SelectTrigger id="llm-ability-enabled" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="true">enabled</SelectItem>
                    <SelectItem value="false">disabled</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
            </div>
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              disabled={pending}
              onClick={() => onOpenChange(false)}
            >
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={submitDisabled}>
              {pending && <Loader2 className="mr-2 size-3.5 animate-spin" />}
              {t('common.save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export function RouteDialog({
  open,
  target,
  pending,
  onOpenChange,
  onSubmit,
}: {
  open: boolean
  target: LLMRoute | null
  pending: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (body: UpsertLLMRouteRequest) => Promise<unknown>
}) {
  const { t } = useTranslation()
  const [form, setForm] = useState(routeForm(target))
  useEffect(() => setForm(open ? routeForm(target) : routeForm(null)), [open, target])
  const submitDisabled = pending || !form.purpose.trim() || !form.logicalModel.trim()
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form
          onSubmit={(event) => {
            event.preventDefault()
            if (submitDisabled) return
            void onSubmit({
              tenantId: form.tenantId.trim(),
              purpose: form.purpose.trim(),
              logicalModel: form.logicalModel.trim(),
              enabled: form.enabled === 'true',
            })
          }}
        >
          <DialogHeader>
            <DialogTitle>{t('llm_config.routes.dialog_title')}</DialogTitle>
            <DialogDescription>{t('llm_config.routes.dialog_description')}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <Field label={t('llm_config.routes.fields.tenant')} htmlFor="llm-route-tenant">
              <Input
                id="llm-route-tenant"
                value={form.tenantId}
                onChange={(event) => setForm({ ...form, tenantId: event.target.value })}
                disabled={pending || target !== null}
              />
            </Field>
            <Field label={t('llm_config.routes.table.purpose')} htmlFor="llm-route-purpose">
              <Input
                id="llm-route-purpose"
                value={form.purpose}
                onChange={(event) => setForm({ ...form, purpose: event.target.value })}
                disabled={pending || target !== null}
                required
              />
            </Field>
            <Field label={t('llm_config.routes.table.logical')} htmlFor="llm-route-logical">
              <Input
                id="llm-route-logical"
                value={form.logicalModel}
                onChange={(event) => setForm({ ...form, logicalModel: event.target.value })}
                disabled={pending}
                required
              />
            </Field>
            <Field label={t('llm_config.routes.table.status')} htmlFor="llm-route-enabled">
              <Select
                value={form.enabled}
                onValueChange={(enabled) => setForm({ ...form, enabled })}
                disabled={pending}
              >
                <SelectTrigger id="llm-route-enabled" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="true">enabled</SelectItem>
                  <SelectItem value="false">disabled</SelectItem>
                </SelectContent>
              </Select>
            </Field>
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              disabled={pending}
              onClick={() => onOpenChange(false)}
            >
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={submitDisabled}>
              {pending && <Loader2 className="mr-2 size-3.5 animate-spin" />}
              {t('common.save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export function TestChannelDialog({
  target,
  models,
  modelsPending,
  modelsError,
  pending,
  onRefreshModels,
  onClose,
  onSubmit,
}: {
  target: LLMChannel | null
  models: LLMProviderModel[]
  modelsPending: boolean
  modelsError: string
  pending: boolean
  onRefreshModels: () => void
  onClose: () => void
  onSubmit: (body: Omit<TestLLMChannelRequest, 'id'>) => Promise<unknown>
}) {
  const { t } = useTranslation()
  const [providerModel, setProviderModel] = useState('')
  const [prompt, setPrompt] = useState('')
  useEffect(() => {
    if (!target) return
    setProviderModel('')
    setPrompt('')
  }, [target])
  const submitDisabled = pending || (modelsPending && providerModel.trim().length === 0)
  return (
    <Dialog open={target !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <form
          onSubmit={(event) => {
            event.preventDefault()
            void onSubmit({ providerModel: providerModel.trim(), prompt: prompt.trim() })
          }}
        >
          <DialogHeader>
            <DialogTitle>{t('llm_config.test.title')}</DialogTitle>
            {target && <DialogDescription>{target.name}</DialogDescription>}
          </DialogHeader>
          <div className="space-y-4 py-4">
            <ProviderModelField
              id="llm-test-model"
              label={t('llm_config.test.provider_model')}
              value={providerModel}
              models={models}
              modelsPending={modelsPending}
              modelsError={modelsError}
              disabled={pending}
              required={false}
              onChange={setProviderModel}
              onRefreshModels={onRefreshModels}
            />
            <Field label={t('llm_config.test.prompt')} htmlFor="llm-test-prompt">
              <Input
                id="llm-test-prompt"
                value={prompt}
                onChange={(event) => setPrompt(event.target.value)}
                disabled={pending}
              />
            </Field>
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" disabled={pending} onClick={onClose}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={submitDisabled}>
              {pending && <Loader2 className="mr-2 size-3.5 animate-spin" />}
              {t('llm_config.actions.test')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export function ConfirmDialog({
  open,
  title,
  body,
  pending,
  onCancel,
  onConfirm,
}: {
  open: boolean
  title: string
  body: string
  pending: boolean
  onCancel: () => void
  onConfirm: () => void
}) {
  const { t } = useTranslation()
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onCancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{body}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="ghost" disabled={pending} onClick={onCancel}>
            {t('common.cancel')}
          </Button>
          <Button variant="destructive" disabled={pending} onClick={onConfirm}>
            {pending && <Loader2 className="mr-2 size-3.5 animate-spin" />}
            {t('common.delete')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
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

function ProviderModelField({
  id,
  label,
  value,
  models,
  modelsPending,
  modelsError,
  disabled,
  required = true,
  onChange,
  onRefreshModels,
}: {
  id: string
  label: string
  value: string
  models: LLMProviderModel[]
  modelsPending: boolean
  modelsError: string
  disabled: boolean
  required?: boolean
  onChange: (value: string) => void
  onRefreshModels: () => void
}) {
  const { t } = useTranslation()
  const selectedModel = models.find((model) => model.id === value)
  const refreshButton = (
    <Button
      type="button"
      variant="outline"
      size="sm"
      disabled={disabled || modelsPending}
      title={t('llm_config.models.refresh')}
      aria-label={t('llm_config.models.refresh')}
      onClick={onRefreshModels}
    >
      {modelsPending ? (
        <Loader2 className="size-3.5 animate-spin" />
      ) : (
        <RefreshCw className="size-3.5" />
      )}
    </Button>
  )
  return (
    <Field label={label} htmlFor={id}>
      {models.length > 0 && (
        <div className="mb-2 flex items-center gap-2">
          <select
            aria-label={t('llm_config.models.choose')}
            value={selectedModel ? value : ''}
            onChange={(event) => onChange(event.target.value)}
            disabled={disabled || modelsPending}
            className="h-9 min-w-0 flex-1 rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-xs outline-none transition-[color,box-shadow] focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50"
          >
            <option value="" disabled>
              {t('llm_config.models.choose')}
            </option>
            {models.map((model) => (
              <option key={model.id} value={model.id}>
                {modelOptionLabel(model)}
              </option>
            ))}
          </select>
          {refreshButton}
        </div>
      )}
      <div className="flex items-center gap-2">
        <Input
          id={id}
          value={value}
          onChange={(event) => onChange(event.target.value)}
          disabled={disabled}
          required={required}
          placeholder={
            modelsPending
              ? t('llm_config.models.loading')
              : models.length > 0
                ? t('llm_config.models.custom')
                : undefined
          }
        />
        {models.length === 0 && refreshButton}
      </div>
      {modelsError && <p className="text-xs text-destructive">{modelsError}</p>}
    </Field>
  )
}

function modelOptionLabel(model: LLMProviderModel) {
  const suffix = model.displayName || model.ownedBy
  return suffix ? `${model.id} (${suffix})` : model.id
}

function NumberField({
  id,
  label,
  value,
  pending,
  min,
  onChange,
}: {
  id: string
  label: string
  value: string
  pending: boolean
  min?: number
  onChange: (value: string) => void
}) {
  return (
    <Field label={label} htmlFor={id}>
      <Input
        id={id}
        type="number"
        min={min}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        disabled={pending}
      />
    </Field>
  )
}

function channelForm(target: LLMChannel | null) {
  return {
    name: target?.name ?? '',
    protocol: target?.protocol ?? 'openai-compat',
    baseUrl: target?.baseUrl ?? '',
    authMode: target?.authMode ?? 'bearer',
    apiKey: '',
    status: target?.status ?? 'enabled',
    priority: String(target?.priority ?? 0),
    weight: String(target?.weight ?? 1),
    timeoutSeconds: String(target?.timeoutSeconds ?? 60),
  }
}

function abilityForm(target: LLMChannelAbility | null) {
  return {
    logicalModel: target?.logicalModel ?? '',
    providerModel: target?.providerModel ?? '',
    enabled: target?.enabled === false ? 'false' : 'true',
    priority: String(target?.priority ?? 0),
    weight: String(target?.weight ?? 1),
  }
}

function routeForm(target: LLMRoute | null) {
  return {
    tenantId: target?.tenantId ?? '',
    purpose: target?.purpose ?? 'enrich',
    logicalModel: target?.logicalModel ?? '',
    enabled: target?.enabled === false ? 'false' : 'true',
  }
}

function toInt(value: string, fallback: number) {
  const parsed = Number.parseInt(value, 10)
  return Number.isFinite(parsed) ? parsed : fallback
}
