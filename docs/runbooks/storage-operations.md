# Storage operation runbook

## Provider-scoped admission

The Admin policy has three independent layers: the global new-operation gate,
portable Kubernetes workflow access for explicit provider IDs, and provider-
native controls. When `PROVIDER_POLICY_DISABLED` is returned, confirm the
provider shown in the plan, then enable that exact provider under **Admin →
Storage change policy** only if the intended StorageClass/PV belongs to it.
Do not broaden to a wildcard. `PROVIDER_MISMATCH` means the client hint disagrees
with Kubernetes evidence; refresh the target rather than overriding it.

Narrowing policy blocks new plans/submissions immediately. Operations already
stored as approved continue to a terminal state; monitor them in Storage
operations before removing installation-time RBAC.

## Observe an operation

Use the Operations page or:

```sh
kubectl -n highland-system get storageoperations
kubectl -n highland-system get storageoperation OPERATION -o yaml
```

The spec is immutable and contains no credential or confirmation token. Status records phase, step,
bounded conditions, retries, timestamps, and the result reference. Audit events retain plan,
approval, execution start, and terminal result linkage.

## Emergency disable

When runtime policy control is installed, open **Admin → Storage change policy**, clear
**Accept new storage operations**, review the narrowing summary, and apply. New plans and submissions
are rejected as soon as the observed policy generation changes; already-approved operations continue.

If runtime policy control is not installed:

1. Set `storage.writes.enabled=false` to reject new plans/submissions.
2. Set `storage.writes.recoveryEnabled=true` while approved Pending/Running operations must finish.
   Recovery mode keeps the namespaced storage writer and leader-election RBAC required by existing
   operations, but the API continues rejecting every new plan/submission. Leave a provider's own
   write gate enabled only when a pending operation for that provider must recover; it does not
   reopen submission while the global write gate is false.
3. Inspect all nonterminal operations and provider health.
4. After every operation is terminal, set `recoveryEnabled=false` and roll out again.

Do not delete a `StorageOperation` to stop a Kubernetes/Rook reconciliation. No initial action has a
safe cancellation point, so cancellation is intentionally absent.

## Runtime policy troubleshooting

- `POLICY_STALE` or `POLICY_UPDATE_CONFLICT`: refresh the Admin page and generate a new plan.
- `POLICY_PERMISSION_CEILING`: the requested capability was not installed through Helm/GitOps.
- `POLICY_CHALLENGE_EXPIRED`: review again; challenges expire after five minutes.
- `POLICY_NOT_OBSERVED`: the object was persisted but the API watch did not observe its generation;
  inspect `HighlandPolicy/highland`, API watch errors, and policy metrics before retrying.
- A policy that says “effective” grants eligibility only. Provider health, dependency analysis,
  server dry-run, action role, and per-resource confirmation are still required.

## API leader failure

Two API replicas compete for the `highland-storage-operations` Lease. On leader loss, another replica
reconciles Pending/Running records. Steps are idempotent; creates use server-side apply and deletes use
UID/resourceVersion preconditions. A takeover detects whether the mutation already happened; if it
did not, it reruns authoritative preflight before retrying. Check the Lease, API logs, and operation
`lastAttemptAt` if no progress occurs.

## Stuck operations

- `WaitingForBinding`: inspect StorageClass, scheduler/topology, provisioner, quota, and PVC Events.
- `WaitingForExpansion`: inspect PVC conditions and the CSI resizer; never lower the requested size.
- `WaitingForSnapshot`: inspect snapshot controller and `VolumeSnapshot` status.
- `WaitingForRook`: inspect the Rook operator and CRD conditions.
- `POOL_POSTFLIGHT_UNAVAILABLE`: restore fresh Dashboard read access; Highland will not claim success
  from Rook status alone.
- `STALE_PREFLIGHT`/`STALE_TARGET`: obtain a new plan and confirmation after reviewing changes.

Operations time out after 30 minutes with a durable failure. Fix the external state and submit a new
plan; do not edit the immutable spec or remove storage finalizers as a generic remedy.

## Credential rotation

Update the named Ceph Dashboard Secret, roll the API Deployment, and verify provider health. JWTs are
memory-only and are reacquired after restart or one 401 response. Never place credentials in a plan,
operation, support bundle, or audit message.

## Upgrade and rollback

Install the CRD before API pods that use write workflows. The `v1alpha1` schema remains readable by
the immediately previous Highland storage-preview release. Disable new writes and drain operations
before a rollback. Rolling Highland back never rolls back or deletes CSI drivers, Longhorn, Rook,
Ceph, StorageClasses, PVCs, PVs, snapshots, or pools.

## Retention and audit

Without `persistence.audit.enabled`, terminal operation objects are retained indefinitely because
they are the only durable operation record. With a currently writable append-only audit volume, the
elected controller may remove a terminal operation CR older than 30 days only after finding that
operation's terminal event in a structurally valid JSONL stream. Missing or malformed audit evidence
retains the CR. Multi-replica APIs require `ReadWriteMany` for that volume; otherwise use one API replica.
Back up the configured audit volume/file before uninstall if regulatory history is required.
