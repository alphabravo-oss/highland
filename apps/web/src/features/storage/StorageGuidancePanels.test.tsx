import { render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import type {
  ProviderComparison,
  RemediationResult,
} from '@/api/storage/guidance'
import {
  ProviderComparisonPanel,
  RemediationGuidancePanel,
} from './StorageGuidancePanels'

const observedAt = '2026-07-16T12:00:00Z'

function comparison(): ProviderComparison {
  return {
    observedAt,
    policy: { requireSnapshot: true, minimumHeadroomPercent: 20 },
    conditions: [{
      code: 'non-comparable-benchmarks',
      message: 'Provider counters and fio results were not ranked against each other.',
    }],
    assessments: [{
      eligibility: 'eligible',
      candidate: {
        providerId: 'rook-ceph',
        providerName: 'Rook / Ceph',
        storageClass: 'rook-ceph-block',
        supportLevel: 'managed',
        testedProfile: {
          providerKind: 'rook-ceph',
          providerVersion: '20.2.1',
          driver: 'rook-ceph.rbd.csi.ceph.com',
          driverVersion: '3.15',
        },
        health: {
          status: 'healthy',
          evidence: { source: 'ceph-dashboard', strength: 'authoritative', observedAt },
        },
        headroom: {
          percent: 42,
          evidence: { source: 'prometheus', strength: 'authoritative', observedAt },
        },
        capabilities: [],
        accessModes: ['ReadWriteOnce'],
        topologyKeys: ['topology.kubernetes.io/zone'],
        reclaimPolicy: 'Delete',
        operations: [
          { capability: 'ceph.pool.create', surface: 'Highland', detail: 'Guarded Rook CR workflow' },
          { capability: 'osd.repair', surface: 'Ceph Dashboard', readOnly: true },
        ],
        benchmarks: [{
          semantic: 'random-read-iops',
          unit: 'iops',
          method: 'fio',
          profile: '4k-qd32',
          value: 1200,
          evidence: { source: 'benchmark', strength: 'authoritative', observedAt },
        }],
      },
      criteria: [
        {
          criterion: 'snapshot',
          state: 'supported',
          reason: 'requires snapshot.create',
          evidence: { source: 'snapshot-api', strength: 'authoritative', observedAt },
        },
        {
          criterion: 'headroom',
          state: 'supported',
          reason: 'requires at least 20.0% usable headroom',
          evidence: { source: 'prometheus', strength: 'authoritative', observedAt },
        },
      ],
    }],
  }
}

function remediations(): RemediationResult {
  return {
    recommendations: [
      {
        id: 'expand',
        conditionCode: 'pvc-near-capacity',
        providerId: 'rook-ceph',
        title: 'Review PVC expansion',
        explanation: 'Highland can plan a guarded expansion.',
        surface: 'highland',
        highlandActionId: 'volume.expand',
        prerequisites: ['StorageClass expansion is enabled'],
        risks: ['The workload filesystem must support growth'],
        escalation: 'operator',
        evidence: [{
          source: 'prometheus',
          strength: 'authoritative',
          observedAt,
          summary: 'Usable headroom is below policy',
        }],
        fresh: true,
        compatibilityReviewed: true,
        readOnly: true,
      },
      {
        id: 'inspect-osd',
        conditionCode: 'osd-degraded',
        providerId: 'rook-ceph',
        title: 'Inspect OSD health',
        explanation: 'Daemon recovery belongs to the native administration surface.',
        surface: 'ceph-dashboard',
        dashboardDestination: 'osds',
        runbookUrl: 'https://docs.example.test/ceph/osds',
        prerequisites: ['Authenticate separately'],
        risks: ['Daemon administration can affect availability'],
        escalation: 'storage-specialist',
        evidence: [{
          source: 'ceph-health',
          strength: 'authoritative',
          observedAt,
          summary: 'An OSD is down',
        }],
        fresh: false,
        compatibilityReviewed: false,
        readOnly: true,
      },
    ],
  }
}

describe('ProviderComparisonPanel', () => {
  it('renders criteria, exact profile, limitations, and operational surfaces without a score', () => {
    render(<ProviderComparisonPanel comparison={comparison()} />)
    const panel = screen.getByTestId('provider-comparison-panel')
    expect(within(panel).getByText('Rook / Ceph')).toBeInTheDocument()
    expect(within(panel).getByText(/rook-ceph 20.2.1/)).toBeInTheDocument()
    expect(within(panel).getByText('eligible')).toBeInTheDocument()
    expect(within(panel).getByText('requires snapshot.create')).toBeInTheDocument()
    expect(within(panel).getByText(/were not ranked against each other/)).toBeInTheDocument()
    expect(within(panel).getByText(/ceph.pool.create: Highland/)).toBeInTheDocument()
    expect(within(panel).getByText(/random-read-iops: 1,200 iops/)).toBeInTheDocument()
    expect(within(panel).queryByText(/score/i)).toHaveTextContent('does not calculate an opaque provider score')
  })

  it('has loading, error, and empty states', () => {
    const { rerender } = render(<ProviderComparisonPanel isLoading />)
    expect(screen.getByTestId('guidance-skeleton')).toBeInTheDocument()
    rerender(<ProviderComparisonPanel error={new Error('comparison denied')} />)
    expect(screen.getByRole('alert')).toHaveTextContent('comparison denied')
    rerender(<ProviderComparisonPanel comparison={{ assessments: [], policy: {}, observedAt }} />)
    expect(screen.getByText('No comparable storage candidates')).toBeInTheDocument()
  })
})

describe('RemediationGuidancePanel', () => {
  it('shows action boundaries, prerequisites, risks, evidence, and compatibility state without executing', () => {
    const resolver = vi.fn((destination: string) =>
      destination === 'osds' ? 'https://ceph.example.test/#/osd' : undefined,
    )
    render(
      <RemediationGuidancePanel
        result={remediations()}
        resolveDashboardDestination={resolver}
      />,
    )
    const panel = screen.getByTestId('remediation-guidance-panel')
    expect(within(panel).getByText('Review Highland workflow: volume.expand')).toBeInTheDocument()
    expect(within(panel).getByText('StorageClass expansion is enabled')).toBeInTheDocument()
    expect(within(panel).getByText('Usable headroom is below policy · prometheus · authoritative ·', { exact: false })).toBeInTheDocument()
    expect(within(panel).getByText('Evidence is stale or incomplete')).toBeInTheDocument()
    expect(within(panel).getByText('Version compatibility is not reviewed')).toBeInTheDocument()
    expect(within(panel).getByRole('link', { name: /Open Ceph Dashboard/ })).toHaveAttribute(
      'href',
      'https://ceph.example.test/#/osd',
    )
    expect(within(panel).getByRole('link', { name: /Open runbook/ })).toHaveAttribute(
      'href',
      'https://docs.example.test/ceph/osds',
    )
    expect(within(panel).queryByRole('button')).not.toBeInTheDocument()
  })

  it('does not render unsafe resolved destinations', () => {
    render(
      <RemediationGuidancePanel
        result={remediations()}
        resolveDashboardDestination={() => 'javascript:alert(1)'}
      />,
    )
    expect(screen.queryByRole('link', { name: /Open Ceph Dashboard/ })).not.toBeInTheDocument()
  })

  it('has loading, error, and empty states', () => {
    const { rerender } = render(<RemediationGuidancePanel isLoading />)
    expect(screen.getByTestId('guidance-skeleton')).toBeInTheDocument()
    rerender(<RemediationGuidancePanel error={new Error('guidance denied')} />)
    expect(screen.getByRole('alert')).toHaveTextContent('guidance denied')
    rerender(<RemediationGuidancePanel result={{ recommendations: [] }} />)
    expect(screen.getByText('No reviewed remediation guidance')).toBeInTheDocument()
  })
})
