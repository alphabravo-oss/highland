import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import type { OperationPlan } from '@/api/storage/types'
import { OperationApprovalDialog, OperationSafetyStatus } from './StorageOperationsPage'

function plan(overrides: Partial<OperationPlan> = {}): OperationPlan {
  return {
    action: {
      id: 'longhorn-volume-detach',
      capability: 'longhorn.volume.detach',
      minimumRole: 'operator',
      providerKind: 'longhorn',
      risk: 'high',
      confirmation: 'typed-name',
      featureFlag: 'storage.writes.enabled',
      preflightChecks: [],
      auditAction: 'longhorn_volume_detach',
    },
    providerId: 'longhorn',
    target: { kind: 'LonghornVolume', name: 'data' },
    resources: [],
    checks: [],
    warnings: ['Active workloads may lose access.'],
    blastRadius: 'one attached Longhorn volume',
    hash: 'hash',
    challenge: 'challenge',
    challengeExpiresAt: '2026-07-16T01:00:00Z',
    observedAt: '2026-07-16T00:00:00Z',
    ...overrides,
  }
}

describe('OperationApprovalDialog', () => {
  it('requires warning acknowledgement and exact typed name for disruptive actions', () => {
    const onConfirm = vi.fn()
    let typedName = ''
    let warningsAcknowledged = false
    const setTypedName = vi.fn((value: string) => { typedName = value })
    const setWarningsAcknowledged = vi.fn((value: boolean) => { warningsAcknowledged = value })
    const props = {
      plan: plan(),
      open: true,
      onOpenChange: vi.fn(),
      typedName,
      setTypedName,
      warningsAcknowledged,
      setWarningsAcknowledged,
      onConfirm,
      submitting: false,
    }
    const { rerender } = render(<OperationApprovalDialog {...props} />)
    const confirm = screen.getByTestId('operation-confirm-submit')
    expect(confirm).toBeDisabled()

    fireEvent.change(screen.getByTestId('typed-operation-confirmation'), { target: { value: 'wrong' } })
    expect(setTypedName).toHaveBeenCalledWith('wrong')
    rerender(<OperationApprovalDialog {...props} typedName="wrong" />)
    expect(confirm).toBeDisabled()

    fireEvent.click(screen.getByRole('checkbox'))
    expect(setWarningsAcknowledged).toHaveBeenCalledWith(true)
    rerender(<OperationApprovalDialog {...props} typedName="data" warningsAcknowledged />)
    expect(screen.getByTestId('operation-confirm-submit')).toBeEnabled()
    fireEvent.click(screen.getByTestId('operation-confirm-submit'))
    expect(onConfirm).toHaveBeenCalledOnce()
  })

  it('still requires a modal for low-risk summary confirmation', () => {
    const onConfirm = vi.fn()
    const summaryPlan = plan({
      action: { ...plan().action, id: 'longhorn-volume-backup', risk: 'low', confirmation: 'summary' },
      warnings: [],
    })
    render(<OperationApprovalDialog
      plan={summaryPlan}
      open
      onOpenChange={vi.fn()}
      typedName=""
      setTypedName={vi.fn()}
      warningsAcknowledged={false}
      setWarningsAcknowledged={vi.fn()}
      onConfirm={onConfirm}
      submitting={false}
    />)
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.queryByTestId('typed-operation-confirmation')).not.toBeInTheDocument()
    expect(screen.getByTestId('operation-confirm-submit')).toBeEnabled()
  })
})

describe('OperationSafetyStatus', () => {
  it('keeps the lock icon beside the enabled-state heading', () => {
    render(<OperationSafetyStatus writesEnabled nativeActions={[]} portableProviderAllowed={false} />)
    const heading = screen.getByRole('heading', { name: 'Changes are enabled' })
    expect(heading.querySelector('svg')).not.toBeNull()
    expect(screen.getByText('Every change still requires a fresh plan, dependency review, role authorization, and explicit confirmation.')).toBeVisible()
  })
})
