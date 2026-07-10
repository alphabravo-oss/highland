import type { LucideIcon } from 'lucide-react'
import {
  Activity,
  Archive,
  Box,
  CalendarClock,
  CloudUpload,
  Cpu,
  DatabaseBackup,
  Gauge,
  Ghost,
  HardDrive,
  Image,
  KeyRound,
  LayoutDashboard,
  LifeBuoy,
  ScrollText,
  Server,
  Settings,
  Shield,
  Trash2,
} from 'lucide-react'

export type NavItem = {
  id: string
  /** i18n key under `nav.*` (e.g. `nav.volumes`) */
  labelKey: string
  /** English fallback label (tests + SSR-safe) */
  label: string
  path: string
  icon: LucideIcon
  /** If set, only these roles see the item */
  roles?: Array<'admin' | 'operator' | 'viewer'>
}

export type NavGroup = {
  id: string
  /** i18n key under `nav.*` */
  labelKey: string
  label: string
  items: NavItem[]
  roles?: Array<'admin' | 'operator' | 'viewer'>
}

/** Grouped sidebar navigation matching HIGHLAND_PLAN §14.2. */
export const navGroups: NavGroup[] = [
  {
    id: 'overview',
    labelKey: 'nav.overview',
    label: 'Overview',
    items: [
      {
        id: 'dashboard',
        labelKey: 'nav.dashboard',
        label: 'Dashboard',
        path: '/dashboard',
        icon: LayoutDashboard,
      },
    ],
  },
  {
    id: 'storage',
    labelKey: 'nav.storage',
    label: 'Storage',
    items: [
      {
        id: 'volumes',
        labelKey: 'nav.volumes',
        label: 'Volumes',
        path: '/volumes',
        icon: HardDrive,
      },
      {
        id: 'nodes',
        labelKey: 'nav.nodes',
        label: 'Nodes & Disks',
        path: '/nodes',
        icon: Server,
      },
    ],
  },
  {
    id: 'data-protection',
    labelKey: 'nav.dataProtection',
    label: 'Data Protection',
    items: [
      {
        id: 'backups',
        labelKey: 'nav.backups',
        label: 'Backups',
        path: '/backups',
        icon: Archive,
      },
      {
        id: 'backup-targets',
        labelKey: 'nav.backupTargets',
        label: 'Backup Targets',
        path: '/backup-targets',
        icon: CloudUpload,
      },
      {
        id: 'recurring-jobs',
        labelKey: 'nav.recurringJobs',
        label: 'Recurring Jobs',
        path: '/recurring-jobs',
        icon: CalendarClock,
      },
      {
        id: 'system-backups',
        labelKey: 'nav.systemBackups',
        label: 'System Backups',
        path: '/system-backups',
        icon: DatabaseBackup,
      },
    ],
  },
  {
    id: 'performance',
    labelKey: 'nav.performance',
    label: 'Performance',
    items: [
      {
        id: 'live-io',
        labelKey: 'nav.liveIo',
        label: 'Live I/O',
        path: '/performance',
        icon: Activity,
      },
      {
        id: 'benchmarks',
        labelKey: 'nav.benchmarks',
        label: 'Benchmarks',
        path: '/benchmarks',
        icon: Gauge,
      },
    ],
  },
  {
    id: 'runtime',
    labelKey: 'nav.runtime',
    label: 'Images & Runtime',
    items: [
      {
        id: 'backing-images',
        labelKey: 'nav.backingImages',
        label: 'Backing Images',
        path: '/backing-images',
        icon: Image,
      },
      {
        id: 'engine-images',
        labelKey: 'nav.engineImages',
        label: 'Engine Images',
        path: '/engine-images',
        icon: Box,
      },
      {
        id: 'instance-managers',
        labelKey: 'nav.instanceManagers',
        label: 'Instance Managers',
        path: '/instance-managers',
        icon: Cpu,
      },
    ],
  },
  {
    id: 'maintenance',
    labelKey: 'nav.maintenance',
    label: 'Maintenance',
    items: [
      {
        id: 'orphans',
        labelKey: 'nav.orphans',
        label: 'Orphaned Data',
        path: '/orphans',
        icon: Ghost,
      },
      {
        id: 'support-bundle',
        labelKey: 'nav.supportBundle',
        label: 'Support Bundle',
        path: '/support-bundle',
        icon: LifeBuoy,
      },
      {
        id: 'preflight',
        labelKey: 'nav.preflight',
        label: 'Preflight',
        path: '/preflight',
        icon: Trash2,
      },
    ],
  },
  {
    id: 'settings',
    labelKey: 'nav.settingsGroup',
    label: 'Settings',
    items: [
      {
        id: 'settings',
        labelKey: 'nav.settings',
        label: 'Settings',
        path: '/settings',
        icon: Settings,
      },
    ],
  },
  {
    id: 'admin',
    labelKey: 'nav.adminGroup',
    label: 'Admin',
    roles: ['admin'],
    items: [
      {
        id: 'admin',
        labelKey: 'nav.users',
        label: 'Users',
        path: '/admin',
        icon: Shield,
        roles: ['admin'],
      },
      {
        id: 'sso',
        labelKey: 'nav.sso',
        label: 'Enterprise SSO',
        path: '/admin/sso',
        icon: KeyRound,
        roles: ['admin'],
      },
      {
        id: 'audit',
        labelKey: 'nav.auditLog',
        label: 'Audit Log',
        path: '/admin/audit',
        icon: ScrollText,
        roles: ['admin'],
      },
    ],
  },
]

export function filterNavForRole(
  groups: NavGroup[],
  role: string | undefined,
): NavGroup[] {
  const r = (role ?? 'viewer') as 'admin' | 'operator' | 'viewer'
  return groups
    .filter((g) => !g.roles || g.roles.includes(r))
    .map((g) => ({
      ...g,
      items: g.items.filter((i) => !i.roles || i.roles.includes(r)),
    }))
    .filter((g) => g.items.length > 0)
}

export function findNavItem(pathname: string): NavItem | undefined {
  let best: NavItem | undefined
  for (const group of navGroups) {
    for (const item of group.items) {
      if (pathname === item.path || pathname.startsWith(item.path + '/')) {
        if (!best || item.path.length > best.path.length) {
          best = item
        }
      }
    }
  }
  return best
}
