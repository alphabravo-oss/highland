import { useMemo, type ReactNode } from 'react'
import { Camera, ChevronRight, Layers } from 'lucide-react'
import { formatBytes } from '@/api/longhorn'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

/** Minimal shape of a Longhorn snapshot object needed to draw the tree. */
export interface SnapshotNodeData {
  name: string
  parent?: string
  children?: string[] | Record<string, unknown>
  created?: string
  size?: string | number
  usercreated?: boolean
  removed?: boolean
  [k: string]: unknown
}

export interface SnapshotTreeLabels {
  volumeHead: string
  start: string
  systemTag: string
  empty: string
  created: string
  size: string
}

export interface SnapshotTreeProps {
  snapshots: SnapshotNodeData[]
  /** When false, snapshots that are system-generated or removed are hidden. */
  showSystem: boolean
  labels: SnapshotTreeLabels
  /** Render prop for per-node actions (revert/backup/delete). */
  renderActions?: (snap: SnapshotNodeData) => ReactNode
}

const VOLUME_HEAD = 'volume-head'

interface OrderedRow {
  snap: SnapshotNodeData
  depth: number
}

function childNamesOf(snap: SnapshotNodeData | undefined): string[] {
  if (!snap?.children) return []
  if (Array.isArray(snap.children)) return snap.children.map(String)
  return Object.keys(snap.children)
}

function isSystem(snap: SnapshotNodeData): boolean {
  return snap.usercreated === false || snap.removed === true
}

/**
 * Derive an ordered, top-to-bottom list of snapshot rows from the flat
 * Longhorn snapshot objects. Children (newer snapshots) are emitted before
 * their parent (older) so the newest sits at the top and the root at the
 * bottom, matching a "Volume Head → … → Start" timeline. Branch points are
 * indented via `depth`.
 */
function buildOrder(snapshots: SnapshotNodeData[]): OrderedRow[] {
  const byName = new Map<string, SnapshotNodeData>()
  for (const s of snapshots) {
    if (s.name && s.name !== VOLUME_HEAD) byName.set(s.name, s)
  }

  // Prefer explicit children pointers; fall back to reconstructing them from
  // parent pointers when children arrays are missing.
  const childrenMap = new Map<string, string[]>()
  for (const s of byName.values()) childrenMap.set(s.name, [])
  let anyChildren = false
  for (const s of byName.values()) {
    for (const c of childNamesOf(s)) {
      if (byName.has(c)) {
        childrenMap.get(s.name)!.push(c)
        anyChildren = true
      }
    }
  }
  if (!anyChildren) {
    for (const s of byName.values()) {
      if (s.parent && byName.has(s.parent)) childrenMap.get(s.parent)!.push(s.name)
    }
  }

  const createdOf = (name: string) => byName.get(name)?.created ?? ''
  const sortDesc = (names: string[]) =>
    [...names].sort((a, b) => (createdOf(a) < createdOf(b) ? 1 : createdOf(a) > createdOf(b) ? -1 : 0))

  const roots = [...byName.values()]
    .filter((s) => !s.parent || !byName.has(s.parent))
    .map((s) => s.name)

  const out: OrderedRow[] = []
  const seen = new Set<string>()
  const walk = (name: string, depth: number) => {
    if (seen.has(name)) return
    seen.add(name)
    const kids = sortDesc(childrenMap.get(name) ?? [])
    const branching = kids.length > 1
    for (const k of kids) walk(k, branching ? depth + 1 : depth)
    const snap = byName.get(name)
    if (snap) out.push({ snap, depth })
  }
  for (const r of sortDesc(roots)) walk(r, 0)
  // Any orphans not reachable from a root (defensive).
  for (const s of byName.values()) if (!seen.has(s.name)) out.push({ snap: s, depth: 0 })

  return out
}

function TreeRow({
  children,
  depth = 0,
  connectorTop = true,
  connectorBottom = true,
}: {
  children: ReactNode
  depth?: number
  connectorTop?: boolean
  connectorBottom?: boolean
}) {
  return (
    <div className="flex items-stretch" style={{ marginLeft: depth * 24 }}>
      {/* Connector rail */}
      <div className="relative flex w-6 flex-none flex-col items-center">
        <span
          className={cn(
            'w-px flex-1',
            connectorTop ? 'bg-[var(--color-border)]' : 'bg-transparent',
          )}
        />
        <span className="my-1 size-2 flex-none rounded-full bg-[var(--color-primary)]" />
        <span
          className={cn(
            'w-px flex-1',
            connectorBottom ? 'bg-[var(--color-border)]' : 'bg-transparent',
          )}
        />
      </div>
      <div className="min-w-0 flex-1 py-1.5">{children}</div>
    </div>
  )
}

export function SnapshotTree({ snapshots, showSystem, labels, renderActions }: SnapshotTreeProps) {
  const head = useMemo(() => snapshots.find((s) => s.name === VOLUME_HEAD), [snapshots])
  const rows = useMemo(() => buildOrder(snapshots), [snapshots])
  const visible = useMemo(
    () => rows.filter((r) => showSystem || !isSystem(r.snap)),
    [rows, showSystem],
  )

  if (visible.length === 0 && !head) {
    return <p className="text-sm text-[var(--color-muted-foreground)]">{labels.empty}</p>
  }

  return (
    <div data-testid="snapshot-tree" className="flex flex-col">
      {/* Volume Head (top / newest) */}
      <TreeRow connectorTop={false} connectorBottom={visible.length > 0}>
        <div className="inline-flex items-center gap-2 rounded-md border border-[var(--color-border)] bg-[var(--color-muted)] px-3 py-1.5 text-sm font-semibold">
          <ChevronRight size={14} aria-hidden />
          <Layers size={14} aria-hidden />
          {labels.volumeHead}
          {head?.size !== undefined ? (
            <span className="font-normal text-[var(--color-muted-foreground)]">
              {formatBytes(head.size)}
            </span>
          ) : null}
        </div>
      </TreeRow>

      {/* Snapshot chain */}
      {visible.map((row) => {
        const s = row.snap
        const system = isSystem(s)
        return (
          <TreeRow key={s.name} depth={row.depth}>
            <div
              className={cn(
                'flex flex-wrap items-center gap-x-3 gap-y-1 rounded-md border px-3 py-2',
                system
                  ? 'border-dashed border-[var(--color-border)] bg-transparent'
                  : 'border-[var(--color-border)] bg-[var(--color-card)] shadow-sm',
              )}
              data-testid={`snapshot-node-${s.name}`}
            >
              <Camera
                size={14}
                aria-hidden
                className={system ? 'text-[var(--color-muted-foreground)]' : 'text-[var(--color-primary)]'}
              />
              <span className="min-w-0 break-all font-medium" title={s.name}>
                {s.name}
              </span>
              {system ? (
                <Badge tone="default">{labels.systemTag}</Badge>
              ) : null}
              <span className="text-xs text-[var(--color-muted-foreground)]">
                {labels.created}: {s.created ?? '—'}
              </span>
              <span className="text-xs tabular-nums text-[var(--color-muted-foreground)]">
                {labels.size}: {formatBytes(s.size)}
              </span>
              {renderActions ? (
                <span className="ms-auto flex flex-none flex-wrap justify-end gap-1">
                  {renderActions(s)}
                </span>
              ) : null}
            </div>
          </TreeRow>
        )
      })}

      {/* Start marker (bottom / oldest) */}
      <TreeRow connectorBottom={false}>
        <div className="inline-flex items-center rounded-md border border-[var(--color-border)] bg-[var(--color-muted)] px-3 py-1 text-xs font-medium text-[var(--color-muted-foreground)]">
          {labels.start}
        </div>
      </TreeRow>
    </div>
  )
}
