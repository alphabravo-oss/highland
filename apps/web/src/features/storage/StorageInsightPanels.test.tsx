import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import type {
  CapacityForecast,
  CapacityOwnership,
  StorageTimeline,
} from '@/api/storage/insights'
import {
  CapacityOwnershipPanel,
  TimelinePanel,
} from './StorageInsightPanels'
import { formatByteValue, safeInsightHref } from './insightFormatting'

const observedAt = '2026-07-16T12:00:00Z'

function timeline(): StorageTimeline {
  return {
    total: 2,
    entries: [
      {
        id: 'event-1',
        providerId: 'rook-ceph',
        namespace: 'apps',
        resource: { kind: 'PersistentVolumeClaim', namespace: 'apps', name: 'data', uid: 'pvc-1' },
        severity: 'warning',
        source: 'kubernetes-event',
        reason: 'ProvisioningFailed',
        message: 'waiting for an OSD',
        count: 4,
        firstOccurredAt: '2026-07-16T11:50:00Z',
        lastOccurredAt: observedAt,
        observedAt,
        ordering: 'known',
        attribution: { providerId: 'rook-ceph', evidence: 'authoritative' },
        retention: 'transient',
        links: [{ kind: 'resource-graph', href: '/storage/relationships/pvc-1' }],
      },
      {
        id: 'health-1',
        providerId: 'rook-ceph',
        severity: 'error',
        source: 'ceph-health',
        message: 'pool is degraded',
        count: 1,
        observedAt,
        ordering: 'clock-skew',
        attribution: { providerId: 'rook-ceph', evidence: 'derived' },
        retention: 'transient',
      },
    ],
  }
}

function ownership(): CapacityOwnership {
  return {
    observedAt,
    groups: [
      {
        measure: 'pvc-requested',
        bytes: '10737418240',
        dimensions: {
          providerId: 'rook-ceph',
          namespace: 'apps',
          workloadKind: 'StatefulSet',
          workload: 'db',
          storageClass: 'rook-ceph-block',
          pool: 'replicapool',
        },
        observations: 1,
        evidence: ['authoritative'],
      },
      {
        measure: 'cluster-raw',
        bytes: '32212254720',
        dimensions: { providerId: 'rook-ceph' },
        observations: 1,
        evidence: ['potential'],
      },
    ],
  }
}

describe('TimelinePanel', () => {
  it('renders normalized sources, counts, links, and partial evidence explicitly', () => {
    render(<TimelinePanel timeline={timeline()} />)
    expect(screen.getByTestId('storage-timeline-panel')).toBeInTheDocument()
    expect(screen.getByText('ProvisioningFailed:')).toBeInTheDocument()
    expect(screen.getByText('×4')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'View relationships' })).toHaveAttribute(
      'href',
      '/storage/relationships/pvc-1',
    )
    expect(screen.getByText('Timeline includes partial evidence')).toBeInTheDocument()
    expect(screen.getByText(/clock skew or unknown ordering/)).toBeInTheDocument()
  })

  it('does not render executable timeline links', () => {
    const unsafe = timeline()
    unsafe.entries[0]!.links = [{ kind: 'native-dashboard', href: 'javascript:alert(1)' }]
    render(<TimelinePanel timeline={unsafe} />)
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })

  it('has loading, error, and empty states', () => {
    const { rerender } = render(<TimelinePanel isLoading />)
    expect(screen.getByTestId('insight-panel-skeleton')).toBeInTheDocument()
    rerender(<TimelinePanel error={new Error('timeline denied')} />)
    expect(screen.getByRole('alert')).toHaveTextContent('timeline denied')
    rerender(<TimelinePanel timeline={{ entries: [], total: 0 }} />)
    expect(screen.getByText('No storage activity')).toBeInTheDocument()
  })
})

describe('CapacityOwnershipPanel', () => {
  it('keeps unlike capacity measures in separate labeled sections', () => {
    render(<CapacityOwnershipPanel ownership={ownership()} />)
    const panel = screen.getByTestId('capacity-ownership-panel')
    expect(within(panel).getByRole('heading', { name: 'PVC requested' })).toBeInTheDocument()
    expect(within(panel).getByRole('heading', { name: 'Cluster physical raw' })).toBeInTheDocument()
    expect(within(panel).getByText('10.0 GiB')).toBeInTheDocument()
    expect(within(panel).getByText('30.0 GiB')).toBeInTheDocument()
    expect(within(panel).getByText('Capacity attribution is partial')).toBeInTheDocument()
    expect(within(panel).getByText(/not additive/)).toBeInTheDocument()
  })

  it('renders unavailable and available forecast evidence', () => {
    const unavailable: CapacityForecast = {
      providerId: 'rook-ceph',
      measure: 'backend-allocated',
      status: 'unavailable',
      sampleCount: 1,
      window: 0,
      conditions: [{ code: 'insufficient-samples', message: 'At least twelve samples are required.' }],
    }
    const { rerender } = render(
      <CapacityOwnershipPanel ownership={ownership()} forecast={unavailable} />,
    )
    expect(screen.getByText('Forecast unavailable')).toBeInTheDocument()
    expect(screen.getByText(/At least twelve samples/)).toBeInTheDocument()

    rerender(
      <CapacityOwnershipPanel
        ownership={ownership()}
        forecast={{
          ...unavailable,
          status: 'available',
          sampleCount: 24,
          projectedBytes: '42949672960',
          projectionAt: '2026-07-23T12:00:00Z',
          slopeBytesPerDay: 1073741824,
          confidence: 'high',
        }}
      />,
    )
    expect(screen.getByText(/40.0 GiB by/)).toBeInTheDocument()
    expect(screen.getByText(/historical trend, not a capacity guarantee/)).toBeInTheDocument()
  })

  it('has loading, error, and empty states', () => {
    const { rerender } = render(<CapacityOwnershipPanel isLoading />)
    expect(screen.getByTestId('insight-panel-skeleton')).toBeInTheDocument()
    rerender(<CapacityOwnershipPanel error={new Error('capacity denied')} />)
    expect(screen.getByRole('alert')).toHaveTextContent('capacity denied')
    rerender(<CapacityOwnershipPanel ownership={{ groups: [], observedAt }} />)
    expect(screen.getByText('No capacity ownership data')).toBeInTheDocument()
  })
})

describe('formatByteValue', () => {
  it('formats decimal uint64 strings without converting them to unsafe numbers', () => {
    expect(formatByteValue('18446744073709551615')).toMatch(/EiB$/)
    expect(formatByteValue('not-bytes')).toBe('Unknown')
  })

  it('only permits safe insight link schemes', () => {
    expect(safeInsightHref('/storage/relationships')).toBe('/storage/relationships')
    expect(safeInsightHref('https://ceph.example.test/#/pool')).toBe('https://ceph.example.test/#/pool')
    expect(safeInsightHref('//attacker.example.test')).toBeUndefined()
    expect(safeInsightHref('javascript:alert(1)')).toBeUndefined()
    expect(safeInsightHref('relative/path')).toBeUndefined()
  })
})
