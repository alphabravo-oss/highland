import { Link } from 'react-router-dom'
import { Database, HardDrive, Layers3, RefreshCw } from 'lucide-react'
import { useStorageList } from '@/api/storage/hooks'
import { Button } from '@/components/ui/button'

type FootprintKind = 'classes' | 'claims' | 'volumes' | 'snapshots'

const items: Array<{ kind: FootprintKind; label: string; detail: string; icon: typeof Database }> = [
  { kind: 'classes', label: 'StorageClasses', detail: 'Provisioning policies', icon: Layers3 },
  { kind: 'claims', label: 'Claims', detail: 'Workload requests', icon: Database },
  { kind: 'volumes', label: 'PersistentVolumes', detail: 'Provisioned volumes', icon: HardDrive },
  { kind: 'snapshots', label: 'Snapshots', detail: 'Kubernetes restore points', icon: RefreshCw },
]

/** A consistent, bounded view of how Kubernetes consumes any CSI provider. */
export function ProviderWorkloadFootprint({ provider }: { provider: string }) {
  const classes = useStorageList<unknown>('classes', { provider, limit: 1 })
  const claims = useStorageList<unknown>('claims', { provider, limit: 1 })
  const volumes = useStorageList<unknown>('volumes', { provider, limit: 1 })
  const snapshots = useStorageList<unknown>('snapshots', { provider, limit: 1 })
  const queries = { classes, claims, volumes, snapshots }
  const loading = Object.values(queries).some((query) => query.isLoading)
  const failed = Object.values(queries).some((query) => query.isError)

  return <section aria-labelledby={`${provider}-workload-footprint`}>
    <div className="mb-3 flex items-end justify-between gap-3">
      <div>
        <h2 id={`${provider}-workload-footprint`} className="text-base font-semibold">Kubernetes consumption</h2>
        <p className="text-sm text-[var(--color-muted-foreground)]">The workload objects currently relying on this provider. Counts are provider-scoped.</p>
      </div>
      {failed ? <Button type="button" variant="ghost" size="sm" onClick={() => void Promise.all(Object.values(queries).map((query) => query.refetch()))}><RefreshCw size={14} /> Retry</Button> : null}
    </div>
    <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
      {items.map(({ kind, label, detail, icon: Icon }) => {
        const query = queries[kind]
        const value = query.isError ? 'Unavailable' : loading && !query.data ? '—' : String(query.data?.page.total ?? 0)
        return <Link key={kind} to={`/storage/${kind}?provider=${encodeURIComponent(provider)}`} className="group rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4 transition-colors hover:border-[var(--color-primary)]">
          <div className="flex items-center justify-between gap-3">
            <span className="flex items-center gap-2 text-xs font-medium text-[var(--color-muted-foreground)]"><Icon size={15} /> {label}</span>
            <span className="text-xl font-semibold tabular-nums">{value}</span>
          </div>
          <p className="mt-2 text-xs text-[var(--color-muted-foreground)]">{query.isError ? 'This inventory source could not be read.' : detail}</p>
        </Link>
      })}
    </div>
  </section>
}
