import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { VERSION } from '../src/version'

describe('VERSION', () => {
  it('matches package.json (no drift)', () => {
    const pkg = JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf8'))
    expect(VERSION).toBe(pkg.version)
  })
})
