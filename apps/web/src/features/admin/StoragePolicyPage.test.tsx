import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { storagePolicyPreset, updatePolicyField } from '@/api/policy'
import type { StoragePolicyPlan, StorageWritePolicy } from '@/api/policy'
import { PolicyConfirmationDialog, updatePortableProvider } from './StoragePolicyPage'

const disabled: StorageWritePolicy = {
  acceptNewOperations: false,
  portableKubernetesWrites: false,
  portableKubernetesProviderIds: [],
  longhornWrites: false,
  rookCephWrites: false,
  allowCephStorageClassDelete: false,
  allowCephPoolDelete: false,
}

function plan(overrides: Partial<StoragePolicyPlan> = {}): StoragePolicyPlan {
  return {
    current: disabled,
    requested: { ...disabled, acceptNewOperations: true, longhornWrites: true },
    effective: { ...disabled, acceptNewOperations: true, longhornWrites: true },
    ceiling: {
      portableKubernetesWrites: false,
      longhornWrites: true,
      rookCephWrites: false,
      allowCephStorageClassDelete: false,
      allowCephPoolDelete: false,
    },
    conditions: [],
    resourceVersion: '1',
    policyGeneration: 1,
    broadening: true,
    enablesCephPoolDelete: false,
    impact: { actionIds: ['longhorn-volume-attach'], roles: ['operator'], addedPortableProviderIds: [], removedPortableProviderIds: [] },
    inFlightOperations: 0,
    clusterIdentity: 'lab-cluster',
    actor: 'admin',
    requestId: 'request-1',
    hash: 'hash',
    challenge: 'challenge',
    challengeExpiresAt: '2026-07-16T13:00:00Z',
    observedAt: '2026-07-16T12:00:00Z',
    ...overrides,
  }
}

describe('PolicyConfirmationDialog', () => {
  it('blocks policy broadening until every typed confirmation is exact', () => {
    const onApply = vi.fn()
    const base = {
      plan: plan(),
      open: true,
      onOpenChange: vi.fn(),
      clusterIdentity: '',
      setClusterIdentity: vi.fn(),
      enablePhrase: '',
      setEnablePhrase: vi.fn(),
      cephPhrase: '',
      setCephPhrase: vi.fn(),
      acknowledged: false,
      setAcknowledged: vi.fn(),
      applying: false,
      onApply,
    }
    const { rerender } = render(<PolicyConfirmationDialog {...base} />)
    expect(screen.getByTestId('apply-storage-policy')).toBeDisabled()
    fireEvent.change(screen.getByTestId('policy-cluster-confirmation'), { target: { value: 'lab-cluster' } })
    expect(base.setClusterIdentity).toHaveBeenCalledWith('lab-cluster')
    rerender(<PolicyConfirmationDialog {...base} clusterIdentity="lab-cluster" enablePhrase="ENABLE STORAGE CHANGES" acknowledged />)
    expect(screen.getByTestId('apply-storage-policy')).toBeEnabled()
  })

  it('adds an independent phrase for enabling Ceph pool deletion', () => {
    const basePlan = plan({ enablesCephPoolDelete: true })
    const props = {
      plan: basePlan,
      open: true,
      onOpenChange: vi.fn(),
      clusterIdentity: 'lab-cluster',
      setClusterIdentity: vi.fn(),
      enablePhrase: 'ENABLE STORAGE CHANGES',
      setEnablePhrase: vi.fn(),
      cephPhrase: '',
      setCephPhrase: vi.fn(),
      acknowledged: true,
      setAcknowledged: vi.fn(),
      applying: false,
      onApply: vi.fn(),
    }
    const { rerender } = render(<PolicyConfirmationDialog {...props} />)
    expect(screen.getByTestId('apply-storage-policy')).toBeDisabled()
    rerender(<PolicyConfirmationDialog {...props} cephPhrase="ENABLE CEPH POOL DELETE" />)
    expect(screen.getByTestId('apply-storage-policy')).toBeEnabled()
  })

  it('uses summary confirmation when narrowing policy', () => {
    render(<PolicyConfirmationDialog
      plan={plan({ broadening: false, requested: disabled, effective: disabled, impact: { actionIds: [], roles: [], addedPortableProviderIds: [], removedPortableProviderIds: [] } })}
      open
      onOpenChange={vi.fn()}
      clusterIdentity=""
      setClusterIdentity={vi.fn()}
      enablePhrase=""
      setEnablePhrase={vi.fn()}
      cephPhrase=""
      setCephPhrase={vi.fn()}
      acknowledged={false}
      setAcknowledged={vi.fn()}
      applying={false}
      onApply={vi.fn()}
    />)
    expect(screen.queryByTestId('policy-cluster-confirmation')).not.toBeInTheDocument()
    expect(screen.getByTestId('apply-storage-policy')).toBeEnabled()
  })
})

describe('storage policy scope model', () => {
  it('builds a Longhorn-native-only draft without cross-provider or Ceph access', () => {
    expect(storagePolicyPreset('longhorn-native-only')).toEqual({
      ...disabled,
      acceptNewOperations: true,
      longhornWrites: true,
    })
  })

  it('scopes the Longhorn PVC preset to Longhorn only', () => {
    expect(storagePolicyPreset('longhorn-full')).toEqual({
      ...disabled,
      acceptNewOperations: true,
      portableKubernetesWrites: true,
      portableKubernetesProviderIds: ['longhorn'],
      longhornWrites: true,
    })
  })

  it('enables and disables the portable family from explicit provider selection', () => {
    const longhorn = updatePortableProvider({ ...disabled, acceptNewOperations: true }, 'longhorn', true)
    expect(longhorn.portableKubernetesWrites).toBe(true)
    expect(longhorn.portableKubernetesProviderIds).toEqual(['longhorn'])
    const both = updatePortableProvider(longhorn, 'rook-ceph', true)
    expect(both.portableKubernetesProviderIds).toEqual(['longhorn', 'rook-ceph'])
    const none = updatePortableProvider(updatePortableProvider(both, 'longhorn', false), 'rook-ceph', false)
    expect(none.portableKubernetesWrites).toBe(false)
    expect(none.portableKubernetesProviderIds).toEqual([])
  })

  it('clears child scopes when a parent gate is disabled', () => {
    const enabled: StorageWritePolicy = {
      acceptNewOperations: true,
      portableKubernetesWrites: true,
      portableKubernetesProviderIds: ['longhorn'],
      longhornWrites: true,
      rookCephWrites: true,
      allowCephStorageClassDelete: true,
      allowCephPoolDelete: true,
    }
    expect(updatePolicyField(enabled, 'rookCephWrites', false)).toMatchObject({
      acceptNewOperations: true,
      longhornWrites: true,
      rookCephWrites: false,
      allowCephStorageClassDelete: false,
      allowCephPoolDelete: false,
    })
    expect(updatePolicyField(enabled, 'acceptNewOperations', false)).toEqual(disabled)
  })
})
