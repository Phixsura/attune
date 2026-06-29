// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { type WidgetOptions, widget } from '../src/widget'

function stubFetch(): typeof fetch {
  return vi.fn().mockResolvedValue(
    new Response(JSON.stringify({ id: '42' }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    }),
  )
}

const baseOpts: WidgetOptions = {
  baseURL: 'https://attune.test',
  apiKey: 'ak_test_key',
}

beforeEach(() => {
  document.body.innerHTML = ''
  globalThis.fetch = stubFetch()
})

describe('widget', () => {
  it('mounts a trigger button', () => {
    const w = widget(baseOpts)
    const trigger = document.querySelector('.attune-w-trigger')
    expect(trigger).toBeTruthy()
    expect(trigger?.getAttribute('aria-label')).toBe('Send feedback')
    w.destroy()
  })

  it('opens panel on trigger click', () => {
    const w = widget(baseOpts)
    const trigger = document.querySelector('.attune-w-trigger') as HTMLElement
    trigger.click()
    const panel = document.querySelector('.attune-w-panel')
    expect(panel).toBeTruthy()
    const textarea = document.querySelector('.attune-w-input') as HTMLTextAreaElement
    expect(textarea).toBeTruthy()
    w.destroy()
  })

  it('closes panel on close button click', () => {
    const w = widget(baseOpts)
    const trigger = document.querySelector('.attune-w-trigger') as HTMLElement
    trigger.click()
    const close = document.querySelector('.attune-w-close') as HTMLElement
    close.click()
    expect(document.querySelector('.attune-w-panel')).toBeNull()
    expect(document.querySelector('.attune-w-trigger')).toBeTruthy()
    w.destroy()
  })

  it('destroys removes all elements', () => {
    const w = widget(baseOpts)
    w.destroy()
    expect(document.getElementById('attune-feedback-widget')).toBeNull()
    expect(document.getElementById('attune-feedback-widget-styles')).toBeNull()
  })

  it('replaces existing widget on re-mount', () => {
    const w1 = widget(baseOpts)
    const w2 = widget(baseOpts)
    const widgets = document.querySelectorAll('#attune-feedback-widget')
    expect(widgets).toHaveLength(1)
    w2.destroy()
    void w1 // first one was already destroyed by re-mount
  })

  it('applies custom title and position', () => {
    const w = widget({ ...baseOpts, title: 'Bug Report', position: 'bottom-left' })
    const trigger = document.querySelector('.attune-w-trigger') as HTMLElement
    expect(trigger.getAttribute('aria-label')).toBe('Bug Report')
    expect(trigger.style.cssText).toContain('left: 20px')
    w.destroy()
  })

  it('sanitizes CSS color to prevent injection', () => {
    const w = widget({ ...baseOpts, primaryColor: 'red;} body{display:none' })
    const style = document.getElementById('attune-feedback-widget-styles')
    expect(style?.textContent).not.toContain('display:none')
    w.destroy()
  })

  it('submits feedback via ingest', async () => {
    const mockFetch = stubFetch()
    globalThis.fetch = mockFetch
    const w = widget(baseOpts)

    const trigger = document.querySelector('.attune-w-trigger') as HTMLElement
    trigger.click()

    const textarea = document.querySelector('.attune-w-input') as HTMLTextAreaElement
    textarea.value = 'Great product!'

    const form = document.querySelector('.attune-w-form') as HTMLFormElement
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))

    await vi.waitFor(() => {
      expect(mockFetch).toHaveBeenCalled()
    })

    const [url, init] = (mockFetch as ReturnType<typeof vi.fn>).mock.calls[0] as [
      string,
      RequestInit,
    ]
    expect(url).toBe('https://attune.test/v1/feedback/ingest')
    const body = JSON.parse(init.body as string) as Record<string, string>
    expect(body.content).toBe('Great product!')
    expect(body.source).toBe('web')

    w.destroy()
  })
})
