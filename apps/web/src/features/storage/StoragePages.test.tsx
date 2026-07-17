import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { ProviderDescriptor } from '@/api/storage/types'
import { CephDashboardHandoff } from './StoragePages'

const mocks = vi.hoisted(() => ({
  admin: false,
  reveal: vi.fn(),
}))

vi.mock('@/auth/AuthContext', () => ({
  useAuth: () => ({ isAdmin: mocks.admin }),
}))

vi.mock('@/api/storage/client', () => ({
  storageClient: {
    revealCephDashboardCredential: mocks.reveal,
  },
}))

function labProvider(): ProviderDescriptor {
  return {
    id: 'rook-ceph',
    kind: 'rook-ceph',
    displayName: 'Rook / Ceph',
    supportLevel: 'managed',
    drivers: [],
    capabilities: [],
    health: { status: 'ok', conditions: [], observedAt: '2026-07-16T00:00:00Z' },
    metadata: {
      cephVersion: '20.2.1',
      dashboardPublicUrl: 'http://ceph.lab.test',
      dashboardAvailability: 'available',
      dashboardPublicUrlSecurity: 'insecure-lab-http',
    },
  }
}

describe('CephDashboardHandoff', () => {
  beforeEach(() => {
    mocks.admin = false
    mocks.reveal.mockReset()
  })

  it('visibly warns before handing off to an explicitly allowed HTTP lab dashboard', () => {
    render(<CephDashboardHandoff provider={labProvider()} resourceKind="osds" />)
    expect(screen.getByText('Disposable-lab HTTP link')).toBeInTheDocument()
    expect(screen.getByText(/not TLS protected/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Open Ceph Dashboard' })).toHaveAttribute('href', 'http://ceph.lab.test/#/osd')
    expect(screen.getByText(/Private reader status:/)).toHaveTextContent('available')
  })

  it('shows the Highland gateway when the private dashboard is configured without a public URL', () => {
    const provider = labProvider()
    provider.metadata = {
      cephVersion: '20.2.1',
      dashboardGatewayPath: '/ceph-dashboard/',
      dashboardAvailability: 'available',
    }
    render(<CephDashboardHandoff provider={provider} resourceKind="osds" />)
    expect(screen.getByRole('link', { name: 'Open Ceph Dashboard' })).toHaveAttribute('href', '/ceph-dashboard/#/osd')
    expect(screen.getByText(/through Highland’s authenticated gateway/)).toBeInTheDocument()
    expect(screen.queryByText('Disposable-lab HTTP link')).not.toBeInTheDocument()
  })

  it('reveals the audited credential action only to administrators', async () => {
    const provider = labProvider()
    provider.metadata = {
      cephVersion: '20.2.1',
      dashboardGatewayPath: '/ceph-dashboard/',
      dashboardAvailability: 'available',
    }
    const { rerender } = render(<CephDashboardHandoff provider={provider} />)
    expect(screen.queryByRole('button', { name: 'Reveal credential' })).not.toBeInTheDocument()

    mocks.admin = true
    mocks.reveal.mockResolvedValue({ username: 'admin', password: 'test-ceph-password' })
    rerender(<CephDashboardHandoff provider={provider} />)
    fireEvent.click(screen.getByRole('button', { name: 'Reveal credential' }))
    await waitFor(() => expect(screen.getByText('test-ceph-password')).toBeInTheDocument())
    expect(mocks.reveal).toHaveBeenCalledOnce()
    expect(screen.getByRole('button', { name: 'Hide credential' })).toBeInTheDocument()
  })
})
