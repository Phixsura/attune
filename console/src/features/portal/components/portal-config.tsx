import { ExternalLink, Globe } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

export interface PortalSettings {
  enabled: boolean
  slug: string
  brandColor: string
  welcomeMessage: string
}

export function PortalConfig({
  settings,
  onSave,
}: {
  settings: PortalSettings
  onSave?: (settings: PortalSettings) => void
}) {
  const { t } = useTranslation()
  const [form, setForm] = useState(settings)

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    onSave?.(form)
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <Globe className="size-4 text-muted-foreground" />
        <h3 className="text-sm font-semibold">{t('portal.title')}</h3>
      </div>

      <form onSubmit={handleSubmit} className="space-y-3">
        <div className="flex items-center gap-3">
          <input
            type="checkbox"
            id="portal-enabled"
            checked={form.enabled}
            onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
          />
          <label htmlFor="portal-enabled" className="text-sm">
            {t('portal.enable')}
          </label>
        </div>

        <div className="space-y-1">
          <div className="text-xs text-muted-foreground">{t('portal.slug')}</div>
          <input
            type="text"
            className="w-full rounded border px-2 py-1 text-sm"
            value={form.slug}
            onChange={(e) => setForm({ ...form, slug: e.target.value })}
          />
        </div>

        <div className="space-y-1">
          <div className="text-xs text-muted-foreground">{t('portal.brand_color')}</div>
          <input
            type="color"
            value={form.brandColor}
            onChange={(e) => setForm({ ...form, brandColor: e.target.value })}
          />
        </div>

        <div className="space-y-1">
          <div className="text-xs text-muted-foreground">{t('portal.welcome_message')}</div>
          <textarea
            className="w-full rounded border px-2 py-1 text-sm"
            rows={3}
            value={form.welcomeMessage}
            onChange={(e) => setForm({ ...form, welcomeMessage: e.target.value })}
          />
        </div>

        <button
          type="submit"
          className="rounded bg-primary px-3 py-1.5 text-sm text-primary-foreground"
        >
          {t('portal.save')}
        </button>
      </form>

      {form.enabled && (
        <div className="flex items-center gap-1 text-xs text-muted-foreground">
          <ExternalLink className="size-3" />
          <span>/portal/{form.slug}</span>
        </div>
      )}
    </div>
  )
}
