import { ChevronDown, ChevronRight, Plus, Trash2 } from 'lucide-react'
import { useId, useRef, useState } from 'react'
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
import { useDisplayName } from '@/lib/i18n-resolve'
import { cn } from '@/lib/utils'
import type { Dimension, Taxonomy } from '@/proto/attune/v1/common'

// dimRefKey is a wire-safe identity for a Dimension *during editing*. It
// isn't the Dimension.Name (which would be empty on a brand-new card) and
// it isn't the array index (unstable across remove operations). Instead,
// the editor maintains a WeakMap of Dimension objects → opaque ids; new
// cards get a fresh id, persisted cards reuse one tied to the loaded
// reference. This is what differentiates a persisted dim (Name + Kind
// locked) from a brand-new one (Name + Kind editable) — independent of
// the operator's chosen Name.
type DimRefKey = string

// DimensionsEditor is the Settings page's editor surface. It is
// metadata-driven: any number of Dimensions, each with i18n
// DisplayName and per-Taxonomy i18n. Stable identifiers (Dimension.Name,
// Taxonomy.Value) become read-only once the row exists — renaming is
// done via delete + recreate, preserving wire/SQL stability.
//
// State management lives at the route level (the parent owns the
// edited list and the dirty bit). This component just reports edits
// up via onChange.
export function DimensionsEditor({
  value,
  onChange,
}: {
  value: Dimension[]
  onChange: (next: Dimension[]) => void
}) {
  const { t } = useTranslation()
  const baseId = useId()
  // WeakMap: Dimension reference → stable opaque id. Each Dimension
  // object created locally (addDim) goes into newDimIds; dims loaded
  // from the server are tracked too but classified as "not new".
  const refs = useRef<WeakMap<Dimension, DimRefKey>>(new WeakMap())
  const newDimIds = useRef<Set<DimRefKey>>(new Set())
  const seqRef = useRef(0)

  const idOf = (dim: Dimension): DimRefKey => {
    const existing = refs.current.get(dim)
    if (existing) return existing
    seqRef.current += 1
    const id = `${baseId}-${seqRef.current}`
    refs.current.set(dim, id)
    return id
  }

  const updateDim = (i: number, patch: Partial<Dimension>) => {
    const prev = value[i]
    const next = [...value]
    const merged = { ...prev, ...patch }
    next[i] = merged
    // Identity transfers: the merged object is "the same dim" as prev
    // for the purposes of new-vs-persisted classification.
    const id = refs.current.get(prev)
    if (id) {
      refs.current.set(merged, id)
      if (newDimIds.current.has(id)) {
        // remains new
      }
    }
    onChange(next)
  }
  const addDim = () => {
    const fresh = emptyDimension()
    const id = idOf(fresh)
    newDimIds.current.add(id)
    onChange([...value, fresh])
  }
  const removeDim = (i: number) => {
    const dropped = value[i]
    const id = refs.current.get(dropped)
    if (id) newDimIds.current.delete(id)
    onChange(value.filter((_, idx) => idx !== i))
  }

  return (
    <div className="space-y-4">
      {value.map((dim, i) => {
        const id = idOf(dim)
        const isNew = newDimIds.current.has(id)
        return (
          <DimensionCard
            // Stable per-card id — survives Name edits and array reordering.
            key={id}
            dim={dim}
            isNew={isNew}
            onChange={(patch) => updateDim(i, patch)}
            onRemove={() => removeDim(i)}
          />
        )
      })}
      <Button type="button" variant="outline" onClick={addDim} data-testid="dim-editor-add-dim">
        <Plus className="h-4 w-4 mr-1" />
        {t('dim.editor.add_dim')}
      </Button>
    </div>
  )
}

function DimensionCard({
  dim,
  isNew,
  onChange,
  onRemove,
}: {
  dim: Dimension
  isNew: boolean
  onChange: (patch: Partial<Dimension>) => void
  onRemove: () => void
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const displayOf = useDisplayName()
  // Same identity trick as the parent: track newly-added Taxonomy
  // entries by object reference so Value editability survives renames
  // and array reorders, without needing a "value_*" placeholder prefix.
  const baseId = useId()
  const taxRefs = useRef<WeakMap<Taxonomy, string>>(new WeakMap())
  const newTaxIds = useRef<Set<string>>(new Set())
  const taxSeq = useRef(0)
  const taxIdOf = (tax: Taxonomy): string => {
    const existing = taxRefs.current.get(tax)
    if (existing) return existing
    taxSeq.current += 1
    const id = `${baseId}-tax-${taxSeq.current}`
    taxRefs.current.set(tax, id)
    return id
  }

  const setTaxonomy = (taxonomy: Taxonomy[]) => onChange({ taxonomy })
  const setUrgentSet = (urgentSet: string[]) => onChange({ urgentSet })

  const addTaxonomy = () => {
    const fresh: Taxonomy = { value: '', displayName: { entries: { default: '' } } }
    const id = taxIdOf(fresh)
    newTaxIds.current.add(id)
    setTaxonomy([...dim.taxonomy, fresh])
  }

  return (
    <Card>
      <CardHeader
        className="cursor-pointer select-none py-3"
        onClick={() => setOpen((v) => !v)}
        data-testid="dim-card-header"
      >
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2 min-w-0">
            <Button type="button" variant="ghost" size="sm" className="h-7 w-7 p-0">
              {open ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
            </Button>
            <span className="font-medium truncate" data-testid="dim-card-title">
              {displayOf(dim.displayName) || dim.name || t('dim.editor.unnamed')}
            </span>
            <span className="font-mono text-xs text-muted-foreground truncate">
              ({dim.name || '?'}, {dim.kind})
            </span>
          </div>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-7 w-7 p-0 text-destructive hover:bg-destructive/10"
            onClick={(e) => {
              e.stopPropagation()
              onRemove()
            }}
            aria-label={t('dim.editor.delete_dim')}
            data-testid="dim-editor-delete-dim"
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>
      </CardHeader>
      {open && (
        <CardContent className="space-y-4 pt-0">
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-2">
              <Label htmlFor={`dim-name-${dim.name}`}>{t('dim.editor.name_label')}</Label>
              <Input
                id={`dim-name-${dim.name}`}
                value={dim.name}
                readOnly={!isNew}
                onChange={(e) => onChange({ name: e.target.value })}
                placeholder="lowercase_only"
                className={cn('font-mono text-sm', !isNew && 'opacity-70')}
              />
              <p className="text-xs text-muted-foreground">{t('dim.editor.name_help')}</p>
            </div>
            <div className="space-y-2">
              <Label>{t('dim.editor.kind_label')}</Label>
              <Select
                value={dim.kind}
                onValueChange={(v) => onChange({ kind: v })}
                disabled={!isNew}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="single">{t('dim.editor.kind_single')}</SelectItem>
                  <SelectItem value="multi">{t('dim.editor.kind_multi')}</SelectItem>
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">{t('dim.editor.kind_help')}</p>
            </div>
          </div>

          <div className="space-y-2">
            <Label>{t('dim.editor.display_name_label')}</Label>
            <I18nInput
              value={(dim.displayName?.entries ?? {}) as Record<string, string>}
              onChange={(entries) => onChange({ displayName: { entries } })}
              placeholder={dim.name}
            />
            <p className="text-xs text-muted-foreground">{t('dim.editor.display_name_help')}</p>
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label>{t('dim.editor.taxonomy_label')}</Label>
              <Button type="button" variant="ghost" size="sm" onClick={addTaxonomy}>
                <Plus className="h-3 w-3 mr-1" />
                {t('dim.editor.add_value')}
              </Button>
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
              {dim.taxonomy.map((tax, taxIdx) => {
                const id = taxIdOf(tax)
                const isNewTax = newTaxIds.current.has(id)
                return (
                  <TaxonomyRow
                    key={id}
                    tax={tax}
                    isNew={isNewTax}
                    onChange={(patch) => {
                      const prev = dim.taxonomy[taxIdx]
                      const merged = { ...prev, ...patch }
                      // Carry the identity (and "new" flag) over to the
                      // merged object so Value remains editable across edits.
                      const prevId = taxRefs.current.get(prev)
                      if (prevId) taxRefs.current.set(merged, prevId)
                      const next = [...dim.taxonomy]
                      next[taxIdx] = merged
                      setTaxonomy(next)
                    }}
                    onRemove={() => {
                      const dropped = dim.taxonomy[taxIdx]
                      const droppedId = taxRefs.current.get(dropped)
                      if (droppedId) newTaxIds.current.delete(droppedId)
                      setTaxonomy(dim.taxonomy.filter((_, i) => i !== taxIdx))
                      setUrgentSet(dim.urgentSet.filter((v) => v !== tax.value))
                    }}
                  />
                )
              })}
              {dim.taxonomy.length === 0 && (
                <p className="text-xs italic text-muted-foreground">
                  {dim.kind === 'multi'
                    ? t('dim.editor.taxonomy_empty_multi')
                    : t('dim.editor.taxonomy_empty_single')}
                </p>
              )}
            </div>
          </div>

          {dim.taxonomy.length > 0 && (
            <div className="space-y-2">
              <Label>{t('dim.editor.urgent_set_label')}</Label>
              <p className="text-xs text-muted-foreground">{t('dim.editor.urgent_set_help')}</p>
              <div className="flex flex-wrap gap-2">
                {dim.taxonomy.map((tax) => {
                  const checked = dim.urgentSet.includes(tax.value)
                  return (
                    <button
                      key={tax.value}
                      type="button"
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
                      )}
                    >
                      {displayOf(tax.displayName) || tax.value}
                    </button>
                  )
                })}
              </div>
            </div>
          )}
        </CardContent>
      )}
    </Card>
  )
}

function TaxonomyRow({
  tax,
  isNew,
  onChange,
  onRemove,
}: {
  tax: Taxonomy
  isNew: boolean
  onChange: (patch: Partial<Taxonomy>) => void
  onRemove: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Input
          value={tax.value}
          readOnly={!isNew}
          onChange={(e) => onChange({ value: e.target.value })}
          placeholder="stable_value"
          className={cn('h-8 max-w-[200px] font-mono text-sm', !isNew && 'opacity-70')}
        />
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="h-7 w-7 p-0 text-destructive hover:bg-destructive/10"
          onClick={onRemove}
          aria-label={t('dim.editor.remove_value')}
          data-testid="dim-editor-remove-value"
        >
          <Trash2 className="h-3 w-3" />
        </Button>
      </div>
      <div className="pl-4">
        <I18nInput
          inline
          value={(tax.displayName?.entries ?? {}) as Record<string, string>}
          onChange={(entries) => onChange({ displayName: { entries } })}
          placeholder={tax.value}
        />
      </div>
    </div>
  )
}

// emptyDimension returns the blank-slate Dimension a fresh editor card
// starts with. Name + DisplayName are empty; the operator types them
// before Save. The identity-tracking WeakMap (newDimIds) is what marks
// the row as "new" — not a `dim_<n>` placeholder name like before.
function emptyDimension(): Dimension {
  return {
    name: '',
    displayName: { entries: { default: '' } },
    kind: 'single',
    taxonomy: [],
    urgentSet: [],
    required: false,
  }
}
