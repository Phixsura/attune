import { ChevronDown, ChevronRight, Plus, Trash2 } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { I18nInput } from '@/components/dim/i18n-input'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
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
  type EditableDimension,
  type EditableTaxonomy,
  newDimension,
  newTaxonomy,
} from '@/lib/editable-rows'
import { useDisplayName } from '@/lib/i18n-resolve'
import { cn } from '@/lib/utils'

// DimensionsEditor is the Settings page's editor surface. It is a pure,
// controlled view over EditableDimension[]: row identity (`_key`) and the
// new-vs-persisted flag (`_isNew`) live in the data the parent owns, so they
// survive remount and are immune to what the operator types (issue #90).
// Stable identifiers (Dimension.Name, Taxonomy.Value) are read-only once the
// row is persisted — renaming is delete + recreate, preserving wire/SQL
// stability. The parent seeds the model once and strips `_key`/`_isNew` before
// sending to the server.
export function DimensionsEditor({
  value,
  onChange,
  disabled = false,
}: {
  value: EditableDimension[]
  onChange: (next: EditableDimension[]) => void
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const addBtnRef = useRef<HTMLButtonElement>(null)

  const updateDim = (i: number, patch: Partial<EditableDimension>) => {
    const next = [...value]
    next[i] = { ...next[i], ...patch }
    onChange(next)
  }
  const addDim = () => onChange([...value, newDimension()])
  const removeDim = (i: number) => {
    onChange(value.filter((_, idx) => idx !== i))
    // Don't drop keyboard focus to <body> (WCAG 2.4.3): the removed card's
    // controls are gone, so move focus to the still-mounted Add button.
    addBtnRef.current?.focus()
  }

  return (
    <div className="space-y-4">
      {value.map((dim, i) => (
        <DimensionCard
          // Stable per-card key from the row's own identity — survives Name
          // edits, array reordering, and remount.
          key={dim._key}
          dim={dim}
          disabled={disabled}
          onChange={(patch) => updateDim(i, patch)}
          onRemove={() => removeDim(i)}
        />
      ))}
      {!disabled && (
        <Button
          ref={addBtnRef}
          type="button"
          variant="outline"
          onClick={addDim}
          data-testid="dim-editor-add-dim"
        >
          <Plus className="h-4 w-4 mr-1" />
          {t('dim.editor.add_dim')}
        </Button>
      )}
    </div>
  )
}

function DimensionCard({
  dim,
  disabled = false,
  onChange,
  onRemove,
}: {
  dim: EditableDimension
  disabled?: boolean
  onChange: (patch: Partial<EditableDimension>) => void
  onRemove: () => void
}) {
  const { t } = useTranslation()
  // A freshly-added card opens so the operator can fill it in immediately.
  const [open, setOpen] = useState(dim._isNew)
  const displayOf = useDisplayName()
  const nameRef = useRef<HTMLInputElement>(null)
  const focusedRef = useRef(false)
  const addValueBtnRef = useRef<HTMLButtonElement>(null)

  // The identifier is locked for persisted rows and whenever editing is
  // disabled. One semantic for both Name and Kind: read-only, focusable,
  // announced — never `disabled` (which drops the control from the tab order).
  const locked = !dim._isNew || disabled
  const key = dim._key
  const contentId = `dim-card-content-${key}`

  // Focus a brand-new card's Name input once, so adding a dimension lands the
  // caret where the operator types next (WCAG 2.4.3) — guarded to fire once.
  useEffect(() => {
    if (dim._isNew && !focusedRef.current && open) {
      focusedRef.current = true
      nameRef.current?.focus()
    }
  }, [dim._isNew, open])

  const setTaxonomy = (taxonomy: EditableTaxonomy[]) => onChange({ taxonomy })
  const setUrgentSet = (urgentSet: string[]) => onChange({ urgentSet })

  const addTaxonomy = () => setTaxonomy([...dim.taxonomy, newTaxonomy()])

  return (
    <Card>
      <CardHeader className="select-none py-3">
        <div className="flex items-center justify-between gap-2">
          {/* The whole title row is the disclosure control: a real <button>
              whose text content (title + machine name) names it per-card, with
              aria-expanded for state and aria-controls only while the panel is
              actually mounted. */}
          <button
            type="button"
            onClick={() => setOpen((v) => !v)}
            aria-expanded={open}
            aria-controls={open ? contentId : undefined}
            data-testid="dim-card-header"
            className="flex flex-1 cursor-pointer items-center gap-2 min-w-0 text-left"
          >
            {open ? (
              <ChevronDown className="h-4 w-4 shrink-0" />
            ) : (
              <ChevronRight className="h-4 w-4 shrink-0" />
            )}
            <span className="font-medium truncate" data-testid="dim-card-title">
              {displayOf(dim.displayName) || dim.name || t('dim.editor.unnamed')}
            </span>
            <span className="font-mono text-xs text-muted-foreground truncate">
              ({dim.name || '?'}, {dim.kind})
            </span>
          </button>
          {!disabled && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-7 w-7 p-0 text-destructive hover:bg-destructive/10"
              onClick={onRemove}
              aria-label={`${t('dim.editor.delete_dim')}: ${dim.name || t('dim.editor.unnamed')}`}
              data-testid="dim-editor-delete-dim"
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          )}
        </div>
      </CardHeader>
      {open && (
        <CardContent id={contentId} className="space-y-4 pt-0">
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-2">
              <Label htmlFor={`dim-name-${key}`}>{t('dim.editor.name_label')}</Label>
              <Input
                ref={nameRef}
                id={`dim-name-${key}`}
                aria-describedby={`dim-name-help-${key}`}
                value={dim.name}
                readOnly={locked}
                onChange={(e) => onChange({ name: e.target.value })}
                placeholder="lowercase_only"
                className={cn('font-mono text-sm', locked && 'opacity-70')}
              />
              <p id={`dim-name-help-${key}`} className="text-xs text-muted-foreground">
                {t('dim.editor.name_help')}
              </p>
            </div>
            <div className="space-y-2">
              <Label htmlFor={`dim-kind-${key}`}>{t('dim.editor.kind_label')}</Label>
              {locked ? (
                // Persisted Kind is immutable: show it as a read-only field
                // (focusable + announced), consistent with the locked Name.
                <Input
                  id={`dim-kind-${key}`}
                  aria-describedby={`dim-kind-help-${key}`}
                  data-testid="dim-kind-readonly"
                  readOnly
                  value={
                    dim.kind === 'multi' ? t('dim.editor.kind_multi') : t('dim.editor.kind_single')
                  }
                  className="text-sm opacity-70"
                />
              ) : (
                <Select value={dim.kind} onValueChange={(v) => onChange({ kind: v })}>
                  <SelectTrigger id={`dim-kind-${key}`} aria-describedby={`dim-kind-help-${key}`}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="single">{t('dim.editor.kind_single')}</SelectItem>
                    <SelectItem value="multi">{t('dim.editor.kind_multi')}</SelectItem>
                  </SelectContent>
                </Select>
              )}
              <p id={`dim-kind-help-${key}`} className="text-xs text-muted-foreground">
                {t('dim.editor.kind_help')}
              </p>
            </div>
          </div>

          <div className="space-y-2">
            <Label>{t('dim.editor.display_name_label')}</Label>
            <I18nInput
              value={(dim.displayName?.entries ?? {}) as Record<string, string>}
              onChange={(entries) => onChange({ displayName: { entries } })}
              placeholder={dim.name}
              disabled={disabled}
            />
            <p className="text-xs text-muted-foreground">{t('dim.editor.display_name_help')}</p>
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label>{t('dim.editor.taxonomy_label')}</Label>
              {!disabled && (
                <Button
                  ref={addValueBtnRef}
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={addTaxonomy}
                  data-testid="dim-editor-add-value"
                >
                  <Plus className="h-3 w-3 mr-1" />
                  {t('dim.editor.add_value')}
                </Button>
              )}
            </div>
            <p
              className="text-xs text-muted-foreground"
              data-testid="dim-card-taxonomy-help"
              data-kind={dim.kind}
            >
              {dim.kind === 'multi'
                ? t('dim.editor.taxonomy_help_multi')
                : t('dim.editor.taxonomy_help_single')}
            </p>
            <div className="space-y-3 pl-3 border-l-2 border-border">
              {dim.taxonomy.map((tax, taxIdx) => (
                <TaxonomyRow
                  key={tax._key}
                  tax={tax}
                  disabled={disabled}
                  onChange={(patch) => {
                    const prev = dim.taxonomy[taxIdx]
                    const next = [...dim.taxonomy]
                    next[taxIdx] = { ...prev, ...patch }
                    // Urgent membership is value-addressed; when a (still-new)
                    // value is renamed in place, remap/drop the matching entry
                    // so urgentSet never holds a dangling value.
                    if (
                      patch.value !== undefined &&
                      patch.value !== prev.value &&
                      dim.urgentSet.includes(prev.value)
                    ) {
                      const renamed = patch.value
                      const urgentSet = [
                        ...new Set(
                          dim.urgentSet
                            .map((v) => (v === prev.value ? renamed : v))
                            .filter((v) => v !== ''),
                        ),
                      ]
                      onChange({ taxonomy: next, urgentSet })
                    } else {
                      setTaxonomy(next)
                    }
                  }}
                  onRemove={() => {
                    // Single atomic patch: two sequential onChange calls would
                    // both read the same captured `value` and the second
                    // (urgentSet) would clobber the first (taxonomy), leaving
                    // the value undeleted.
                    onChange({
                      taxonomy: dim.taxonomy.filter((_, i) => i !== taxIdx),
                      urgentSet: dim.urgentSet.filter((v) => v !== tax.value),
                    })
                    // Don't drop focus to <body> (WCAG 2.4.3).
                    addValueBtnRef.current?.focus()
                  }}
                />
              ))}
              {dim.taxonomy.length === 0 && (
                <p className="text-xs italic text-muted-foreground">
                  {dim.kind === 'multi'
                    ? t('dim.editor.taxonomy_empty_multi')
                    : t('dim.editor.taxonomy_empty_single')}
                </p>
              )}
            </div>
          </div>

          {dim.taxonomy.some((tax) => tax.value !== '') && (
            // <fieldset>/<legend> group the toggle chips under their label for
            // assistive tech (the semantic form of role="group").
            <fieldset className="m-0 space-y-2 border-0 p-0">
              <legend className="text-sm font-medium leading-none">
                {t('dim.editor.urgent_set_label')}
              </legend>
              <p className="text-xs text-muted-foreground">{t('dim.editor.urgent_set_help')}</p>
              <div className="flex flex-wrap gap-2">
                {dim.taxonomy.map((tax) => {
                  // A not-yet-named value can't be urgent — urgentSet is
                  // value-addressed on the wire, so an empty value is ineligible.
                  if (!tax.value) return null
                  const checked = dim.urgentSet.includes(tax.value)
                  return (
                    <button
                      // Key on the row's stable identity, not its (possibly
                      // duplicate) Value.
                      key={tax._key}
                      type="button"
                      aria-pressed={checked}
                      disabled={disabled}
                      onClick={() =>
                        setUrgentSet(
                          checked
                            ? dim.urgentSet.filter((v) => v !== tax.value)
                            : [...dim.urgentSet, tax.value],
                        )
                      }
                      className={cn(
                        'rounded-full border px-3 py-1 text-xs font-medium transition-colors',
                        checked
                          ? 'border-destructive bg-destructive/10 text-destructive'
                          : 'border-border bg-muted hover:border-foreground/30',
                        disabled && 'cursor-not-allowed opacity-50',
                      )}
                    >
                      {displayOf(tax.displayName) || tax.value}
                    </button>
                  )
                })}
              </div>
            </fieldset>
          )}
        </CardContent>
      )}
    </Card>
  )
}

function TaxonomyRow({
  tax,
  disabled = false,
  onChange,
  onRemove,
}: {
  tax: EditableTaxonomy
  disabled?: boolean
  onChange: (patch: Partial<EditableTaxonomy>) => void
  onRemove: () => void
}) {
  const { t } = useTranslation()
  const locked = !tax._isNew || disabled
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Input
          id={`tax-value-${tax._key}`}
          // Stable label — interpolating the live value would re-announce the
          // focused input on every keystroke; the value is conveyed by the
          // input's own value to AT.
          aria-label={t('dim.editor.value_label')}
          value={tax.value}
          readOnly={locked}
          onChange={(e) => onChange({ value: e.target.value })}
          placeholder="stable_value"
          className={cn('h-8 max-w-[200px] font-mono text-sm', locked && 'opacity-70')}
        />
        {!disabled && (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-7 w-7 p-0 text-destructive hover:bg-destructive/10"
            onClick={onRemove}
            aria-label={`${t('dim.editor.remove_value')} ${tax.value}`.trim()}
            data-testid="dim-editor-remove-value"
          >
            <Trash2 className="h-3 w-3" />
          </Button>
        )}
      </div>
      <div className="pl-4">
        <I18nInput
          inline
          value={(tax.displayName?.entries ?? {}) as Record<string, string>}
          onChange={(entries) => onChange({ displayName: { entries } })}
          placeholder={tax.value}
          disabled={disabled}
        />
      </div>
    </div>
  )
}
