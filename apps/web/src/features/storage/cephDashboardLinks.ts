import type { ProviderDescriptor } from '@/api/storage/types'

export type CephDashboardHandoff = {
  href: string
  deepLinked: boolean
  insecure: boolean
  gateway: boolean
}

// Ceph Dashboard routes are deliberately versioned and contain no resource
// identifiers. Unknown versions and unstable areas fall back to the configured
// dashboard root.
const verifiedRoutes: Record<string, Record<string, string>> = {
  '19.2': {
    pools: '/pool',
    osds: '/osd',
    filesystems: '/cephfs',
    'rbd-images': '/block/rbd',
  },
  '20.2': {
    pools: '/pool',
    osds: '/osd',
    filesystems: '/cephfs',
    'rbd-images': '/block/rbd',
  },
}

function compatibleCephRelease(version?: string) {
  const match = version?.match(/^v?(\d+)\.(\d+)\.\d+(?:[-+].*)?$/)
  return match ? `${match[1]}.${match[2]}` : undefined
}

function validHostname(hostname: string) {
  if (hostname.startsWith('[') && hostname.endsWith(']')) {
    return /^[0-9a-f:.]+$/i.test(hostname.slice(1, -1))
  }
  if (hostname.length > 253) return false
  return hostname.split('.').every((label) =>
    label.length > 0 &&
    label.length <= 63 &&
    !label.startsWith('-') &&
    !label.endsWith('-') &&
    /^[a-z0-9-]+$/i.test(label),
  )
}

export function cephDashboardHandoff(provider?: ProviderDescriptor, resourceKind?: string): CephDashboardHandoff | undefined {
  const publicUrl = provider?.metadata?.dashboardPublicUrl
  const gatewayPath = provider?.metadata?.dashboardGatewayPath
  const configured = publicUrl || gatewayPath
  if (!configured) return undefined

  const gateway = !publicUrl
  let root: URL
  try {
    root = gateway ? new URL(configured, 'http://highland.internal') : new URL(configured)
  } catch {
    return undefined
  }
  if (
    !['https:', 'http:'].includes(root.protocol) ||
    root.username ||
    root.password ||
    root.search ||
    root.hash ||
    !validHostname(root.hostname) ||
    (gateway && (root.origin !== 'http://highland.internal' || !root.pathname.startsWith('/ceph-dashboard/')))
  ) {
    return undefined
  }

  const release = compatibleCephRelease(provider?.metadata?.cephVersion)
  const route = release && resourceKind ? verifiedRoutes[release]?.[resourceKind] : undefined
  if (route) root.hash = route
  return {
    href: gateway ? `${root.pathname}${root.hash}` : root.toString(),
    deepLinked: Boolean(route),
    insecure: !gateway && root.protocol === 'http:',
    gateway,
  }
}
