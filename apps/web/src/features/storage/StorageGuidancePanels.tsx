import { AlertTriangle, ExternalLink, GitCompareArrows, Lightbulb, ShieldCheck } from 'lucide-react'
import type {
  ActionSurface,
  CandidateAssessment,
  ProviderComparison,
  Remediation,
  RemediationResult,
} from '@/api/storage/guidance'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { EmptyState } from '@/components/ui/empty-state'
import { Skeleton } from '@/components/ui/skeleton'
import { safeInsightHref } from './insightFormatting'

type AsyncPanelProps = {
  isLoading?: boolean
  error?: Error | null
}

export function ProviderComparisonPanel({
  comparison,
  isLoading = false,
  error,
  title = 'Storage placement guidance',
  compact = false,
}: AsyncPanelProps & {
  comparison?: ProviderComparison
  title?: string
  compact?: boolean
}) {
  if (isLoading) return <GuidanceSkeleton title={title} />
  if (error) return <GuidanceError title="Provider comparison unavailable" error={error} />
  if (!comparison || comparison.assessments.length === 0) {
    return (
      <EmptyState
        icon={GitCompareArrows}
        title="No comparable storage candidates"
        description="No providers or StorageClasses match the current visibility and policy filters."
      />
    )
  }
  return (
    <Card data-testid="provider-comparison-panel">
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <p className="text-xs text-[var(--color-muted-foreground)]">
          Candidates are evaluated against explicit requirements. Highland does not calculate an opaque provider score.
        </p>
      </CardHeader>
      <CardContent className="space-y-4">
        {comparison.conditions?.length ? (
          <GuidanceConditions title="Comparison limitations" conditions={comparison.conditions} />
        ) : null}
        {compact ? (
          <div className="overflow-x-auto rounded-md border border-[var(--color-border)]">
            <table className="w-full text-left text-sm">
              <thead className="bg-[var(--color-muted)]/40 text-xs text-[var(--color-muted-foreground)]">
                <tr>
                  <th className="px-3 py-2 font-medium">StorageClass</th>
                  <th className="px-3 py-2 font-medium">Eligibility</th>
                  <th className="px-3 py-2 font-medium">Health</th>
                  <th className="px-3 py-2 font-medium">Headroom</th>
                  <th className="px-3 py-2 font-medium">Policy</th>
                </tr>
              </thead>
              <tbody>
                {comparison.assessments.map((assessment) => (
                  <CompactCandidateRow
                    key={`${assessment.candidate.providerId}:${assessment.candidate.storageClass}`}
                    assessment={assessment}
                  />
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="grid gap-4 xl:grid-cols-2">
            {comparison.assessments.map((assessment) => (
              <CandidateCard
                key={`${assessment.candidate.providerId}:${assessment.candidate.storageClass}`}
                assessment={assessment}
              />
            ))}
          </div>
        )}
        {comparison.observedAt ? (
          <p className="text-xs text-[var(--color-muted-foreground)]">
            Facts last observed {formatTimestamp(comparison.observedAt)}
          </p>
        ) : null}
      </CardContent>
    </Card>
  )
}

function CompactCandidateRow({ assessment }: { assessment: CandidateAssessment }) {
  const candidate = assessment.candidate
  const failed = assessment.criteria.filter((criterion) => criterion.state === 'unsupported')
  const unknown = assessment.criteria.filter((criterion) => criterion.state === 'unknown')
  const policy = failed.length
    ? failed.map((criterion) => criterion.reason).join('; ')
    : unknown.length
      ? `${unknown.length} requirement${unknown.length === 1 ? '' : 's'} unknown`
      : 'All requested requirements met'
  return (
    <tr className="border-t border-[var(--color-border)] first:border-t-0">
      <td className="px-3 py-3">
        <div className="font-medium">{candidate.storageClass}</div>
        <div className="text-xs text-[var(--color-muted-foreground)]">
          {candidate.testedProfile.driver || candidate.providerName || candidate.providerId}
        </div>
      </td>
      <td className="px-3 py-3"><Badge tone={eligibilityTone(assessment.eligibility)}>{assessment.eligibility}</Badge></td>
      <td className="px-3 py-3">{candidate.health?.status || 'Unavailable'}</td>
      <td className="px-3 py-3 tabular-nums">{candidate.headroom ? `${candidate.headroom.percent.toFixed(1)}%` : 'Unavailable'}</td>
      <td className="max-w-md px-3 py-3 text-xs text-[var(--color-muted-foreground)]">{policy}</td>
    </tr>
  )
}

export function RemediationGuidancePanel({
  result,
  isLoading = false,
  error,
  title = 'Guided remediation',
  resolveDashboardDestination,
}: AsyncPanelProps & {
  result?: RemediationResult
  title?: string
  resolveDashboardDestination?: (destination: string) => string | undefined
}) {
  if (isLoading) return <GuidanceSkeleton title={title} />
  if (error) return <GuidanceError title="Remediation guidance unavailable" error={error} />
  if (!result || result.recommendations.length === 0) {
    return (
      <EmptyState
        icon={Lightbulb}
        title="No reviewed remediation guidance"
        description="Highland has no evidence-backed guidance for the selected conditions."
      />
    )
  }
  return (
    <Card data-testid="remediation-guidance-panel">
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <p className="text-xs text-[var(--color-muted-foreground)]">
          Guidance is read-only. Native tools authenticate separately and remain authoritative for actions performed there.
        </p>
      </CardHeader>
      <CardContent className="space-y-4">
        {result.conditions?.length ? (
          <GuidanceConditions title="Guidance limitations" conditions={result.conditions} />
        ) : null}
        {result.recommendations.map((recommendation) => (
          <RemediationCard
            key={`${recommendation.conditionCode}:${recommendation.id}:${recommendation.providerId}`}
            remediation={recommendation}
            dashboardHref={
              recommendation.dashboardDestination
                ? resolveDashboardDestination?.(recommendation.dashboardDestination)
                : undefined
            }
          />
        ))}
      </CardContent>
    </Card>
  )
}

function CandidateCard({ assessment }: { assessment: CandidateAssessment }) {
  const candidate = assessment.candidate
  return (
    <section className="rounded-md border border-[var(--color-border)] p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h4 className="font-semibold">{candidate.providerName || candidate.providerId}</h4>
          <p className="text-sm text-[var(--color-muted-foreground)]">
            StorageClass {candidate.storageClass}
          </p>
        </div>
        <div className="flex gap-2">
          <Badge>{candidate.supportLevel}</Badge>
          <Badge tone={eligibilityTone(assessment.eligibility)}>{assessment.eligibility}</Badge>
        </div>
      </div>
      <dl className="mt-3 grid grid-cols-2 gap-x-4 gap-y-2 text-xs">
        <Fact label="Provider profile" value={profileLabel(candidate.testedProfile)} />
        <Fact label="Health" value={candidate.health?.status || 'Unavailable'} />
        <Fact label="Usable headroom" value={candidate.headroom ? `${candidate.headroom.percent.toFixed(1)}%` : 'Unavailable'} />
        <Fact label="Reclaim policy" value={candidate.reclaimPolicy || 'Unavailable'} />
        <Fact label="Access modes" value={candidate.accessModes?.join(', ') || 'Unavailable'} />
        <Fact label="Topology" value={candidate.topologyKeys?.join(', ') || 'Unavailable'} />
      </dl>
      <div className="mt-4 overflow-x-auto">
        <table className="w-full text-left text-xs">
          <thead className="text-[var(--color-muted-foreground)]">
            <tr>
              <th className="pb-2 font-medium">Requirement</th>
              <th className="pb-2 font-medium">Result</th>
              <th className="pb-2 font-medium">Evidence</th>
            </tr>
          </thead>
          <tbody>
            {assessment.criteria.map((criterion) => (
              <tr key={criterion.criterion} className="border-t border-[var(--color-border)]">
                <td className="py-2 pr-2">{criterion.reason}</td>
                <td className="py-2 pr-2"><Badge tone={factTone(criterion.state)}>{criterion.state}</Badge></td>
                <td className="py-2 text-[var(--color-muted-foreground)]">
                  {criterion.evidence?.source || 'Unavailable'}
                  {criterion.evidence?.stale ? ' · stale' : ''}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {assessment.conditions?.length ? (
        <div className="mt-3"><GuidanceConditions title="Candidate limitations" conditions={assessment.conditions} /></div>
      ) : null}
      {candidate.operations?.length ? (
        <div className="mt-3">
          <p className="text-xs font-medium">Operational surfaces</p>
          <ul className="mt-1 space-y-1 text-xs text-[var(--color-muted-foreground)]">
            {candidate.operations.map((operation) => (
              <li key={`${operation.capability}:${operation.surface}`}>
                {operation.capability}: {operation.surface}{operation.readOnly ? ' (read-only)' : ''}
                {operation.detail ? ` — ${operation.detail}` : ''}
              </li>
            ))}
          </ul>
        </div>
      ) : null}
      {candidate.benchmarks?.length ? (
        <div className="mt-3">
          <p className="text-xs font-medium">Benchmark context</p>
          <ul className="mt-1 space-y-1 text-xs text-[var(--color-muted-foreground)]">
            {candidate.benchmarks.map((benchmark) => (
              <li key={`${benchmark.semantic}:${benchmark.method}:${benchmark.profile}`}>
                {benchmark.semantic}: {benchmark.value.toLocaleString()} {benchmark.unit} · {benchmark.method} / {benchmark.profile}
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </section>
  )
}

function RemediationCard({
  remediation,
  dashboardHref,
}: {
  remediation: Remediation
  dashboardHref?: string
}) {
  const safeDashboardHref = dashboardHref ? safeInsightHref(dashboardHref) : undefined
  const safeRunbookHref = remediation.runbookUrl ? safeInsightHref(remediation.runbookUrl) : undefined
  return (
    <section className="rounded-md border border-[var(--color-border)] p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h4 className="font-semibold">{remediation.title}</h4>
          <p className="mt-1 text-sm">{remediation.explanation}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Badge tone={surfaceTone(remediation.surface)}>{surfaceLabel(remediation.surface)}</Badge>
          <Badge>{remediation.escalation}</Badge>
        </div>
      </div>
      {!remediation.fresh ? (
        <Alert tone="warning" className="mt-3">
          <AlertTitle>Evidence is stale or incomplete</AlertTitle>
          <AlertDescription>Refresh the source evidence before acting on this guidance.</AlertDescription>
        </Alert>
      ) : null}
      {!remediation.compatibilityReviewed ? (
        <Alert tone="warning" className="mt-3">
          <AlertTitle>Version compatibility is not reviewed</AlertTitle>
          <AlertDescription>Native deep links and version-specific steps are intentionally unavailable.</AlertDescription>
        </Alert>
      ) : null}
      <div className="mt-3 grid gap-3 md:grid-cols-2">
        <GuidanceList title="Prerequisites" values={remediation.prerequisites} empty="No additional prerequisites recorded." />
        <GuidanceList title="Risks" values={remediation.risks} empty="No provider-specific risks recorded." />
      </div>
      <div className="mt-3">
        <p className="text-xs font-medium">Evidence</p>
        <ul className="mt-1 space-y-1 text-xs text-[var(--color-muted-foreground)]">
          {remediation.evidence.length ? remediation.evidence.map((evidence, index) => (
            <li key={`${evidence.source}:${evidence.observedAt}:${index}`}>
              {evidence.summary} · {evidence.source} · {evidence.strength} · {formatTimestamp(evidence.observedAt)}
            </li>
          )) : <li>Evidence unavailable</li>}
        </ul>
      </div>
      <div className="mt-4 flex flex-wrap items-center gap-3">
        {remediation.surface === 'highland' && remediation.highlandActionId ? (
          <span className="inline-flex items-center gap-1.5 text-xs font-medium">
            <ShieldCheck size={14} aria-hidden />
            Review Highland workflow: {remediation.highlandActionId}
          </span>
        ) : null}
        {safeDashboardHref ? (
          <a
            href={safeDashboardHref}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1.5 text-xs font-medium text-[var(--color-primary)] hover:underline"
          >
            Open Ceph Dashboard <ExternalLink size={13} aria-hidden />
          </a>
        ) : null}
        {safeRunbookHref ? (
          <a
            href={safeRunbookHref}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1.5 text-xs font-medium text-[var(--color-primary)] hover:underline"
          >
            Open runbook <ExternalLink size={13} aria-hidden />
          </a>
        ) : null}
      </div>
    </section>
  )
}

function GuidanceConditions({
  title,
  conditions,
}: {
  title: string
  conditions: Array<{ code: string; message: string }>
}) {
  return (
    <Alert tone="warning">
      <AlertTitle className="flex items-center gap-2"><AlertTriangle size={15} aria-hidden />{title}</AlertTitle>
      <AlertDescription>
        <ul className="list-disc pl-4">
          {conditions.map((condition) => <li key={condition.code}>{condition.message}</li>)}
        </ul>
      </AlertDescription>
    </Alert>
  )
}

function GuidanceList({ title, values, empty }: { title: string; values: string[]; empty: string }) {
  return (
    <div>
      <p className="text-xs font-medium">{title}</p>
      <ul className="mt-1 list-disc space-y-1 pl-4 text-xs text-[var(--color-muted-foreground)]">
        {values.length ? values.map((value) => <li key={value}>{value}</li>) : <li>{empty}</li>}
      </ul>
    </div>
  )
}

function Fact({ label, value }: { label: string; value: string }) {
  return <div><dt className="text-[var(--color-muted-foreground)]">{label}</dt><dd className="mt-0.5">{value}</dd></div>
}

function GuidanceSkeleton({ title }: { title: string }) {
  return (
    <Card aria-label={`${title} loading`}>
      <CardHeader><CardTitle>{title}</CardTitle></CardHeader>
      <CardContent className="space-y-3" data-testid="guidance-skeleton">
        <Skeleton className="h-5 w-1/3" />
        <Skeleton className="h-36 w-full" />
        <Skeleton className="h-36 w-full" />
      </CardContent>
    </Card>
  )
}

function GuidanceError({ title, error }: { title: string; error: Error }) {
  return (
    <Alert tone="danger">
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{error.message || 'Highland could not load this guidance.'}</AlertDescription>
    </Alert>
  )
}

function profileLabel(profile: CandidateAssessment['candidate']['testedProfile']) {
  const provider = [profile.providerKind, profile.providerVersion].filter(Boolean).join(' ')
  const driver = [profile.driver, profile.driverVersion].filter(Boolean).join(' ')
  return `${provider || 'Unknown provider'} · ${driver || 'Unknown driver'}`
}

function formatTimestamp(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.valueOf()) ? 'Time unavailable' : date.toLocaleString()
}

function eligibilityTone(eligibility: CandidateAssessment['eligibility']): 'success' | 'warning' | 'danger' {
  if (eligibility === 'eligible') return 'success'
  if (eligibility === 'ineligible') return 'danger'
  return 'warning'
}

function factTone(state: 'supported' | 'unsupported' | 'unknown'): 'success' | 'warning' | 'danger' {
  if (state === 'supported') return 'success'
  if (state === 'unsupported') return 'danger'
  return 'warning'
}

function surfaceTone(surface: ActionSurface): 'success' | 'info' | 'warning' | 'default' {
  if (surface === 'highland') return 'success'
  if (surface === 'rook-cr') return 'info'
  if (surface === 'ceph-dashboard' || surface === 'ceph-cli') return 'warning'
  return 'default'
}

function surfaceLabel(surface: ActionSurface) {
  const labels: Record<ActionSurface, string> = {
    highland: 'Highland workflow',
    'rook-cr': 'Rook desired state',
    'ceph-dashboard': 'Ceph Dashboard',
    'ceph-cli': 'Ceph CLI specialist',
    runbook: 'Reviewed runbook',
    'observe-only': 'Observe only',
  }
  return labels[surface]
}
