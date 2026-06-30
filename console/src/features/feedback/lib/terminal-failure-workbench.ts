export interface TerminalFailureWorkbenchClusterLike {
  key: string
  label: string
  count: string | number
  oldestCreatedAt: string
  newestCreatedAt: string
  sampleFeedbackIds: Array<string | number>
  remediationHint?: string | null | undefined
}

export interface TerminalFailureWorkbenchSectionLike {
  key: 'reason_class' | 'model_channel' | 'config_fingerprint' | 'age_bucket'
  title: string
  clusters: TerminalFailureWorkbenchClusterLike[]
  description?: string
  tone?: 'danger' | 'active' | 'neutral' | 'success'
  remediationPath?: string
  remediationLabel?: string
}

export interface TerminalFailureWorkbenchPriority {
  sectionKey: TerminalFailureWorkbenchSectionLike['key']
  sectionTitle: string
  cluster: TerminalFailureWorkbenchClusterLike
  remediationPath?: string
  remediationLabel?: string
}

export function selectTerminalFailurePriority(
  sections: TerminalFailureWorkbenchSectionLike[],
): TerminalFailureWorkbenchPriority | null {
  const candidates = sections.flatMap((section) =>
    section.clusters.map((cluster) => ({
      sectionKey: section.key,
      sectionTitle: section.title,
      cluster,
      remediationPath: section.remediationPath,
      remediationLabel: section.remediationLabel,
    })),
  )

  candidates.sort(compareTerminalFailurePriority)
  return candidates[0] ?? null
}

function compareTerminalFailurePriority(
  a: TerminalFailureWorkbenchPriority,
  b: TerminalFailureWorkbenchPriority,
) {
  const countDiff = Number(b.cluster.count) - Number(a.cluster.count)
  if (countDiff !== 0) return countDiff

  const newestDiff =
    new Date(b.cluster.newestCreatedAt).getTime() - new Date(a.cluster.newestCreatedAt).getTime()
  if (newestDiff !== 0) return newestDiff

  const oldestDiff =
    new Date(a.cluster.oldestCreatedAt).getTime() - new Date(b.cluster.oldestCreatedAt).getTime()
  if (oldestDiff !== 0) return oldestDiff

  const aKey = `${a.sectionKey}:${a.cluster.key}`
  const bKey = `${b.sectionKey}:${b.cluster.key}`
  return aKey.localeCompare(bKey)
}
