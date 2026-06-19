// Minimal browser widget: ingest feedback straight from the page.
//
// IMPORTANT: the API key here is an `ingest:write` key, which is a *publishable*
// credential (like a Segment write key or a Sentry DSN). Ship ONLY this narrow,
// write-only scope to the browser, over HTTPS — never a broader-scope key. A
// scraped key can still spam ingest, so treat it as low-trust: set the key's
// IP/CIDR allowlist where the origin is predictable, rely on the tenant ingest
// rate limit to cap volume, and rotate/revoke the key if it leaks.
import { AttuneError, Client } from '@phixsura/attune'

// In a real app these come from your build-time public config.
const client = new Client({
  baseURL: import.meta.env?.VITE_ATTUNE_BASE_URL ?? 'https://attune.example.com',
  apiKey: import.meta.env?.VITE_ATTUNE_INGEST_KEY ?? 'ak_publishable_ingest_key',
})

const form = document.querySelector<HTMLFormElement>('#feedback-form')
const input = document.querySelector<HTMLTextAreaElement>('#feedback-text')
const status = document.querySelector<HTMLParagraphElement>('#status')

form?.addEventListener('submit', async (event) => {
  event.preventDefault()
  if (!input || !status) return
  status.textContent = 'sending…'
  try {
    const { id } = await client.ingest({
      content: input.value,
      source: 'web',
      pageUrl: window.location.href,
    })
    status.textContent = `thanks! (id ${id})`
    input.value = ''
  } catch (err) {
    const code = err instanceof AttuneError ? err.code : 'UNKNOWN'
    status.textContent = `sorry, that failed (${code})`
  }
})

// Vite/TS: declare the public env vars this example reads.
declare global {
  interface ImportMeta {
    readonly env?: { VITE_ATTUNE_BASE_URL?: string; VITE_ATTUNE_INGEST_KEY?: string }
  }
}
