export function triggerBlobDownload(blob: Blob, filename: string) {
  const url = window.URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.style.display = 'none'
  document.body.append(link)
  link.click()
  window.setTimeout(() => {
    window.URL.revokeObjectURL(url)
    link.remove()
  }, 0)
}
