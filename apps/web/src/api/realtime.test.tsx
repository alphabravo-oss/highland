import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { RealtimeProvider, useSseConnected } from './realtime'

// Mock auth so the provider thinks we're logged in.
vi.mock('@/auth/AuthContext', () => ({
  useAuth: () => ({ user: { username: 'admin', role: 'admin' }, refresh: vi.fn() }),
}))

// Minimal fake EventSource we can drive from the test.
class FakeEventSource {
  static instances: FakeEventSource[] = []
  static CLOSED = 2
  url: string
  readyState = 0
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  private listeners: Record<string, ((e: MessageEvent) => void)[]> = {}
  constructor(url: string) {
    this.url = url
    FakeEventSource.instances.push(this)
  }
  addEventListener(type: string, cb: (e: MessageEvent) => void) {
    ;(this.listeners[type] ??= []).push(cb)
  }
  emit(type: string, data: string) {
    for (const cb of this.listeners[type] ?? []) cb({ data } as MessageEvent)
  }
  close = vi.fn()
}

describe('RealtimeProvider', () => {
  afterEach(() => {
    FakeEventSource.instances = []
    vi.unstubAllGlobals()
  })

  it('opens the stream and invalidates the keys from a change frame', () => {
    vi.stubGlobal('EventSource', FakeEventSource as unknown as typeof EventSource)
    const qc = new QueryClient()
    const invalidate = vi.spyOn(qc, 'invalidateQueries').mockResolvedValue()

    render(
      <QueryClientProvider client={qc}>
        <RealtimeProvider>
          <div />
        </RealtimeProvider>
      </QueryClientProvider>,
    )

    const es = FakeEventSource.instances[0]!
    expect(es.url).toBe('/api/v1/events/stream')

    es.emit('change', JSON.stringify({ keys: ['volumes', 'dashboard'], name: 'pvc-x' }))
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['volumes'] })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['dashboard'] })
  })

  it('treats __all__ as invalidate-everything', () => {
    vi.stubGlobal('EventSource', FakeEventSource as unknown as typeof EventSource)
    const qc = new QueryClient()
    const invalidate = vi.spyOn(qc, 'invalidateQueries').mockResolvedValue()

    render(
      <QueryClientProvider client={qc}>
        <RealtimeProvider>
          <div />
        </RealtimeProvider>
      </QueryClientProvider>,
    )
    FakeEventSource.instances[0]!.emit('change', JSON.stringify({ keys: ['__all__'] }))
    expect(invalidate).toHaveBeenCalledWith() // no args = invalidate all
  })

  it('flips the useSseConnected signal true on stream open (drives adaptive polling)', () => {
    vi.stubGlobal('EventSource', FakeEventSource as unknown as typeof EventSource)
    const qc = new QueryClient()
    function Probe() {
      return <span data-testid="sig">{String(useSseConnected())}</span>
    }
    const { getByTestId } = render(
      <QueryClientProvider client={qc}>
        <RealtimeProvider>
          <Probe />
        </RealtimeProvider>
      </QueryClientProvider>,
    )
    expect(getByTestId('sig').textContent).toBe('false')
    act(() => {
      FakeEventSource.instances[0]!.onopen?.()
    })
    expect(getByTestId('sig').textContent).toBe('true')
  })
})
