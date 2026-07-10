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
import { formatBytes, hasAction, type Node } from '@/api/longhorn'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Alert } from '@/components/ui/alert'
import { Badge, stateTone } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Table, TBody, TD, TH, THead, TR } from '@/components/ui/table'
import { ConfirmDialog } from '@/components/data/ConfirmDialog'
import { useAppTranslation } from '@/i18n/useAppTranslation'

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
  const [diskJson, setDiskJson] = useState('')

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

  async function saveDisks() {
    if (!diskNode) return
    setError(null)
    try {
      const disks = JSON.parse(diskJson) as Node['disks']
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
            const ready = node.conditions?.find((c) => c.type === 'Ready')
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
                          onClick={() => {
                            setDiskNode(node)
                            setDiskJson(JSON.stringify(node.disks ?? {}, null, 2))
                          }}
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
        title={t('nodes.editDisksJson')}
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
        <textarea
          className="min-h-[200px] w-full rounded-md border border-[var(--color-input)] bg-[var(--color-background)] p-2 font-mono text-xs"
          value={diskJson}
          onChange={(e) => setDiskJson(e.target.value)}
        />
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
