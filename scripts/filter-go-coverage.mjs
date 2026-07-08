#!/usr/bin/env node
import { readFile, writeFile } from 'node:fs/promises'

function usage() {
  return [
    'usage: node scripts/filter-go-coverage.mjs <input-coverprofile> <output-coverprofile>',
    '',
    'Filters generated Go files from a coverprofile before aggregate coverage comparison.',
  ].join('\n')
}

const [inputFile, outputFile] = process.argv.slice(2)

if (!inputFile || !outputFile) {
  console.error(usage())
  process.exit(2)
}

const raw = await readFile(inputFile, 'utf8')
const lines = raw.split(/\r?\n/)
const filtered = lines.filter((line, index) => {
  if (line === '' && index === lines.length - 1) return false
  if (line.startsWith('mode:')) return true
  return !line.includes('.pb.go:')
})

await writeFile(outputFile, `${filtered.join('\n')}\n`)
