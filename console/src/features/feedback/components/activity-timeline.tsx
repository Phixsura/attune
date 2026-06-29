import { Clock } from 'lucide-react'
import { useTranslation } from 'react-i18next'

export interface ActivityEvent {
  id: string
  action: string
  actor: string
  timestamp: string
  detail?: string
}

export function ActivityTimeline({ events }: { events: ActivityEvent[] }) {
  const { t } = useTranslation()

  if (events.length === 0) {
    return <p className="text-center text-muted-foreground py-4">{t('activity.empty')}</p>
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <Clock className="size-4 text-muted-foreground" />
        <h3 className="text-sm font-semibold">{t('activity.title')}</h3>
      </div>
      <ol className="relative border-l border-muted ml-2 space-y-4">
        {events.map((event) => (
          <li key={event.id} className="ml-4">
            <div className="absolute -left-1.5 mt-1.5 size-3 rounded-full border bg-background" />
            <time className="text-xs text-muted-foreground">{event.timestamp}</time>
            <p className="text-sm">
              <span className="font-medium">{event.actor}</span>{' '}
              <span className="text-muted-foreground">{event.action}</span>
            </p>
            {event.detail && <p className="text-xs text-muted-foreground mt-0.5">{event.detail}</p>}
          </li>
        ))}
      </ol>
    </div>
  )
}
