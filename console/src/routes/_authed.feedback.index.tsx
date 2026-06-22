import { createFileRoute } from '@tanstack/react-router'
import { FeedbackRoutePage } from '@/routes/-feedback-route-page'

export const Route = createFileRoute('/_authed/feedback/')({
  component: FeedbackRoutePage,
})
