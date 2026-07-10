import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { X } from 'lucide-react'
import { cn } from '@/lib/utils'

type ToastTone = 'default' | 'success' | 'danger' | 'warning'

type ToastItem = {
  id: string
  title: string
  description?: string
  tone: ToastTone
}

type ToastContextValue = {
  toast: (opts: { title: string; description?: string; tone?: ToastTone }) => void
  success: (title: string, description?: string) => void
  error: (title: string, description?: string) => void
}

const ToastContext = createContext<ToastContextValue | null>(null)

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([])

  const dismiss = useCallback((id: string) => {
    setItems((xs) => xs.filter((t) => t.id !== id))
  }, [])

  const toast = useCallback(
    (opts: { title: string; description?: string; tone?: ToastTone }) => {
      const id = `${Date.now()}-${Math.random().toString(36).slice(2, 7)}`
      const item: ToastItem = {
        id,
        title: opts.title,
        description: opts.description,
        tone: opts.tone ?? 'default',
      }
      setItems((xs) => [...xs.slice(-4), item])
      window.setTimeout(() => dismiss(id), 4200)
    },
    [dismiss],
  )

  const value = useMemo(
    () => ({
      toast,
      success: (title: string, description?: string) =>
        toast({ title, description, tone: 'success' }),
      error: (title: string, description?: string) =>
        toast({ title, description, tone: 'danger' }),
    }),
    [toast],
  )

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div
        className="pointer-events-none fixed bottom-4 right-4 z-[100] flex w-full max-w-sm flex-col gap-2 p-2"
        aria-live="polite"
      >
        {items.map((t) => (
          <div
            key={t.id}
            className={cn(
              'pointer-events-auto animate-toast-in rounded-lg border px-4 py-3 shadow-[var(--shadow-md)]',
              'bg-[var(--color-card)] text-[var(--color-card-foreground)]',
              t.tone === 'success' && 'border-emerald-500/40',
              t.tone === 'danger' && 'border-red-500/40',
              t.tone === 'warning' && 'border-amber-500/40',
            )}
          >
            <div className="flex items-start gap-2">
              <div className="min-w-0 flex-1">
                <p className="text-sm font-semibold">{t.title}</p>
                {t.description ? (
                  <p className="mt-0.5 text-xs text-[var(--color-muted-foreground)]">
                    {t.description}
                  </p>
                ) : null}
              </div>
              <button
                type="button"
                className="rounded p-0.5 text-[var(--color-muted-foreground)] hover:bg-[var(--color-accent)]"
                onClick={() => dismiss(t.id)}
                aria-label="Dismiss"
              >
                <X size={14} />
              </button>
            </div>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  )
}

export function useToast(): ToastContextValue {
  const ctx = useContext(ToastContext)
  if (!ctx) throw new Error('useToast within ToastProvider')
  return ctx
}
