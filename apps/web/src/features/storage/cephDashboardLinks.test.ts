import { describe, expect, it } from 'vitest'
import type { ProviderDescriptor } from '@/api/storage/types'
import { cephDashboardHandoff } from './cephDashboardLinks'

function provider(publicUrl: string, cephVersion = '20.2.1'): ProviderDescriptor {
  return {
    id: 'rook-ceph',
    kind: 'rook-ceph',
    displayName: 'Rook / Ceph',
    supportLevel: 'managed',
    drivers: [],
    capabilities: [],
    health: { status: 'ok', conditions: [], observedAt: '2026-07-16T00:00:00Z' },
    metadata: { dashboardPublicUrl: publicUrl, cephVersion },
  }
}

describe('cephDashboardHandoff', () => {
  it('uses an allowlisted route for a compatible Ceph release', () => {
    expect(cephDashboardHandoff(provider('https://ceph.example.test/dashboard'), 'osds')).toEqual({
      href: 'https://ceph.example.test/dashboard#/osd',
      deepLinked: true,
      insecure: false,
      gateway: false,
    })
  })

  it('falls back to the root for unknown versions and unstable resource areas', () => {
    expect(cephDashboardHandoff(provider('https://ceph.example.test', '21.0.0'), 'osds')?.href).toBe('https://ceph.example.test/')
    expect(cephDashboardHandoff(provider('https://ceph.example.test'), 'quorum')?.deepLinked).toBe(false)
  })

  it('never includes resource identifiers in a handoff', () => {
    const handoff = cephDashboardHandoff(provider('https://ceph.example.test'), 'pools')
    expect(handoff?.href).toBe('https://ceph.example.test/#/pool')
    expect(handoff?.href).not.toContain('tenant-secret-pool')
  })

  it('rejects unsafe descriptor values defensively', () => {
    for (const value of [
      'javascript:alert(1)',
      'data:text/html,hello',
      '//ceph.example.test',
      'https://admin:secret@ceph.example.test',
      'https://ceph.example.test/?token=secret',
      'https://ceph.example.test/#/osd',
      'https://ceph..example.test',
      'https://-ceph.example.test',
    ]) {
      expect(cephDashboardHandoff(provider(value), 'osds')).toBeUndefined()
    }
  })

  it('marks explicitly allowed lab HTTP links as insecure', () => {
    expect(cephDashboardHandoff(provider('http://ceph.lab.test'), 'osds')?.insecure).toBe(true)
  })

  it('uses the authenticated same-origin gateway when no public URL is configured', () => {
    const descriptor = provider('')
    descriptor.metadata = {
      cephVersion: '20.2.1',
      dashboardGatewayPath: '/ceph-dashboard/',
      dashboardAvailability: 'available',
    }
    expect(cephDashboardHandoff(descriptor, 'osds')).toEqual({
      href: '/ceph-dashboard/#/osd',
      deepLinked: true,
      insecure: false,
      gateway: true,
    })
  })

  it('rejects gateway paths outside the fixed Ceph prefix', () => {
    const descriptor = provider('')
    descriptor.metadata = { dashboardGatewayPath: '/auth/logout' }
    expect(cephDashboardHandoff(descriptor)).toBeUndefined()
  })
})
