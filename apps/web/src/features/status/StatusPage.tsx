import type { ReactNode } from 'react'
import { Activity, CheckCircle2, Clock3, Database, ExternalLink, RefreshCw, ShieldCheck, XCircle } from 'lucide-react'
import { Link } from 'react-router-dom'
import { useStatus } from '@/api/hooks'
import type { ProviderDescriptor, StorageCondition } from '@/api/storage/types'
import { useAuth } from '@/auth/AuthContext'
import { HighlandLogo } from '@/components/layout/HighlandLogo'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TBody, TD, TH, THead, TR } from '@/components/ui/table'
import { useAppTranslation } from '@/i18n/useAppTranslation'

function Row({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-3 border-b border-[var(--color-border)] py-2.5 text-sm last:border-0">
      <span className="text-[var(--color-muted-foreground)]">{label}</span>
      <span className="min-w-0 max-w-[65%] text-right font-medium tabular-nums">{value}</span>
    </div>
  )
}

function StatusPill({ ok, okLabel, badLabel }: { ok: boolean; okLabel: string; badLabel: string }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      {ok ? <CheckCircle2 size={15} className="text-emerald-600" aria-hidden /> : <XCircle size={15} className="text-red-600" aria-hidden />}
      <span>{ok ? okLabel : badLabel}</span>
    </span>
  )
}

function Fact({ icon: Icon, label, value, detail }: { icon: typeof Activity; label: string; value: ReactNode; detail: string }) {
  return (
    <Card>
      <CardContent className="flex items-start gap-3 py-4">
        <span className="mt-0.5 rounded-md bg-[var(--color-primary)]/10 p-2 text-[var(--color-primary)]"><Icon size={18} aria-hidden /></span>
        <div className="min-w-0">
          <div className="text-xs font-semibold uppercase tracking-wide text-[var(--color-muted-foreground)]">{label}</div>
          <div className="mt-1 text-lg font-semibold tracking-tight">{value}</div>
          <div className="mt-0.5 truncate text-xs text-[var(--color-muted-foreground)]" title={detail}>{detail}</div>
        </div>
      </CardContent>
    </Card>
  )
}

function formatTime(value?: string) {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

function providerMode(provider: ProviderDescriptor) {
  if (provider.supportLevel === 'detected') return 'inventoryOnly'
  if (provider.metadata?.readOnly === 'true') return 'managedReadOnly'
  return 'managed'
}

function providerVersion(provider: ProviderDescriptor) {
  if (provider.id === 'rook-ceph' && provider.metadata?.cephVersion) {
    return <><span className="block">Rook {provider.version || '—'}</span><span className="block text-xs text-[var(--color-muted-foreground)]">Ceph {provider.metadata.cephVersion}</span></>
  }
  return provider.version || 'Detected through CSI'
}

function compatibilityKey(provider: ProviderDescriptor) {
  if (provider.id === 'longhorn' || provider.id === 'rook-ceph' || provider.id === 'openebs' || provider.id === 'linstor') return provider.id
  return 'generic-csi'
}

function healthTone(status?: string): 'success' | 'warning' | 'danger' | 'info' | 'default' {
  if (status === 'ok') return 'success'
  if (status === 'error') return 'danger'
  if (status === 'warning') return 'warning'
  if (status === 'info') return 'info'
  return 'default'
}

function notableConditions(providers: ProviderDescriptor[], core: StorageCondition[], coreLabel: string) {
  return [
    ...core.map((condition) => ({ provider: coreLabel, condition })),
    ...providers.flatMap((provider) => provider.health.conditions
      .filter((condition) => condition.severity !== 'ok')
      .map((condition) => ({ provider: provider.displayName, condition }))),
  ]
}

export function StatusPage() {
  const { t } = useAppTranslation()
  const { user } = useAuth()
  const status = useStatus()
  const s = status.data
  const providers = s?.storage?.providers ?? []
  const healthyProviders = providers.filter((provider) => provider.health.status === 'ok').length
  const attentionProviders = providers.filter((provider) => provider.health.status === 'warning' || provider.health.status === 'error').length
  const unknownProviders = providers.filter((provider) => provider.health.status === 'unknown').length
  const conditions = notableConditions(providers, s?.storage?.conditions ?? [], t('status.storageCore'))
  const policy = s?.storagePolicy
  const effective = policy?.effective
  const operationsEnabled = Boolean(effective?.acceptNewOperations)
  const overallOperational = Boolean(s?.storage?.ready) && attentionProviders === 0

  return (
    <div data-testid="status-page">
      <PageHeader
        title={t('status.title')}
        description={t('status.description')}
        actions={<>
          <Badge tone={status.isLoading ? 'default' : overallOperational ? 'success' : 'danger'} data-testid="overall-system-status">
            {status.isLoading ? <RefreshCw size={13} className="mr-1 animate-spin" aria-hidden /> : overallOperational ? <CheckCircle2 size={13} className="mr-1" aria-hidden /> : <XCircle size={13} className="mr-1" aria-hidden />}
            {status.isLoading ? t('status.checking') : overallOperational ? t('status.operational') : t('status.attentionRequired')}
          </Badge>
          <Button type="button" size="sm" variant="outline" onClick={() => void status.refetch()} disabled={status.isFetching}>
            <RefreshCw size={14} className={status.isFetching ? 'animate-spin' : ''} aria-hidden />{t('common.refresh')}
          </Button>
        </>}
      />
      <QueryState isLoading={status.isLoading} error={status.error as Error | null} onRetry={() => void status.refetch()}>
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4" data-testid="status-summary">
          <Fact icon={Activity} label={t('status.platform')} value={`Highland ${s?.highland.version ?? '—'}`} detail={`Kubernetes ${s?.kubernetes.version ?? '—'}`} />
          <Fact icon={Database} label={t('status.storageCore')} value={s?.storage?.ready ? t('status.ready') : t('status.notReady')} detail={`${t('status.lastSync')}: ${formatTime(s?.storage?.lastSync)}`} />
          <Fact icon={ShieldCheck} label={t('status.providers')} value={t('status.providerHealthSummary', { healthy: healthyProviders, unknown: unknownProviders })} detail={attentionProviders ? t('status.providersNeedAttention', { count: attentionProviders }) : t('status.providersDetectedNoErrors', { count: providers.length })} />
          <Fact icon={Clock3} label={t('status.changeWorkflows')} value={operationsEnabled ? t('status.enabled') : t('status.readOnly')} detail={policy ? `${policy.source} · ${t('status.generation')} ${policy.generation}` : t('status.policyUnavailable')} />
        </div>

        <Card className="mt-4" data-testid="status-providers">
          <CardHeader>
            <CardTitle>{t('status.storageProviders')}</CardTitle>
            <p className="text-sm text-[var(--color-muted-foreground)]">{t('status.storageProvidersDescription')}</p>
          </CardHeader>
          <CardContent>
            <Table className="min-w-[860px] table-fixed" aria-label={t('status.storageProviders')}>
              <THead><TR>
                <TH className="w-48">{t('status.provider')}</TH>
                <TH className="w-24">{t('status.health')}</TH>
                <TH className="w-36">{t('status.installedVersion')}</TH>
                <TH>{t('status.testedCompatibility')}</TH>
                <TH className="w-36">{t('status.managementMode')}</TH>
                <TH className="w-16"><span className="sr-only">{t('common.actions')}</span></TH>
              </TR></THead>
              <TBody>
                {providers.map((provider) => {
                  const compatibility = s?.compatibility?.providers[compatibilityKey(provider)]
                  return <TR key={provider.id} data-testid={`status-provider-${provider.id}`}>
                    <TD><div className="font-medium">{provider.displayName}</div><div className="mt-1 flex min-w-0 items-center gap-1.5"><Badge tone={provider.supportLevel === 'managed' ? 'primary' : 'default'}>{provider.supportLevel}</Badge><span className="truncate text-[10px] text-[var(--color-muted-foreground)]" title={provider.namespace}>{provider.namespace || t('status.clusterWide')}</span></div></TD>
                    <TD><Badge tone={healthTone(provider.health.status)}>{provider.health.status}</Badge>{provider.health.stale ? <span className="mt-1 block text-[10px] font-medium text-amber-700 dark:text-amber-300">{t('status.stale')}</span> : null}</TD>
                    <TD className="break-words">{providerVersion(provider)}</TD>
                    <TD><div className="text-sm">{compatibility?.tested ?? '—'}</div><div className="mt-1 text-xs capitalize text-[var(--color-muted-foreground)]">{compatibility?.stage ?? provider.supportLevel}</div></TD>
                    <TD>{t(`status.${providerMode(provider)}`)}</TD>
                    <TD><Link to={`/storage/providers/${encodeURIComponent(provider.id)}`} className="inline-flex h-8 w-8 items-center justify-center rounded-md hover:bg-[var(--color-accent)]" aria-label={t('status.openProvider', { provider: provider.displayName })}><ExternalLink size={15} aria-hidden /></Link></TD>
                  </TR>
                })}
                {!providers.length ? <TR><TD colSpan={6} className="py-8 text-center text-[var(--color-muted-foreground)]">{t('status.noProvidersDetected')}</TD></TR> : null}
              </TBody>
            </Table>
            <div className="mt-3 flex flex-wrap items-center justify-between gap-2 text-xs text-[var(--color-muted-foreground)]">
              <span>{t('status.providerDataObserved')}: {formatTime(s?.storage?.providersObservedAt)}</span>
              {s?.storage?.providersStale ? <Badge tone="warning">{t('status.refreshingCachedData')}</Badge> : <Badge tone="success">{t('status.current')}</Badge>}
            </div>
          </CardContent>
        </Card>

        <div className="mt-4 grid gap-4 lg:grid-cols-2">
          <Card>
            <CardHeader><CardTitle>{t('status.components')}</CardTitle></CardHeader>
            <CardContent>
              <Row label={t('status.componentApi')} value={<StatusPill ok={s?.components.api === 'ok'} okLabel={t('status.ok')} badLabel={t('status.error')} />} />
              <Row label={t('status.storageInventory')} value={<StatusPill ok={Boolean(s?.storage?.ready)} okLabel={t('status.ready')} badLabel={t('status.notReady')} />} />
              <Row label={t('status.snapshotApi')} value={<StatusPill ok={Boolean(s?.storage?.snapshotApi)} okLabel={t('status.available')} badLabel={t('status.unavailable')} />} />
              {s?.longhorn.enabled ? <>
                <Row label={t('status.componentManager')} value={<StatusPill ok={Boolean(s.longhorn.reachable)} okLabel={t('status.reachable')} badLabel={t('status.unreachable')} />} />
                <Row label={t('status.componentScraper')} value={<StatusPill ok={s.components.metricsScraper === 'ok'} okLabel={t('status.ok')} badLabel={s.components.scrapeError || t('status.error')} />} />
              </> : null}
              <Row label={t('status.lastSync')} value={formatTime(s?.storage?.lastSync)} />
            </CardContent>
          </Card>

          <Card data-testid="status-runtime-policy">
            <CardHeader><CardTitle>{t('status.runtimeAndPolicy')}</CardTitle></CardHeader>
            <CardContent>
              <Row label={t('status.sessionBackend')} value={<Badge tone="info">{s?.highland.sessionBackend ?? '—'}</Badge>} />
              <Row label={t('status.benchmarkMode')} value={<Badge tone="default">{s?.highland.benchmarkMode ?? '—'}</Badge>} />
              <Row label={t('status.currentUser')} value={<Badge tone="info">{user?.username} · {user?.role}</Badge>} />
              <Row label={t('status.newOperations')} value={<Badge tone={operationsEnabled ? 'success' : 'default'}>{operationsEnabled ? t('status.accepted') : t('status.blocked')}</Badge>} />
              <Row label={t('status.portableWrites')} value={effective?.portableKubernetesWrites ? (effective.portableKubernetesProviderIds.join(', ') || t('status.noProvidersScoped')) : t('status.disabled')} />
              <Row label={t('status.nativeProviderWrites')} value={<span className="inline-flex flex-wrap justify-end gap-1"><Badge tone={effective?.longhornWrites ? 'success' : 'default'}>Longhorn {effective?.longhornWrites ? t('status.on') : t('status.off')}</Badge><Badge tone={effective?.rookCephWrites ? 'success' : 'default'}>Rook/Ceph {effective?.rookCephWrites ? t('status.on') : t('status.off')}</Badge></span>} />
              <Row label={t('status.cephDestructiveGates')} value={(effective?.allowCephStorageClassDelete || effective?.allowCephPoolDelete) ? t('status.partiallyEnabled') : t('status.disabled')} />
              <Row label={t('status.policyFreshness')} value={policy ? <Badge tone={policy.stale || policy.partial ? 'warning' : 'success'}>{policy.stale ? t('status.stale') : policy.partial ? t('status.partial') : t('status.current')}</Badge> : '—'} />
            </CardContent>
          </Card>
        </div>

        {conditions.length ? <Card className="mt-4" data-testid="status-conditions">
          <CardHeader><CardTitle>{t('status.notableConditions')}</CardTitle><p className="text-sm text-[var(--color-muted-foreground)]">{t('status.notableConditionsDescription')}</p></CardHeader>
          <CardContent className="divide-y divide-[var(--color-border)]">
            {conditions.map(({ provider, condition }, index) => <div key={`${provider}-${condition.type}-${index}`} className="grid gap-2 py-3 sm:grid-cols-[10rem_1fr_auto] sm:items-start">
              <div className="font-medium">{provider}</div>
              <div><div className="text-sm font-medium">{condition.type}{condition.reason ? ` · ${condition.reason}` : ''}</div><div className="mt-0.5 text-sm text-[var(--color-muted-foreground)]">{condition.message || t('status.noConditionDetail')}</div></div>
              <Badge tone={healthTone(condition.severity)}>{condition.severity}</Badge>
            </div>)}
          </CardContent>
        </Card> : null}

        <div className="mt-4 grid gap-4 lg:grid-cols-2">
          <Card data-testid="status-compatibility">
            <CardHeader><CardTitle>{t('status.compatibility')}</CardTitle><p className="text-sm text-[var(--color-muted-foreground)]">{t('status.compatibilityDescription')}</p></CardHeader>
            <CardContent>
              <Row label={t('status.releaseLine')} value={s?.compatibility?.releaseLine ?? '—'} />
              <Row label={t('status.testedKubernetes')} value={s?.compatibility ? `${s.compatibility.kubernetes.minimum} – ${s.compatibility.kubernetes.maximum}` : '—'} />
              <Row label={t('status.matrixUpdated')} value={s?.compatibility?.lastUpdated ?? '—'} />
              <Row label={t('status.installedKubernetes')} value={s?.kubernetes.version ?? '—'} />
            </CardContent>
          </Card>

          <Card>
            <CardHeader><CardTitle>{t('status.about')}</CardTitle></CardHeader>
            <CardContent className="space-y-3">
              <div className="flex items-center gap-3"><HighlandLogo size={40} className="text-[var(--color-primary)]" /><div><div className="text-lg font-semibold tracking-tight">{t('app.name')}</div><div className="text-sm text-[var(--color-muted-foreground)]">{t('app.by')}</div></div></div>
              <p className="text-sm text-[var(--color-muted-foreground)]">{s?.vendor.tagline ?? t('app.tagline')}</p>
              <a href={s?.vendor.url ?? 'https://alphabravo.io'} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1.5 text-sm font-medium text-[var(--color-primary)] hover:underline">{t('status.visitAlphaBravo')} <ExternalLink size={14} aria-hidden /></a>
            </CardContent>
          </Card>
        </div>
      </QueryState>
    </div>
  )
}
