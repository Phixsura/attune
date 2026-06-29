// SPDX-License-Identifier: Apache-2.0

// Embeddable feedback widget. Builds to a standalone IIFE bundle that
// product teams include with a single <script> tag. Renders a floating
// button + a slide-up form overlay. Submits feedback via the attune
// ingest API using a publishable (ingest:write-only) API key.
//
// Usage:
//   <script src="https://cdn.example.com/attune-widget.iife.js"></script>
//   <script>
//     Attune.widget({ baseURL: 'https://attune.example.com', apiKey: 'ak_...' })
//   </script>

import { AttuneError, Client } from './index'

export interface WidgetOptions {
  baseURL: string
  apiKey: string
  position?: 'bottom-right' | 'bottom-left'
  primaryColor?: string
  title?: string
  placeholder?: string
  thankYou?: string
  source?: string
}

const WIDGET_ID = 'attune-feedback-widget'
const CSS_COLOR_RE = /^#[0-9a-fA-F]{3,8}$|^[a-zA-Z]{1,20}$/

let activeWidget: WidgetInstance | null = null

function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  attrs?: Record<string, string>,
  children?: (Node | string)[],
): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag)
  if (attrs) {
    for (const [k, v] of Object.entries(attrs)) {
      if (k === 'className') e.className = v
      else e.setAttribute(k, v)
    }
  }
  if (children) {
    for (const child of children) {
      e.appendChild(typeof child === 'string' ? document.createTextNode(child) : child)
    }
  }
  return e
}

function chatSvg(): SVGSVGElement {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg')
  svg.setAttribute('width', '20')
  svg.setAttribute('height', '20')
  svg.setAttribute('viewBox', '0 0 24 24')
  svg.setAttribute('fill', 'none')
  svg.setAttribute('stroke', 'currentColor')
  svg.setAttribute('stroke-width', '2')
  svg.setAttribute('stroke-linecap', 'round')
  svg.setAttribute('stroke-linejoin', 'round')
  const path = document.createElementNS('http://www.w3.org/2000/svg', 'path')
  path.setAttribute('d', 'M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z')
  svg.appendChild(path)
  return svg
}

function safeColor(input: string): string {
  return CSS_COLOR_RE.test(input) ? input : '#6366f1'
}

class WidgetInstance {
  private client: Client
  private options: Required<WidgetOptions>
  private root: HTMLDivElement | null = null
  private open = false

  constructor(opts: WidgetOptions) {
    this.client = new Client({ baseURL: opts.baseURL, apiKey: opts.apiKey })
    this.options = {
      baseURL: opts.baseURL,
      apiKey: opts.apiKey,
      position: opts.position ?? 'bottom-right',
      primaryColor: safeColor(opts.primaryColor ?? '#6366f1'),
      title: opts.title ?? 'Send feedback',
      placeholder: opts.placeholder ?? 'Tell us what you think...',
      thankYou: opts.thankYou ?? 'Thanks for your feedback!',
      source: opts.source ?? 'web',
    }
  }

  mount(): void {
    const existing = document.getElementById(WIDGET_ID)
    if (existing) existing.remove()

    this.root = document.createElement('div')
    this.root.id = WIDGET_ID
    this.root.appendChild(this.buildTrigger())
    this.injectStyles()
    document.body.appendChild(this.root)
    this.bind()
  }

  destroy(): void {
    this.root?.remove()
    this.root = null
    const style = document.getElementById(`${WIDGET_ID}-styles`)
    style?.remove()
  }

  private positionStyle(): string {
    return this.options.position === 'bottom-left' ? 'left: 20px' : 'right: 20px'
  }

  private buildTrigger(): HTMLButtonElement {
    const btn = el('button', {
      className: 'attune-w-trigger',
      style: this.positionStyle(),
      'aria-label': this.options.title,
    })
    btn.appendChild(chatSvg())
    return btn
  }

  private buildPanel(): HTMLDivElement {
    const header = el('div', { className: 'attune-w-header' }, [
      el('span', {}, [this.options.title]),
      el('button', { className: 'attune-w-close', 'aria-label': 'Close' }, ['×']),
    ])

    const textarea = el('textarea', {
      className: 'attune-w-input',
      placeholder: this.options.placeholder,
      rows: '4',
      required: '',
    })

    const submit = el('button', { type: 'submit', className: 'attune-w-submit' }, ['Send'])

    const form = el('form', { className: 'attune-w-form' }, [textarea, submit])

    const status = el('div', { className: 'attune-w-status', style: 'display:none' })

    return el('div', {
      className: 'attune-w-panel',
      style: this.positionStyle(),
    }, [header, form, status]) as HTMLDivElement
  }

  private bind(): void {
    if (!this.root) return
    this.root.addEventListener('click', (e) => {
      const target = e.target as HTMLElement
      if (target.closest('.attune-w-trigger')) {
        this.toggle()
      } else if (target.closest('.attune-w-close')) {
        this.toggle()
      }
    })
    this.root.addEventListener('submit', (e) => {
      e.preventDefault()
      this.submit()
    })
  }

  private toggle(): void {
    if (!this.root) return
    this.open = !this.open
    this.root.replaceChildren(this.open ? this.buildPanel() : this.buildTrigger())
    if (this.open) {
      const input = this.root.querySelector<HTMLTextAreaElement>('.attune-w-input')
      input?.focus()
    }
  }

  private async submit(): Promise<void> {
    if (!this.root) return
    const input = this.root.querySelector<HTMLTextAreaElement>('.attune-w-input')
    const btn = this.root.querySelector<HTMLButtonElement>('.attune-w-submit')
    const status = this.root.querySelector<HTMLDivElement>('.attune-w-status')
    if (!input || !btn || !status) return

    const content = input.value.trim()
    if (!content) return

    btn.disabled = true
    btn.textContent = '...'

    try {
      await this.client.ingest({
        content,
        source: this.options.source,
        pageUrl: window.location.href,
      })
      input.value = ''
      status.style.display = 'block'
      status.textContent = this.options.thankYou
      status.className = 'attune-w-status attune-w-ok'
      setTimeout(() => this.toggle(), 2000)
    } catch (err) {
      const code = err instanceof AttuneError ? err.code : 'UNKNOWN'
      status.style.display = 'block'
      status.textContent = `Error: ${code}`
      status.className = 'attune-w-status attune-w-err'
    } finally {
      btn.disabled = false
      btn.textContent = 'Send'
    }
  }

  private injectStyles(): void {
    if (document.getElementById(`${WIDGET_ID}-styles`)) return
    const style = document.createElement('style')
    style.id = `${WIDGET_ID}-styles`
    const c = this.options.primaryColor
    style.textContent = `
      .attune-w-trigger{position:fixed;bottom:20px;z-index:99999;width:48px;height:48px;border-radius:50%;border:none;background:${c};color:#fff;cursor:pointer;display:flex;align-items:center;justify-content:center;box-shadow:0 4px 12px rgba(0,0,0,.15);transition:transform .15s}
      .attune-w-trigger:hover{transform:scale(1.08)}
      .attune-w-panel{position:fixed;bottom:20px;z-index:99999;width:340px;background:#fff;border-radius:12px;box-shadow:0 8px 32px rgba(0,0,0,.18);overflow:hidden;font-family:system-ui,-apple-system,sans-serif}
      .attune-w-header{display:flex;align-items:center;justify-content:space-between;padding:14px 16px;background:${c};color:#fff;font-size:14px;font-weight:600}
      .attune-w-close{background:none;border:none;color:#fff;font-size:20px;cursor:pointer;padding:0 4px;line-height:1}
      .attune-w-form{padding:16px;display:flex;flex-direction:column;gap:10px}
      .attune-w-input{resize:vertical;border:1px solid #e2e8f0;border-radius:8px;padding:10px;font-size:13px;font-family:inherit;outline:none;min-height:80px}
      .attune-w-input:focus{border-color:${c};box-shadow:0 0 0 2px ${c}33}
      .attune-w-submit{background:${c};color:#fff;border:none;border-radius:8px;padding:10px;font-size:13px;font-weight:600;cursor:pointer;transition:opacity .15s}
      .attune-w-submit:hover{opacity:.9}
      .attune-w-submit:disabled{opacity:.5;cursor:not-allowed}
      .attune-w-status{padding:0 16px 14px;font-size:12px;text-align:center}
      .attune-w-ok{color:#16a34a}
      .attune-w-err{color:#dc2626}
    `
    document.head.appendChild(style)
  }
}

export function widget(opts: WidgetOptions): { destroy: () => void } {
  if (activeWidget) activeWidget.destroy()
  activeWidget = new WidgetInstance(opts)
  activeWidget.mount()
  return {
    destroy: () => {
      activeWidget?.destroy()
      activeWidget = null
    },
  }
}
