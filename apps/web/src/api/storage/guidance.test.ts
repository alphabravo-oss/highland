import { describe, expect, it } from 'vitest'
import { storageGuidanceKeys } from './guidance'

describe('storage guidance query keys', () => {
  it('separates comparison policies and remediation scopes', () => {
    expect(storageGuidanceKeys.comparison({ requireSnapshot: true })).not.toEqual(
      storageGuidanceKeys.comparison({ requireSnapshot: false }),
    )
    expect(storageGuidanceKeys.remediations({ provider: 'rook-ceph' })).not.toEqual(
      storageGuidanceKeys.remediations({ provider: 'longhorn' }),
    )
  })
})
