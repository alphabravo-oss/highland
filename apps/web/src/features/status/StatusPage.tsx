import { CheckCircle2, ExternalLink, XCircle } from 'lucide-react'
import { useCapacity, useNodes, useStatus, useVolumes } from '@/api/hooks'
import { formatBytes } from '@/api/longhorn'
import { useAuth } from '@/auth/AuthContext'
import { HighlandLogo } from '@/components/layout/HighlandLogo'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { useAppTranslation } from '@/i18n/useAppTranslation'

function Row({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3 border-b border-[var(--color-border)] py-2 text-sm last:border-0">
      <span className="text-[var(--color-muted-foreground)]">{label}</span>
      <span className="text-right font-medium tabular-nums">{value}</span>
    </div>
  )
}

function StatusPill({ ok, okLabel, badLabel }: { ok: boolean; okLabel: string; badLabel: string }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      {ok ? (
        <CheckCircle2 size={15} className="text-[color:#16a34a]" aria-hidden />
      ) : (
        <XCircle size={15} className="text-[color:#dc2626]" aria-hidden />
      )}
      <span className="font-medium">{ok ? okLabel : badLabel}</span>
    </span>
  )
}

export function StatusPage() {
  const { t } = useAppTranslation()
  const { user } = useAuth()
  const status = useStatus()
  const nodes = useNodes()
  const volumes = useVolumes()
  const capacity = useCapacity()

  const s = status.data
  const nodeList = nodes.data ?? []
  const volList = volumes.data ?? []
  const healthy = volList.filter((v) => (v.robustness ?? '').toLowerCase() === 'healthy').length
  const degraded = volList.filter((v) => (v.robustness ?? '').toLowerCase() === 'degraded').length
  const faulted = volList.filter((v) => (v.robustness ?? '').toLowerCase() === 'faulted').length
  const schedulable = nodeList.filter((n) => n.allowScheduling).length

  return (
    <div data-testid="status-page">
      <PageHeader title={t('status.title')} description={t('status.description')} />
      <QueryState isLoading={status.isLoading} error={status.error as Error | null} onRetry={() => void status.refetch()}>
        <div className="grid gap-4 lg:grid-cols-2">
          {/* Versions */}
          <Card>
            <CardHeader>
              <CardTitle>{t('status.versions')}</CardTitle>
            </CardHeader>
            <CardContent>
              <Row label={t('status.highlandVersion')} value={s?.highland.version ?? '—'} />
              <Row label={t('status.longhornVersion')} value={s?.longhorn.version ?? '—'} />
              <Row label={t('status.kubernetesVersion')} value={s?.kubernetes.version ?? '—'} />
              <Row
                label={t('status.longhornSupported')}
                value={(s?.longhorn.supported ?? []).join(', ') || '—'}
              />
            </CardContent>
          </Card>

          {/* Component health */}
          <Card>
            <CardHeader>
              <CardTitle>{t('status.components')}</CardTitle>
            </CardHeader>
            <CardContent>
              <Row label={t('status.componentApi')} value={<StatusPill ok okLabel={t('status.ok')} badLabel={t('status.error')} />} />
              <Row
                label={t('status.componentManager')}
                value={<StatusPill ok={Boolean(s?.longhorn.reachable)} okLabel={t('status.reachable')} badLabel={t('status.unreachable')} />}
              />
              <Row
                label={t('status.componentScraper')}
                value={<StatusPill ok={s?.components.metricsScraper === 'ok'} okLabel={t('status.ok')} badLabel={s?.components.scrapeError || t('status.error')} />}
              />
              <Row label={t('status.sessionBackend')} value={<Badge tone="info">{s?.highland.sessionBackend ?? '—'}</Badge>} />
              <Row label={t('status.benchmarkMode')} value={<Badge tone="default">{s?.highland.benchmarkMode ?? '—'}</Badge>} />
            </CardContent>
          </Card>

          {/* Cluster summary */}
          <Card>
            <CardHeader>
              <CardTitle>{t('status.cluster')}</CardTitle>
            </CardHeader>
            <CardContent>
              <Row label={t('status.nodes')} value={t('status.nodesValue', { total: nodeList.length, schedulable })} />
              <Row
                label={t('status.volumes')}
                value={
                  <span className="inline-flex gap-1">
                    <Badge tone="success">{healthy}</Badge>
                    <Badge tone="warning">{degraded}</Badge>
                    <Badge tone="danger">{faulted}</Badge>
                  </span>
                }
              />
              <Row
                label={t('status.capacity')}
                value={
                  capacity.data && capacity.data.totalBytes > 0
                    ? `${formatBytes(capacity.data.usedBytes)} / ${formatBytes(capacity.data.totalBytes)}`
                    : '—'
                }
              />
              <Row label={t('status.longhornNamespace')} value={s?.longhorn.namespace ?? '—'} />
              <Row
                label={t('status.managerUrl')}
                value={<span className="max-w-[16rem] truncate font-mono text-xs">{s?.longhorn.managerUrl ?? '—'}</span>}
              />
              <Row label={t('status.currentUser')} value={<Badge tone="info">{user?.username} · {user?.role}</Badge>} />
            </CardContent>
          </Card>

          {/* AlphaBravo */}
          <Card>
            <CardHeader>
              <CardTitle>{t('status.about')}</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              <div className="flex items-center gap-3">
                <HighlandLogo size={40} className="text-[var(--color-primary)]" />
                <div>
                  <div className="text-lg font-semibold tracking-tight">{t('app.name')}</div>
                  <div className="text-sm text-[var(--color-muted-foreground)]">{t('app.by')}</div>
                </div>
              </div>
              <p className="text-sm text-[var(--color-muted-foreground)]">{s?.vendor.tagline ?? t('app.tagline')}</p>
              <a
                href={s?.vendor.url ?? 'https://alphabravo.io'}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-1.5 text-sm font-medium text-[var(--color-primary)] hover:underline"
              >
                {t('status.visitAlphaBravo')} <ExternalLink size={14} aria-hidden />
              </a>
            </CardContent>
          </Card>
        </div>
      </QueryState>
    </div>
  )
}
