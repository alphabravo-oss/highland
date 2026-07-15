import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { optimisticPatch, optimisticRemove, pick } from './optimistic'

type Row = { name?: string; value?: string; extra?: number }

describe('optimisticRemove', () => {
  it('removes the row from the list and drops its detail entry on mutate', async () => {
    const qc = new QueryClient()
    qc.setQueryData(['volumes'], [{ name: 'a' }, { name: 'b' }])
    qc.setQueryData(['volumes', 'a'], { name: 'a' })

    const frag = optimisticRemove<Row, Row>(qc, 'volumes', (v) => v.name, ['volumes'])
    const ctx = await frag.onMutate({ name: 'a' })

    expect(qc.getQueryData(['volumes'])).toEqual([{ name: 'b' }])
    expect(qc.getQueryData(['volumes', 'a'])).toBeUndefined()
    expect(ctx.prevList).toEqual([{ name: 'a' }, { name: 'b' }])
  })

  it('rolls back list and detail on error and refetches for truth', async () => {
    const qc = new QueryClient()
    qc.setQueryData(['volumes'], [{ name: 'a' }, { name: 'b' }])
    qc.setQueryData(['volumes', 'a'], { name: 'a' })
    const invalidate = vi.spyOn(qc, 'invalidateQueries').mockResolvedValue()

    const frag = optimisticRemove<Row, Row>(qc, 'volumes', (v) => v.name, ['volumes'])
    const ctx = await frag.onMutate({ name: 'a' })
    frag.onError(new Error('boom'), { name: 'a' }, ctx)

    expect(qc.getQueryData(['volumes'])).toEqual([{ name: 'a' }, { name: 'b' }])
    expect(qc.getQueryData(['volumes', 'a'])).toEqual({ name: 'a' })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['volumes'] })
  })

  it('onError with no snapshot (undefined ctx) is a harmless no-op', () => {
    const qc = new QueryClient()
    const set = vi.spyOn(qc, 'setQueryData')
    const frag = optimisticRemove<Row, Row>(qc, 'volumes', (v) => v.name, ['volumes'])
    expect(() => frag.onError(new Error('x'), { name: 'a' }, undefined)).not.toThrow()
    expect(set).not.toHaveBeenCalled()
  })

  it('cancels in-flight queries before writing, and invalidates each key on success', async () => {
    const qc = new QueryClient()
    const cancel = vi.spyOn(qc, 'cancelQueries').mockResolvedValue()
    const invalidate = vi.spyOn(qc, 'invalidateQueries').mockResolvedValue()

    const frag = optimisticRemove<Row, Row>(qc, 'volumes', (v) => v.name, ['volumes', 'dashboard'])
    await frag.onMutate({ name: 'a' })
    expect(cancel).toHaveBeenCalledWith({ queryKey: ['volumes'] })

    frag.onSuccess()
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['volumes'] })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['dashboard'] })
  })

  it('refetchOnSuccess:false skips the immediate success refetch (async-delete types)', async () => {
    const qc = new QueryClient()
    const invalidate = vi.spyOn(qc, 'invalidateQueries').mockResolvedValue()
    const frag = optimisticRemove<Row, Row>(qc, 'volumes', (v) => v.name, ['volumes'], {
      refetchOnSuccess: false,
    })
    frag.onSuccess()
    expect(invalidate).not.toHaveBeenCalled()
  })
})

describe('optimisticPatch', () => {
  it('merges only the patched fields, preserving others', async () => {
    const qc = new QueryClient()
    qc.setQueryData(['settings'], [{ name: 's', value: 'old', extra: 1 }])
    qc.setQueryData(['settings', 's'], { name: 's', value: 'old', extra: 1 })

    const frag = optimisticPatch<Row, { name: string; value: string }>(
      qc,
      'settings',
      (v) => v.name,
      (v) => ({ value: v.value }),
      ['settings'],
    )
    await frag.onMutate({ name: 's', value: 'new' })

    expect(qc.getQueryData(['settings'])).toEqual([{ name: 's', value: 'new', extra: 1 }])
    expect(qc.getQueryData(['settings', 's'])).toEqual({ name: 's', value: 'new', extra: 1 })
  })

  it('rolls back the merged state on error and reconciles on settle', async () => {
    const qc = new QueryClient()
    qc.setQueryData(['settings'], [{ name: 's', value: 'old', extra: 1 }])
    qc.setQueryData(['settings', 's'], { name: 's', value: 'old', extra: 1 })
    const invalidate = vi.spyOn(qc, 'invalidateQueries').mockResolvedValue()

    const frag = optimisticPatch<Row, { name: string; value: string }>(
      qc,
      'settings',
      (v) => v.name,
      (v) => ({ value: v.value }),
      ['settings'],
    )
    const ctx = await frag.onMutate({ name: 's', value: 'new' })
    frag.onError(new Error('boom'), { name: 's', value: 'new' }, ctx)

    expect(qc.getQueryData(['settings'])).toEqual([{ name: 's', value: 'old', extra: 1 }])
    expect(qc.getQueryData(['settings', 's'])).toEqual({ name: 's', value: 'old', extra: 1 })

    frag.onSettled()
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['settings'] })
  })
})

describe('pick', () => {
  it('keeps only the requested keys and drops undefined', () => {
    const body = { allowScheduling: true, disks: { a: 1 }, tags: ['x'], evictionRequested: undefined }
    expect(pick(body, ['allowScheduling', 'evictionRequested', 'tags'])).toEqual({
      allowScheduling: true,
      tags: ['x'],
    })
  })
})
