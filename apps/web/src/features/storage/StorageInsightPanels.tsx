import { AlertTriangle, CalendarClock, Database, History } from 'lucide-react'
import { formatBytes } from '@/api/longhorn'
import type {
  CapacityForecast,
  CapacityMeasure,
  CapacityOwnership,
  CapacityOwnershipGroup,
  EvidenceStrength,
  StorageTimeline,
  TimelineEntry,
} from '@/api/storage/insights'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { EmptyState } from '@/components/ui/empty-state'
import { Skeleton } from '@/components/ui/skeleton'
import { formatByteValue, safeInsightHref } from './insightFormatting'

type AsyncPanelProps = {
  isLoading?: boolean
  error?: Error | null
}

export function TimelinePanel({
  timeline,
  isLoading = false,
  error,
  title = 'Storage timeline',
}: AsyncPanelProps & {
  timeline?: StorageTimeline
  title?: string
}) {
  if (isLoading) return <InsightPanelSkeleton title={title} />
  if (error) return <InsightError title="Timeline unavailable" error={error} />
  if (!timeline || timeline.entries.length === 0) {
    return (
      <EmptyState
        icon={History}
        title="No storage activity"
        description="No timeline entries match the current provider, resource, or time filters."
      />
    )
  }

  const partialReasons = timelinePartialReasons(timeline)

  return (
    <Card data-testid="storage-timeline-panel">
      <CardHeader>
        <div className="flex items-center justify-between gap-3">
          <CardTitle>{title}</CardTitle>
          <span className="text-xs text-[var(--color-muted-foreground)]">
            {timeline.entries.length} of {timeline.total}
          </span>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        {timeline.truncated ? (
          <Alert tone="warning">
            <AlertTitle>Timeline truncated</AlertTitle>
            <AlertDescription>Refine the filters to inspect all matching activity.</AlertDescription>
          </Alert>
        ) : null}
        {partialReasons.length > 0 ? (
          <PartialEvidenceAlert title="Timeline includes partial evidence" reasons={partialReasons} />
        ) : null}
        <ol className="space-y-0" aria-label={title}>
          {timeline.entries.map((entry) => (
            <TimelineRow key={entry.id} entry={entry} />
          ))}
        </ol>
      </CardContent>
    </Card>
  )
}

export function CapacityOwnershipPanel({
  ownership,
  forecast,
  isLoading = false,
  error,
  title = 'Capacity ownership',
}: AsyncPanelProps & {
  ownership?: CapacityOwnership
  forecast?: CapacityForecast
  title?: string
}) {
  if (isLoading) return <InsightPanelSkeleton title={title} />
  if (error) return <InsightError title="Capacity ownership unavailable" error={error} />
  if (!ownership || ownership.groups.length === 0) {
    return (
      <EmptyState
        icon={Database}
        title="No capacity ownership data"
        description="No capacity records match the current provider, namespace, or measurement filters."
      />
    )
  }

  const partialReasons = capacityPartialReasons(ownership)
  const measures = groupCapacityByMeasure(ownership.groups)

  return (
    <Card data-testid="capacity-ownership-panel">
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <p className="text-xs text-[var(--color-muted-foreground)]">
          Measurements are shown separately because logical, allocated, usable, and raw bytes are not additive.
        </p>
      </CardHeader>
      <CardContent className="space-y-5">
        {partialReasons.length > 0 ? (
          <PartialEvidenceAlert title="Capacity attribution is partial" reasons={partialReasons} />
        ) : null}
        {Array.from(measures.entries()).map(([measure, groups]) => (
          <CapacityMeasureSection key={measure} measure={measure} groups={groups} />
        ))}
        {forecast ? <ForecastSummary forecast={forecast} /> : null}
        {ownership.observedAt ? (
          <p className="text-xs text-[var(--color-muted-foreground)]">
            Ownership observed {formatTimestamp(ownership.observedAt)}
          </p>
        ) : null}
      </CardContent>
    </Card>
  )
}

function TimelineRow({ entry }: { entry: TimelineEntry }) {
  const subject = entry.workload
    ? `${entry.workload.kind} ${entry.workload.namespace}/${entry.workload.name}`
    : entry.resource
      ? `${entry.resource.kind} ${entry.resource.namespace ? `${entry.resource.namespace}/` : ''}${entry.resource.name}`
      : entry.providerId || 'Storage system'
  return (
    <li className="relative border-l border-[var(--color-border)] pb-5 pl-5 last:pb-0">
      <span
        className={`absolute -left-1.5 top-1 h-3 w-3 rounded-full border-2 border-[var(--color-card)] ${severityDot(entry.severity)}`}
        aria-hidden
      />
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-sm font-medium">{subject}</span>
        <Badge tone={severityTone(entry.severity)}>{entry.severity}</Badge>
        <Badge>{sourceLabel(entry.source)}</Badge>
        {entry.count > 1 ? <span className="text-xs tabular-nums">×{entry.count}</span> : null}
      </div>
      <p className="mt-1 text-sm">
        {entry.reason || entry.action ? (
          <span className="font-medium">{entry.reason || entry.action}: </span>
        ) : null}
        {entry.message || 'State changed'}
      </p>
      <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-[var(--color-muted-foreground)]">
        <span>{formatTimestamp(entry.lastOccurredAt || entry.observedAt)}</span>
        <span>{evidenceLabel(entry.attribution.evidence)}</span>
        <span>{entry.retention} history</span>
        {entry.ordering === 'clock-skew' ? <span>clock skew detected</span> : null}
        {entry.ordering === 'unknown' ? <span>source ordering unknown</span> : null}
      </div>
      {entry.links?.length ? (
        <div className="mt-2 flex flex-wrap gap-3">
          {entry.links.map((link) => {
            const href = safeInsightHref(link.href)
            if (!href) return null
            const external = href.startsWith('http')
            return (
              <a
                key={`${link.kind}:${href}`}
                href={href}
                target={external ? '_blank' : undefined}
                rel={external ? 'noopener noreferrer' : undefined}
                className="text-xs font-medium text-[var(--color-primary)] hover:underline"
              >
                {linkLabel(link.kind)}
              </a>
            )
          })}
        </div>
      ) : null}
    </li>
  )
}

function CapacityMeasureSection({
  measure,
  groups,
}: {
  measure: CapacityMeasure
  groups: CapacityOwnershipGroup[]
}) {
  return (
    <section aria-labelledby={`capacity-${measure}`}>
      <div className="mb-2 flex flex-wrap items-baseline justify-between gap-2">
        <h4 id={`capacity-${measure}`} className="text-sm font-semibold">
          {capacityMeasureLabel(measure)}
        </h4>
        <span className="text-xs text-[var(--color-muted-foreground)]">
          {capacityMeasureDescription(measure)}
        </span>
      </div>
      <div className="overflow-x-auto rounded-md border border-[var(--color-border)]">
        <table className="w-full text-left text-sm">
          <thead className="bg-[var(--color-muted)]/40 text-xs text-[var(--color-muted-foreground)]">
            <tr>
              <th className="px-3 py-2 font-medium">Owner</th>
              <th className="px-3 py-2 font-medium">Backend</th>
              <th className="px-3 py-2 text-right font-medium">Bytes</th>
              <th className="px-3 py-2 font-medium">Evidence</th>
            </tr>
          </thead>
          <tbody>
            {groups.map((group, index) => (
              <tr
                key={`${capacityDimensionKey(group)}:${index}`}
                className="border-t border-[var(--color-border)]"
              >
                <td className="px-3 py-2">{capacityOwner(group)}</td>
                <td className="px-3 py-2 text-[var(--color-muted-foreground)]">
                  {capacityBackend(group)}
                </td>
                <td className="px-3 py-2 text-right font-medium tabular-nums">
                  {formatByteValue(group.bytes)}
                </td>
                <td className="px-3 py-2">
                  <div className="flex flex-wrap gap-1">
                    {group.evidence.map((evidence) => (
                      <Badge key={evidence} tone={evidence === 'authoritative' ? 'success' : 'warning'}>
                        {evidence}
                      </Badge>
                    ))}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}

function ForecastSummary({ forecast }: { forecast: CapacityForecast }) {
  if (forecast.status === 'unavailable') {
    return (
      <Alert>
        <AlertTitle>Forecast unavailable</AlertTitle>
        <AlertDescription>
          {conditionMessages(forecast.conditions, 'Fresh, sufficiently long metrics history is required.')}
          <span className="mt-1 block">
            {forecast.sampleCount} samples{forecast.latestSampleAt ? `; latest ${formatTimestamp(forecast.latestSampleAt)}` : ''}
          </span>
        </AlertDescription>
      </Alert>
    )
  }
  return (
    <div className="rounded-md border border-[var(--color-border)] bg-[var(--color-muted)]/20 p-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <CalendarClock size={16} aria-hidden />
          <span className="text-sm font-semibold">Capacity trend</span>
          {forecast.confidence ? <Badge>{forecast.confidence} confidence</Badge> : null}
        </div>
        <span className="text-sm font-medium tabular-nums">
          {forecast.projectedBytes !== undefined ? formatByteValue(forecast.projectedBytes) : '—'}
          {forecast.projectionAt ? ` by ${formatTimestamp(forecast.projectionAt)}` : ''}
        </span>
      </div>
      <p className="mt-1 text-xs text-[var(--color-muted-foreground)]">
        {forecast.sampleCount} samples · {formatSlope(forecast.slopeBytesPerDay)} · historical trend, not a capacity guarantee
      </p>
    </div>
  )
}

function PartialEvidenceAlert({ title, reasons }: { title: string; reasons: string[] }) {
  return (
    <Alert tone="warning">
      <AlertTitle className="flex items-center gap-2">
        <AlertTriangle size={15} aria-hidden />
        {title}
      </AlertTitle>
      <AlertDescription>
        <ul className="list-disc pl-4">
          {reasons.map((reason) => <li key={reason}>{reason}</li>)}
        </ul>
      </AlertDescription>
    </Alert>
  )
}

function InsightPanelSkeleton({ title }: { title: string }) {
  return (
    <Card aria-label={`${title} loading`}>
      <CardHeader><CardTitle>{title}</CardTitle></CardHeader>
      <CardContent className="space-y-3" data-testid="insight-panel-skeleton">
        <Skeleton className="h-5 w-1/3" />
        <Skeleton className="h-16 w-full" />
        <Skeleton className="h-16 w-full" />
      </CardContent>
    </Card>
  )
}

function InsightError({ title, error }: { title: string; error: Error }) {
  return (
    <Alert tone="danger">
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{error.message || 'Highland could not load this storage insight.'}</AlertDescription>
    </Alert>
  )
}

function timelinePartialReasons(timeline: StorageTimeline): string[] {
  const reasons = timeline.conditions?.map((condition) => condition.message) ?? []
  if (timeline.entries.some((entry) => entry.attribution.evidence !== 'authoritative')) {
    reasons.push('Some entries use derived or incomplete provider attribution.')
  }
  if (timeline.entries.some((entry) => entry.ordering !== 'known')) {
    reasons.push('Some source timestamps have clock skew or unknown ordering.')
  }
  return unique(reasons)
}

function capacityPartialReasons(ownership: CapacityOwnership): string[] {
  const reasons = ownership.conditions?.map((condition) => condition.message) ?? []
  if (ownership.groups.some((group) => group.evidence.some((evidence) => evidence !== 'authoritative'))) {
    reasons.push('Some ownership dimensions are not authoritatively correlated.')
  }
  return unique(reasons)
}

function groupCapacityByMeasure(groups: CapacityOwnershipGroup[]) {
  const result = new Map<CapacityMeasure, CapacityOwnershipGroup[]>()
  for (const group of groups) {
    result.set(group.measure, [...(result.get(group.measure) ?? []), group])
  }
  return result
}

function capacityOwner(group: CapacityOwnershipGroup) {
  const { dimensions } = group
  if (dimensions.workload) {
    return `${dimensions.workloadKind || 'Workload'} ${dimensions.namespace ? `${dimensions.namespace}/` : ''}${dimensions.workload}`
  }
  if (dimensions.namespace) return `Namespace ${dimensions.namespace}`
  if (dimensions.storageClass) return `StorageClass ${dimensions.storageClass}`
  return `Provider ${dimensions.providerId}`
}

function capacityBackend(group: CapacityOwnershipGroup) {
  const parts = [
    group.dimensions.pool ? `pool ${group.dimensions.pool}` : '',
    group.dimensions.filesystem ? `filesystem ${group.dimensions.filesystem}` : '',
    group.dimensions.storageClass ? `class ${group.dimensions.storageClass}` : '',
  ].filter(Boolean)
  return parts.join(' · ') || group.dimensions.driver || '—'
}

function capacityDimensionKey(group: CapacityOwnershipGroup) {
  return [
    group.measure,
    group.dimensions.providerId,
    group.dimensions.namespace,
    group.dimensions.workload,
    group.dimensions.storageClass,
    group.dimensions.pool,
    group.dimensions.filesystem,
  ].join(':')
}

function formatTimestamp(value?: string) {
  if (!value) return 'Time unavailable'
  const date = new Date(value)
  return Number.isNaN(date.valueOf()) ? 'Time unavailable' : date.toLocaleString()
}

function formatSlope(value?: number) {
  if (value === undefined || !Number.isFinite(value)) return 'trend unavailable'
  const direction = value >= 0 ? '+' : '−'
  return `${direction}${formatBytes(Math.abs(value))}/day`
}

function capacityMeasureLabel(measure: CapacityMeasure) {
  const labels: Record<CapacityMeasure, string> = {
    'pvc-requested': 'PVC requested',
    'pv-provisioned': 'PV provisioned',
    'backend-logical': 'Backend logical',
    'backend-allocated': 'Backend allocated / used',
    'pool-usable': 'Pool usable',
    'pool-raw': 'Pool raw',
    'cluster-raw': 'Cluster physical raw',
  }
  return labels[measure]
}

function capacityMeasureDescription(measure: CapacityMeasure) {
  const descriptions: Record<CapacityMeasure, string> = {
    'pvc-requested': 'Logical Kubernetes demand',
    'pv-provisioned': 'Logical Kubernetes supply',
    'backend-logical': 'Image or subvolume address space',
    'backend-allocated': 'Backend consumption under provider accounting',
    'pool-usable': 'Client-usable capacity after backend policy',
    'pool-raw': 'Pool physical capacity before redundancy overhead',
    'cluster-raw': 'Physical storage across the cluster',
  }
  return descriptions[measure]
}

function severityTone(severity: TimelineEntry['severity']): 'default' | 'info' | 'warning' | 'danger' {
  if (severity === 'critical' || severity === 'error') return 'danger'
  if (severity === 'warning') return 'warning'
  if (severity === 'info') return 'info'
  return 'default'
}

function severityDot(severity: TimelineEntry['severity']) {
  if (severity === 'critical' || severity === 'error') return 'bg-red-500'
  if (severity === 'warning') return 'bg-amber-500'
  if (severity === 'info') return 'bg-sky-500'
  return 'bg-slate-400'
}

function sourceLabel(source: TimelineEntry['source']) {
  return source.split('-').map((word) => word[0]?.toUpperCase() + word.slice(1)).join(' ')
}

function evidenceLabel(evidence: EvidenceStrength) {
  return `${evidence} attribution`
}

function linkLabel(kind: string) {
  if (kind === 'resource-graph') return 'View relationships'
  if (kind === 'operation') return 'View operation'
  if (kind === 'native-dashboard') return 'Open native dashboard'
  return 'View details'
}

function conditionMessages(conditions: { message: string }[] | undefined, fallback: string) {
  return conditions?.map((condition) => condition.message).filter(Boolean).join(' ') || fallback
}

function unique(values: string[]) {
  return [...new Set(values)]
}
