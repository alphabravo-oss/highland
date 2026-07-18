import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page, type Route } from '@playwright/test'

const observedAt = '2026-07-15T12:00:00Z'
const healthyProvider = {
  id: 'rook-ceph',
  kind: 'rook-ceph',
  displayName: 'Rook / Ceph',
  supportLevel: 'managed',
  drivers: ['rook-ceph.rbd.csi.ceph.com', 'rook-ceph.cephfs.csi.ceph.com'],
  version: '1.20.2',
  namespace: 'rook-ceph',
  capabilities: ['inventory.claims.read', 'provider.health.read'],
  resourceKinds: ['clusters', 'pools', 'filesystems', 'mirroring', 'osds', 'rbd-images', 'quorum'],
  health: { status: 'ok', conditions: [], observedAt },
  metadata: {
    cephVersion: '20.2.1',
    dashboardPublicUrl: 'https://ceph.example.test',
    dashboardAvailability: 'available',
    dashboardPublicUrlSecurity: 'https',
  },
}
const openEBSProvider = {
  id: 'openebs',
  kind: 'openebs',
  displayName: 'OpenEBS',
  supportLevel: 'managed',
  drivers: ['openebs.io/local', 'local.csi.openebs.io', 'zfs.csi.openebs.io', 'io.openebs.csi-mayastor'],
  version: '4.5.1',
  namespace: 'openebs',
  capabilities: ['inventory.claims.read', 'inventory.volumes.read', 'inventory.snapshots.read', 'inventory.capacity.read', 'provider.health.read'],
  resourceKinds: ['components', 'engines', 'disk-pools', 'lvm-nodes', 'lvm-volumes', 'lvm-snapshots', 'zfs-nodes', 'zfs-volumes', 'zfs-snapshots', 'zfs-backups', 'zfs-restores', 'hostpath-volumes'],
  health: { status: 'ok', conditions: [{ type: 'ComponentsReady', status: 'True', severity: 'ok', reason: 'RolloutsReady', message: '1 OpenEBS component is ready.', observedAt }], observedAt },
  metadata: {
    'engine.hostpath': 'true',
    'engine.lvm': 'false',
    'engine.zfs': 'false',
    'engine.mayastor': 'false',
    installedEngines: 'hostpath',
    engineCount: '1',
    readOnly: 'true',
  },
}

function pageResult(data: unknown[], total = data.length, conditions?: unknown[]) {
  return { data, page: { limit: 100, total }, conditions }
}

async function fulfillStorage(route: Route) {
  const request = route.request()
  const url = new URL(request.url())
  const path = url.pathname.replace('/api/v1', '')
  let body: unknown
  switch (path) {
    case '/storage/providers':
      body = { data: [healthyProvider, openEBSProvider], meta: { lastSync: observedAt, snapshotApi: false } }
      break
    case '/storage/providers/rook-ceph':
      body = healthyProvider
      break
    case '/storage/providers/openebs':
      body = openEBSProvider
      break
    case '/storage/actions':
      body = { writesEnabled: true, data: [{ action: { id: 'restore-snapshot', capability: 'snapshot.restore', minimumRole: 'operator', risk: 'medium', confirmation: 'summary', featureFlag: 'storage.writes.enabled', preflightChecks: ['snapshot-ready', 'storage-class'], auditAction: 'storage_snapshot_restore' }, enabled: true, available: true }] }
      break
    case '/providers/rook-ceph/summary':
      body = {
        health: healthyProvider.health,
        runtimeHealth: {
          health: { status: 'HEALTH_OK' },
          mon_status: { quorum: [0, 1, 2] },
          mgr_map: { active_name: 'a', standbys: [] },
          df: { stats: { total_bytes: 107374182400, total_used_raw_bytes: 21474836480, total_avail_bytes: 85899345920 } },
          pg_info: { statuses: { 'active+clean': 64 }, object_stats: { num_objects: 1200, num_objects_degraded: 0, num_objects_misplaced: 0, num_objects_unfound: 0 } },
          client_perf: { read_bytes_sec: 1048576, write_bytes_sec: 524288, recovering_bytes_per_sec: 0 },
        },
        runtimeObservedAt: observedAt,
        runtimeStale: false,
        pools: [{ name: 'replicapool' }],
        filesystems: [{ name: 'cephfs' }],
        osds: [{ name: 'osd.0', up: 1, in: 1 }, { name: 'osd.1', up: 1, in: 1 }, { name: 'osd.2', up: 1, in: 1 }],
      }
      break
    case '/providers/rook-ceph/resources/pools':
      body = pageResult([{ name: 'replicapool', state: 'Ready', source: 'rook-crd', observedAt, password: 'must-never-render' }])
      break
    case '/providers/rook-ceph/resources/pools/replicapool':
      body = { name: 'replicapool', state: 'Ready', source: 'rook-crd', observedAt, spec: { replicatedSize: 3, failureDomain: 'host', secretName: 'must-never-render' } }
      break
    case '/providers/openebs/summary':
      body = {
        providerId: 'openebs',
        providerKind: 'openebs',
        namespace: 'openebs',
        version: '4.5.1',
        health: openEBSProvider.health,
        engines: [
          { id: 'mayastor', name: 'Replicated PV / Mayastor', driver: 'io.openebs.csi-mayastor', mode: 'replicated', installed: false, description: 'Replicated block storage.', resourceKinds: ['disk-pools'] },
          { id: 'hostpath', name: 'Dynamic LocalPV HostPath', driver: 'openebs.io/local', mode: 'local', installed: true, description: 'Non-replicated local directories.', resourceKinds: ['hostpath-volumes'] },
        ],
        components: [{ id: 'openebs-localpv-provisioner', name: 'openebs-localpv-provisioner', kind: 'Deployment', engine: 'hostpath', desired: 1, readyReplicas: 1, ready: true, images: ['openebs/provisioner-localpv:4.5.1'] }],
        resourceCounts: { 'hostpath-volumes': 1 },
        conditions: [],
        observedAt,
      }
      break
    case '/providers/openebs/resources/hostpath-volumes':
      body = pageResult([{ id: 'pvc-openebs', name: 'pvc-openebs', engine: 'hostpath', state: 'Bound', phase: 'Bound', node: 'storage-1', storageClass: 'openebs-hostpath', capacity: '1Gi', path: '/var/openebs/local/pvc-openebs', source: 'kubernetes-pv', observedAt }])
      break
    case '/providers/openebs/resources/hostpath-volumes/pvc-openebs':
      body = { id: 'pvc-openebs', name: 'pvc-openebs', engine: 'hostpath', state: 'Bound', phase: 'Bound', node: 'storage-1', storageClass: 'openebs-hostpath', capacity: '1Gi', path: '/var/openebs/local/pvc-openebs', source: 'kubernetes-pv', observedAt }
      break
    case '/storage/classes':
      body = pageResult([{ name: 'ceph-rbd', kubernetesUid: 'sc-1', providerId: 'rook-ceph', provisioner: 'rook-ceph.rbd.csi.ceph.com', reclaimPolicy: 'Delete', volumeBindingMode: 'WaitForFirstConsumer', allowVolumeExpansion: true, default: true, claimCount: 1, volumeCount: 1 }])
      break
    case '/storage/claims': {
      const rows = Array.from({ length: 100 }, (_, index) => ({ id: `cluster/scale/pvc/claim-${index}`, namespace: 'scale', name: `claim-${index}`, kubernetesUid: `pvc-${index}`, providerId: 'rook-ceph', driver: 'rook-ceph.rbd.csi.ceph.com', storageClass: 'ceph-rbd', pvName: `pv-${index}`, phase: 'Bound', requestedCapacity: '1Gi', provisionedCapacity: '1Gi', workloads: index === 0 ? [{ namespace: 'scale', kind: 'Deployment', name: 'app', podName: 'app-0', podPhase: 'Running', nodeName: 'storage-1' }] : [] }))
      body = pageResult(rows, 10_000)
      break
    }
    case '/storage/claims/scale/claim-0':
      body = { id: 'cluster/scale/pvc/claim-0', namespace: 'scale', name: 'claim-0', kubernetesUid: 'pvc-0', providerId: 'rook-ceph', driver: 'rook-ceph.rbd.csi.ceph.com', storageClass: 'ceph-rbd', pvName: 'pv-0', phase: 'Bound', requestedCapacity: '1Gi', provisionedCapacity: '1Gi', accessModes: ['ReadWriteOnce'], volumeMode: 'Filesystem', volumeHandle: 'image-0', reclaimPolicy: 'Delete', workloads: [{ namespace: 'scale', kind: 'Deployment', name: 'app', podName: 'app-0', podPhase: 'Running', nodeName: 'storage-1' }], attachmentIds: ['attach-0'], providerRef: { kind: 'ceph-rbd-image', id: 'image-0' } }
      break
    case '/storage/volumes/pv-0':
      body = { name: 'pv-0', kubernetesUid: 'pv-uid-0', providerId: 'rook-ceph', driver: 'rook-ceph.rbd.csi.ceph.com', volumeHandle: 'image-0', storageClass: 'ceph-rbd', phase: 'Bound', capacity: '1Gi', reclaimPolicy: 'Delete', claimNamespace: 'scale', claimName: 'claim-0', attachmentIds: ['attach-0'], providerRef: { kind: 'ceph-rbd-image', id: 'image-0' } }
      break
    case '/storage/attachments':
      body = pageResult([{ name: 'attach-0', providerId: 'rook-ceph', driver: 'rook-ceph.rbd.csi.ceph.com', pvName: 'pv-0', nodeName: 'storage-1', attached: true }])
      break
    case '/storage/snapshots':
      body = pageResult([], 0, [{ type: 'SnapshotAPIAvailable', status: 'False', severity: 'warning', reason: 'APINotServed', message: 'snapshot.storage.k8s.io/v1 is not installed' }])
      break
    default:
      body = pageResult([])
  }
  await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
}

async function prepareStorageMocks(page: Page) {
  await page.route('**/api/v1/storage/**', fulfillStorage)
  await page.route('**/api/v1/providers/rook-ceph/**', fulfillStorage)
  await page.route('**/api/v1/providers/openebs/**', fulfillStorage)
}

async function login(page: Page) {
  await page.goto('/login')
  await page.locator('#username').fill('admin')
  await page.locator('#password').fill('highland')
  await page.getByRole('button', { name: /sign in/i }).click()
  await expect(page.getByTestId('app-shell')).toBeVisible()
}

test.describe('provider-neutral storage UI', () => {
  test.beforeEach(async ({ page }) => {
    await prepareStorageMocks(page)
    await login(page)
  })

  test('drills from provider to curated Ceph resource detail', async ({ page }) => {
    await page.goto('/storage/providers')
    await expect(page.getByTestId('storage-providers-page')).toBeVisible()
    await page.getByRole('link', { name: /Rook \/ Ceph/ }).click()
    await expect(page.getByRole('heading', { level: 1, name: 'Dashboard' })).toBeVisible()
    await expect(page.getByTestId('sidebar').getByRole('link', { name: 'Dashboard' })).toBeVisible()
    await expect(page.getByText('Cluster is healthy')).toBeVisible()
    await expect(page.getByText('20.0% used')).toBeVisible()
    await expect(page.getByText('3/3 up · 3/3 in')).toBeVisible()
    const overviewHandoff = page.getByTestId('ceph-dashboard-handoff').getByRole('link', { name: 'Open Ceph Dashboard' })
    await expect(overviewHandoff).toHaveAttribute('href', 'https://ceph.example.test/')
    await expect(overviewHandoff).toHaveAttribute('target', '_blank')
    await expect(overviewHandoff).toHaveAttribute('rel', 'noopener noreferrer')
    await expect(page.getByText(/Private reader status: available/)).toBeVisible()
    await page.getByRole('main').getByRole('link', { name: 'Block pools' }).click()
    await expect(page.getByTestId('ceph-resource-page')).toBeVisible()
    await page.getByRole('link', { name: 'replicapool' }).click()
    await expect(page.getByTestId('ceph-resource-detail-page')).toBeVisible()
    await expect(page.getByText('Replication factor')).toBeVisible()
    await expect(page.getByText(/must-never-render/)).toHaveCount(0)
    const detailHandoff = page.getByTestId('ceph-dashboard-handoff').getByRole('link', { name: 'Open Ceph Dashboard' })
    await expect(detailHandoff).toHaveAttribute('href', 'https://ceph.example.test/#/pool')
    await expect(page.getByText(/relevant Ceph area/)).toBeVisible()
  })

  test('switches the sidebar between all-storage and Rook/Ceph workspaces', async ({ page }) => {
    await page.goto('/storage/providers/rook-ceph')
    const sidebar = page.getByTestId('sidebar')
    const workspace = sidebar.getByLabel('Storage workspace')
    await expect(workspace).toHaveValue('rook-ceph')
    await expect(sidebar.getByRole('link', { name: 'Block Pools' })).toBeVisible()
    await expect(sidebar.getByRole('link', { name: 'CephFS Filesystems' })).toBeVisible()
    await expect(sidebar.getByRole('link', { name: 'Backups' })).toHaveCount(0)
    await expect(sidebar.getByText(/^nav\./)).toHaveCount(0)

    await workspace.selectOption('__all__')
    await expect(page).toHaveURL(/\/storage\/providers$/)
    await expect(sidebar.getByLabel('Storage workspace')).toHaveValue('__all__')
    await expect(sidebar.getByRole('link', { name: 'Providers' })).toBeVisible()
    await expect(sidebar.getByRole('link', { name: 'Block Pools' })).toHaveCount(0)
  })

  test('keeps OpenEBS engines distinct and explains local-volume risk', async ({ page }) => {
    await page.goto('/storage/providers/openebs')
    await expect(page.getByTestId('openebs-provider-page')).toBeVisible()
    await expect(page.getByRole('heading', { level: 1, name: 'Dashboard' })).toBeVisible()
    await expect(page.getByTestId('sidebar').getByRole('link', { name: 'Dashboard' })).toBeVisible()
    await expect(page.getByText('OpenEBS is healthy')).toBeVisible()
    await expect(page.getByText('Dynamic LocalPV HostPath', { exact: true })).toBeVisible()
    await page.getByText(/other supported engine.*not installed/).click()
    await expect(page.getByText('Replicated PV / Mayastor', { exact: true })).toBeVisible()
    await expect(page.getByText('not installed', { exact: true })).toBeVisible()
    await expect(page.getByText(/Node-local data does not fail over/)).toBeVisible()
    const sidebar = page.getByTestId('sidebar')
    await expect(sidebar.getByRole('link', { name: 'Local Volumes' })).toBeVisible()
    await expect(sidebar.getByRole('link', { name: 'Disk Pools' })).toHaveCount(0)

    await sidebar.getByRole('link', { name: 'Local Volumes' }).click()
    await expect(page.getByTestId('openebs-resource-page')).toBeVisible()
    await expect(page.getByText('Node-local storage', { exact: true })).toBeVisible()
    await page.getByRole('link', { name: 'pvc-openebs' }).click()
    await expect(page.getByTestId('openebs-resource-detail-page')).toBeVisible()
    await expect(page.getByText('Local failure boundary', { exact: true })).toBeVisible()
    await expect(page.getByText('/var/openebs/local/pvc-openebs')).toBeVisible()
  })

  test('drills from provider through class, claim, PV, workload, and attachment', async ({ page }) => {
    await page.goto('/storage/providers')
    await page.getByRole('link', { name: /Rook \/ Ceph/ }).click()
    await page.getByRole('main').getByRole('link', { name: 'Storage classes' }).click()
    await expect(page.getByRole('textbox', { name: 'Provider', exact: true })).toHaveValue('rook-ceph')
    await page.getByRole('link', { name: 'ceph-rbd' }).click()
    await expect(page.getByTestId('resource-relationships-page')).toBeVisible()
    await page.getByTestId('sidebar').getByRole('link', { name: 'Claims & Workloads' }).click()
    await expect(page.getByRole('textbox', { name: 'Provider', exact: true })).toHaveValue('rook-ceph')
    await page.getByLabel('Search').fill('ceph-rbd')
    await page.getByRole('link', { name: 'scale/claim-0' }).click()
    await expect(page.getByTestId('storage-claim-detail-page')).toBeVisible()
    await expect(page.getByText('Deployment scale/app')).toBeVisible()
    await expect(page.getByText(/Pod app-0.*node storage-1/)).toBeVisible()
    await page.getByRole('link', { name: 'pv-0' }).click()
    await expect(page.getByTestId('storage-volume-detail-page')).toBeVisible()
    await page.getByRole('link', { name: 'attach-0' }).click()
    await expect(page.getByTestId('storage-attachments-page')).toBeVisible()
    await expect(page.getByLabel('Search')).toHaveValue('attach-0')
    await expect(page.getByText('storage-1')).toBeVisible()
  })

  test('preserves filters and explains partial snapshot support accessibly', async ({ page }) => {
    await page.goto('/storage/classes?provider=rook-ceph&namespace=team-a')
    await expect(page.getByRole('textbox', { name: 'Provider', exact: true })).toHaveValue('rook-ceph')
    await expect(page.getByLabel('Namespace')).toHaveValue('team-a')
    await page.reload()
    await expect(page.getByRole('textbox', { name: 'Provider', exact: true })).toHaveValue('rook-ceph')
    await expect(page.getByLabel('Namespace')).toHaveValue('team-a')

    await page.goto('/storage/snapshots')
    await expect(page.getByText('APINotServed')).toBeVisible()
    await expect(page.getByText('snapshot.storage.k8s.io/v1 is not installed')).toBeVisible()
    const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
    expect(results.violations.filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')).toEqual([])
  })

  test('renders typed operation controls without a raw JSON editor', async ({ page }) => {
    await page.goto('/storage/actions/restore-snapshot')
    await expect(page.getByTestId('storage-action-page')).toBeVisible()
    await expect(page.getByLabel('Source snapshot')).toBeVisible()
    await expect(page.getByLabel('StorageClass')).toBeVisible()
    await expect(page.getByLabel('Requested capacity')).toHaveValue('10Gi')
    await expect(page.getByLabel('Volume mode')).toHaveValue('Filesystem')
    await expect(page.locator('#operation-parameters')).toHaveCount(0)
    await expect(page.getByText(/Credentials and secret values are never accepted/)).toBeVisible()
  })

  test('explains disabled benchmark execution instead of offering synthetic results', async ({ page }) => {
    const benchmark = {
      name: 'bench-openebs',
      profile: 'quick',
      phase: 'Succeeded',
      mode: 'kubernetes-job',
      message: 'fio Job completed',
      providerId: 'openebs',
      csiDriver: 'openebs.io/local',
      storageClass: 'openebs-hostpath',
      nodeName: 'storage-1',
      pvcName: 'benchmark-pvc',
      pvName: 'benchmark-pv',
      createdAt: '2026-07-16T00:00:00Z',
      completedAt: '2026-07-16T00:00:42Z',
      results: { seqReadMBps: 500, randWriteIOPS: 12_000 },
      fioCmd: 'fio --name=highland-quick',
    }
    await page.route('**/api/v1/benchmarks**', async (route) => {
      const isDetail = new URL(route.request().url()).pathname.endsWith('/bench-openebs')
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(isDetail ? benchmark : {
          data: [benchmark],
          page: { limit: 50, total: 1 },
          meta: { observedAt: '2026-07-16T00:00:00Z', stale: false, partial: false, benchmarkMode: 'disabled' },
        }),
      })
    })
    await page.goto('/benchmarks')
    await expect(page.getByTestId('benchmarks-page')).toBeVisible()
    const description = page.getByText('Run controlled fio workloads against a selected CSI StorageClass and retain provider-attributed results.')
    const descriptionBox = await description.boundingBox()
    const panelBox = await page.getByTestId('benchmark-run-panel').boundingBox()
    expect(descriptionBox?.width).toBeGreaterThan(400)
    expect(panelBox?.y).toBeGreaterThan((descriptionBox?.y ?? 0) + (descriptionBox?.height ?? 0))
    await expect(page.getByTestId('benchmark-mode-disabled')).toContainText('Real benchmark execution is disabled')
    await expect(page.getByTestId('run-benchmark')).toBeDisabled()
    await expect(page.getByText(/Synthetic offline benchmarks/)).toHaveCount(0)
    await expect(page.getByTestId('benchmark-details-bench-openebs')).toHaveCount(0)
    await page.getByRole('button', { name: 'View details for bench-openebs' }).click()
    const target = page.getByTestId('benchmark-target-bench-openebs')
    await expect(target).toContainText('OpenEBS')
    await expect(target).toContainText('openebs-hostpath')
    await expect(target).toContainText('openebs.io/local')
    await expect(page.getByTestId('benchmark-details-bench-openebs')).toContainText('500 MB/s')
    await expect(page.getByTestId('benchmark-details-bench-openebs')).toContainText('fio --name=highland-quick')
    const expandedWidths = await page.getByTestId('benchmark-details-bench-openebs').evaluate((details) => {
      const table = details.closest('table')
      const scroller = table?.parentElement
      return { table: table?.getBoundingClientRect().width ?? 0, scroller: scroller?.clientWidth ?? 0 }
    })
    expect(expandedWidths.table).toBeLessThanOrEqual(expandedWidths.scroller + 1)
  })

  test('reports provider-neutral system health and compatibility', async ({ page }) => {
    await page.route('**/api/v1/status', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          highland: { version: '0.3.0', sessionBackend: 'signed-cookie', benchmarkMode: 'kubernetes-job' },
          kubernetes: { version: 'v1.36.2+k3s1' },
          longhorn: { enabled: true, version: 'v1.12.0', namespace: 'longhorn-system', managerUrl: 'http://longhorn-backend', reachable: true, supported: ['1.12.x', '1.11.x'] },
          components: { api: 'ok', managerProxy: 'ok', metricsScraper: 'ok', scrapeError: '' },
          compatibility: {
            releaseLine: '0.3.x-storage-preview',
            lastUpdated: '2026-07-18',
            kubernetes: { minimum: '1.34', maximum: '1.36' },
            providers: {
              'rook-ceph': { stage: 'preview', tested: 'Rook 1.19.6 / 1.20.2 · Ceph 19.2.3 / 20.2.1' },
              openebs: { stage: 'preview', tested: 'OpenEBS 4.5.1' },
            },
          },
          storage: {
            ready: true,
            lastSync: observedAt,
            snapshotApi: false,
            providersObservedAt: observedAt,
            providersStale: false,
            providers: [
              { ...healthyProvider, health: { ...healthyProvider.health, conditions: [{ type: 'PrometheusAvailable', status: 'Unknown', severity: 'info', reason: 'NotConfigured', message: 'Ceph time-series metrics are unavailable.', observedAt }] } },
              openEBSProvider,
            ],
          },
          storagePolicy: {
            source: 'runtime-policy',
            effective: { acceptNewOperations: true, portableKubernetesWrites: false, portableKubernetesProviderIds: [], longhornWrites: true, rookCephWrites: false, allowCephStorageClassDelete: false, allowCephPoolDelete: false },
            generation: 2,
            observedGeneration: 2,
            observedAt,
            stale: false,
            partial: false,
            conditions: [],
          },
          vendor: { name: 'AlphaBravo', url: 'https://alphabravo.io', tagline: 'Highland is an enterprise storage operations manager for Kubernetes CSI providers, developed by AlphaBravo.' },
        }),
      })
    })
    await page.goto('/status')
    await expect(page.getByTestId('status-page')).toBeVisible()
    await expect(page.getByTestId('overall-system-status')).toContainText('Operational')
    await expect(page.getByTestId('status-provider-rook-ceph')).toContainText('Rook 1.20.2')
    await expect(page.getByTestId('status-provider-rook-ceph')).toContainText('Ceph 20.2.1')
    await expect(page.getByTestId('status-provider-openebs')).toContainText('OpenEBS 4.5.1')
    await expect(page.getByTestId('status-runtime-policy')).toContainText('Longhorn on')
    await expect(page.getByTestId('status-runtime-policy')).toContainText('Rook/Ceph off')
    await expect(page.getByTestId('status-conditions')).toContainText('Ceph time-series metrics are unavailable.')
    await expect(page.getByText('Supported Longhorn')).toHaveCount(0)
    await expect(page.getByText('Highland is an enterprise storage operations manager for Kubernetes CSI providers, developed by AlphaBravo.')).toBeVisible()
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
    expect(overflow).toBeLessThanOrEqual(1)
    const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
    expect(results.violations.filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')).toEqual([])
  })

  test('renders a bounded page for a 10,000-claim inventory', async ({ page }) => {
    await page.goto('/storage/claims')
    await expect(page.getByTestId('storage-claims-page')).toBeVisible()
    await expect(page.getByText('10000 matching objects in the current cache')).toBeVisible()
    // The API page is bounded to 100 records and the table further renders a
    // 25-row viewport, keeping the DOM small even when the cache contains 10k.
    await expect(page.getByRole('row')).toHaveCount(26)
    await expect(page.getByText('scale/claim-0')).toBeVisible()
  })
})
