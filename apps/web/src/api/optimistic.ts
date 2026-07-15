import type { QueryClient } from '@tanstack/react-query'

// Longhorn list queries return bare T[] and every resource is keyed by `.name`,
// which is also the detail query key (['<list>', name]). We match on `.name`.
type Named = { name?: string }

type Ctx<T> = { prevList?: T[]; prevDetail?: T; name?: string }

/** Keep only the given keys from an object, dropping undefined values. */
export function pick<T extends object>(obj: T, keys: Array<keyof T>): Partial<T> {
  const out: Partial<T> = {}
  for (const k of keys) {
    if (obj[k] !== undefined) out[k] = obj[k]
  }
  return out
}

/**
 * Optimistic list-row removal, returned as useMutation fragments. `getName`
 * derives the row key from the mutation variables. onMutate cancels in-flight
 * polls (so a refetch can't clobber the write), snapshots + removes the row and
 * drops its detail entry; onError rolls back; onSettled invalidates every key —
 * it runs on BOTH success and rollback, so a server-rejected delete re-fetches.
 */
export function optimisticRemove<T extends Named, V>(
  qc: QueryClient,
  listKey: string,
  getName: (vars: V) => string | undefined,
  invalidate: string[],
  // Longhorn deletes are async: the DELETE returns 2xx while the resource lingers
  // in a `deleting` state, so an immediate success-refetch resurrects the row we
  // just optimistically hid (a flicker). For those types pass false and let the
  // periodic poll reconcile once the resource is truly gone. On ERROR we always
  // refetch to restore server truth.
  { refetchOnSuccess = true }: { refetchOnSuccess?: boolean } = {},
) {
  const invalidateAll = () => {
    for (const k of invalidate) void qc.invalidateQueries({ queryKey: [k] })
  }
  return {
    onMutate: async (vars: V): Promise<Ctx<T>> => {
      const name = getName(vars)
      await qc.cancelQueries({ queryKey: [listKey] })
      const prevList = qc.getQueryData<T[]>([listKey])
      const prevDetail = qc.getQueryData<T>([listKey, name])
      qc.setQueryData<T[]>([listKey], (old) => old?.filter((r) => r.name !== name))
      qc.removeQueries({ queryKey: [listKey, name], exact: true })
      return { prevList, prevDetail, name }
    },
    onError: (_e: unknown, _v: V, ctx?: Ctx<T>) => {
      if (ctx?.prevList) qc.setQueryData([listKey], ctx.prevList)
      if (ctx?.prevDetail) qc.setQueryData([listKey, ctx.name], ctx.prevDetail)
      invalidateAll()
    },
    onSuccess: () => {
      if (refetchOnSuccess) invalidateAll()
    },
  }
}

/**
 * Optimistic field patch on one list row (and its detail entry). `getPatch` must
 * return exactly the fields that change — never a full server object with stale
 * computed fields.
 */
export function optimisticPatch<T extends Named, V>(
  qc: QueryClient,
  listKey: string,
  getName: (vars: V) => string | undefined,
  getPatch: (vars: V) => Partial<T>,
  invalidate: string[],
) {
  return {
    onMutate: async (vars: V): Promise<Ctx<T>> => {
      const name = getName(vars)
      const patch = getPatch(vars)
      await qc.cancelQueries({ queryKey: [listKey] })
      const prevList = qc.getQueryData<T[]>([listKey])
      const prevDetail = qc.getQueryData<T>([listKey, name])
      qc.setQueryData<T[]>([listKey], (old) =>
        old?.map((r) => (r.name === name ? { ...r, ...patch } : r)),
      )
      qc.setQueryData<T>([listKey, name], (old) => (old ? { ...old, ...patch } : old))
      return { prevList, prevDetail, name }
    },
    onError: (_e: unknown, _v: V, ctx?: Ctx<T>) => {
      if (ctx?.prevList) qc.setQueryData([listKey], ctx.prevList)
      if (ctx?.prevDetail) qc.setQueryData([listKey, ctx.name], ctx.prevDetail)
    },
    onSettled: () => {
      for (const k of invalidate) void qc.invalidateQueries({ queryKey: [k] })
    },
  }
}
