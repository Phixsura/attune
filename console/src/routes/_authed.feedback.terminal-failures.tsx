import { createFileRoute } from '@tanstack/react-router'
import { FeedbackRoutePage } from '@/routes/-feedback-route-page'

function TerminalFailureWorkbenchRoutePage() {
  return <FeedbackRoutePage initialQueueMode="terminal" showTerminalWorkbench />
}

export const Route = createFileRoute('/_authed/feedback/terminal-failures')({
  component: TerminalFailureWorkbenchRoutePage,
})
