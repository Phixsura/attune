import type { TFunction } from 'i18next'

export function getActionLabel(action: string, t: TFunction): string {
  const key = `audit_log.action_labels.${action}`
  const label = t(key, { defaultValue: '' })
  return label || action
}

export function getTargetTypeLabel(targetType: string, t: TFunction): string {
  const key = `audit_log.target_type_labels.${targetType}`
  const label = t(key, { defaultValue: '' })
  return label || targetType
}

export function getActorTypeLabel(actorType: string, t: TFunction): string {
  const key = `audit_log.actor_type_labels.${actorType}`
  const label = t(key, { defaultValue: '' })
  return label || actorType
}

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

export function formatActorDisplay(actorId: string, actorEmail?: string): string {
  if (actorEmail) return actorEmail
  if (UUID_RE.test(actorId)) return `${actorId.slice(0, 8)}…`
  return actorId
}

export function formatIdDisplay(id: string): string {
  if (UUID_RE.test(id)) return `${id.slice(0, 8)}…`
  return id
}

export function getFieldLabel(path: string, t: TFunction): string {
  const key = `audit_log.field_labels.${path}`
  const label = t(key, { defaultValue: '' })
  return label || path
}

export function buildEventDescription(
  item: {
    actorId: string
    actorEmail?: string
    targetId: string
    targetType: string
    action: string
  },
  t: TFunction,
): string {
  const actor = item.actorEmail || formatIdDisplay(item.actorId)
  const verb = getActionVerb(item.action, t)
  const targetType = item.targetType ? getTargetTypeLabel(item.targetType, t) : ''
  const targetId = item.targetId ? formatIdDisplay(item.targetId) : ''
  if (targetType && targetId) {
    return t('audit_log.event_desc_full', { actor, verb, targetType, targetId })
  }
  if (targetType) {
    return t('audit_log.event_desc_no_id', { actor, verb, targetType })
  }
  return actor
}

function getActionVerb(action: string, t: TFunction): string {
  /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
  const verb = action.split('.').pop() ?? ''
  const key = `audit_log.action_verbs.${verb}`
  const label = t(key, { defaultValue: '' })
  /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
  return label || verb
}

export type ActionCategory = 'create' | 'update' | 'delete' | 'test' | 'neutral'

export function getActionCategory(action: string): ActionCategory {
  /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
  const verb = action.split('.').pop() ?? ''
  if (['create', 'invite', 'upsert', 'resume'].includes(verb)) return 'create'
  if (
    [
      'delete',
      'remove',
      'revoke',
      'expire',
      'batch_delete',
      'cancel',
      'archive',
      'reject',
      'hide',
      'mark_spam',
    ].includes(verb)
  )
    return 'delete'
  if (['test', 'test_connection', 'download'].includes(verb)) return 'test'
  if (
    [
      'update',
      'update_role',
      'update_scoring_settings',
      'activate_version',
      'rotate_secret',
      'pause',
      'replace',
      'run',
      'approve',
      'restore',
    ].includes(verb)
  )
    return 'update'
  return 'neutral'
}

const ACTION_DOT_COLORS: Record<ActionCategory, string> = {
  create: 'bg-emerald-500',
  update: 'bg-blue-500',
  delete: 'bg-red-500',
  test: 'bg-amber-500',
  neutral: 'bg-muted-foreground/50',
}

export function getActionDotColor(action: string): string {
  return ACTION_DOT_COLORS[getActionCategory(action)]
}

const AVATAR_COLORS = [
  { bg: 'bg-blue-100', text: 'text-blue-700' },
  { bg: 'bg-purple-100', text: 'text-purple-700' },
  { bg: 'bg-teal-100', text: 'text-teal-700' },
  { bg: 'bg-rose-100', text: 'text-rose-700' },
  { bg: 'bg-amber-100', text: 'text-amber-700' },
  { bg: 'bg-emerald-100', text: 'text-emerald-700' },
]

export function getActorAvatar(
  actorId: string,
  actorEmail?: string,
): {
  initials: string
  bg: string
  text: string
} {
  const display = actorEmail || actorId
  const initials = actorEmail
    ? actorEmail.slice(0, 2).toUpperCase()
    : display.slice(0, 2).toUpperCase()
  let hash = 0
  for (let i = 0; i < display.length; i++) {
    hash = ((hash << 5) - hash + display.charCodeAt(i)) | 0
  }
  const idx = Math.abs(hash) % AVATAR_COLORS.length
  return { initials, ...AVATAR_COLORS[idx] }
}

export const auditActionOptions = [
  'api_key.create',
  'api_key.revoke',
  'digest_subscription.delete',
  'digest_subscription.upsert',
  'enrich_config.activate_version',
  'enrich_config.update',
  'feedback.batch_delete',
  'feedback_job.cancel',
  'guard_policy.create',
  'guard_policy.delete',
  'guard_policy.update',
  'inbound_source.create',
  'inbound_source.delete',
  'inbound_source.pause',
  'inbound_source.resume',
  'inbound_source.rotate_secret',
  'inbound_source.test_connection',
  'llm_ability.delete',
  'llm_ability.upsert',
  'llm_channel.create',
  'llm_channel.delete',
  'llm_channel.test',
  'llm_channel.update',
  'llm_route.delete',
  'llm_route.upsert',
  'member.invite',
  'member.remove',
  'member.update_role',
  'moderation.approve',
  'moderation.hide',
  'moderation.mark_spam',
  'moderation.reject',
  'moderation.restore',
  'notify_target.create',
  'notify_target.delete',
  'notify_target.test',
  'notify_target.update',
  'public_policy.update',
  'public_request_profile.upsert',
  'tag.archive',
  'tag.create',
  'tag.update',
  'workflow_seed_defaults.run',
  'workflow_state.archive',
  'workflow_state.create',
  'workflow_state.update',
  'audit_evidence.create',
  'audit_evidence.download',
  'audit_evidence.expire',
  'customer_request.update_scoring_settings',
  'workflow_transition.replace',
] as const
