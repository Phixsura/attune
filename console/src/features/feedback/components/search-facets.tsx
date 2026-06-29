import { SlidersHorizontal, X } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

export interface SearchFacet {
  key: string
  label: string
  value: string
}

export function SearchFacetBar({
  facets,
  onAddFacet,
  onRemoveFacet,
  availableDims,
}: {
  facets: SearchFacet[]
  onAddFacet: (key: string, value: string) => void
  onRemoveFacet: (key: string) => void
  availableDims: Array<{ name: string; displayName: string }>
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [selectedDim, setSelectedDim] = useState('')
  const [facetValue, setFacetValue] = useState('')

  const usedKeys = new Set(facets.map((f) => f.key))
  const availableForAdd = availableDims.filter((d) => !usedKeys.has(d.name))

  const handleAdd = () => {
    if (!selectedDim || !facetValue.trim()) return
    onAddFacet(selectedDim, facetValue.trim())
    setSelectedDim('')
    setFacetValue('')
    setOpen(false)
  }

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {facets.map((f) => (
        <span
          key={f.key}
          className="inline-flex items-center gap-1 rounded-full border border-primary/30 bg-primary/5 px-2.5 py-0.5 text-xs font-medium"
        >
          <span className="text-muted-foreground">{f.label}:</span>
          <span>{f.value}</span>
          <button
            type="button"
            onClick={() => onRemoveFacet(f.key)}
            className="ml-0.5 rounded-full p-0.5 hover:bg-primary/10"
          >
            <X className="size-2.5" />
          </button>
        </span>
      ))}
      {availableForAdd.length > 0 && (
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger asChild>
            <Button variant="ghost" size="sm" className="h-6 gap-1 px-2 text-xs">
              <SlidersHorizontal className="size-3" />
              {t('feedback.search.add_facet')}
            </Button>
          </PopoverTrigger>
          <PopoverContent className="w-64 space-y-2" align="start">
            <Select value={selectedDim} onValueChange={setSelectedDim}>
              <SelectTrigger className="h-8 text-sm">
                <SelectValue placeholder={t('feedback.search.select_dimension')} />
              </SelectTrigger>
              <SelectContent>
                {availableForAdd.map((d) => (
                  <SelectItem key={d.name} value={d.name}>
                    {d.displayName}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Input
              placeholder={t('feedback.search.facet_value')}
              value={facetValue}
              onChange={(e) => setFacetValue(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && handleAdd()}
              className="h-8 text-sm"
            />
            <Button
              size="sm"
              onClick={handleAdd}
              disabled={!selectedDim || !facetValue.trim()}
              className="w-full"
            >
              {t('feedback.search.apply_facet')}
            </Button>
          </PopoverContent>
        </Popover>
      )}
    </div>
  )
}
