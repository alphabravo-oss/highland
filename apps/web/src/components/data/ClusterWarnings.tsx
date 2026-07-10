import { useState } from 'react'
import { AlertTriangle, X } from 'lucide-react'
import { useBackupTargets, useNodes, useSettings } from '@/api/hooks'
import { useAppTranslation } from '@/i18n/useAppTranslation'

/**
 * Surfaces cluster-level misconfigurations proactively — e.g. a default
 * replica/backing-image-copy count that the current node topology can't
 * satisfy, so new volumes/images would be born degraded.
 */
export function ClusterWarnings() {
  const { t } = useAppTranslation()
  const settings = useSettings()
  const nodes = useNodes()
  const targets = useBackupTargets()
  const [dismissed, setDismissed] = useState(false)

  const settingVal = (name: string): number | undefined => {
    const raw = (settings.data ?? []).find((x) => x.name === name)?.value
    if (raw == null) return undefined
    // Data-engine-specific settings carry a per-engine JSON map (e.g. {"v1":"3","v2":"3"});
    // take the largest engine value so the strictest requirement is checked.
    try {
      const parsed = JSON.parse(raw) as Record<string, unknown>
      if (parsed && typeof parsed === 'object') {
        const nums = Object.values(parsed).map(Number).filter((n) => Number.isFinite(n))
        return nums.length ? Math.max(...nums) : undefined
      }
    } catch {
      /* not JSON — fall through to plain number */
    }
    const n = Number(raw)
    return Number.isFinite(n) ? n : undefined
  }

  const nodeList = nodes.data ?? []
  const totalNodes = nodeList.length
  const schedulable = nodeList.filter((n) => n.allowScheduling).length

  const warnings: string[] = []

  const replicaDefault = settingVal('default-replica-count')
  if (replicaDefault != null && schedulable > 0 && replicaDefault > schedulable) {
    warnings.push(t('warnings.replicaExceedsNodes', { count: replicaDefault, nodes: schedulable }))
  }

  const minCopies = settingVal('default-min-number-of-backing-image-copies')
  if (minCopies != null && totalNodes > 0 && minCopies > totalNodes) {
    warnings.push(t('warnings.minCopiesExceedsNodes', { count: minCopies, nodes: totalNodes }))
  }

  const hasBackupTarget = (targets.data ?? []).some((bt) => (bt.backupTargetURL ?? '').trim() !== '')
  if (targets.isSuccess && !hasBackupTarget) {
    warnings.push(t('warnings.noBackupTarget'))
  }

  if (dismissed || warnings.length === 0) return null

  return (
    <div
      className="mb-4 flex items-start gap-3 rounded-lg border border-amber-500/40 bg-amber-500/10 px-4 py-3 text-sm text-amber-900 dark:text-amber-200"
      role="alert"
      data-testid="cluster-warnings"
    >
      <AlertTriangle size={18} className="mt-0.5 shrink-0 text-amber-600 dark:text-amber-400" aria-hidden />
      <div className="flex-1">
        <div className="font-medium">{t('warnings.title', { count: warnings.length })}</div>
        <ul className="mt-1 list-disc space-y-0.5 pl-4">
          {warnings.map((wmsg, i) => (
            <li key={i}>{wmsg}</li>
          ))}
        </ul>
      </div>
      <button
        type="button"
        onClick={() => setDismissed(true)}
        className="shrink-0 rounded p-0.5 hover:bg-amber-500/20"
        aria-label={t('warnings.dismiss')}
      >
        <X size={15} aria-hidden />
      </button>
    </div>
  )
}
