import { createFileRoute } from '@tanstack/react-router'
import { FeedbackRoutePage } from '@/routes/-feedback-route-page'

function PortalInboxRoutePage() {
  return (
    <FeedbackRoutePage
      initialSourceFilter="portal"
      titleKey="nav.portal_inbox"
      subtitleKey="feedback.portal.subtitle"
    />
  )
}

export const Route = createFileRoute('/_authed/feedback/portal')({
  component: PortalInboxRoutePage,
})
