import { formatBytes } from '@/api/longhorn'
import type { ByteValue } from '@/api/storage/insights'

export function formatByteValue(value: ByteValue): string {
  if (typeof value === 'number') return formatBytes(value)
  if (!/^\d+$/.test(value)) return 'Unknown'
  const bytes = BigInt(value)
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB']
  let unit = 0
  let divisor = 1n
  while (unit < units.length - 1 && bytes >= divisor * 1024n) {
    divisor *= 1024n
    unit++
  }
  if (unit === 0) return `${bytes.toLocaleString()} B`
  const whole = bytes / divisor
  const tenth = (bytes % divisor) * 10n / divisor
  return `${whole.toLocaleString()}.${tenth.toString()} ${units[unit]}`
}

// Insight links come from server-side correlation and deep-link registries,
// but the browser still refuses executable or protocol-relative destinations.
export function safeInsightHref(href: string): string | undefined {
  if (href.startsWith('/') && !href.startsWith('//')) return href
  try {
    const url = new URL(href)
    if (url.protocol === 'https:' || url.protocol === 'http:') return url.toString()
  } catch {
    // Invalid and relative-without-leading-slash links are not rendered.
  }
  return undefined
}
