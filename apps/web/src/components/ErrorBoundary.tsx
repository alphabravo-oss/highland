import { Component, type ErrorInfo, type ReactNode } from 'react'

type Props = {
  children: ReactNode
  /** When this value changes (e.g. the route path), the boundary resets. */
  resetKey?: unknown
  fallback?: ReactNode
}

type State = {
  error: Error | null
  resetKey: unknown
}

/**
 * Catches render/lifecycle errors in the routed page tree so a single crashing
 * component shows a contained fallback instead of white-screening the whole app.
 * The surrounding shell (nav/topbar) stays interactive, and navigating to a new
 * route (resetKey change) clears the error.
 *
 * Strings are intentionally hardcoded English — an error boundary must render
 * even if i18n or context is the thing that failed.
 */
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null, resetKey: this.props.resetKey }

  static getDerivedStateFromError(error: Error): Partial<State> {
    return { error }
  }

  static getDerivedStateFromProps(props: Props, state: State): Partial<State> | null {
    if (props.resetKey !== state.resetKey) {
      return { error: null, resetKey: props.resetKey }
    }
    return null
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // eslint-disable-next-line no-console
    console.error('Highland UI error boundary caught an error:', error, info.componentStack)
  }

  // A failed dynamic import() is cached by React as a rejected lazy, so simply
  // re-rendering the same lazy component re-throws — the only recovery is a full
  // reload (which re-fetches the chunk, e.g. after a redeploy changed hashes).
  private isChunkError(): boolean {
    const err = this.state.error
    const msg = err?.message ?? ''
    return (
      err?.name === 'ChunkLoadError' ||
      /dynamically imported module|importing a module script failed|failed to fetch dynamically/i.test(
        msg,
      )
    )
  }

  private reset = () => {
    if (this.isChunkError()) {
      window.location.reload()
      return
    }
    this.setState({ error: null })
  }

  render() {
    if (!this.state.error) return this.props.children
    if (this.props.fallback !== undefined) return this.props.fallback

    return (
      <div className="flex min-h-[50vh] items-center justify-center p-6" role="alert">
        <div className="max-w-md rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-6 text-center shadow-[var(--shadow-md)]">
          <h2 className="text-lg font-semibold">Something went wrong</h2>
          <p className="mt-2 text-sm text-[var(--color-muted-foreground)]">
            This page hit an unexpected error. You can try again or reload the app; your session is
            unaffected.
          </p>
          {this.state.error.message ? (
            <pre className="mt-3 max-h-32 overflow-auto rounded bg-[var(--color-muted)] p-2 text-left text-xs text-[var(--color-muted-foreground)]">
              {this.state.error.message}
            </pre>
          ) : null}
          <div className="mt-4 flex justify-center gap-2">
            <button
              type="button"
              onClick={this.reset}
              className="inline-flex h-9 items-center rounded-md bg-[var(--color-primary)] px-4 text-sm font-medium text-[var(--color-primary-foreground)]"
            >
              Try again
            </button>
            <button
              type="button"
              onClick={() => window.location.reload()}
              className="inline-flex h-9 items-center rounded-md border border-[var(--color-border)] px-4 text-sm font-medium"
            >
              Reload
            </button>
          </div>
        </div>
      </div>
    )
  }
}
