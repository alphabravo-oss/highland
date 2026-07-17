type NetworkInformation = {
  saveData?: boolean
  effectiveType?: string
}

function canPrefetch() {
  const connection = (navigator as Navigator & { connection?: NetworkInformation }).connection
  if (connection?.saveData) return false
  return connection?.effectiveType !== 'slow-2g' && connection?.effectiveType !== '2g'
}

export function prefetchRoute(path: string) {
  if (!canPrefetch()) return
  if (path === '/dashboard') {
    void import('@/features/dashboard/DashboardPage')
  } else if (path === '/volumes') {
    void import('@/features/volumes/VolumesPage')
  }
}
