import axeCore from 'axe-core'
import { expect } from 'vitest'

type AxeContext = axeCore.ElementContext
type AxeRunOptions = axeCore.RunOptions
type AxeViolation = axeCore.AxeResults['violations'][number]

export async function expectNoA11yViolations(
  context: AxeContext = document.body,
  options?: AxeRunOptions,
) {
  const results = await axeCore.run<axeCore.AxeResults>(context, {
    resultTypes: ['violations'],
    ...options,
  })

  expect(results.violations, formatViolations(results.violations)).toHaveLength(0)
}

function formatViolations(violations: AxeViolation[]) {
  if (violations.length === 0) {
    return ''
  }

  return violations
    .map((violation) => {
      const nodes = violation.nodes
        .map((node) => `  - ${node.target.join(' ')}: ${node.failureSummary ?? violation.help}`)
        .join('\n')
      return `${violation.id}: ${violation.help}\n${nodes}`
    })
    .join('\n\n')
}
