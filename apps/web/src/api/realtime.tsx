import { useEffect, useSyncExternalStore, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useAuth } from '@/auth/AuthContext'

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
        const { keys } = JSON.parse(e.data) as { keys?: string[] }
        for (const k of keys ?? []) {
          if (k === '__all__') void qc.invalidateQueries()
          else void qc.invalidateQueries({ queryKey: [k] })
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
