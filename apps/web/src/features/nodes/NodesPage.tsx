import { useMemo, useState } from 'react'
import { RefreshCw } from 'lucide-react'
import {
  useInstanceManagers,
  useNodeAction,
  useNodes,
  useUpdateNode,
  useVolumes,
} from '@/api/hooks'
import { useAuth } from '@/auth/AuthContext'
import { formatBytes, hasAction, toConditionArray, type Node } from '@/api/longhorn'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Alert } from '@/components/ui/alert'
import { Badge, stateTone } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select } from '@/components/ui/select'
import { Table, TBody, TD, TH, THead, TR } from '@/components/ui/table'
import { ConfirmDialog } from '@/components/data/ConfirmDialog'
import { useAppTranslation } from '@/i18n/useAppTranslation'

const GIB = 1024 ** 3

// The shared Disk type (Node['disks'][string]) does not include `name` or
// `evictionRequested`, both of which Longhorn supports on the disk update
// payload. Extend it locally so we can read/write them without editing the
// shared longhorn.ts types (which this feature does not own).
type DiskValue = NonNullable<Node['disks']>[string] & {
  name?: string
  evictionRequested?: boolean
}

type DiskDraft = {
  id: string // original map key (disk name for existing disks)
  name: string
  path: string
  diskType: string
  allowScheduling: boolean
  evictionRequested: boolean
  storageReservedGi: string
  tags: string
  isNew: boolean
  removed: boolean
  orig: DiskValue | null
}

function bytesToGiString(bytes?: number): string {
  if (!bytes) return '0'
  const gi = bytes / GIB
  return String(Number(gi.toFixed(4)))
}

function draftsFromNode(node: Node): DiskDraft[] {
  return Object.entries(node.disks ?? {}).map(([id, raw]) => {
    const d = raw as DiskValue
    return {
      id,
      name: d.name ?? id,
      path: d.path ?? '',
      diskType: d.diskType ?? 'filesystem',
      allowScheduling: Boolean(d.allowScheduling),
      evictionRequested: Boolean(d.evictionRequested),
      storageReservedGi: bytesToGiString(d.storageReserved),
      tags: (d.tags ?? []).join(', '),
      isNew: false,
      removed: false,
      orig: d,
    }
  })
}

let newDiskCounter = 0

export function NodesPage() {
  const { t } = useAppTranslation()
  const { canMutate, isAdmin } = useAuth()
  const q = useNodes()
  const vols = useVolumes()
  const ims = useInstanceManagers()
  const updateMut = useUpdateNode()
  const actionMut = useNodeAction()
  const [error, setError] = useState<string | null>(null)
  const [expanded, setExpanded] = useState<string | null>(null)
  const [tagNode, setTagNode] = useState<Node | null>(null)
  const [tags, setTags] = useState('')
  const [deleteNode, setDeleteNode] = useState<Node | null>(null)
  const [diskNode, setDiskNode] = useState<Node | null>(null)
  const [diskDrafts, setDiskDrafts] = useState<DiskDraft[]>([])

  const rows = useMemo(() => q.data ?? [], [q.data])

  async function toggleScheduling(node: Node) {
    setError(null)
    try {
      await updateMut.mutateAsync({
        node,
        body: { ...node, allowScheduling: !node.allowScheduling },
      })
    } catch (e) {
      setError(e instanceof Error ? e.message : t('admin.updateFailed'))
    }
  }

  async function saveTags() {
    if (!tagNode) return
    setError(null)
    try {
      const list = tags
        .split(',')
        .map((t) => t.trim())
        .filter(Boolean)
      await updateMut.mutateAsync({ node: tagNode, body: { ...tagNode, tags: list } })
      setTagNode(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : t('admin.updateFailed'))
    }
  }

  function openDiskEditor(node: Node) {
    setDiskNode(node)
    setDiskDrafts(draftsFromNode(node))
  }

  function updateDraft(id: string, patch: Partial<DiskDraft>) {
    setDiskDrafts((prev) => prev.map((d) => (d.id === id ? { ...d, ...patch } : d)))
  }

  function removeDraft(id: string) {
    setDiskDrafts((prev) =>
      prev.flatMap((d) =>
        d.id === id ? (d.isNew ? [] : [{ ...d, removed: true }]) : [d],
      ),
    )
  }

  function restoreDraft(id: string) {
    updateDraft(id, { removed: false })
  }

  function addDraft() {
    const id = `__new_${newDiskCounter++}`
    setDiskDrafts((prev) => [
      ...prev,
      {
        id,
        name: '',
        path: '',
        diskType: 'filesystem',
        allowScheduling: true,
        evictionRequested: false,
        storageReservedGi: '0',
        tags: '',
        isNew: true,
        removed: false,
        orig: null,
      },
    ])
  }

  async function saveDisks() {
    if (!diskNode) return
    setError(null)
    try {
      const disks: Record<string, DiskValue> = {}
      for (const d of diskDrafts) {
        if (d.removed) continue // omitting the key removes the disk in Longhorn
        const key = (d.name || d.id).trim()
        if (!key) continue
        const reservedBytes = Math.round((parseFloat(d.storageReservedGi) || 0) * GIB)
        const tags = d.tags
          .split(',')
          .map((s) => s.trim())
          .filter(Boolean)
        disks[key] = {
          // Preserve read-only / capacity fields Longhorn returns
          // (storageMaximum/Available/Scheduled, conditions, etc.).
          ...(d.orig ?? {}),
          name: key,
          path: d.path,
          diskType: d.diskType,
          allowScheduling: d.allowScheduling,
          evictionRequested: d.evictionRequested,
          storageReserved: reservedBytes,
          tags,
        }
      }
      if (hasAction(diskNode, 'diskUpdate') || hasAction(diskNode, 'updateDisk')) {
        const key = hasAction(diskNode, 'diskUpdate') ? 'diskUpdate' : 'updateDisk'
        await actionMut.mutateAsync({ node: diskNode, action: key, params: { disks } })
      } else {
        await updateMut.mutateAsync({ node: diskNode, body: { ...diskNode, disks } })
      }
      setDiskNode(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : t('admin.updateFailed'))
    }
  }

  return (
    <div data-testid="nodes-page">
      <PageHeader
        title={t('nodes.title')}
        description={t('nodes.description')}
        actions={
          <Button type="button" variant="outline" size="sm" onClick={() => void q.refetch()}>
            <RefreshCw size={14} /> {t('common.refresh')}
          </Button>
        }
      />
      {error ? (
        <Alert tone="danger" className="mb-3">
          {error}
        </Alert>
      ) : null}

      <QueryState
        isLoading={q.isLoading}
        error={q.error as Error | null}
        isEmpty={!rows.length}
        emptyTitle={t('nodes.empty')}
        onRetry={() => void q.refetch()}
      >
        <div className="space-y-3">
          {rows.map((node) => {
            const disks = Object.entries(node.disks ?? {})
            const ready = toConditionArray(node.conditions).find((c) => c.type === 'Ready')
            const isOpen = expanded === node.name
            const nodeReplicas = (vols.data ?? []).flatMap((v) =>
              (v.replicas ?? [])
                .filter((r) => r.hostId === node.name)
                .map((r) => ({ vol: v.name, ...r })),
            )
            const nodeIms = (ims.data ?? []).filter((im) => im.nodeID === node.name)
            return (
              <Card key={node.id ?? node.name}>
                <CardHeader className="flex-row flex-wrap items-center justify-between gap-2 space-y-0">
                  <div>
                    <CardTitle className="text-base">{node.name}</CardTitle>
                    <p className="mt-1 text-xs text-[var(--color-muted-foreground)]">
                      {node.address ?? '—'}
                      {node.region ? ` · ${node.region}` : ''}
                      {node.zone ? `/${node.zone}` : ''}
                    </p>
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <Badge tone={stateTone(ready?.status === 'True' ? 'ready' : 'faulted')}>
                      {t('nodes.ready', { status: ready?.status ?? '—' })}
                    </Badge>
                    <Badge tone={node.allowScheduling ? 'success' : 'warning'}>
                      {node.allowScheduling ? t('nodes.schedulable') : t('nodes.unschedulable')}
                    </Badge>
                    {(node.tags ?? []).map((tag) => (
                      <Badge key={tag}>{tag}</Badge>
                    ))}
                    {canMutate ? (
                      <>
                        <Button type="button" size="sm" variant="outline" onClick={() => void toggleScheduling(node)}>
                          {node.allowScheduling ? t('nodes.disableScheduling') : t('nodes.enableScheduling')}
                        </Button>
                        <Button
                          type="button"
                          size="sm"
                          variant="outline"
                          onClick={() => {
                            setTagNode(node)
                            setTags((node.tags ?? []).join(', '))
                          }}
                        >
                          {t('nodes.editTags')}
                        </Button>
                        <Button
                          type="button"
                          size="sm"
                          variant="outline"
                          onClick={() => openDiskEditor(node)}
                        >
                          {t('nodes.editDisks')}
                        </Button>
                        {isAdmin ? (
                          <Button type="button" size="sm" variant="ghost" onClick={() => setDeleteNode(node)}>
                            {t('nodes.deleteNode')}
                          </Button>
                        ) : null}
                      </>
                    ) : null}
                    <Button type="button" size="sm" variant="ghost" onClick={() => setExpanded(isOpen ? null : node.name)}>
                      {isOpen
                        ? t('common.hide')
                        : t('nodes.expandDisks', { disks: disks.length, replicas: nodeReplicas.length })}
                    </Button>
                  </div>
                </CardHeader>
                {isOpen ? (
                  <CardContent className="space-y-4">
                    <Table>
                      <THead>
                        <TR>
                          <TH>{t('nodes.disk')}</TH>
                          <TH>{t('common.path')}</TH>
                          <TH>{t('nodes.available')}</TH>
                          <TH>{t('nodes.maximum')}</TH>
                          <TH>{t('nodes.scheduled')}</TH>
                          <TH>{t('nodes.schedulable')}</TH>
                          <TH>{t('nodes.tags')}</TH>
                        </TR>
                      </THead>
                      <TBody>
                        {disks.map(([id, d]) => (
                          <TR key={id}>
                            <TD className="max-w-[8rem] truncate font-mono text-xs">{id}</TD>
                            <TD className="max-w-[12rem] truncate">{d.path ?? '—'}</TD>
                            <TD className="tabular-nums">{formatBytes(d.storageAvailable)}</TD>
                            <TD className="tabular-nums">{formatBytes(d.storageMaximum)}</TD>
                            <TD className="tabular-nums">{formatBytes(d.storageScheduled)}</TD>
                            <TD>
                              <Badge tone={d.allowScheduling ? 'success' : 'warning'}>
                                {d.allowScheduling ? t('common.yes') : t('common.no')}
                              </Badge>
                            </TD>
                            <TD>{(d.tags ?? []).join(', ') || '—'}</TD>
                          </TR>
                        ))}
                      </TBody>
                    </Table>
                    <div>
                      <h4 className="mb-2 text-sm font-semibold">{t('nodes.replicasOnNode')}</h4>
                      {nodeReplicas.length === 0 ? (
                        <p className="text-sm text-[var(--color-muted-foreground)]">{t('common.none')}</p>
                      ) : (
                        <ul className="space-y-1 text-sm">
                          {nodeReplicas.map((r, i) => (
                            <li key={i}>
                              {r.vol} / {r.name} · {r.mode} · {r.running ? t('common.running') : t('common.stopped')}
                            </li>
                          ))}
                        </ul>
                      )}
                    </div>
                    <div>
                      <h4 className="mb-2 text-sm font-semibold">{t('nodes.instanceManagers')}</h4>
                      {nodeIms.length === 0 ? (
                        <p className="text-sm text-[var(--color-muted-foreground)]">{t('common.none')}</p>
                      ) : (
                        <ul className="space-y-1 text-sm">
                          {nodeIms.map((im) => (
                            <li key={im.name}>
                              {im.name} · {im.instanceManagerType} · {im.currentState}
                            </li>
                          ))}
                        </ul>
                      )}
                    </div>
                  </CardContent>
                ) : null}
              </Card>
            )
          })}
        </div>
      </QueryState>

      <Dialog
        open={Boolean(tagNode)}
        onOpenChange={(v) => !v && setTagNode(null)}
        title={t('nodes.editTags')}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setTagNode(null)}>
              {t('common.cancel')}
            </Button>
            <Button type="button" onClick={() => void saveTags()}>
              {t('common.save')}
            </Button>
          </>
        }
      >
        <Input value={tags} onChange={(e) => setTags(e.target.value)} placeholder={t('nodes.tagsPlaceholder')} />
      </Dialog>

      <Dialog
        open={Boolean(diskNode)}
        onOpenChange={(v) => !v && setDiskNode(null)}
        title={t('nodes.editDisks')}
        description={t('nodes.editDisksDescription')}
        className="max-w-2xl"
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setDiskNode(null)}>
              {t('common.cancel')}
            </Button>
            <Button type="button" onClick={() => void saveDisks()}>
              {t('nodes.saveDisks')}
            </Button>
          </>
        }
      >
        <div className="max-h-[65vh] space-y-3 overflow-y-auto">
          {diskDrafts.length === 0 ? (
            <p className="text-sm text-[var(--color-muted-foreground)]">{t('nodes.noDisks')}</p>
          ) : null}
          {diskDrafts.map((d) => (
            <div
              key={d.id}
              className={
                'rounded-md border border-[var(--color-border)] p-3 ' +
                (d.removed ? 'opacity-60' : '')
              }
            >
              <div className="mb-2 flex items-center justify-between gap-2">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-mono text-sm font-semibold">{d.name || t('nodes.newDisk')}</span>
                  <Badge>{d.isNew ? t('nodes.newDisk') : d.diskType}</Badge>
                  {d.removed ? <Badge tone="danger">{t('nodes.markedForRemoval')}</Badge> : null}
                </div>
                {d.removed ? (
                  <Button type="button" size="sm" variant="outline" onClick={() => restoreDraft(d.id)}>
                    {t('common.restore')}
                  </Button>
                ) : (
                  <Button type="button" size="sm" variant="ghost" onClick={() => removeDraft(d.id)}>
                    {t('nodes.removeDisk')}
                  </Button>
                )}
              </div>

              {d.orig ? (
                <div className="mb-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-[var(--color-muted-foreground)]">
                  <span>
                    {t('nodes.available')}: {formatBytes(d.orig.storageAvailable)}
                  </span>
                  <span>
                    {t('nodes.maximum')}: {formatBytes(d.orig.storageMaximum)}
                  </span>
                  <span>
                    {t('nodes.scheduled')}: {formatBytes(d.orig.storageScheduled)}
                  </span>
                </div>
              ) : null}

              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <div className="space-y-1">
                  <Label htmlFor={`disk-name-${d.id}`}>{t('nodes.diskName')}</Label>
                  <Input
                    id={`disk-name-${d.id}`}
                    value={d.name}
                    readOnly={!d.isNew}
                    disabled={d.removed}
                    onChange={(e) => updateDraft(d.id, { name: e.target.value })}
                  />
                </div>
                <div className="space-y-1">
                  <Label htmlFor={`disk-type-${d.id}`}>{t('nodes.diskType')}</Label>
                  <Select
                    id={`disk-type-${d.id}`}
                    value={d.diskType}
                    disabled={!d.isNew || d.removed}
                    onChange={(e) => updateDraft(d.id, { diskType: e.target.value })}
                  >
                    <option value="filesystem">{t('nodes.diskTypeFilesystem')}</option>
                    <option value="block">{t('nodes.diskTypeBlock')}</option>
                  </Select>
                </div>
                <div className="space-y-1 sm:col-span-2">
                  <Label htmlFor={`disk-path-${d.id}`}>{t('common.path')}</Label>
                  <Input
                    id={`disk-path-${d.id}`}
                    value={d.path}
                    readOnly={!d.isNew}
                    disabled={d.removed}
                    placeholder={t('nodes.pathPlaceholder')}
                    onChange={(e) => updateDraft(d.id, { path: e.target.value })}
                  />
                </div>
                <div className="space-y-1">
                  <Label htmlFor={`disk-reserved-${d.id}`}>{t('nodes.reservedStorageGi')}</Label>
                  <Input
                    id={`disk-reserved-${d.id}`}
                    type="number"
                    min={0}
                    value={d.storageReservedGi}
                    disabled={d.removed}
                    onChange={(e) => updateDraft(d.id, { storageReservedGi: e.target.value })}
                  />
                </div>
                <div className="space-y-1">
                  <Label htmlFor={`disk-tags-${d.id}`}>{t('nodes.tags')}</Label>
                  <Input
                    id={`disk-tags-${d.id}`}
                    value={d.tags}
                    disabled={d.removed}
                    placeholder={t('nodes.tagsPlaceholder')}
                    onChange={(e) => updateDraft(d.id, { tags: e.target.value })}
                  />
                </div>
                <label className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    className="h-4 w-4 rounded border-[var(--color-input)] accent-[var(--color-primary)]"
                    checked={d.allowScheduling}
                    disabled={d.removed}
                    onChange={(e) => updateDraft(d.id, { allowScheduling: e.target.checked })}
                  />
                  {t('nodes.allowScheduling')}
                </label>
                <label className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    className="h-4 w-4 rounded border-[var(--color-input)] accent-[var(--color-primary)]"
                    checked={d.evictionRequested}
                    disabled={d.removed}
                    onChange={(e) => updateDraft(d.id, { evictionRequested: e.target.checked })}
                  />
                  {t('nodes.requestEviction')}
                </label>
              </div>
            </div>
          ))}
          <Button type="button" size="sm" variant="outline" onClick={addDraft}>
            {t('nodes.addDisk')}
          </Button>
        </div>
      </Dialog>

      <ConfirmDialog
        open={Boolean(deleteNode)}
        onOpenChange={(v) => !v && setDeleteNode(null)}
        title={t('nodes.deleteNode')}
        confirmText={deleteNode?.name}
        destructive
        confirmLabel={t('common.delete')}
        onConfirm={async () => {
          if (!deleteNode) return
          const self = deleteNode.links?.self
          if (self) {
            const { lhRequest } = await import('@/api/longhorn')
            await lhRequest(self, 'DELETE')
          }
          setDeleteNode(null)
          await q.refetch()
        }}
      />
    </div>
  )
}
