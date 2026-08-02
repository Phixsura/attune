#!/usr/bin/env node

import { existsSync, readdirSync, statSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const scriptDir = path.dirname(fileURLToPath(import.meta.url))
const consoleDir = path.resolve(scriptDir, '..')
const assetsDir = path.join(consoleDir, 'dist', 'assets')
const maxJsChunkKiB = readPositiveNumberEnv('ATTUNE_CONSOLE_MAX_JS_CHUNK_KIB', 500)
const maxCssAssetKiB = readPositiveNumberEnv('ATTUNE_CONSOLE_MAX_CSS_ASSET_KIB', 250)

if (!existsSync(assetsDir)) {
  console.error('bundle budget: dist/assets is missing; run vite build first')
  process.exit(1)
}

const assets = readdirSync(assetsDir)
  .map((name) => {
    const filePath = path.join(assetsDir, name)
    return {
      name,
      bytes: statSync(filePath).size,
    }
  })
  .filter((asset) => asset.name.endsWith('.js') || asset.name.endsWith('.css'))

const jsAssets = assets.filter((asset) => asset.name.endsWith('.js'))
const cssAssets = assets.filter((asset) => asset.name.endsWith('.css'))
const violations = [
  ...findOverBudget(jsAssets, maxJsChunkKiB * 1024, 'JS chunk'),
  ...findOverBudget(cssAssets, maxCssAssetKiB * 1024, 'CSS asset'),
]

if (violations.length > 0) {
  console.error('bundle budget: failed')
  for (const violation of violations) {
    console.error(`  ${violation}`)
  }
  console.error(`  limits: JS <= ${maxJsChunkKiB} KiB, CSS <= ${maxCssAssetKiB} KiB`)
  process.exit(1)
}

console.log(
  `bundle budget: clean (${jsAssets.length} JS chunks <= ${maxJsChunkKiB} KiB, ${cssAssets.length} CSS assets <= ${maxCssAssetKiB} KiB)`,
)
console.log(`bundle budget: largest JS ${formatLargest(jsAssets)}`)
console.log(`bundle budget: largest CSS ${formatLargest(cssAssets)}`)

function readPositiveNumberEnv(name, fallback) {
  const raw = process.env[name]
  if (!raw) {
    return fallback
  }
  const value = Number(raw)
  if (!Number.isFinite(value) || value <= 0) {
    console.error(`bundle budget: ${name} must be a positive number`)
    process.exit(2)
  }
  return value
}

function findOverBudget(assetsToCheck, maxBytes, label) {
  return assetsToCheck
    .filter((asset) => asset.bytes > maxBytes)
    .sort((left, right) => right.bytes - left.bytes)
    .map((asset) => `${label} ${asset.name} is ${formatBytes(asset.bytes)}`)
}

function formatLargest(assetsToFormat) {
  const largest = [...assetsToFormat].sort((left, right) => right.bytes - left.bytes).at(0)
  if (!largest) {
    return 'none'
  }
  return `${largest.name} (${formatBytes(largest.bytes)})`
}

function formatBytes(bytes) {
  return `${(bytes / 1024).toFixed(1)} KiB`
}
