#!/usr/bin/env node
import { readFile, writeFile } from 'node:fs/promises'
import path from 'node:path'

function usage() {
  return [
    'usage: node scripts/compare-go-coverage.mjs <base-cover-func.txt> <pr-cover-func.txt> <comment.md> [result.json]',
    '',
    'Compares the total percentage from `go tool cover -func` output.',
  ].join('\n')
}

async function readTotal(file) {
  const raw = await readFile(file, 'utf8')
  const line = raw
    .split(/\r?\n/)
    .find((candidate) => candidate.startsWith('total:') && candidate.includes('(statements)'))
  if (!line) {
    throw new Error(`${file} does not contain a go cover total line`)
  }
  const match = line.match(/\(statements\)\s+([0-9]+(?:\.[0-9]+)?)%/)
  if (!match) {
    throw new Error(`${file} has an unparseable total line: ${line}`)
  }
  return Number(match[1])
}

function fmt(value) {
  return `${value.toFixed(1)}%`
}

function deltaFmt(value) {
  const sign = value > 0 ? '+' : ''
  return `${sign}${value.toFixed(1)}%`
}

function buildMarkdown(base, pr, artifactHint) {
  const delta = pr - base
  const passed = delta >= 0
  const status = passed ? 'passed' : 'failed'
  const lines = [
    `## Go Coverage ${status}`,
    '',
    '| Metric | main | PR | Delta |',
    '|---|---:|---:|---:|',
    `| statements | ${fmt(base)} | ${fmt(pr)} | ${deltaFmt(delta)} |`,
    '',
  ]
  if (!passed) {
    lines.push('Go statement coverage decreased.')
    lines.push('')
  }
  lines.push(`HTML report artifact: ${artifactHint}`)
  lines.push('')
  lines.push('Use `skip-go-coverage` only when a reviewer accepts the decrease.')
  lines.push('')
  return lines.join('\n')
}

const [baseFile, prFile, markdownFile, resultFile = 'go-coverage-result.json'] =
  process.argv.slice(2)

if (!baseFile || !prFile || !markdownFile) {
  console.error(usage())
  process.exit(2)
}

const base = await readTotal(baseFile)
const pr = await readTotal(prFile)
const delta = pr - base
const passed = delta >= 0

await writeFile(
  resultFile,
  `${JSON.stringify(
    {
      passed,
      metric: 'statements',
      base,
      pr,
      delta: Number(delta.toFixed(4)),
    },
    null,
    2,
  )}\n`,
)
await writeFile(
  markdownFile,
  buildMarkdown(base, pr, 'download `go-coverage-html` from this workflow run'),
)

console.log(`${path.basename(markdownFile)} written; Go coverage ${passed ? 'passed' : 'failed'}`)
