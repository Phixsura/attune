#!/usr/bin/env node
import { readFile, writeFile } from 'node:fs/promises'
import path from 'node:path'

const metrics = ['lines', 'statements', 'branches', 'functions']

function usage() {
  return [
    'usage: node scripts/compare-console-coverage.mjs <base-summary.json> <pr-summary.json> <comment.md> [result.json]',
    '',
    'Compares Istanbul/Vitest coverage-summary.json total percentages.',
  ].join('\n')
}

function pct(value) {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    throw new Error(`invalid coverage percentage: ${value}`)
  }
  return value
}

async function readSummary(file) {
  const raw = await readFile(file, 'utf8')
  const parsed = JSON.parse(raw)
  if (!parsed.total || typeof parsed.total !== 'object') {
    throw new Error(`${file} does not contain a total coverage block`)
  }
  const total = {}
  for (const metric of metrics) {
    if (!parsed.total[metric]) {
      throw new Error(`${file} missing total.${metric}`)
    }
    total[metric] = {
      pct: pct(parsed.total[metric].pct),
      covered: Number(parsed.total[metric].covered ?? 0),
      total: Number(parsed.total[metric].total ?? 0),
    }
  }
  return total
}

function fmt(value) {
  return `${value.toFixed(2)}%`
}

function deltaFmt(value) {
  const sign = value > 0 ? '+' : ''
  return `${sign}${value.toFixed(2)}%`
}

function buildRows(base, pr) {
  return metrics.map((metric) => {
    const delta = pr[metric].pct - base[metric].pct
    return {
      metric,
      base: base[metric],
      pr: pr[metric],
      delta,
      passed: delta >= 0,
    }
  })
}

function buildMarkdown(rows, artifactHint) {
  const passed = rows.every((row) => row.passed)
  const status = passed ? 'passed' : 'failed'
  const lines = [
    `## Console Coverage ${status}`,
    '',
    '| Metric | main | PR | Delta |',
    '|---|---:|---:|---:|',
  ]
  for (const row of rows) {
    lines.push(
      `| ${row.metric} | ${fmt(row.base.pct)} | ${fmt(row.pr.pct)} | ${deltaFmt(row.delta)} |`,
    )
  }
  lines.push('')
  if (!passed) {
    lines.push('Coverage decreased for at least one aggregate metric.')
    lines.push('')
  }
  lines.push(`HTML report artifact: ${artifactHint}`)
  lines.push('')
  lines.push('Use `skip-console-coverage` only when a reviewer accepts the decrease.')
  lines.push('')
  return lines.join('\n')
}

const [baseFile, prFile, markdownFile, resultFile = 'console-coverage-result.json'] =
  process.argv.slice(2)

if (!baseFile || !prFile || !markdownFile) {
  console.error(usage())
  process.exit(2)
}

const base = await readSummary(baseFile)
const pr = await readSummary(prFile)
const rows = buildRows(base, pr)
const passed = rows.every((row) => row.passed)
const result = {
  passed,
  metrics: Object.fromEntries(
    rows.map((row) => [
      row.metric,
      {
        base: row.base.pct,
        pr: row.pr.pct,
        delta: Number(row.delta.toFixed(4)),
        passed: row.passed,
      },
    ]),
  ),
}

await writeFile(resultFile, `${JSON.stringify(result, null, 2)}\n`)
await writeFile(
  markdownFile,
  buildMarkdown(rows, 'download `console-coverage-html` from this workflow run'),
)

console.log(
  `${path.basename(markdownFile)} written; console coverage ${passed ? 'passed' : 'failed'}`,
)
