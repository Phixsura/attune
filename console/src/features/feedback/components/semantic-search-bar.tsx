import { Search, Sparkles, X } from 'lucide-react'
import { useCallback, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'

interface SemanticSearchBarProps {
  onSearch: (query: string) => void
  onClear?: () => void
  isLoading?: boolean
  placeholder?: string
  defaultValue?: string
  className?: string
}

export function SemanticSearchBar({
  onSearch,
  onClear,
  isLoading,
  placeholder,
  defaultValue = '',
  className,
}: SemanticSearchBarProps) {
  const { t } = useTranslation()
  const [query, setQuery] = useState(defaultValue)

  const handleSubmit = useCallback(
    (e: React.FormEvent) => {
      e.preventDefault()
      if (query.trim()) {
        onSearch(query.trim())
      }
    },
    [query, onSearch],
  )

  const handleClear = useCallback(() => {
    setQuery('')
    onClear?.()
  }, [onClear])

  return (
    <form onSubmit={handleSubmit} className={cn('flex gap-2', className)}>
      <div className="relative flex-1">
        <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={placeholder ?? t('feedback.search.placeholder')}
          aria-label={placeholder ?? t('feedback.search.input_label')}
          className="pl-10 pr-10"
          disabled={isLoading}
        />
        {query && (
          <button
            type="button"
            onClick={handleClear}
            aria-label={t('feedback.search.clear')}
            className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
          >
            <X aria-hidden="true" className="h-4 w-4" />
          </button>
        )}
      </div>
      <Button type="submit" disabled={isLoading || !query.trim()}>
        <Sparkles className="mr-2 h-4 w-4" />
        {t('feedback.search.semantic')}
      </Button>
    </form>
  )
}
