import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import type { ColumnDef } from '@tanstack/react-table'
import { AlertTriangle, CheckCircle2, Clock3, ShieldAlert } from 'lucide-react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { storageClient } from '@/api/storage/client'
import { useStorageActions, useStorageOperation, useStorageOperations } from '@/api/storage/hooks'
import type { ActionAvailability, OperationPlan, OperationRequest, StorageOperation } from '@/api/storage/types'
import { DataTable } from '@/components/data/DataTable'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
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
  const columns = useMemo<ColumnDef<StorageOperation, any>[]>(() => [
    { accessorKey: 'name', header: 'Operation', cell: ({ row }) => <Link className="font-medium text-[var(--color-primary)] hover:underline" to={`/storage/operations/${row.original.name}`}>{row.original.name}</Link> },
    { id: 'action', header: 'Action', accessorFn: (row) => row.spec.actionId }, { id: 'provider', header: 'Provider', accessorFn: (row) => row.spec.providerId || 'kubernetes' },
    { id: 'target', header: 'Target', accessorFn: (row) => `${row.spec.target.namespace ? `${row.spec.target.namespace}/` : ''}${row.spec.target.name}` },
    { id: 'phase', header: 'Phase', cell: ({ row }) => <Badge tone={phaseTone(row.original.status.phase)}>{row.original.status.phase}</Badge> },
    { id: 'requester', header: 'Requester', accessorFn: (row) => row.spec.requester }, { accessorKey: 'creationTimestamp', header: 'Created' },
  ], [])
  const setFilter = (key: string, value: string) => { const next = new URLSearchParams(params); if (value) next.set(key, value); else next.delete(key); setParams(next) }
  return <div data-testid="storage-operations-page">
    <PageHeader title="Storage operations" description="Durable, auditable storage workflows that survive API restarts and leader changes." />
    <div className="mb-5 grid gap-4 lg:grid-cols-[2fr_1fr]">
      <Card><CardHeader><CardTitle>Approved workflows</CardTitle></CardHeader><CardContent>
        <QueryState isLoading={actions.isLoading} error={actions.error as Error | null} onRetry={() => void actions.refetch()}>
          <div className="grid gap-2 sm:grid-cols-2">{(actions.data?.data ?? []).map((entry) => <ActionLink key={entry.action.id} entry={entry} />)}</div>
        </QueryState>
      </CardContent></Card>
      <Card><CardHeader><CardTitle>Safety model</CardTitle></CardHeader><CardContent className="space-y-2 text-sm text-[var(--color-muted-foreground)]"><p>Every change is planned against current dependencies, bound to a short-lived confirmation, then persisted as a Kubernetes StorageOperation.</p><p>Disabling writes stops submissions. Recovery mode can finish already-approved work without reopening the API gate.</p></CardContent></Card>
    </div>
    <div className="mb-3 grid gap-2 sm:grid-cols-2 lg:grid-cols-4"><Input placeholder="Provider" aria-label="Provider filter" value={filters.provider ?? ''} onChange={(e) => setFilter('provider', e.target.value)} /><Input placeholder="User" aria-label="User filter" value={filters.user ?? ''} onChange={(e) => setFilter('user', e.target.value)} /><Input placeholder="Action" aria-label="Action filter" value={filters.action ?? ''} onChange={(e) => setFilter('action', e.target.value)} /><Input placeholder="State" aria-label="State filter" value={filters.state ?? ''} onChange={(e) => setFilter('state', e.target.value)} /></div>
    <QueryState isLoading={operations.isLoading} error={operations.error as Error | null} onRetry={() => void operations.refetch()}><DataTable columns={columns} data={operations.data?.data ?? []} tableId="storage-operations" getRowId={(row) => row.name} enableExport exportName="highland-storage-operations" /></QueryState>
  </div>
}

function ActionLink({ entry }: { entry: ActionAvailability }) {
  const content = <div className={`rounded-md border p-3 ${entry.available ? 'border-[var(--color-border)] hover:border-[var(--color-primary)]' : 'border-[var(--color-border)] opacity-60'}`}><div className="flex items-center justify-between gap-2"><span className="text-sm font-medium">{entry.action.id.replaceAll('-', ' ')}</span><Badge tone={entry.action.risk === 'critical' || entry.action.risk === 'high' ? 'warning' : 'default'}>{entry.action.risk}</Badge></div>{entry.unavailableReason ? <p className="mt-1 text-xs text-[var(--color-muted-foreground)]">{entry.unavailableReason}</p> : null}</div>
  return entry.available ? <Link to={`/storage/actions/${entry.action.id}`}>{content}</Link> : content
}

export function StorageActionPage() {
  const { actionId = '' } = useParams()
  const actions = useStorageActions()
  const entry = actions.data?.data.find((candidate) => candidate.action.id === actionId)
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [namespace, setNamespace] = useState(actionId.includes('ceph') && actionId.includes('blockpool') ? 'rook-ceph' : 'default')
  const [name, setName] = useState('')
  const [providerId, setProviderId] = useState(actionId.includes('ceph') ? 'rook-ceph' : '')
  const [parameters, setParameters] = useState<Record<string, unknown>>(() => defaultParameters(actionId))
  const [typedName, setTypedName] = useState('')
  const [warningsAcknowledged, setWarningsAcknowledged] = useState(false)
  const [plan, setPlan] = useState<OperationPlan | null>(null)
  const [formError, setFormError] = useState('')

  useEffect(() => {
    setNamespace(actionId.includes('ceph') && actionId.includes('blockpool') ? 'rook-ceph' : 'default')
    setName('')
    setProviderId(actionId.includes('ceph') ? 'rook-ceph' : '')
    setParameters(defaultParameters(actionId))
    setTypedName('')
    setWarningsAcknowledged(false)
    setPlan(null)
    setFormError('')
  }, [actionId])

  function request(): OperationRequest {
    return { actionId, providerId: providerId || undefined, target: { kind: targetKind(actionId), namespace: targetKind(actionId) === 'StorageClass' ? undefined : namespace, name }, parameters }
  }

  function updateParameter(key: string, value: unknown) {
    setParameters((current) => ({ ...current, [key]: value }))
    setPlan(null)
  }

  const planMutation = useMutation({ mutationFn: storageClient.plan, onSuccess: (value) => { setPlan(value); setWarningsAcknowledged(false); setFormError('') }, onError: (error) => setFormError((error as Error).message) })
  const submitMutation = useMutation({ mutationFn: storageClient.submit, onSuccess: (value) => { void qc.invalidateQueries({ queryKey: ['storage', 'operations'] }); navigate(`/storage/operations/${value.operationId}`) }, onError: (error) => setFormError((error as Error).message) })
  function createPlan() { try { setPlan(null); planMutation.mutate(request()) } catch (error) { setFormError((error as Error).message) } }
  function submit() { if (!plan) return; try { const next = request(); next.target = plan.target; next.confirmation = { challenge: plan.challenge, typedName: typedName || undefined, warningsAcknowledged }; submitMutation.mutate(next) } catch (error) { setFormError((error as Error).message) } }

  return <div data-testid="storage-action-page">
    <PageHeader title={actionId.replaceAll('-', ' ')} description="Plan the change, review server-authoritative dependencies and warnings, then confirm the exact approved plan." />
    <QueryState isLoading={actions.isLoading} error={actions.error as Error | null} onRetry={() => void actions.refetch()}>
      {!entry?.available ? <Alert tone="warning"><ShieldAlert size={18} /><AlertTitle>Action unavailable</AlertTitle><AlertDescription>{entry?.unavailableReason ?? 'This action is not exposed by the current capability and role policy.'}</AlertDescription></Alert> : <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.3fr)]">
        <Card><CardHeader><CardTitle>Request</CardTitle></CardHeader><CardContent className="space-y-4">
          {targetKind(actionId) !== 'StorageClass' ? <div><Label htmlFor="operation-namespace">Namespace</Label><Input id="operation-namespace" value={namespace} onChange={(e) => { setNamespace(e.target.value); setPlan(null) }} /></div> : null}
          <div><Label htmlFor="operation-name">Resource name</Label><Input id="operation-name" value={name} onChange={(e) => { setName(e.target.value); setPlan(null) }} /></div>
          {actionId.includes('ceph') ? <div><Label htmlFor="operation-provider">Provider</Label><Input id="operation-provider" value={providerId} onChange={(e) => { setProviderId(e.target.value); setPlan(null) }} /></div> : null}
          <ActionParameterFields actionId={actionId} parameters={parameters} update={updateParameter} />
          <p className="text-xs text-[var(--color-muted-foreground)]">Only the typed fields shown for this action are submitted. Credentials and secret values are never accepted here.</p>
          <Button onClick={createPlan} disabled={!name || planMutation.isPending}>{planMutation.isPending ? 'Planning…' : 'Generate safe plan'}</Button>
          {formError ? <Alert tone="danger"><AlertTriangle size={18} /><AlertTitle>Request rejected</AlertTitle><AlertDescription>{formError}</AlertDescription></Alert> : null}
        </CardContent></Card>
        <Card><CardHeader><CardTitle>Authoritative review</CardTitle></CardHeader><CardContent>{plan ? <PlanReview plan={plan} typedName={typedName} setTypedName={setTypedName} warningsAcknowledged={warningsAcknowledged} setWarningsAcknowledged={setWarningsAcknowledged} onSubmit={submit} submitting={submitMutation.isPending} /> : <div className="py-16 text-center text-sm text-[var(--color-muted-foreground)]"><Clock3 className="mx-auto mb-3" />Generate a plan to see current dependencies, dry-run checks, blast radius, and confirmation requirements.</div>}</CardContent></Card>
      </div>}
    </QueryState>
  </div>
}

function PlanReview({ plan, typedName, setTypedName, warningsAcknowledged, setWarningsAcknowledged, onSubmit, submitting }: { plan: OperationPlan; typedName: string; setTypedName: (value: string) => void; warningsAcknowledged: boolean; setWarningsAcknowledged: (value: boolean) => void; onSubmit: () => void; submitting: boolean }) {
  return <div className="space-y-4">
    <div className="grid gap-2 text-sm sm:grid-cols-2"><div><span className="text-[var(--color-muted-foreground)]">Provider</span><div className="font-medium">{plan.providerId || 'Kubernetes'}</div></div><div><span className="text-[var(--color-muted-foreground)]">Cluster</span><div className="font-medium">local</div></div><div><span className="text-[var(--color-muted-foreground)]">Namespace</span><div className="font-medium">{plan.target.namespace || 'cluster-scoped'}</div></div><div><span className="text-[var(--color-muted-foreground)]">Blast radius</span><div className="font-medium">{plan.blastRadius}</div></div><div><span className="text-[var(--color-muted-foreground)]">Target</span><div className="font-medium">{plan.target.name}</div></div><div><span className="text-[var(--color-muted-foreground)]">Required role</span><div className="font-medium">{plan.action.minimumRole}</div></div></div>
    <div><h3 className="mb-2 text-sm font-semibold">Preflight checks</h3><div className="space-y-2">{plan.checks.map((check) => <div key={check.id} className="flex gap-2 rounded-md bg-[var(--color-muted)] p-2 text-sm"><CheckCircle2 size={16} className="mt-0.5 text-[var(--color-success)]" /><div><span className="font-medium">{check.id}</span><div className="text-[var(--color-muted-foreground)]">{check.message}</div></div></div>)}</div></div>
    {plan.dependencies?.length ? <div><h3 className="mb-2 text-sm font-semibold">Dependencies checked</h3><ul className="space-y-2">{plan.dependencies.map((dependency) => <li key={`${dependency.kind}/${dependency.namespace}/${dependency.name}`} className="rounded-md border border-[var(--color-border)] p-2 text-sm"><span className="font-medium">{dependency.kind}</span> {dependency.namespace ? `${dependency.namespace}/` : ''}{dependency.name}{dependency.uid ? <span className="ml-2 font-mono text-xs text-[var(--color-muted-foreground)]">UID {dependency.uid}</span> : null}</li>)}</ul></div> : null}
    {plan.warnings?.length ? <Alert tone="warning"><AlertTriangle size={18} /><AlertTitle>Warnings require review</AlertTitle><AlertDescription><ul className="list-disc pl-5">{plan.warnings.map((warning) => <li key={warning}>{warning}</li>)}</ul><label className="mt-3 flex items-start gap-2 font-medium"><input className="mt-0.5" type="checkbox" checked={warningsAcknowledged} onChange={(event) => setWarningsAcknowledged(event.target.checked)} />I reviewed and acknowledge these warnings.</label></AlertDescription></Alert> : null}
    <div><h3 className="mb-2 text-sm font-semibold">Rendered changes</h3>{plan.resources.map((resource) => <div key={`${resource.kind}/${resource.name}`} className="rounded-md border border-[var(--color-border)] p-3"><div className="flex justify-between text-sm"><span className="font-medium">{resource.kind} {resource.namespace ? `${resource.namespace}/` : ''}{resource.name}</span><Badge tone="info">{resource.operation}</Badge></div>{resource.manifest ? <pre className="mt-2 max-h-64 overflow-auto rounded bg-[var(--color-muted)] p-2 text-xs">{JSON.stringify(resource.manifest, null, 2)}</pre> : null}</div>)}</div>
    {plan.action.confirmation === 'typed-name' ? <div><Label htmlFor="typed-confirmation">Type {plan.target.name} to confirm</Label><Input id="typed-confirmation" value={typedName} onChange={(e) => setTypedName(e.target.value)} autoComplete="off" /></div> : null}
    <Button variant={plan.action.risk === 'critical' || plan.action.risk === 'high' ? 'destructive' : 'default'} onClick={onSubmit} disabled={submitting || (Boolean(plan.warnings?.length) && !warningsAcknowledged) || (plan.action.confirmation === 'typed-name' && typedName !== plan.target.name)}>{submitting ? 'Submitting…' : `Approve ${plan.action.id.replaceAll('-', ' ')}`}</Button>
    <p className="text-xs text-[var(--color-muted-foreground)]">Challenge expires {new Date(plan.challengeExpiresAt).toLocaleString()}. The server reruns all preflight checks before mutation.</p>
  </div>
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
  return <p className="rounded-md bg-[var(--color-muted)] p-3 text-sm text-[var(--color-muted-foreground)]">This workflow has no user-supplied parameters. The server will resolve dependencies and current resource state during planning.</p>
}

function targetKind(action: string) { if (action === 'create-snapshot' || action === 'delete-snapshot') return 'VolumeSnapshot'; if (action.includes('blockpool')) return 'CephBlockPool'; if (action.includes('storageclass')) return 'StorageClass'; return 'PersistentVolumeClaim' }
function defaultParameters(action: string): Record<string, unknown> {
  if (action === 'create-pvc') return { storageClass: '', size: '10Gi', accessModes: ['ReadWriteOnce'], volumeMode: 'Filesystem' }
  if (action === 'expand-pvc') return { size: '20Gi' }
  if (action === 'create-snapshot') return { sourceClaim: '', snapshotClass: '' }
  if (action === 'restore-snapshot') return { sourceSnapshot: '', storageClass: '', size: '10Gi', accessModes: ['ReadWriteOnce'] }
  if (action === 'clone-pvc') return { sourceClaim: '', storageClass: '', size: '10Gi', accessModes: ['ReadWriteOnce'] }
  if (action === 'create-ceph-blockpool') return { replicatedSize: 3, failureDomain: 'host', compressionMode: 'none' }
  if (action === 'create-ceph-rbd-storageclass') return { pool: '', reclaimPolicy: 'Delete', volumeBindingMode: 'Immediate', allowVolumeExpansion: true, default: false, imageFeatures: 'layering', filesystemType: 'ext4', encrypted: false, mountOptions: [] }
  if (action === 'create-cephfs-storageclass') return { filesystem: '', pool: '', subvolumeGroup: 'csi', reclaimPolicy: 'Delete', volumeBindingMode: 'Immediate', allowVolumeExpansion: true, default: false, mountOptions: [] }
  return {}
}
