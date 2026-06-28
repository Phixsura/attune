#!/usr/bin/env node

import net from 'node:net'

const args = process.argv.slice(2)
const preferred =
  args[0] === undefined || args[0] === 'random' ? undefined : parsePort(args[0], 'preferred port')
const excluded = new Set(args.slice(1).map((arg, index) => parsePort(arg, `exclude port #${index + 1}`)))

const port = await pickFreePort(preferred, excluded)
process.stdout.write(`${port}\n`)

async function pickFreePort(preferredPort, excludedPorts) {
  if (preferredPort !== undefined && !excludedPorts.has(preferredPort)) {
    const chosen = await tryPort(preferredPort)
    if (chosen !== null) return chosen
  }

  for (let attempt = 0; attempt < 32; attempt++) {
    const chosen = await tryPort(0)
    if (chosen === null || excludedPorts.has(chosen)) {
      continue
    }
    return chosen
  }

  throw new Error('could not allocate a free localhost TCP port')
}

async function tryPort(port) {
  const server = net.createServer()
  try {
    await new Promise((resolve, reject) => {
      server.once('error', reject)
      server.listen(port, '127.0.0.1', resolve)
    })
  } catch {
    server.removeAllListeners('error')
    server.close()
    return null
  }

  const address = server.address()
  const actual = typeof address === 'object' && address ? address.port : null
  await new Promise((resolve, reject) => {
    server.close((error) => {
      if (error) reject(error)
      else resolve()
    })
  })
  return actual
}

function parsePort(raw, label) {
  const value = Number(raw)
  if (!Number.isInteger(value) || value < 1 || value > 65535) {
    throw new Error(`${label} must be an integer port`)
  }
  return value
}
