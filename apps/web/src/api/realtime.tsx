import { useEffect, useSyncExternalStore, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useAuth } from '@/auth/AuthContext'
import type { Benchmark, BenchmarkPage } from '@/api/hooks'
import type { StorageOperation } from '@/api/storage/types'

// Module-level connection signal so any hook can adapt (e.g. slow the polling
// fallback when the stream is healthy). Exposed via useSseConnected().
let connected = false
const listeners = new Set<() => void>()
function setConnected(v: boolean) {
  if (v !== connected) {
    connected = v
    for (const l of listeners) l()
  }
}

export function useSseConnected(): boolean {
  return useSyncExternalStore(
    (cb) => {
      listeners.add(cb)
      return () => listeners.delete(cb)
    },
    () => connected,
    () => false,
  )
}

/**
 * Opens a Server-Sent Events stream to the BFF while authenticated and, on each
 * change frame, invalidates the affected TanStack Query keys so the UI refreshes
 * the instant the cluster changes. Auth rides the same-origin session cookie
 * (EventSource can't set headers; the GET passes CSRF). Purely additive — if the
 * stream drops, the existing query polling still refreshes everything.
 */
export function RealtimeProvider({ children }: { children: ReactNode }) {
  const { user, refresh } = useAuth()
  const qc = useQueryClient()
  const username = user?.username

  useEffect(() => {
    if (!username) return
    let es: EventSource | null = null
    let retryTimer: number | null = null
    let backoff = 3000
    let unmounted = false

    const onChange = (e: MessageEvent) => {
      try {
        const { keys, version, eventType, providerId, namespace, resource, name, entity } = JSON.parse(e.data) as {
          keys?: string[]
          version?: number
          eventType?: string
          providerId?: string
          namespace?: string
          resource?: string
          name?: string
          entity?: unknown
        }
        const handled = new Set<string>()
        if (eventType?.startsWith('benchmark.') && name) {
          const deleted = eventType === 'benchmark.deleted'
          const benchmark = entity as Benchmark | undefined
          qc.setQueriesData<BenchmarkPage>({ queryKey: ['benchmarks'] }, (current) => {
            if (!current) return current
            const data = current.data.filter((item) => item.name !== name)
            if (!deleted && benchmark && current.page.total <= current.page.limit) data.unshift(benchmark)
            else if (!deleted && benchmark) {
              const existing = current.data.findIndex((item) => item.name === name)
              if (existing >= 0) data.splice(existing, 0, benchmark)
            }
            const total = Math.max(0, current.page.total + (deleted ? -1 : current.data.some((item) => item.name === name) ? 0 : 1))
            return { ...current, data: data.slice(0, current.page.limit), page: { ...current.page, total } }
          })
          if (!deleted && benchmark) qc.setQueryData(['benchmarks', name], benchmark)
          else qc.removeQueries({ queryKey: ['benchmarks', name], exact: true })
          handled.add('benchmarks')
        }
        if (eventType?.startsWith('storage.operation.') && name) {
          const deleted = eventType === 'storage.operation.deleted'
          const operation = entity as StorageOperation | undefined
          qc.setQueriesData<{ data: StorageOperation[] }>(
            { predicate: (query) => query.queryKey[0] === 'storage' && query.queryKey[1] === 'operations' && typeof query.queryKey[2] === 'object' },
            (current) => {
              if (!current) return current
              const data = current.data.filter((item) => item.name !== name)
              if (!deleted && operation) data.unshift(operation)
              return { ...current, data }
            },
          )
          if (!deleted && operation) qc.setQueryData(['storage', 'operations', name], operation)
          else qc.removeQueries({ queryKey: ['storage', 'operations', name], exact: true })
        }
        if (eventType === 'policy.updated') {
          void qc.invalidateQueries({ queryKey: ['admin-storage-policy'] })
          void qc.invalidateQueries({ queryKey: ['admin-storage-policy-history'] })
          void qc.invalidateQueries({ queryKey: ['storage'] })
        }
        for (const k of keys ?? []) {
          if (handled.has(k)) continue
          if (k === '__all__') void qc.invalidateQueries()
          else void qc.invalidateQueries({ queryKey: [k] })
        }
        if (version === 2 && resource) {
          void qc.invalidateQueries({
            predicate: (query) => {
              const key = query.queryKey
              if (key[0] !== 'storage') return false
              if (resource === 'operations') return key[1] === 'operations'
              const filters = key.find((part) => typeof part === 'object' && part !== null) as Record<string, string> | undefined
              const providerMatches = !providerId || !filters?.provider || filters.provider === providerId
              const namespaceMatches = !namespace || !filters?.namespace || filters.namespace === namespace
              if (!providerMatches || !namespaceMatches) return false
              if (key[1] === resource) return true
              if (key[1] === 'provider-resources') {
                return (!providerId || key[2] === providerId) && key[3] === resource
              }
              if (resource === 'claims' && key[1] === 'claims' && typeof key[2] === 'string') return true
              if (resource === 'volumes' && key[1] === 'volumes' && typeof key[2] === 'string') return true
              return false
            },
          })
        }
      } catch {
        // ignore a malformed frame
      }
    }

    const open = () => {
      es = new EventSource('/api/v1/events/stream', { withCredentials: true })
      es.onopen = () => {
        backoff = 3000
        setConnected(true)
      }
      es.addEventListener('change', onChange as EventListener)
      es.onerror = () => {
        setConnected(false)
        // A non-terminal error (readyState CONNECTING) auto-reconnects natively.
        if (!es || es.readyState !== EventSource.CLOSED) return
        // Terminal close (server 4xx/5xx/restart): re-check auth — a genuine
        // expiry flips user=null and unmounts us — then reconnect with capped
        // backoff so a valid session recovers instead of degrading to polling.
        es.close()
        void refresh()
        if (unmounted) return
        retryTimer = window.setTimeout(open, backoff)
        backoff = Math.min(backoff * 2, 30000)
      }
    }

    open()
    return () => {
      unmounted = true
      if (retryTimer) window.clearTimeout(retryTimer)
      setConnected(false)
      es?.close()
    }
  }, [username, qc, refresh])

  return <>{children}</>
}
