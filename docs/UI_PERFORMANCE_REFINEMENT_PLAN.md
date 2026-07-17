# Highland UI Performance and Data Delivery Refinement Plan

Status: Complete

This is the authoritative implementation and validation checklist for the
site-wide UI performance and API delivery work. Every completed item below has
source, automated-test, built-artifact, or deployed-runtime evidence.

## Baseline

- [x] Record a live authenticated cold-load trace.
  - `/benchmarks` FCP was approximately 100 ms on the local network and usable
    data was approximately 538 ms.
  - Cold transfer was approximately 1.2 MB.
- [x] Measure representative APIs.
  - `/api/v1/benchmarks` and `/api/v1/storage/classes` were 0.4-0.7 ms.
  - `/api/v1/storage/providers` varied from 0.25-3.75 seconds, with production
    log spikes above seven seconds.
- [x] Confirm original delivery behavior.
  - Static text assets were uncompressed.
  - Benchmarks downloaded the Live I/O chart dependency.
  - The shell preloaded Dashboard and Volumes on every authenticated page.
  - Highland already had one authenticated global SSE connection.

## Phase 1: Asset Delivery and Route Boundaries

- [x] Enable gzip for HTML, CSS, JavaScript, JSON, SVG, and text responses.
- [x] Generate build-time Brotli and gzip variants for eligible text assets.
  - The deployed nginx build serves gzip; Brotli artifacts are present for
    runtimes with Brotli-static support.
- [x] Preserve immutable caching for content-hashed assets.
- [x] Preserve no-cache behavior for the SPA document.
- [x] Split Live I/O and Benchmarks into independent source/route modules.
- [x] Prove Benchmarks no longer downloads the chart bundle.
- [x] Replace unconditional shell preloading with intent- and network-aware
      prefetching that respects save-data and slow connections.
- [x] Add a bundle report and enforce entry and route budgets during builds.
- [x] Add an accessible, reduced-motion-aware boot shell and early adoption of
      authenticated primary-route responses to remove serial startup requests.
- [x] Lazy-load the Spanish locale and remove the unused i18next runtime.

Definition of done:

- [x] Eligible live text responses include `Content-Encoding: gzip`.
- [x] A cold Benchmarks load transfers substantially less than the 1.2 MB
      baseline.
- [x] Dashboard, Volumes, TanStack Table, and chart chunks are absent from the
      Benchmarks critical load.
- [x] Production build, bundle-budget, and CSP hash tests pass.

Evidence:

- Deployed critical assets are approximately 178 KB compressed.
- Live `/benchmarks` loads only the entry graph and its small UI dependencies;
  `dashcharts`, `DashboardPage`, `VolumesPage`, and `tanstack-table` are absent.
- Live normal-network result: FCP 44 ms, usable 184 ms.
- Final live fast-3G result: FCP 560 ms, usable 1,417 ms.

## Phase 2: Realtime Events and Query Invalidation

- [x] Publish benchmark created, running, succeeded, failed, and deleted events.
- [x] Publish Highland storage-operation created, updated, and deleted events.
- [x] Extend SSE v2 frames with a bounded event type and optional JSON entity.
- [x] Update TanStack caches directly when a complete entity is present.
- [x] Map inventory changes to precise resource query families.
- [x] Remove root-wide storage invalidation for ordinary resource changes.
- [x] Keep a slow safety poll while SSE is connected.
- [x] Use fast polling only when SSE is unavailable or foreground work is active.
- [x] Stop job and operation polling at terminal states.
- [x] Test reconnects, malformed frames, scoped invalidation, direct cache
      updates, fallback polling, and Highland lifecycle frames.

Definition of done:

- [x] A benchmark phase change appears without waiting for polling.
- [x] One PVC event does not refetch providers or unrelated storage families.
- [x] SSE failure still converges through adaptive polling.
- [x] No second site-wide SSE connection was introduced.

Evidence:

- A real live benchmark emitted `benchmark.running` with the complete entity.
- The new benchmark appeared in the page cache with benchmark GET count
  unchanged at one, proving direct SSE cache application.
- Go SSE integration coverage publishes both benchmark and storage-operation
  lifecycle entities through the actual stream writer.
- Frontend tests cover malformed frames and precise invalidation predicates.

## Phase 3: Fast Provider and Status Snapshots

- [x] Introduce a concurrency-safe provider descriptor snapshot cache.
- [x] Deduplicate concurrent refreshes.
- [x] Refresh provider health asynchronously with a four-second bound.
- [x] Return the last successful snapshot immediately with `observedAt` and
      `stale` metadata.
- [x] Remove synchronous provider descriptor probes from driver discovery and
      navigation requests.
- [x] Make `/status` consume cached provider facts.
- [x] Add hit, miss, expiry, stale fallback, and refresh tests.
- [x] Add concurrent request deduplication tests.

Definition of done:

- [x] Warm `/api/v1/storage/providers` p95 is below 150 ms locally.
- [x] Concurrent callers cause one refresh instead of duplicate probes.
- [x] Failed refreshes preserve the last good stale snapshot.
- [x] Navigation chrome does not wait on fresh Ceph, Longhorn, or OpenEBS probes.

Evidence:

- Before: 0.25-3.75 seconds, with spikes over seven seconds.
- After: 40-request live p50 1.25 ms, p95 1.98 ms, max 6.20 ms.
- The root cause found during deployment validation was
  `DiscoveredDriverNames` calling each provider descriptor on every request; it
  now reads only informer stores.

## Phase 4: Consistent API Contracts and Cancellation

- [x] Define shared collection and freshness metadata contracts.
- [x] Include `observedAt`, `stale`, `partial`, and request identity where
      applicable.
- [x] Keep cursor pagination metadata consistent across storage collections.
- [x] Add cursor pagination to benchmark history.
- [x] Return bounded benchmark summaries from collection reads.
- [x] Add semantic ETag/If-None-Match support to Highland-native JSON GETs.
- [x] Ignore request identity and observation timestamps when computing semantic
      validators while retaining them in response bodies.
- [x] Propagate TanStack abort signals through Highland frontend clients.
- [x] Replace untyped benchmark records with explicit TypeScript contracts.
- [x] Preserve individual-resource response compatibility during the collection
      envelope transition.

Definition of done:

- [x] Collection pages render freshness and partial-result state consistently.
- [x] Abandoned route requests are cancelled.
- [x] Benchmark history remains bounded as records grow.
- [x] Unchanged conditional GETs return HTTP 304 with an empty body.

Evidence:

- Live conditional GETs for operations and OIDC configuration returned 304 and
  zero response bytes.
- Live collection/API matrix returned HTTP 200 for benchmarks, providers,
  drivers, classes, claims, volumes, snapshots, attachments, capacity, events,
  operations, timeline, ownership, comparison, and remediations.

## Phase 5: Whole-Site Loading and Large-List UX

- [x] Preserve cached content during background refreshes.
- [x] Show consistent Updating/Applying indicators without replacing content.
- [x] Show last-observed, stale, and partial state on data-heavy pages.
- [x] Standardize empty, initial loading, retry, and degraded states.
- [x] Keep server-side filtering and cursor pagination as the large-list default.
- [x] Evaluate TanStack Virtual using measured DOM cost.
- [x] Document the virtualization threshold and decision.
- [x] Verify keyboard, screen-reader, mobile, and reduced-motion behavior.

Virtualization decision:

- Highland does not currently virtualize because server reads are bounded
  (normally 25-100 visible rows, at most 500 by contract) and the 10,000-record
  E2E fixture renders only 26 table rows.
- Add TanStack Virtual when a page intentionally renders more than 1,000
  simultaneous rows or a measured React commit for the table exceeds 50 ms on
  the supported baseline device. Cursor pagination remains preferable before
  that threshold.

Definition of done:

- [x] Background refreshes do not blank or jump established pages.
- [x] Lists do not render unbounded server datasets.
- [x] Data-heavy pages communicate freshness and degradation.
- [x] Accessibility regressions pass.

Evidence:

- Axe WCAG 2.1 AA found no serious or critical findings on live Benchmarks,
  Providers, Rook/Ceph Dashboard, OSDs, OpenEBS Dashboard, Storage Context, and
  Status pages.
- Keyboard traversal reached a real navigation link; mobile 390x844 validation
  had no horizontal overflow and rendered complete provider content.
- The boot indicator disables animation under `prefers-reduced-motion`.

## Phase 6: Performance Budgets and Validation

- [x] Add an authenticated cold-load performance smoke test.
- [x] Assert route-specific request and chunk expectations.
- [x] Assert eligible static text assets are compressed.
- [x] Add provider cache latency/concurrency tests.
- [x] Add SSE integration coverage for benchmarks and operations.
- [x] Run all frontend unit tests.
- [x] Run all API unit and integration tests.
- [x] Run race tests for benchmark, watch, storage, operations, and middleware.
- [x] Run Playwright E2E, accessibility, live-context, and visual tests.
- [x] Build production web and API artifacts.
- [x] Deploy final images to local k3s.
- [x] Re-run live authenticated traces and API timing.
- [x] Validate every major route and API family.
- [x] Repeatedly exercise Ceph OSD reads after one transient Dashboard
      authentication response: 30/30 and final 20/20 requests returned 200.
- [x] Make cross-provider timeline attribution tolerate a transient provider
      relationship-reader failure while retaining Kubernetes and healthy
      provider evidence.

Target budgets:

- [x] Fast-3G first contentful paint under 1 second: 560 ms.
- [x] Fast-3G usable route under 1.5 seconds: 1,417 ms.
- [x] Initial compressed JavaScript under 250 KB: approximately 169 KB
      JavaScript, approximately 178 KB including CSS.
- [x] Route-specific compressed JavaScript under 100 KB unless chart-heavy.
- [x] Cached Highland API p95 under 100 ms.
- [x] Provider discovery warm p95 under 150 ms: 1.98 ms.
- [x] No polling faster than 10 seconds unless foreground work is active.
- [x] No full-domain invalidation from an ordinary single-resource event.

Automated validation summary:

- Frontend: 17 test files, 74 tests passed; TypeScript passed.
- API: full `go test ./...` passed.
- Race detector: benchmark, watch, storage, operations, and middleware passed.
- Playwright: the full selected E2E/accessibility/admin/parity/storage matrix
  passed after updating the benchmark fixture to the real execution contract;
  live storage-context and visual suites passed.
- Production web bundle budgets and CSP checks passed.

## Final Completion Audit

- [x] Inspect every checkbox against source, tests, built artifacts, HTTP
      headers, runtime traces, and deployed behavior.
- [x] Record before/after measurements.
- [x] Confirm no unrelated user changes were overwritten.
- [x] Confirm final live workloads are Ready with zero restarts.
- [x] Mark this document `Status: Complete`.

Final deployment:

- Helm release: `highland`, namespace `longhorn-system`, revision 44 after the
  final partial-timeline resilience rollout.
- Web image: `highland-web:ui-refine-final2-20260716`.
- API image: `highland-api:ui-refine-final3-20260716`.
- Live URL: `http://100.116.90.61:30284`.
