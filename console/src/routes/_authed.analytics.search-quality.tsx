import { createFileRoute } from '@tanstack/react-router'
import {
  defaultSearchQualityFilters,
  searchQualityQuery,
} from '@/features/search-quality/api/get-search-quality'
import { SearchQualityPage } from '@/features/search-quality/components/search-quality-page'

export const Route = createFileRoute('/_authed/analytics/search-quality')({
  component: SearchQualityPage,
  loader: ({ context }) =>
    context.queryClient.ensureQueryData(searchQualityQuery(defaultSearchQualityFilters)),
})
