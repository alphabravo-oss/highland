import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import type { ColumnDef } from '@tanstack/react-table'
import { AlertTriangle, CheckCircle2, Clock3, History, LockKeyhole, ShieldAlert, Workflow } from 'lucide-react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { storageClient } from '@/api/storage/client'
import { useStorageActions, useStorageOperation, useStorageOperations, useStorageProviders } from '@/api/storage/hooks'
import type { ActionAvailability, OperationPlan, OperationRequest, ProviderDescriptor, StorageOperation } from '@/api/storage/types'
import { DataTable } from '@/components/data/DataTable'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select } from '@/components/ui/select'
import { canonicalGraphId } from '@/api/storage/context'
import { ResourceContextLink } from './StorageContextPages'

function phaseTone(phase: string) {
  if (phase === 'Succeeded') return 'success'
  if (phase === 'Failed') return 'danger'
  if (phase === 'Running') return 'info'
  return 'default'
}

export function StorageOperationsPage() {
  const [params, setParams] = useSearchParams()
  const filters = { provider: params.get('provider') || undefined, action: params.get('action') || undefined, state: params.get('state') || undefined, user: params.get('user') || undefined, limit: 100 }
  const operations = useStorageOperations(filters)
  const actions = useStorageActions()
  const providers = useStorageProviders()
  const providerList = providers.data?.data ?? []
  const scopedProvider = providerList.find((candidate) => candidate.id === filters.provider)
  const portableProviderIds = actions.data?.portableProviderIds ?? []
  const portableProviderAllowed = !scopedProvider || portableProviderIds.includes('*') || portableProviderIds.includes(scopedProvider.id)
  const portableActions = (actions.data?.data ?? []).filter((entry) => !entry.action.providerKind).map((entry) => portableProviderAllowed ? entry : {
    ...entry,
    enabled: false,
    available: false,
    unavailableReason: `Common Kubernetes workflows are disabled for ${scopedProvider?.displayName ?? filters.provider}`,
  })
  const nativeActions = (actions.data?.data ?? []).filter((entry) =>
    scopedProvider
      ? entry.action.providerKind === scopedProvider.kind
      : Boolean(entry.action.providerKind),
  )
  const relevantActions = [...nativeActions, ...portableActions]
  const displayName = scopedProvider?.displayName || filters.provider
  const title = displayName ? `${displayName} operations` : 'Storage operations'
  const writesEnabled = Boolean(actions.data?.writesEnabled)
  const columns = useMemo<ColumnDef<StorageOperation, any>[]>(() => [
    { accessorKey: 'name', header: 'Operation', cell: ({ row }) => <Link className="font-medium text-[var(--color-primary)] hover:underline" to={`/storage/operations/${row.original.name}`}>{row.original.name}</Link> },
    { id: 'action', header: 'Action', accessorFn: (row) => row.spec.actionId }, { id: 'provider', header: 'Provider', accessorFn: (row) => row.spec.providerId || 'kubernetes' },
    { id: 'target', header: 'Target', accessorFn: (row) => `${row.spec.target.namespace ? `${row.spec.target.namespace}/` : ''}${row.spec.target.name}` },
    { id: 'phase', header: 'Phase', cell: ({ row }) => <Badge tone={phaseTone(row.original.status.phase)}>{row.original.status.phase}</Badge> },
    { id: 'requester', header: 'Requester', accessorFn: (row) => row.spec.requester }, { accessorKey: 'creationTimestamp', header: 'Created' },
  ], [])
  const setFilter = (key: string, value: string) => { const next = new URLSearchParams(params); if (value) next.set(key, value); else next.delete(key); setParams(next) }
  return <div data-testid="storage-operations-page">
    <PageHeader
      title={title}
      description={scopedProvider
        ? `Plan and audit changes attributed to ${scopedProvider.displayName}. Provider-native controls are kept separate from portable Kubernetes storage workflows.`
        : 'Plan and audit storage changes across providers. Provider-native controls are kept separate from portable Kubernetes workflows.'}
      actions={<Badge tone={writesEnabled ? 'warning' : 'info'}>{writesEnabled ? 'changes enabled' : 'changes disabled'}</Badge>}
    />
    <OperationSafetyStatus provider={scopedProvider} writesEnabled={writesEnabled} nativeActions={nativeActions} portableProviderAllowed={portableProviderAllowed} />
    <div className="mb-5 mt-4 grid items-start gap-4 xl:grid-cols-2">
      <WorkflowGroup
        title={scopedProvider ? `${scopedProvider.displayName} controls` : 'Provider-native controls'}
        description={nativeActions.length
          ? scopedProvider
            ? `Actions implemented specifically for the ${scopedProvider.displayName} control plane.`
            : 'Actions implemented against a provider-specific control plane.'
          : 'Provider-specific durable workflows will appear here when this integration is implemented.'}
        actions={nativeActions}
        isLoading={actions.isLoading || providers.isLoading}
        error={(actions.error || providers.error) as Error | null}
        preview={!writesEnabled}
        emptyMessage={scopedProvider
          ? `Durable native workflow integration: not implemented for ${scopedProvider.displayName}. Use the provider's dedicated management surfaces for native changes.`
          : 'No provider-native workflows are currently registered.'}
      />
      <WorkflowGroup
        title="Kubernetes storage workflows"
        description={scopedProvider
          ? `Portable PVC and snapshot changes. ${scopedProvider.displayName} is selected through the target resource or one of its StorageClasses.`
          : 'Portable PVC and snapshot changes. The target resource or StorageClass determines the CSI provider.'}
        actions={portableActions}
        isLoading={actions.isLoading}
        error={actions.error as Error | null}
        preview={!writesEnabled}
        emptyMessage="No portable Kubernetes storage workflows are registered."
      />
    </div>
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2"><History size={17} /> Operation history</CardTitle>
        <p className="text-xs text-[var(--color-muted-foreground)]">These filters apply to durable operation records, not to the supported-workflow catalogue above.</p>
      </CardHeader>
      <CardContent>
        <div className="mb-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <OperationFilter label="Provider">
            <Select aria-label="Provider filter" value={filters.provider ?? ''} onChange={(event) => setFilter('provider', event.target.value)}>
              <option value="">All providers</option>
              {providerList.map((provider) => <option key={provider.id} value={provider.id}>{provider.displayName}</option>)}
            </Select>
          </OperationFilter>
          <OperationFilter label="Requester">
            <Input placeholder="Any user" aria-label="User filter" value={filters.user ?? ''} onChange={(event) => setFilter('user', event.target.value)} />
          </OperationFilter>
          <OperationFilter label="Action">
            <Select aria-label="Action filter" value={filters.action ?? ''} onChange={(event) => setFilter('action', event.target.value)}>
              <option value="">All relevant actions</option>
              {relevantActions.map((entry) => <option key={entry.action.id} value={entry.action.id}>{actionLabel(entry.action.id)}</option>)}
            </Select>
          </OperationFilter>
          <OperationFilter label="Phase">
            <Select aria-label="State filter" value={filters.state ?? ''} onChange={(event) => setFilter('state', event.target.value)}>
              <option value="">All phases</option>
              {['Pending', 'Running', 'Succeeded', 'Failed', 'Cancelled'].map((phase) => <option key={phase} value={phase}>{phase}</option>)}
            </Select>
          </OperationFilter>
        </div>
        <QueryState isLoading={operations.isLoading} error={operations.error as Error | null} onRetry={() => void operations.refetch()}>
          <DataTable
            columns={columns}
            data={operations.data?.data ?? []}
            tableId="storage-operations"
            getRowId={(row) => row.name}
            enableExport
            exportName="highland-storage-operations"
            emptyState={<div className="space-y-1"><div className="font-medium text-[var(--color-foreground)]">{displayName ? `No ${displayName} operations yet` : 'No storage operations yet'}</div><div className="text-xs">No planned change matching these history filters has been submitted through Highland.</div></div>}
          />
        </QueryState>
      </CardContent>
    </Card>
  </div>
}

function OperationSafetyStatus({ provider, writesEnabled, nativeActions, portableProviderAllowed }: {
  provider?: ProviderDescriptor
  writesEnabled: boolean
  nativeActions: ActionAvailability[]
  portableProviderAllowed: boolean
}) {
  const nativeEnabled = nativeActions.some((entry) => entry.enabled)
  const nativeImplemented = nativeActions.length > 0
  const nativeState = provider ? (nativeImplemented ? 'Implemented' : 'Not implemented') : 'Partial coverage'
  return <div className="space-y-4" data-testid="operations-safety-status">
    <Alert tone={!writesEnabled ? 'default' : nativeEnabled || !provider ? 'warning' : 'default'}>
      <LockKeyhole size={18} />
      <AlertTitle>{!writesEnabled ? 'Changes are disabled' : provider && !nativeEnabled && !portableProviderAllowed ? `All ${provider.displayName} changes are disabled` : provider && !nativeEnabled ? `${provider.displayName} native changes are disabled` : 'Changes are enabled'}</AlertTitle>
      <AlertDescription>{!writesEnabled
        ? 'This is a Highland configuration state, not a provider health warning. You do not need to take action; workflow submissions are blocked cluster-wide.'
        : provider && !nativeEnabled && !portableProviderAllowed
          ? `Neither common Kubernetes workflows nor ${provider.displayName}-native workflows are permitted by the current admin policy.`
        : provider && !nativeEnabled
          ? `Portable Kubernetes workflows may be available, but ${provider.displayName}-native changes remain separately gated.`
          : 'Every change still requires a fresh plan, dependency review, role authorization, and explicit confirmation.'}</AlertDescription>
    </Alert>
    <Card>
      <CardHeader>
        <CardTitle>How to read this page</CardTitle>
      </CardHeader>
      <CardContent className="grid gap-4 md:grid-cols-3">
        <MeaningFact
          label="Execution mode"
          value={writesEnabled ? 'Changes enabled' : 'Changes disabled'}
          detail={writesEnabled ? 'Available workflows can generate and submit reviewed plans.' : 'Workflow cards are previews; Highland cannot submit them.'}
          tone={writesEnabled ? 'warning' : 'info'}
        />
        <MeaningFact
          label="Durable native integration"
          value={nativeState}
          detail={nativeImplemented
            ? 'Highland has provider-specific workflows in its planned, auditable operation pipeline.'
            : provider
              ? `Highland does not yet wrap ${provider.displayName}-specific controls in the durable operation pipeline.`
              : 'Provider-native workflow coverage currently varies by provider.'}
          tone={provider && nativeImplemented ? 'success' : 'default'}
        />
        <div>
          <div className="text-xs font-medium text-[var(--color-muted-foreground)]">Change risk</div>
          <div className="mt-2 flex flex-wrap gap-1.5">
            <RiskBadge risk="low" />
            <RiskBadge risk="medium" />
            <RiskBadge risk="high" />
            <RiskBadge risk="critical" />
          </div>
          <p className="mt-2 text-xs leading-relaxed text-[var(--color-muted-foreground)]">Risk describes the potential impact if executed. It is not an alert, health status, recommendation, or urgency level.</p>
          <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-2 gap-y-1 text-[11px] leading-relaxed text-[var(--color-muted-foreground)]">
            <dt className="font-medium text-[var(--color-foreground)]">Low</dt><dd>Creates a generally reversible resource.</dd>
            <dt className="font-medium text-[var(--color-foreground)]">Medium</dt><dd>Changes capacity, placement, or derived data.</dd>
            <dt className="font-medium text-[var(--color-foreground)]">High</dt><dd>May disrupt workloads or remove data references.</dd>
            <dt className="font-medium text-[var(--color-foreground)]">Critical</dt><dd>May affect a backend or create broad data-loss risk.</dd>
          </dl>
        </div>
      </CardContent>
    </Card>
  </div>
}

function MeaningFact({ label, value, detail, tone }: {
  label: string
  value: string
  detail: string
  tone: 'default' | 'info' | 'success' | 'warning'
}) {
  return <div>
    <div className="text-xs font-medium text-[var(--color-muted-foreground)]">{label}</div>
    <div className="mt-1"><Badge tone={tone}>{value}</Badge></div>
    <p className="mt-2 text-xs leading-relaxed text-[var(--color-muted-foreground)]">{detail}</p>
  </div>
}

function WorkflowGroup({
  title,
  description,
  actions,
  isLoading,
  error,
  preview,
  emptyMessage,
}: {
  title: string
  description: string
  actions: ActionAvailability[]
  isLoading: boolean
  error: Error | null
  preview: boolean
  emptyMessage: string
}) {
  return <Card>
    <CardHeader>
      <div className="flex items-center justify-between gap-3">
        <CardTitle className="flex items-center gap-2"><Workflow size={17} /> {title}</CardTitle>
        {preview ? <Badge tone="info">workflow preview</Badge> : null}
      </div>
      <p className="text-xs text-[var(--color-muted-foreground)]">{description}</p>
    </CardHeader>
    <CardContent>
      <QueryState isLoading={isLoading} error={error}>
        {actions.length ? <div className="grid gap-2 sm:grid-cols-2">{actions.map((entry) => <ActionLink key={entry.action.id} entry={entry} />)}</div> : <p className="rounded-md bg-[var(--color-muted)]/50 p-4 text-sm text-[var(--color-muted-foreground)]">{emptyMessage}</p>}
      </QueryState>
    </CardContent>
  </Card>
}

function ActionLink({ entry }: { entry: ActionAvailability }) {
  const content = <div className={`h-full rounded-md border p-3 ${entry.available ? 'border-[var(--color-border)] hover:border-[var(--color-primary)]' : 'border-[var(--color-border)] bg-[var(--color-muted)]/20'}`}><div className="flex items-start justify-between gap-2"><div><span className="text-sm font-medium">{actionLabel(entry.action.id)}</span><p className="mt-1 text-xs text-[var(--color-muted-foreground)]">{actionDescription(entry.action.id)}</p></div><div className="flex shrink-0 flex-col items-end gap-1"><Badge tone={entry.available ? 'success' : 'default'}>{entry.available ? 'available' : 'unavailable'}</Badge><RiskBadge risk={entry.action.risk} /></div></div>{entry.unavailableReason ? <p className="mt-2 flex items-start gap-1.5 text-xs font-medium text-[var(--color-muted-foreground)]"><LockKeyhole size={13} className="mt-0.5 shrink-0" />{gateExplanation(entry.unavailableReason)}</p> : null}</div>
  return entry.available ? <Link to={`/storage/actions/${entry.action.id}`}>{content}</Link> : content
}

function RiskBadge({ risk }: { risk: ActionAvailability['action']['risk'] }) {
  const tone = risk === 'critical' ? 'danger' : risk === 'high' ? 'warning' : risk === 'medium' ? 'info' : 'default'
  return <Badge tone={tone}>change risk: {risk}</Badge>
}

function OperationFilter({ label, children }: { label: string; children: React.ReactNode }) {
  return <label className="grid gap-1.5 text-xs font-medium text-[var(--color-muted-foreground)]"><span>{label}</span>{children}</label>
}

function actionLabel(id: string) {
  const labels: Record<string, string> = {
    'create-pvc': 'Create PVC',
    'expand-pvc': 'Expand PVC',
    'delete-pvc': 'Delete PVC',
    'create-snapshot': 'Create snapshot',
    'delete-snapshot': 'Delete snapshot',
    'restore-snapshot': 'Restore snapshot',
    'clone-pvc': 'Clone PVC',
    'create-ceph-blockpool': 'Create Ceph block pool',
    'delete-ceph-blockpool': 'Delete Ceph block pool',
    'create-ceph-rbd-storageclass': 'Create Ceph RBD StorageClass',
    'create-cephfs-storageclass': 'Create CephFS StorageClass',
    'delete-ceph-storageclass': 'Delete Ceph StorageClass',
    'longhorn-volume-attach': 'Attach Longhorn volume',
    'longhorn-volume-detach': 'Detach Longhorn volume',
    'longhorn-volume-replica-count': 'Change Longhorn replica count',
    'longhorn-volume-backup': 'Create Longhorn backup',
    'longhorn-recurring-job-add': 'Assign recurring job',
    'longhorn-recurring-job-remove': 'Remove recurring job',
    'longhorn-volume-salvage': 'Salvage Longhorn volume',
    'longhorn-engine-upgrade': 'Upgrade Longhorn volume engine',
    'longhorn-backup-target-configure': 'Configure Longhorn backup target',
    'longhorn-backup-delete': 'Delete Longhorn backup',
    'longhorn-backup-restore': 'Restore Longhorn backup',
  }
  return labels[id] || id.replaceAll('-', ' ').replace(/\b\w/g, (letter) => letter.toUpperCase())
}

function actionDescription(id: string) {
  const descriptions: Record<string, string> = {
    'create-pvc': 'Create a claim through a selected StorageClass.',
    'expand-pvc': 'Increase the requested capacity of an existing claim.',
    'delete-pvc': 'Delete a claim after workload and reclaim-policy checks.',
    'create-snapshot': 'Create a CSI snapshot from an existing claim.',
    'delete-snapshot': 'Delete a CSI snapshot with deletion-policy review.',
    'restore-snapshot': 'Restore a new claim from a ready snapshot.',
    'clone-pvc': 'Create a new claim using another claim as its data source.',
    'create-ceph-blockpool': 'Create a replicated Rook CephBlockPool.',
    'delete-ceph-blockpool': 'Delete a CephBlockPool after deep dependency checks.',
    'create-ceph-rbd-storageclass': 'Create a Kubernetes StorageClass backed by Ceph RBD.',
    'create-cephfs-storageclass': 'Create a Kubernetes StorageClass backed by CephFS.',
    'delete-ceph-storageclass': 'Delete a Ceph StorageClass after usage checks.',
    'longhorn-volume-attach': 'Attach a detached Longhorn volume to a validated node.',
    'longhorn-volume-detach': 'Detach a Longhorn volume after workload-impact review.',
    'longhorn-volume-replica-count': 'Change volume redundancy after replica-safety checks.',
    'longhorn-volume-backup': 'Back up an existing Longhorn snapshot to a configured target.',
    'longhorn-recurring-job-add': 'Assign an existing Longhorn recurring job to a volume.',
    'longhorn-recurring-job-remove': 'Stop a recurring job from running on a volume.',
    'longhorn-volume-salvage': 'Select surviving replica data for a faulted volume.',
    'longhorn-engine-upgrade': 'Move a volume to a deployed Longhorn engine image.',
    'longhorn-backup-target-configure': 'Create or replace a Longhorn backup-target configuration.',
    'longhorn-backup-delete': 'Permanently remove one Longhorn-native recovery point.',
    'longhorn-backup-restore': 'Create a new Longhorn volume from a selected backup.',
  }
  return descriptions[id] || 'Plan and execute a reviewed storage change.'
}

function gateExplanation(reason: string) {
  if (reason === 'storage.writes.enabled is disabled') return 'Cluster-wide storage changes are disabled.'
  if (reason === 'providers.rookCeph.writes.enabled is disabled') return 'Rook/Ceph native changes are disabled.'
  if (reason === 'providers.rookCeph.writes.allowPoolDelete is disabled') return 'Ceph pool deletion is separately disabled.'
  if (reason === 'providers.rookCeph.writes.allowStorageClassDelete is disabled') return 'Ceph StorageClass deletion is separately disabled.'
  return reason
}

export function StorageActionPage() {
  const { actionId = '' } = useParams()
  const actions = useStorageActions()
  const entry = actions.data?.data.find((candidate) => candidate.action.id === actionId)
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [namespace, setNamespace] = useState(actionId.includes('ceph') && actionId.includes('blockpool') ? 'rook-ceph' : 'default')
  const [name, setName] = useState('')
  const [providerId, setProviderId] = useState(actionProvider(actionId))
  const [parameters, setParameters] = useState<Record<string, unknown>>(() => defaultParameters(actionId))
  const [typedName, setTypedName] = useState('')
  const [warningsAcknowledged, setWarningsAcknowledged] = useState(false)
  const [plan, setPlan] = useState<OperationPlan | null>(null)
  const [formError, setFormError] = useState('')
  const [confirmOpen, setConfirmOpen] = useState(false)

  useEffect(() => {
    setNamespace(actionId.includes('ceph') && actionId.includes('blockpool') ? 'rook-ceph' : 'default')
    setName('')
    setProviderId(actionProvider(actionId))
    setParameters(defaultParameters(actionId))
    setTypedName('')
    setWarningsAcknowledged(false)
    setPlan(null)
    setFormError('')
    setConfirmOpen(false)
  }, [actionId])

  function request(): OperationRequest {
    return { actionId, providerId: providerId || undefined, target: { kind: targetKind(actionId), namespace: targetNeedsNamespace(actionId) ? namespace : undefined, name }, parameters }
  }

  function updateParameter(key: string, value: unknown) {
    setParameters((current) => ({ ...current, [key]: value }))
    setPlan(null)
    setConfirmOpen(false)
  }

  const planMutation = useMutation({ mutationFn: storageClient.plan, onSuccess: (value) => { setPlan(value); setWarningsAcknowledged(false); setTypedName(''); setConfirmOpen(false); setFormError('') }, onError: (error) => setFormError((error as Error).message) })
  const submitMutation = useMutation({ mutationFn: storageClient.submit, onSuccess: (value) => { void qc.invalidateQueries({ queryKey: ['storage', 'operations'] }); navigate(`/storage/operations/${value.operationId}`) }, onError: (error) => setFormError((error as Error).message) })
  function createPlan() { try { setPlan(null); planMutation.mutate(request()) } catch (error) { setFormError((error as Error).message) } }
  function submit() { if (!plan) return; try { const next = request(); next.target = plan.target; next.confirmation = { challenge: plan.challenge, typedName: typedName || undefined, warningsAcknowledged }; submitMutation.mutate(next) } catch (error) { setFormError((error as Error).message) } }

  return <div data-testid="storage-action-page">
    <PageHeader title={actionId.replaceAll('-', ' ')} description="Plan the change, review server-authoritative dependencies and warnings, then confirm the exact approved plan." />
    <QueryState isLoading={actions.isLoading} error={actions.error as Error | null} onRetry={() => void actions.refetch()}>
      {!entry?.available ? <Alert tone="warning"><ShieldAlert size={18} /><AlertTitle>Action unavailable</AlertTitle><AlertDescription>{entry?.unavailableReason ?? 'This action is not exposed by the current capability and role policy.'}</AlertDescription></Alert> : <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.3fr)]">
        <Card><CardHeader><CardTitle>Request</CardTitle></CardHeader><CardContent className="space-y-4">
          {targetNeedsNamespace(actionId) ? <div><Label htmlFor="operation-namespace">Namespace</Label><Input id="operation-namespace" value={namespace} onChange={(e) => { setNamespace(e.target.value); setPlan(null); setConfirmOpen(false) }} /></div> : null}
          <div><Label htmlFor="operation-name">{targetLabel(actionId)}</Label><Input id="operation-name" value={name} onChange={(e) => { setName(e.target.value); setPlan(null); setConfirmOpen(false) }} /></div>
          {actionId.includes('ceph') ? <div><Label htmlFor="operation-provider">Provider</Label><Input id="operation-provider" value={providerId} onChange={(e) => { setProviderId(e.target.value); setPlan(null) }} /></div> : null}
          <ActionParameterFields actionId={actionId} parameters={parameters} update={updateParameter} />
          <p className="text-xs text-[var(--color-muted-foreground)]">Only the typed fields shown for this action are submitted. Credentials and secret values are never accepted here.</p>
          <Button onClick={createPlan} disabled={!name || planMutation.isPending}>{planMutation.isPending ? 'Planning…' : 'Generate safe plan'}</Button>
          {formError ? <Alert tone="danger"><AlertTriangle size={18} /><AlertTitle>Request rejected</AlertTitle><AlertDescription>{formError}</AlertDescription></Alert> : null}
        </CardContent></Card>
        <Card><CardHeader><CardTitle>Authoritative review</CardTitle></CardHeader><CardContent>{plan ? <PlanReview plan={plan} onRequestConfirmation={() => setConfirmOpen(true)} /> : <div className="py-16 text-center text-sm text-[var(--color-muted-foreground)]"><Clock3 className="mx-auto mb-3" />Generate a plan to see current dependencies, dry-run checks, blast radius, and confirmation requirements.</div>}</CardContent></Card>
      </div>}
    </QueryState>
    <OperationApprovalDialog
      plan={plan}
      open={confirmOpen}
      onOpenChange={setConfirmOpen}
      typedName={typedName}
      setTypedName={setTypedName}
      warningsAcknowledged={warningsAcknowledged}
      setWarningsAcknowledged={setWarningsAcknowledged}
      onConfirm={submit}
      submitting={submitMutation.isPending}
    />
  </div>
}

function PlanReview({ plan, onRequestConfirmation }: { plan: OperationPlan; onRequestConfirmation: () => void }) {
  return <div className="space-y-4">
    <div className="grid gap-2 text-sm sm:grid-cols-2"><div><span className="text-[var(--color-muted-foreground)]">Provider</span><div className="font-medium">{plan.providerId || 'Kubernetes'}</div></div><div><span className="text-[var(--color-muted-foreground)]">Cluster</span><div className="font-medium">local</div></div><div><span className="text-[var(--color-muted-foreground)]">Namespace</span><div className="font-medium">{plan.target.namespace || 'cluster-scoped'}</div></div><div><span className="text-[var(--color-muted-foreground)]">Blast radius</span><div className="font-medium">{plan.blastRadius}</div></div><div><span className="text-[var(--color-muted-foreground)]">Target</span><div className="font-medium">{plan.target.name}</div></div><div><span className="text-[var(--color-muted-foreground)]">Required role</span><div className="font-medium">{plan.action.minimumRole}</div></div></div>
    <div><h3 className="mb-2 text-sm font-semibold">Preflight checks</h3><div className="space-y-2">{plan.checks.map((check) => <div key={check.id} className="flex gap-2 rounded-md bg-[var(--color-muted)] p-2 text-sm"><CheckCircle2 size={16} className="mt-0.5 text-[var(--color-success)]" /><div><span className="font-medium">{check.id}</span><div className="text-[var(--color-muted-foreground)]">{check.message}</div></div></div>)}</div></div>
    {plan.dependencies?.length ? <div><h3 className="mb-2 text-sm font-semibold">Dependencies checked</h3><ul className="space-y-2">{plan.dependencies.map((dependency) => <li key={`${dependency.kind}/${dependency.namespace}/${dependency.name}`} className="rounded-md border border-[var(--color-border)] p-2 text-sm"><span className="font-medium">{dependency.kind}</span> {dependency.namespace ? `${dependency.namespace}/` : ''}{dependency.name}{dependency.uid ? <span className="ml-2 font-mono text-xs text-[var(--color-muted-foreground)]">UID {dependency.uid}</span> : null}</li>)}</ul></div> : null}
    {plan.warnings?.length ? <Alert tone="warning"><AlertTriangle size={18} /><AlertTitle>Warnings require review</AlertTitle><AlertDescription><ul className="list-disc pl-5">{plan.warnings.map((warning) => <li key={warning}>{warning}</li>)}</ul></AlertDescription></Alert> : null}
    <div><h3 className="mb-2 text-sm font-semibold">Rendered changes</h3>{plan.resources.map((resource) => <div key={`${resource.kind}/${resource.name}`} className="rounded-md border border-[var(--color-border)] p-3"><div className="flex justify-between text-sm"><span className="font-medium">{resource.kind} {resource.namespace ? `${resource.namespace}/` : ''}{resource.name}</span><Badge tone="info">{resource.operation}</Badge></div>{resource.manifest ? <pre className="mt-2 max-h-64 overflow-auto rounded bg-[var(--color-muted)] p-2 text-xs">{JSON.stringify(resource.manifest, null, 2)}</pre> : null}</div>)}</div>
    <Button variant={plan.action.risk === 'critical' || plan.action.risk === 'high' ? 'destructive' : 'default'} onClick={onRequestConfirmation}>Review and confirm</Button>
    <p className="text-xs text-[var(--color-muted-foreground)]">Challenge expires {new Date(plan.challengeExpiresAt).toLocaleString()}. The server reruns all preflight checks before mutation.</p>
  </div>
}

export function OperationApprovalDialog({
  plan,
  open,
  onOpenChange,
  typedName,
  setTypedName,
  warningsAcknowledged,
  setWarningsAcknowledged,
  onConfirm,
  submitting,
}: {
  plan: OperationPlan | null
  open: boolean
  onOpenChange: (open: boolean) => void
  typedName: string
  setTypedName: (value: string) => void
  warningsAcknowledged: boolean
  setWarningsAcknowledged: (value: boolean) => void
  onConfirm: () => void
  submitting: boolean
}) {
  if (!plan) return null
  const typedRequired = plan.action.confirmation === 'typed-name'
  const disabled = submitting
    || (Boolean(plan.warnings?.length) && !warningsAcknowledged)
    || (typedRequired && typedName !== plan.target.name && typedName !== `${plan.target.namespace}/${plan.target.name}`)
  return <Dialog
    open={open}
    onOpenChange={onOpenChange}
    title={`Confirm ${actionLabel(plan.action.id)}`}
    description="This modal is the final approval boundary. Highland will rerun the plan before executing it."
    className="max-w-2xl"
    footer={<>
      <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
      <Button
        type="button"
        variant={plan.action.risk === 'critical' || plan.action.risk === 'high' ? 'destructive' : 'default'}
        disabled={disabled}
        onClick={onConfirm}
        data-testid="operation-confirm-submit"
      >
        {submitting ? 'Submitting…' : `Confirm ${actionLabel(plan.action.id)}`}
      </Button>
    </>}
  >
    <div className="space-y-4">
      <div className="grid gap-3 rounded-md bg-[var(--color-muted)]/50 p-3 text-sm sm:grid-cols-2">
        <div><span className="text-xs text-[var(--color-muted-foreground)]">Target</span><div className="font-medium">{plan.target.namespace ? `${plan.target.namespace}/` : ''}{plan.target.name}</div></div>
        <div><span className="text-xs text-[var(--color-muted-foreground)]">Provider</span><div className="font-medium">{plan.providerId || 'Kubernetes'}</div></div>
        <div><span className="text-xs text-[var(--color-muted-foreground)]">Blast radius</span><div className="font-medium">{plan.blastRadius}</div></div>
        <div><span className="text-xs text-[var(--color-muted-foreground)]">Classification</span><div className="mt-1"><RiskBadge risk={plan.action.risk} /></div></div>
      </div>
      {plan.warnings?.length ? <Alert tone="warning"><AlertTriangle size={18} /><AlertTitle>Explicit warning acknowledgement required</AlertTitle><AlertDescription><ul className="list-disc pl-5">{plan.warnings.map((warning) => <li key={warning}>{warning}</li>)}</ul><label className="mt-3 flex items-start gap-2 font-medium"><input className="mt-0.5" type="checkbox" checked={warningsAcknowledged} onChange={(event) => setWarningsAcknowledged(event.target.checked)} />I reviewed the current warnings and blast radius.</label></AlertDescription></Alert> : null}
      {typedRequired ? <div className="rounded-md border border-[var(--color-destructive)]/40 p-3"><Label htmlFor="typed-confirmation">Type <span className="font-mono">{plan.target.name}</span> to confirm</Label><p className="mb-2 mt-1 text-xs text-[var(--color-muted-foreground)]">This additional confirmation is required because the operation can detach, delete, replace, or materially alter provider data.</p><Input id="typed-confirmation" value={typedName} onChange={(event) => setTypedName(event.target.value)} autoComplete="off" data-testid="typed-operation-confirmation" /></div> : <p className="text-sm text-[var(--color-muted-foreground)]">Review the target and rendered changes above, then confirm this planned operation.</p>}
    </div>
  </Dialog>
}

export function StorageOperationDetailPage() {
  const { operationId = '' } = useParams()
  const query = useStorageOperation(operationId)
  const operation = query.data
  return <div data-testid="storage-operation-detail"><PageHeader title={operation?.name ?? 'Storage operation'} description="Durable reconciliation state and sanitized server-authoritative diagnostics." />
    <QueryState isLoading={query.isLoading} error={query.error as Error | null} onRetry={() => void query.refetch()}>{operation ? <div className="grid gap-4 lg:grid-cols-3">
      <Card><CardHeader><CardTitle>Status</CardTitle></CardHeader><CardContent className="space-y-2 text-sm"><div className="flex justify-between"><span>Phase</span><Badge tone={phaseTone(operation.status.phase)}>{operation.status.phase}</Badge></div><div className="flex justify-between"><span>Step</span><span>{operation.status.step || '—'}</span></div><div className="flex justify-between"><span>Retries</span><span>{operation.status.retries ?? 0}</span></div>{operation.status.errorCode ? <Alert tone="danger"><AlertTriangle size={18} /><AlertTitle>{operation.status.errorCode}</AlertTitle><AlertDescription>{operation.status.diagnostics}<p className="mt-2 font-medium">{remediation(operation.status.errorCode)}</p></AlertDescription></Alert> : null}</CardContent></Card>
      <Card><CardHeader><CardTitle>Request</CardTitle></CardHeader><CardContent className="space-y-2 text-sm"><div>{operation.spec.actionId}</div><div>{operation.spec.providerId || 'kubernetes'}</div><OperationTargetLink operation={operation} /><div className="text-[var(--color-muted-foreground)]">Requested by {operation.spec.requester} ({operation.spec.requesterRole})</div></CardContent></Card>
      <Card><CardHeader><CardTitle>Audit linkage</CardTitle></CardHeader><CardContent className="space-y-2 text-sm"><div className="font-mono text-xs">Operation: {operation.name}</div><div className="break-all font-mono text-xs">Plan: {operation.spec.planHash}</div></CardContent></Card>
      {operation.spec.providerId ? <Card className="lg:col-span-3"><CardHeader><CardTitle>Context and impact</CardTitle></CardHeader><CardContent><ResourceContextLink provider={operation.spec.providerId} kind="storage-operation" id={canonicalGraphId('storage-operation', operation.spec.providerId, operation.spec.target.namespace ?? '', operation.name)} /></CardContent></Card> : null}
      <Card className="lg:col-span-3"><CardHeader><CardTitle>Timeline</CardTitle></CardHeader><CardContent className="space-y-3"><Timeline label="Requested" at={operation.spec.requestedAt} /><Timeline label="Execution started" at={operation.status.startedAt} />{operation.status.conditions?.map((condition) => <Timeline key={`${condition.type}-${condition.lastTransitionTime}`} label={`${condition.type}: ${condition.reason ?? ''}`} at={condition.lastTransitionTime} detail={condition.message} />)}<Timeline label="Finished" at={operation.status.finishedAt} /></CardContent></Card>
    </div> : null}</QueryState>
  </div>
}

function Timeline({ label, at, detail }: { label: string; at?: string; detail?: string }) { if (!at) return null; return <div className="flex gap-3"><div className="mt-1 size-2 rounded-full bg-[var(--color-primary)]" /><div><div className="text-sm font-medium">{label}</div><div className="text-xs text-[var(--color-muted-foreground)]">{new Date(at).toLocaleString()}</div>{detail ? <p className="mt-1 text-sm">{detail}</p> : null}</div></div> }

function OperationTargetLink({ operation }: { operation: StorageOperation }) {
  const target = operation.spec.target
  let path = ''
  if (target.kind === 'PersistentVolumeClaim' && target.namespace) path = `/storage/claims/${encodeURIComponent(target.namespace)}/${encodeURIComponent(target.name)}`
  if (target.kind === 'StorageClass') path = `/storage/classes?search=${encodeURIComponent(target.name)}`
  if (target.kind === 'VolumeSnapshot') path = `/storage/snapshots?namespace=${encodeURIComponent(target.namespace ?? '')}&search=${encodeURIComponent(target.name)}`
  if (target.kind === 'CephBlockPool' && operation.spec.providerId) path = `/storage/providers/${encodeURIComponent(operation.spec.providerId)}/ceph/pools/${encodeURIComponent(target.name)}`
  const label = `${target.namespace ? `${target.namespace}/` : ''}${target.name}`
  return path ? <Link className="text-[var(--color-primary)] hover:underline" to={path}>{label}</Link> : <div>{label}</div>
}

function remediation(code: string) {
  if (code.includes('STALE')) return 'Generate a new plan and review the changed resource or dependency state.'
  if (code.includes('DEPENDENC') || code.includes('WORKLOAD')) return 'Remove or stop the listed dependencies, then generate a new plan.'
  if (code.includes('HEALTH') || code.includes('POSTFLIGHT')) return 'Restore provider health and fresh runtime visibility before retrying.'
  if (code.includes('FORBIDDEN') || code.includes('NAMESPACE')) return 'Verify the Highland role, namespace scope, and Kubernetes RBAC policy.'
  if (code.includes('TIMEOUT')) return 'Inspect the target controller conditions and events; do not force-remove finalizers.'
  return 'Review the authoritative diagnostic and provider events, then generate a fresh plan after correcting the cause.'
}

type ParameterFieldsProps = { actionId: string; parameters: Record<string, unknown>; update: (key: string, value: unknown) => void }

function TextParameter({ name, label, value, update, placeholder }: { name: string; label: string; value: unknown; update: ParameterFieldsProps['update']; placeholder?: string }) {
  return <div><Label htmlFor={`parameter-${name}`}>{label}</Label><Input id={`parameter-${name}`} value={String(value ?? '')} placeholder={placeholder} onChange={(event) => update(name, event.target.value)} /></div>
}

function SelectParameter({ name, label, value, update, options }: { name: string; label: string; value: unknown; update: ParameterFieldsProps['update']; options: Array<[string, string]> }) {
  return <div><Label htmlFor={`parameter-${name}`}>{label}</Label><Select id={`parameter-${name}`} value={String(value ?? '')} onChange={(event) => update(name, event.target.value)}>{options.map(([optionValue, optionLabel]) => <option key={optionValue} value={optionValue}>{optionLabel}</option>)}</Select></div>
}

function BooleanParameter({ name, label, value, update }: { name: string; label: string; value: unknown; update: ParameterFieldsProps['update'] }) {
  return <label className="flex items-start gap-2 rounded-md border border-[var(--color-border)] p-3 text-sm"><input className="mt-0.5" type="checkbox" checked={Boolean(value)} onChange={(event) => update(name, event.target.checked)} /><span>{label}</span></label>
}

function AccessModeParameter({ parameters, update }: Pick<ParameterFieldsProps, 'parameters' | 'update'>) {
  const selected = Array.isArray(parameters.accessModes) ? parameters.accessModes.map(String) : []
  const toggle = (mode: string, checked: boolean) => update('accessModes', checked ? [...new Set([...selected, mode])] : selected.filter((value) => value !== mode))
  return <fieldset><legend className="text-sm font-medium">Access modes</legend><div className="mt-1 grid gap-2 sm:grid-cols-2">{['ReadWriteOnce', 'ReadOnlyMany', 'ReadWriteMany', 'ReadWriteOncePod'].map((mode) => <label key={mode} className="flex items-center gap-2 text-sm"><input type="checkbox" checked={selected.includes(mode)} onChange={(event) => toggle(mode, event.target.checked)} />{mode}</label>)}</div></fieldset>
}

function VolumeParameters({ parameters, update, source }: Pick<ParameterFieldsProps, 'parameters' | 'update'> & { source?: 'snapshot' | 'claim' }) {
  return <>
    {source === 'snapshot' ? <TextParameter name="sourceSnapshot" label="Source snapshot" value={parameters.sourceSnapshot} update={update} /> : null}
    {source === 'claim' ? <TextParameter name="sourceClaim" label="Source claim" value={parameters.sourceClaim} update={update} /> : null}
    <TextParameter name="storageClass" label="StorageClass" value={parameters.storageClass} update={update} />
    <TextParameter name="size" label="Requested capacity" value={parameters.size} update={update} placeholder="10Gi" />
    <AccessModeParameter parameters={parameters} update={update} />
    <SelectParameter name="volumeMode" label="Volume mode" value={parameters.volumeMode} update={update} options={[["Filesystem", "Filesystem"], ["Block", "Block"]]} />
  </>
}

function StorageClassParameters({ actionId, parameters, update }: ParameterFieldsProps) {
  const rbd = actionId === 'create-ceph-rbd-storageclass'
  return <>
    {rbd ? <TextParameter name="pool" label="Ready Ceph block pool" value={parameters.pool} update={update} /> : <><TextParameter name="filesystem" label="Ready Ceph filesystem" value={parameters.filesystem} update={update} /><TextParameter name="pool" label="CephFS data pool" value={parameters.pool} update={update} /><TextParameter name="subvolumeGroup" label="Subvolume group" value={parameters.subvolumeGroup} update={update} /></>}
    <SelectParameter name="reclaimPolicy" label="Reclaim policy" value={parameters.reclaimPolicy} update={update} options={[["Delete", "Delete"], ["Retain", "Retain"]]} />
    <SelectParameter name="volumeBindingMode" label="Volume binding mode" value={parameters.volumeBindingMode} update={update} options={[["Immediate", "Immediate"], ["WaitForFirstConsumer", "Wait for first consumer"]]} />
    {rbd ? <><SelectParameter name="filesystemType" label="Filesystem type" value={parameters.filesystemType} update={update} options={[["ext4", "ext4"], ["xfs", "xfs"]]} /><SelectParameter name="imageFeatures" label="RBD image features" value={parameters.imageFeatures} update={update} options={[["layering", "layering"], ["layering,fast-diff,object-map,deep-flatten,exclusive-lock", "layering + advanced features"]]} /><BooleanParameter name="encrypted" label="Enable RBD encryption through the Rook CSI configuration" value={parameters.encrypted} update={update} /></> : null}
    <TextParameter name="mountOptions" label="Mount options (comma-separated allowlisted values)" value={Array.isArray(parameters.mountOptions) ? parameters.mountOptions.join(', ') : ''} update={(_, value) => update('mountOptions', String(value).split(',').map((item) => item.trim()).filter(Boolean))} />
    <BooleanParameter name="allowVolumeExpansion" label="Allow volume expansion" value={parameters.allowVolumeExpansion} update={update} />
    <BooleanParameter name="default" label="Request default StorageClass status (blocked if another default exists)" value={parameters.default} update={update} />
  </>
}

function ActionParameterFields({ actionId, parameters, update }: ParameterFieldsProps) {
  if (actionId === 'create-pvc') return <VolumeParameters parameters={parameters} update={update} />
  if (actionId === 'restore-snapshot') return <VolumeParameters parameters={parameters} update={update} source="snapshot" />
  if (actionId === 'clone-pvc') return <VolumeParameters parameters={parameters} update={update} source="claim" />
  if (actionId === 'expand-pvc') return <TextParameter name="size" label="New requested capacity" value={parameters.size} update={update} placeholder="20Gi" />
  if (actionId === 'create-snapshot') return <><TextParameter name="sourceClaim" label="Source claim" value={parameters.sourceClaim} update={update} /><TextParameter name="snapshotClass" label="VolumeSnapshotClass" value={parameters.snapshotClass} update={update} /></>
  if (actionId === 'create-ceph-rbd-storageclass' || actionId === 'create-cephfs-storageclass') return <StorageClassParameters actionId={actionId} parameters={parameters} update={update} />
  if (actionId === 'create-ceph-blockpool') return <><SelectParameter name="replicatedSize" label="Replica count" value={parameters.replicatedSize} update={(_, value) => update('replicatedSize', Number(value))} options={Array.from({ length: 8 }, (_, index) => [String(index + 2), String(index + 2)])} /><SelectParameter name="failureDomain" label="Failure domain" value={parameters.failureDomain} update={update} options={[["host", "Host"], ["rack", "Rack"], ["zone", "Zone"]]} /><SelectParameter name="deviceClass" label="Device class" value={parameters.deviceClass} update={update} options={[["", "Any validated device class"], ["hdd", "HDD"], ["ssd", "SSD"], ["nvme", "NVMe"]]} /><SelectParameter name="compressionMode" label="Compression mode" value={parameters.compressionMode} update={update} options={[["none", "None"], ["passive", "Passive"], ["aggressive", "Aggressive"], ["force", "Force"]]} /></>
  if (actionId === 'longhorn-volume-attach') return <TextParameter name="hostId" label="Target Longhorn node" value={parameters.hostId} update={update} />
  if (actionId === 'longhorn-volume-detach') return <BooleanParameter name="force" label="Force detach if normal detachment cannot complete" value={parameters.force} update={update} />
  if (actionId === 'longhorn-volume-replica-count') return <TextParameter name="replicaCount" label="Desired replica count" value={parameters.replicaCount} update={(_, value) => update('replicaCount', Number(value))} />
  if (actionId === 'longhorn-volume-backup') return <><TextParameter name="snapshotName" label="Existing Longhorn snapshot" value={parameters.snapshotName} update={update} /><TextParameter name="backupTargetName" label="Backup target (optional)" value={parameters.backupTargetName} update={update} /><SelectParameter name="backupMode" label="Backup mode" value={parameters.backupMode} update={update} options={[["incremental", "Incremental"], ["full", "Full"]]} /></>
  if (actionId === 'longhorn-recurring-job-add' || actionId === 'longhorn-recurring-job-remove') return <TextParameter name="jobName" label="Recurring job name" value={parameters.jobName} update={update} />
  if (actionId === 'longhorn-volume-salvage') return <TextParameter name="replicas" label="Replica names (comma-separated)" value={Array.isArray(parameters.replicas) ? parameters.replicas.join(', ') : ''} update={(_, value) => update('replicas', String(value).split(',').map((item) => item.trim()).filter(Boolean))} />
  if (actionId === 'longhorn-engine-upgrade') return <TextParameter name="image" label="Deployed Longhorn engine image" value={parameters.image} update={update} />
  if (actionId === 'longhorn-backup-target-configure') return <><TextParameter name="url" label="Backup target URL" value={parameters.url} update={update} placeholder="s3://bucket@region/ or nfs://server:/path" /><TextParameter name="credentialSecret" label="Credential Secret name (optional)" value={parameters.credentialSecret} update={update} /><TextParameter name="pollInterval" label="Poll interval" value={parameters.pollInterval} update={update} placeholder="5m" /></>
  if (actionId === 'longhorn-backup-delete') return <TextParameter name="backupVolume" label="Longhorn backup volume" value={parameters.backupVolume} update={update} />
  if (actionId === 'longhorn-backup-restore') return <><TextParameter name="backupVolume" label="Longhorn backup volume" value={parameters.backupVolume} update={update} /><TextParameter name="backupName" label="Backup name" value={parameters.backupName} update={update} /><TextParameter name="size" label="Restored volume size (optional bytes)" value={parameters.size} update={update} /><TextParameter name="replicaCount" label="Replica count" value={parameters.replicaCount} update={(_, value) => update('replicaCount', Number(value))} /><BooleanParameter name="standby" label="Create as a disaster-recovery standby volume" value={parameters.standby} update={update} /></>
  return <p className="rounded-md bg-[var(--color-muted)] p-3 text-sm text-[var(--color-muted-foreground)]">This workflow has no user-supplied parameters. The server will resolve dependencies and current resource state during planning.</p>
}

function targetKind(action: string) {
  if (action === 'create-snapshot' || action === 'delete-snapshot') return 'VolumeSnapshot'
  if (action.includes('blockpool')) return 'CephBlockPool'
  if (action.includes('storageclass')) return 'StorageClass'
  if (action === 'longhorn-backup-target-configure') return 'LonghornBackupTarget'
  if (action === 'longhorn-backup-delete') return 'LonghornBackup'
  if (action.startsWith('longhorn-')) return 'LonghornVolume'
  return 'PersistentVolumeClaim'
}

function targetNeedsNamespace(action: string) {
  return !action.startsWith('longhorn-') && targetKind(action) !== 'StorageClass'
}

function targetLabel(action: string) {
  if (action === 'longhorn-backup-target-configure') return 'Backup target name'
  if (action === 'longhorn-backup-delete') return 'Backup name'
  if (action === 'longhorn-backup-restore') return 'New Longhorn volume name'
  if (action.startsWith('longhorn-')) return 'Longhorn volume name'
  return 'Resource name'
}

function actionProvider(action: string) {
  if (action.includes('ceph')) return 'rook-ceph'
  if (action.startsWith('longhorn-')) return 'longhorn'
  return ''
}

function defaultParameters(action: string): Record<string, unknown> {
  if (action === 'create-pvc') return { storageClass: '', size: '10Gi', accessModes: ['ReadWriteOnce'], volumeMode: 'Filesystem' }
  if (action === 'expand-pvc') return { size: '20Gi' }
  if (action === 'create-snapshot') return { sourceClaim: '', snapshotClass: '' }
  if (action === 'restore-snapshot') return { sourceSnapshot: '', storageClass: '', size: '10Gi', accessModes: ['ReadWriteOnce'] }
  if (action === 'clone-pvc') return { sourceClaim: '', storageClass: '', size: '10Gi', accessModes: ['ReadWriteOnce'] }
  if (action === 'create-ceph-blockpool') return { replicatedSize: 3, failureDomain: 'host', compressionMode: 'none' }
  if (action === 'create-ceph-rbd-storageclass') return { pool: '', reclaimPolicy: 'Delete', volumeBindingMode: 'Immediate', allowVolumeExpansion: true, default: false, imageFeatures: 'layering', filesystemType: 'ext4', encrypted: false, mountOptions: [] }
  if (action === 'create-cephfs-storageclass') return { filesystem: '', pool: '', subvolumeGroup: 'csi', reclaimPolicy: 'Delete', volumeBindingMode: 'Immediate', allowVolumeExpansion: true, default: false, mountOptions: [] }
  if (action === 'longhorn-volume-attach') return { hostId: '' }
  if (action === 'longhorn-volume-detach') return { force: false }
  if (action === 'longhorn-volume-replica-count') return { replicaCount: 3 }
  if (action === 'longhorn-volume-backup') return { snapshotName: '', backupTargetName: '', backupMode: 'incremental' }
  if (action === 'longhorn-recurring-job-add' || action === 'longhorn-recurring-job-remove') return { jobName: '' }
  if (action === 'longhorn-volume-salvage') return { replicas: [] }
  if (action === 'longhorn-engine-upgrade') return { image: '' }
  if (action === 'longhorn-backup-target-configure') return { url: '', credentialSecret: '', pollInterval: '5m' }
  if (action === 'longhorn-backup-delete') return { backupVolume: '' }
  if (action === 'longhorn-backup-restore') return { backupVolume: '', backupName: '', size: '', replicaCount: 3, standby: false }
  return {}
}
