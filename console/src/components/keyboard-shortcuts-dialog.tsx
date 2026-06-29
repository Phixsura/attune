import { useTranslation } from 'react-i18next'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { HotkeyBinding } from '@/hooks/use-hotkeys'

function KeyBadge({ children }: { children: string }) {
  return (
    <kbd className="inline-flex min-w-[1.5rem] items-center justify-center rounded border border-border bg-muted px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground">
      {children}
    </kbd>
  )
}

function ShortcutRow({ binding }: { binding: HotkeyBinding }) {
  return (
    <div className="flex items-center justify-between py-1.5">
      <span className="text-sm">{binding.description}</span>
      <div className="flex items-center gap-1">
        {binding.mod && <KeyBadge>⌘</KeyBadge>}
        {binding.shift && <KeyBadge>⇧</KeyBadge>}
        <KeyBadge>{binding.key === ' ' ? '␣' : binding.key}</KeyBadge>
      </div>
    </div>
  )
}

export function KeyboardShortcutsDialog({
  open,
  onOpenChange,
  bindings,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  bindings: HotkeyBinding[]
}) {
  const { t } = useTranslation()

  const groups = new Map<string, HotkeyBinding[]>()
  for (const b of bindings) {
    const group = b.group ?? t('shortcuts.general')
    const list = groups.get(group) ?? []
    list.push(b)
    groups.set(group, list)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{t('shortcuts.title')}</DialogTitle>
        </DialogHeader>
        <div className="space-y-4">
          {[...groups.entries()].map(([group, items]) => (
            <div key={group}>
              <div className="mb-1 text-xs font-semibold tracking-wider text-muted-foreground uppercase">
                {group}
              </div>
              <div className="divide-y divide-border/50">
                {items.map((b) => (
                  <ShortcutRow key={`${b.key}-${b.mod}-${b.shift}`} binding={b} />
                ))}
              </div>
            </div>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  )
}
