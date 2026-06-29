import type { Feedback } from '@/features/feedback/api/list-feedback-infinite'

function escapeCSV(value: string): string {
  if (value.includes(',') || value.includes('"') || value.includes('\n')) {
    return `"${value.replace(/"/g, '""')}"`
  }
  return value
}

function formatDate(iso: string): string {
  try {
    return new Date(iso).toISOString()
  } catch {
    return iso
  }
}

export function exportFeedbackCSV(items: Feedback[], filename = 'feedback-export.csv'): void {
  const headers = [
    'id',
    'content',
    'source',
    'source_user',
    'kind',
    'severity',
    'sentiment',
    'language',
    'urgent',
    'modules',
    'tags',
    'workflow_state',
    'created_at',
  ]

  const rows = items.map((item) => {
    const attrs = (item.enrichedAttrs ?? {}) as Record<string, unknown>
    return [
      String(item.id),
      escapeCSV(item.content ?? ''),
      escapeCSV(item.source ?? ''),
      escapeCSV(item.userId ?? ''),
      escapeCSV(String(attrs.kind ?? '')),
      escapeCSV(String(attrs.severity ?? '')),
      escapeCSV(String(attrs.sentiment ?? '')),
      escapeCSV(item.language ?? ''),
      item.isUrgent ? 'true' : 'false',
      escapeCSV(Array.isArray(attrs.modules) ? attrs.modules.join('; ') : ''),
      escapeCSV((item.tags ?? []).map((t) => t.name ?? t.id).join('; ')),
      escapeCSV(item.workflowState?.name ?? ''),
      formatDate(item.createdAt ?? ''),
    ]
  })

  const csv = [headers.join(','), ...rows.map((r) => r.join(','))].join('\n')
  const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}
