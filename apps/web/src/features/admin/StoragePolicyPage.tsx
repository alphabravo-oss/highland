import { useEffect, useMemo, useState } from 'react'
import { AlertTriangle, Boxes, CheckCircle2, Database, LockKeyhole, Save, Server, Shield } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { Link } from 'react-router-dom'
import { useAuth } from '@/auth/AuthContext'
import { useApplyStoragePolicy, usePlanStoragePolicy, useStoragePolicy, useStoragePolicyHistory } from '@/api/hooks'
import { storagePolicyPreset, updatePolicyField } from '@/api/policy'
import type { StoragePolicyBooleanField, StoragePolicyPlan, StoragePolicyResponse, StorageWritePolicy } from '@/api/policy'
import { useStorageProviders } from '@/api/storage/hooks'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import { EmptyState } from '@/components/ui/empty-state'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useToast } from '@/components/ui/toast'

type PolicyField = {
  key: StoragePolicyBooleanField
  title: string
  description: string
  parent?: StoragePolicyBooleanField
}

const policyFields: PolicyField[] = [
  { key: 'acceptNewOperations', title: 'Allow new storage changes', description: 'Global admission gate. Every workflow scope below also requires this to be enabled.' },
  { key: 'portableKubernetesWrites', title: 'Common Kubernetes workflows', description: 'PVC and VolumeSnapshot create, resize, clone, restore, and guarded deletion across any supported CSI provider.', parent: 'acceptNewOperations' },
  { key: 'longhornWrites', title: 'Longhorn-native workflows', description: 'Only Longhorn manager actions: attach, detach, backups, recurring jobs, salvage, engine upgrades, targets, deletion, and restore.', parent: 'acceptNewOperations' },
  { key: 'rookCephWrites', title: 'Rook/Ceph-native workflows', description: 'Only Rook/Ceph infrastructure actions: create supported Ceph StorageClasses and block pools after provider safety checks.', parent: 'acceptNewOperations' },
  { key: 'allowCephStorageClassDelete', title: 'Ceph StorageClass deletion', description: 'Separately permits typed-confirmation deletion after dependency checks.', parent: 'rookCephWrites' },
  { key: 'allowCephPoolDelete', title: 'Ceph pool deletion', description: 'Critical gate for verified empty-pool deletion with additional typed confirmation.', parent: 'rookCephWrites' },
]

export function StoragePolicyPage() {
  const { isAdmin } = useAuth()
  const query = useStoragePolicy(isAdmin)
  const history = useStoragePolicyHistory(isAdmin)
  const providersQuery = useStorageProviders()
  const planMutation = usePlanStoragePolicy()
  const applyMutation = useApplyStoragePolicy()
  const toast = useToast()
  const [draft, setDraft] = useState<StorageWritePolicy | null>(null)
  const [plan, setPlan] = useState<StoragePolicyPlan | null>(null)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [clusterIdentity, setClusterIdentity] = useState('')
  const [enablePhrase, setEnablePhrase] = useState('')
  const [cephPhrase, setCephPhrase] = useState('')
  const [acknowledged, setAcknowledged] = useState(false)
  const [applied, setApplied] = useState<StoragePolicyResponse | null>(null)

  useEffect(() => {
    if (query.data?.requested) setDraft(query.data.requested)
  }, [query.data?.requested, query.data?.resourceVersion])

  const changed = useMemo(
    () => Boolean(draft && query.data && JSON.stringify(draft) !== JSON.stringify(query.data.requested)),
    [draft, query.data],
  )

  if (!isAdmin) {
    return <div data-testid="storage-policy-page"><PageHeader title="Storage change policy" description="Administrator access is required." /><EmptyState icon={Shield} title="Administrators only" description="Policy mutation is enforced by the Highland API. UI visibility is not the authorization boundary." /></div>
  }

  async function review() {
    if (!draft || !query.data) return
    try {
      const next = await planMutation.mutateAsync({ policy: draft, resourceVersion: query.data.resourceVersion })
      setPlan(next)
      setClusterIdentity('')
      setEnablePhrase('')
      setCephPhrase('')
      setAcknowledged(false)
      setConfirmOpen(true)
    } catch (error) {
      toast.error('Policy plan failed', error instanceof Error ? error.message : undefined)
    }
  }

  async function apply() {
    if (!draft || !query.data || !plan) return
    try {
      const result = await applyMutation.mutateAsync({
        policy: draft,
        resourceVersion: query.data.resourceVersion,
        confirmation: {
          challenge: plan.challenge,
          clusterIdentity,
          enablePhrase,
          cephPoolPhrase: cephPhrase,
          impactAcknowledged: acknowledged,
        },
      })
      setConfirmOpen(false)
      setPlan(null)
      setApplied(result)
      toast.success('Storage change policy updated', 'The effective policy generation has been observed.')
    } catch (error) {
      toast.error('Policy update failed', error instanceof Error ? error.message : undefined)
    }
  }

  const data = query.data
  const providers = (providersQuery.data?.data ?? []).filter((provider) => provider.drivers.length > 0).sort((left, right) => left.displayName.localeCompare(right.displayName))
  return <div data-testid="storage-policy-page">
    <PageHeader
      title="Storage change policy"
      description="Control which reviewed storage workflows Highland may accept. Kubernetes permissions remain an installation-time ceiling."
      actions={data ? <Badge tone={data.effective.acceptNewOperations ? 'warning' : 'info'}>{data.effective.acceptNewOperations ? 'accepting changes' : 'changes disabled'}</Badge> : null}
    />
    <QueryState isLoading={query.isLoading} error={query.error as Error | null} onRetry={() => void query.refetch()}>
      {data && draft ? <div className="space-y-4">
        {data.source !== 'runtime-policy' ? <Alert tone="warning"><LockKeyhole size={18} /><AlertTitle>Runtime policy control is not active</AlertTitle><AlertDescription>This installation is using static Helm policy. Set <code>adminPolicyControl.enabled=true</code> and install an explicit writer ceiling before this page can apply changes.</AlertDescription></Alert> : null}
        {data.meta.stale || data.meta.partial ? <Alert tone="danger"><AlertTriangle size={18} /><AlertTitle>Policy state is not authoritative</AlertTitle><AlertDescription>Highland fails closed until the singleton policy is observed cleanly.</AlertDescription></Alert> : null}
        {applied ? <Alert tone="success"><CheckCircle2 size={18} /><AlertTitle>Policy generation {applied.observedGeneration} is effective</AlertTitle><AlertDescription>Request {applied.meta.requestId || 'completed'} was observed by the API. <Link className="underline" to="/storage/operations">Review storage operations</Link>.</AlertDescription></Alert> : null}
        <div className="grid gap-4 lg:grid-cols-4">
          <PolicyFact title="Source" value={data.source} detail={`Generation ${data.observedGeneration}/${data.generation}`} />
          <PolicyFact title="Active operations" value={String(data.inFlightOperations)} detail="Already-approved operations continue when admission is disabled." />
          <PolicyFact title="Last change" value={data.lastChange?.username || 'Helm / initial state'} detail={data.lastChange?.at ? new Date(data.lastChange.at).toLocaleString() : 'No runtime change recorded'} />
          <PolicyFact title="Installed ceiling" value={Object.values(data.ceiling).filter(Boolean).length ? 'Write capabilities installed' : 'Read-only'} detail="Only Helm or GitOps can change this ceiling." />
        </div>
        <Card>
          <CardHeader>
            <CardTitle>Choose what Highland may change</CardTitle>
            <p className="max-w-3xl text-sm text-[var(--color-muted-foreground)]">The global gate admits new work. The scope controls decide which kinds of work are available. A provider scope never enables another provider.</p>
          </CardHeader>
          <CardContent className="space-y-5">
            <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-muted)]/35 p-4">
              <div className="mb-3 text-xs font-semibold uppercase tracking-wide text-[var(--color-muted-foreground)]">Common starting points</div>
              <div className="flex flex-wrap gap-2">
                <Button type="button" variant="outline" onClick={() => setDraft(storagePolicyPreset('disabled'))}>Disable all changes</Button>
                <Button type="button" variant="outline" disabled={!data.ceiling.longhornWrites} onClick={() => setDraft(storagePolicyPreset('longhorn-native-only'))}>Longhorn native only</Button>
                <Button type="button" variant="outline" disabled={!data.ceiling.longhornWrites || !data.ceiling.portableKubernetesWrites} onClick={() => setDraft(storagePolicyPreset('longhorn-full'))}>Longhorn + PVC lifecycle</Button>
              </div>
              <p className="mt-3 text-xs text-[var(--color-muted-foreground)]"><strong className="text-[var(--color-foreground)]">Longhorn native only</strong> excludes PVC/snapshot workflows and all Rook/Ceph actions. <strong className="text-[var(--color-foreground)]">Longhorn + PVC lifecycle</strong> enables common Kubernetes workflows for Longhorn only.</p>
            </div>

            <section aria-labelledby="global-policy-heading" className={`rounded-xl border-2 p-4 ${draft.acceptNewOperations ? 'border-[var(--color-warning)] bg-[var(--color-warning)]/5' : 'border-[var(--color-border)]'}`}>
              <div className="grid gap-3 sm:grid-cols-[auto_1fr_auto] sm:items-center">
                <input
                  type="checkbox"
                  aria-label="Allow new storage changes"
                  checked={draft.acceptNewOperations}
                  disabled={data.source !== 'runtime-policy' || data.meta.stale}
                  onChange={(event) => setDraft((current) => current ? updatePolicyField(current, 'acceptNewOperations', event.target.checked) : current)}
                />
                <div>
                  <div className="mb-1 flex flex-wrap items-center gap-2"><span className="text-xs font-semibold uppercase tracking-wide text-[var(--color-muted-foreground)]">Global gate</span><Badge tone={draft.acceptNewOperations ? 'warning' : 'default'}>{draft.acceptNewOperations ? 'new changes allowed' : 'new changes paused'}</Badge></div>
                  <h2 id="global-policy-heading" className="font-semibold">Allow Highland to accept new storage changes</h2>
                  <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">This must be on before any scope below can be used. Turning it off clears every scope and blocks new plans; already-approved operations continue safely.</p>
                </div>
                <Badge tone={data.effective.acceptNewOperations ? 'warning' : 'default'}>{data.effective.acceptNewOperations ? 'effective now' : 'inactive now'}</Badge>
              </div>
            </section>

            <div>
              <div className="mb-3">
                <h2 className="font-semibold">Workflow scopes</h2>
                <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">Choose workflow families, then choose exactly which providers may receive common Kubernetes changes. Provider-native scopes remain isolated.</p>
              </div>
              <div className="grid gap-3 lg:grid-cols-3">
                <PolicyScopeCard
                  icon={Boxes}
                  eyebrow="Provider-scoped"
                  title="Common Kubernetes"
                  description="PVC and VolumeSnapshot lifecycle through Kubernetes. Access is limited to the providers selected below."
                  fields={[policyFields[1]!]}
                  data={data}
                  draft={draft}
                  onChange={(key, value) => setDraft((current) => current ? updatePolicyField(current, key, value) : current)}
                />
                <PolicyScopeCard
                  icon={Database}
                  eyebrow="Longhorn only"
                  title="Longhorn native"
                  description="Direct Longhorn manager workflows only. This does not enable Rook/Ceph or common Kubernetes PVC operations."
                  fields={[policyFields[2]!]}
                  data={data}
                  draft={draft}
                  onChange={(key, value) => setDraft((current) => current ? updatePolicyField(current, key, value) : current)}
                />
                <PolicyScopeCard
                  icon={Server}
                  eyebrow="Rook/Ceph only"
                  title="Rook/Ceph native"
                  description="Ceph StorageClass and pool management. Destructive delete permissions remain separate nested gates."
                  fields={policyFields.slice(3)}
                  data={data}
                  draft={draft}
                  onChange={(key, value) => setDraft((current) => current ? updatePolicyField(current, key, value) : current)}
                />
              </div>
              <ProviderScopePicker
                providers={providers}
                draft={draft}
                mutable={data.source === 'runtime-policy' && !data.meta.stale && Boolean(data.ceiling.portableKubernetesWrites)}
                onMigrate={() => setDraft((current) => current ? {
                  ...current,
                  portableKubernetesProviderIds: providers.map((provider) => provider.id).sort(),
                  portableKubernetesWrites: providers.length > 0,
                } : current)}
                onChange={(providerId, checked) => setDraft((current) => {
                  if (!current) return current
                  const explicit = current.portableKubernetesProviderIds.includes('*')
                    ? { ...current, portableKubernetesProviderIds: providers.map((provider) => provider.id) }
                    : current
                  return updatePortableProvider(explicit, providerId, checked)
                })}
              />
              <div className="mt-3 flex items-start gap-3 rounded-lg border border-dashed border-[var(--color-border)] p-3 text-sm">
                <LockKeyhole size={17} className="mt-0.5 shrink-0 text-[var(--color-muted-foreground)]" />
                <div><span className="font-medium">OpenEBS native management remains read-only.</span><span className="text-[var(--color-muted-foreground)]"> Common Kubernetes PVC/snapshot workflows can still use an OpenEBS StorageClass when that cross-provider scope is enabled.</span></div>
              </div>
            </div>

            <div className="flex flex-wrap justify-between gap-3 border-t border-[var(--color-border)] pt-4">
              <Button type="button" variant="outline" disabled={!changed} onClick={() => setDraft(data.requested)}>Reset unsaved changes</Button>
              <Button type="button" disabled={!changed || data.source !== 'runtime-policy' || planMutation.isPending} onClick={() => void review()}><Save size={15} />{planMutation.isPending ? 'Planning…' : 'Review policy change'}</Button>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader><CardTitle>Policy is permission, not provider health</CardTitle></CardHeader>
          <CardContent className="text-sm text-[var(--color-muted-foreground)]">
            Enabling a gate allows eligible roles to request that workflow; each request still runs provider, dependency, dry-run, and confirmation checks. Verify <Link className="underline" to="/storage/providers/longhorn">Longhorn</Link>, <Link className="underline" to="/storage/providers/rook-ceph">Rook / Ceph</Link>, or <Link className="underline" to="/storage/providers/openebs">OpenEBS</Link> health separately.
          </CardContent>
        </Card>
        <Card>
          <CardHeader><CardTitle>Policy conditions</CardTitle></CardHeader>
          <CardContent className="space-y-2">{data.conditions.map((condition) => <div key={condition.type} className="flex gap-2 rounded-md bg-[var(--color-muted)] p-2 text-sm"><CheckCircle2 size={16} className={condition.status === 'True' ? 'text-[var(--color-success)]' : 'text-[var(--color-warning)]'} /><div><span className="font-medium">{condition.type}: {condition.status}</span><div className="text-[var(--color-muted-foreground)]">{condition.message}</div></div></div>)}</CardContent>
        </Card>
        <Card>
          <CardHeader><CardTitle>Recent policy audit</CardTitle></CardHeader>
          <CardContent>{history.data?.data.length ? <div className="space-y-2">{history.data.data.slice(0, 10).map((event, index) => <div key={String(event.id ?? index)} className="grid gap-1 rounded-md border border-[var(--color-border)] p-2 text-sm sm:grid-cols-3"><span>{String(event.action ?? '')}</span><span>{String(event.username ?? '')}</span><span>{String(event.timestamp ?? '')}</span></div>)}</div> : <p className="text-sm text-[var(--color-muted-foreground)]">No runtime policy changes have been recorded.</p>}</CardContent>
        </Card>
      </div> : null}
    </QueryState>
    <PolicyConfirmationDialog
      plan={plan}
      open={confirmOpen}
      onOpenChange={setConfirmOpen}
      clusterIdentity={clusterIdentity}
      setClusterIdentity={setClusterIdentity}
      enablePhrase={enablePhrase}
      setEnablePhrase={setEnablePhrase}
      cephPhrase={cephPhrase}
      setCephPhrase={setCephPhrase}
      acknowledged={acknowledged}
      setAcknowledged={setAcknowledged}
      applying={applyMutation.isPending}
      onApply={() => void apply()}
    />
  </div>
}

function PolicyScopeCard({ icon: Icon, eyebrow, title, description, fields, data, draft, onChange }: {
  icon: LucideIcon
  eyebrow: string
  title: string
  description: string
  fields: PolicyField[]
  data: StoragePolicyResponse
  draft: StorageWritePolicy
  onChange: (key: StoragePolicyBooleanField, value: boolean) => void
}) {
  const mutable = data.source === 'runtime-policy' && !data.meta.stale
  return <section className="flex min-w-0 flex-col rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-4" aria-label={`${title} workflow scope`}>
    <div className="flex items-start gap-3">
      <span className="rounded-lg bg-[var(--color-muted)] p-2"><Icon size={18} aria-hidden="true" /></span>
      <div className="min-w-0">
        <div className="text-xs font-semibold uppercase tracking-wide text-[var(--color-muted-foreground)]">{eyebrow}</div>
        <h3 className="mt-0.5 font-semibold">{title}</h3>
      </div>
    </div>
    <p className="mt-3 min-h-16 text-sm text-[var(--color-muted-foreground)]">{description}</p>
    <div className="mt-4 space-y-2 border-t border-[var(--color-border)] pt-3">
      {fields.map((field, index) => {
        const installed = data.ceiling[field.key as keyof typeof data.ceiling]
        const parentEnabled = !field.parent || draft[field.parent]
        const nested = index > 0
        return <div key={field.key} className={nested ? 'ml-4 border-l-2 border-[var(--color-border)] pl-3' : ''}>
          <label className="flex cursor-pointer items-start gap-3 rounded-md p-2 hover:bg-[var(--color-muted)]/50">
            <input
              type="checkbox"
              aria-label={field.title}
              checked={draft[field.key]}
              disabled={!mutable || !draft.acceptNewOperations || !parentEnabled || !installed || field.key === 'portableKubernetesWrites'}
              onChange={(event) => onChange(field.key, event.target.checked)}
            />
            <span className="min-w-0 flex-1">
              <span className="flex flex-wrap items-center gap-2"><span className="text-sm font-medium">{field.title}</span><Badge tone={data.effective[field.key] ? 'warning' : 'default'}>{data.effective[field.key] ? 'effective' : 'off'}</Badge></span>
              {nested ? <span className="mt-1 block text-xs text-[var(--color-muted-foreground)]">Additional destructive permission</span> : null}
            </span>
          </label>
          {!installed ? <div className="mb-2 ml-8 rounded-md bg-[var(--color-muted)] p-2 text-xs text-[var(--color-muted-foreground)]">
            Not installed by Helm. <code className="mt-1 block break-all">adminPolicyControl.ceiling.{field.key}=true</code>
          </div> : null}
        </div>
      })}
    </div>
  </section>
}

function PolicyFact({ title, value, detail }: { title: string; value: string; detail: string }) {
  return <Card><CardHeader><CardTitle>{title}</CardTitle></CardHeader><CardContent><div className="font-medium">{value}</div><p className="mt-1 text-xs text-[var(--color-muted-foreground)]">{detail}</p></CardContent></Card>
}

export function updatePortableProvider(current: StorageWritePolicy, providerId: string, checked: boolean): StorageWritePolicy {
  const selected = new Set(current.portableKubernetesProviderIds.filter((id) => id !== '*'))
  if (checked) selected.add(providerId)
  else selected.delete(providerId)
  const portableKubernetesProviderIds = [...selected].sort()
  return {
    ...current,
    portableKubernetesProviderIds,
    portableKubernetesWrites: portableKubernetesProviderIds.length > 0,
  }
}

function ProviderScopePicker({ providers, draft, mutable, onMigrate, onChange }: {
  providers: Array<{ id: string; displayName: string; kind: string; drivers: string[] }>
  draft: StorageWritePolicy
  mutable: boolean
  onMigrate: () => void
  onChange: (providerId: string, checked: boolean) => void
}) {
  const legacyWildcard = draft.portableKubernetesProviderIds.includes('*')
  return <section className="mt-3 rounded-xl border border-[var(--color-border)] bg-[var(--color-muted)]/25 p-4" aria-labelledby="portable-provider-heading">
    <div className="flex flex-wrap items-start justify-between gap-3">
      <div>
        <div className="text-xs font-semibold uppercase tracking-wide text-[var(--color-muted-foreground)]">Common Kubernetes provider access</div>
        <h3 id="portable-provider-heading" className="mt-1 font-semibold">Which providers may common workflows change?</h3>
        <p className="mt-1 max-w-3xl text-sm text-[var(--color-muted-foreground)]">A checked provider permits PVC and snapshot workflows only when the global gate and Common Kubernetes family are also enabled. It does not enable that provider’s native management actions.</p>
      </div>
      <Badge tone={draft.portableKubernetesWrites ? 'warning' : 'default'}>{legacyWildcard ? 'all providers (legacy)' : `${draft.portableKubernetesProviderIds.length} selected`}</Badge>
    </div>
    {legacyWildcard ? <Alert tone="warning"><AlertTriangle size={18} /><AlertTitle>Legacy all-provider access is active</AlertTitle><AlertDescription><span className="block">Future and generic CSI providers are also allowed until this is replaced with explicit scopes.</span><Button type="button" variant="outline" className="mt-3" disabled={!mutable || !providers.length} onClick={onMigrate}>Replace with detected providers</Button></AlertDescription></Alert> : null}
    <div className="mt-4 grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
      {providers.map((provider) => {
        const checked = legacyWildcard || draft.portableKubernetesProviderIds.includes(provider.id)
        return <label key={provider.id} className="flex cursor-pointer items-start gap-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-3 hover:bg-[var(--color-muted)]/50">
          <input
            type="checkbox"
            aria-label={`Allow common workflows for ${provider.displayName}`}
            checked={checked}
            disabled={!mutable || !draft.acceptNewOperations}
            onChange={(event) => onChange(provider.id, event.target.checked)}
          />
          <span className="min-w-0"><span className="block text-sm font-medium">{provider.displayName}</span><span className="mt-0.5 block break-all text-xs text-[var(--color-muted-foreground)]">{provider.id} · {provider.drivers.join(', ')}</span></span>
        </label>
      })}
    </div>
    {!providers.length ? <p className="mt-3 text-sm text-[var(--color-muted-foreground)]">No CSI-backed providers are currently detected. Refresh provider discovery before enabling common workflows.</p> : null}
    {draft.acceptNewOperations && !draft.portableKubernetesProviderIds.length ? <p className="mt-3 text-xs font-medium text-[var(--color-muted-foreground)]">Common Kubernetes workflows remain off until at least one provider is selected.</p> : null}
  </section>
}

export function PolicyConfirmationDialog({
  plan, open, onOpenChange, clusterIdentity, setClusterIdentity,
  enablePhrase, setEnablePhrase, cephPhrase, setCephPhrase,
  acknowledged, setAcknowledged, applying, onApply,
}: {
  plan: StoragePolicyPlan | null
  open: boolean
  onOpenChange: (value: boolean) => void
  clusterIdentity: string
  setClusterIdentity: (value: string) => void
  enablePhrase: string
  setEnablePhrase: (value: string) => void
  cephPhrase: string
  setCephPhrase: (value: string) => void
  acknowledged: boolean
  setAcknowledged: (value: boolean) => void
  applying: boolean
  onApply: () => void
}) {
  if (!plan) return null
  const disabled = applying || (plan.broadening && (!acknowledged || clusterIdentity !== plan.clusterIdentity || enablePhrase !== 'ENABLE STORAGE CHANGES')) || (plan.enablesCephPoolDelete && cephPhrase !== 'ENABLE CEPH POOL DELETE')
  return <Dialog
    open={open}
    onOpenChange={onOpenChange}
    title={plan.broadening ? 'Enable storage change capabilities' : 'Narrow storage change policy'}
    description="This is the final server-verified approval boundary."
    className="max-w-3xl"
    footer={<><Button type="button" variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button><Button type="button" variant={plan.broadening ? 'destructive' : 'default'} disabled={disabled} onClick={onApply} data-testid="apply-storage-policy">{applying ? 'Applying…' : 'Apply policy'}</Button></>}
  >
    <div className="space-y-4">
      <div className="grid gap-3 rounded-md bg-[var(--color-muted)] p-3 text-sm sm:grid-cols-5"><div><span className="text-xs text-[var(--color-muted-foreground)]">Cluster</span><div className="font-medium">{plan.clusterIdentity}</div></div><div><span className="text-xs text-[var(--color-muted-foreground)]">Administrator</span><div className="font-medium">{plan.actor}</div></div><div><span className="text-xs text-[var(--color-muted-foreground)]">Active operations</span><div className="font-medium">{plan.inFlightOperations}</div></div><div><span className="text-xs text-[var(--color-muted-foreground)]">Policy generation</span><div className="font-medium">{plan.policyGeneration}</div></div><div><span className="text-xs text-[var(--color-muted-foreground)]">Audit request</span><div className="break-all font-mono text-xs">{plan.requestId}</div></div></div>
      <div className="overflow-x-auto rounded-md border border-[var(--color-border)]">
        <table className="w-full min-w-[640px] text-left text-xs">
          <thead className="bg-[var(--color-muted)]"><tr><th className="p-2">Capability</th><th className="p-2">Current</th><th className="p-2">Requested</th><th className="p-2">Effective after apply</th><th className="p-2">Installed ceiling</th></tr></thead>
          <tbody>{policyFields.map((field) => {
            const ceiling = field.key === 'acceptNewOperations' || plan.ceiling[field.key]
            return <tr key={field.key} className="border-t border-[var(--color-border)]"><td className="p-2 font-medium">{field.title}</td><PolicyCell value={plan.current[field.key]} /><PolicyCell value={plan.requested[field.key]} /><PolicyCell value={plan.effective[field.key]} /><PolicyCell value={ceiling} /></tr>
          })}</tbody>
        </table>
      </div>
      <div><h3 className="text-sm font-semibold">Newly enabled workflows</h3>{plan.impact.actionIds.length ? <ul className="mt-2 grid list-disc gap-1 pl-5 text-sm sm:grid-cols-2">{plan.impact.actionIds.map((action) => <li key={action}>{humanizeAction(action)}</li>)}</ul> : <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">No workflow family is newly enabled.</p>}</div>
      {plan.impact.addedPortableProviderIds.length || plan.impact.removedPortableProviderIds.length ? <div className="grid gap-3 sm:grid-cols-2">
        <ProviderImpact title="Common workflows added" providers={plan.impact.addedPortableProviderIds} tone="warning" />
        <ProviderImpact title="Common workflows removed" providers={plan.impact.removedPortableProviderIds} tone="default" />
      </div> : null}
      <div className="flex gap-2 text-sm"><span>Roles affected:</span>{plan.impact.roles.map((role) => <Badge key={role} tone="warning">{role}</Badge>)}</div>
      {plan.broadening ? <Alert tone="warning"><AlertTriangle size={18} /><AlertTitle>Explicit enablement confirmation required</AlertTitle><AlertDescription>Operators may execute the listed operator workflows after this policy is effective. Individual workflows still require fresh plans and confirmations.</AlertDescription></Alert> : <Alert><LockKeyhole size={18} /><AlertTitle>New submissions will be narrowed</AlertTitle><AlertDescription>Already-approved operations continue reconciliation to a terminal state.</AlertDescription></Alert>}
      {plan.broadening ? <>
        <label className="flex items-start gap-2 text-sm font-medium"><input type="checkbox" checked={acknowledged} onChange={(event) => setAcknowledged(event.target.checked)} />I reviewed the affected workflows, roles, installed ceiling, and active operations.</label>
        <div><Label htmlFor="policy-cluster">Type the cluster identity</Label><Input id="policy-cluster" value={clusterIdentity} onChange={(event) => setClusterIdentity(event.target.value)} data-testid="policy-cluster-confirmation" /></div>
        <div><Label htmlFor="policy-enable">Type ENABLE STORAGE CHANGES</Label><Input id="policy-enable" value={enablePhrase} onChange={(event) => setEnablePhrase(event.target.value)} data-testid="policy-enable-confirmation" /></div>
      </> : null}
      {plan.enablesCephPoolDelete ? <div><Label htmlFor="policy-ceph-delete">Type ENABLE CEPH POOL DELETE</Label><Input id="policy-ceph-delete" value={cephPhrase} onChange={(event) => setCephPhrase(event.target.value)} data-testid="policy-ceph-confirmation" /></div> : null}
      {applyMutationMessage(plan) ? <p className="text-xs text-[var(--color-muted-foreground)]">{applyMutationMessage(plan)}</p> : null}
    </div>
  </Dialog>
}

function ProviderImpact({ title, providers, tone }: { title: string; providers: string[]; tone: 'warning' | 'default' }) {
  return <div className="rounded-md border border-[var(--color-border)] p-3"><div className="text-xs font-semibold uppercase tracking-wide text-[var(--color-muted-foreground)]">{title}</div><div className="mt-2 flex flex-wrap gap-2">{providers.length ? providers.map((provider) => <Badge key={provider} tone={tone}>{provider === '*' ? 'all current and future providers' : provider}</Badge>) : <span className="text-sm text-[var(--color-muted-foreground)]">None</span>}</div></div>
}

function applyMutationMessage(plan: StoragePolicyPlan) {
  return `Challenge expires ${new Date(plan.challengeExpiresAt).toLocaleString()}. The server will reject stale or concurrently changed policy.`
}

function humanizeAction(action: string) {
  return action.split('-').map((word) => word === 'pvc' ? 'PVC' : word === 'ceph' ? 'Ceph' : word === 'longhorn' ? 'Longhorn' : word.charAt(0).toUpperCase() + word.slice(1)).join(' ')
}

function PolicyCell({ value }: { value: boolean }) {
  return <td className="p-2"><Badge tone={value ? 'success' : 'default'}>{value ? 'enabled' : 'disabled'}</Badge></td>
}
