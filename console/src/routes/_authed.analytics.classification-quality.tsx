import { createFileRoute } from '@tanstack/react-router'
import {
  classificationQualityQuery,
  defaultClassificationQualityFilters,
} from '@/features/classification-quality/api/get-classification-quality'
import { ClassificationQualityPage } from '@/features/classification-quality/components/classification-quality-page'

export const Route = createFileRoute('/_authed/analytics/classification-quality')({
  component: ClassificationQualityPage,
  loader: ({ context }) =>
    context.queryClient.ensureQueryData(
      classificationQualityQuery(defaultClassificationQualityFilters),
    ),
})
