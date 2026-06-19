// Minimal Node example: ingest one feedback item.
//
//   ATTUNE_BASE_URL=https://attune.example.com \
//   ATTUNE_API_KEY=ak_... \
//   node index.ts "your feedback text"
//
// Running .ts directly needs Node's type-stripping (Node 23.6+, or 22.6+ with
// --experimental-strip-types). On older Node, compile first: `tsc && node index.js`.
import { AttuneError, Client } from '@phixsura/attune'

const baseURL = process.env.ATTUNE_BASE_URL
const apiKey = process.env.ATTUNE_API_KEY
if (!baseURL || !apiKey) {
  console.error('set ATTUNE_BASE_URL and ATTUNE_API_KEY')
  process.exit(2)
}

const client = new Client({ baseURL, apiKey })

try {
  const { id, enrichmentStatus } = await client.ingest({
    content: process.argv[2] ?? 'Hello from the attune Node SDK',
    source: 'api',
    sourceUser: 'demo-user',
  })
  console.log(`ingested id=${id} status=${enrichmentStatus}`)
} catch (err) {
  if (err instanceof AttuneError) {
    console.error(`ingest failed: code=${err.code} status=${err.status} requestId=${err.requestId}`)
    process.exit(1)
  }
  throw err
}
