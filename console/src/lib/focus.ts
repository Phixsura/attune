export function restoreFocusWhenReady(target: HTMLElement | null | undefined) {
  if (!target?.isConnected) return

  let attempts = 0

  const focusTarget = (respectExistingFocus = false) => {
    if (!target.isConnected) return 'yield'
    const active = document.activeElement
    if (active === target) return 'target'
    if (
      respectExistingFocus &&
      active instanceof HTMLElement &&
      active.isConnected &&
      active !== document.body &&
      active !== document.documentElement &&
      active !== target
    ) {
      const activeIsInteractive = active.matches(
        'a[href], button, input, select, textarea, [role="button"], [role="link"], [role="textbox"], [tabindex]:not([tabindex="-1"])',
      )
      const activeDialog = active.closest('[role="dialog"]')
      if (
        activeIsInteractive &&
        (!(activeDialog instanceof HTMLElement) || activeDialog.dataset.state !== 'closed')
      ) {
        return 'yield'
      }
    }
    target.focus({ preventScroll: true })
    return document.activeElement === target ? 'target' : 'retry'
  }

  focusTarget()

  const retryAfterPaint = () => {
    attempts += 1
    if (typeof window === 'undefined' || typeof window.requestAnimationFrame !== 'function') {
      setTimeout(() => {
        if (focusTarget(true) !== 'yield' && attempts < 60) retryAfterPaint()
      }, 0)
      return
    }

    window.requestAnimationFrame(() => {
      window.requestAnimationFrame(() => {
        if (focusTarget(true) !== 'yield' && attempts < 60) retryAfterPaint()
      })
    })
  }

  if (typeof queueMicrotask === 'function') {
    queueMicrotask(retryAfterPaint)
    return
  }

  retryAfterPaint()
}
