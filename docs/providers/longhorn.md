# Longhorn adapter compatibility and migration

Portable PVC and VolumeSnapshot workflows are independent from Longhorn-native
manager actions. Select provider ID `longhorn` in the Admin policy to authorize
only the portable lifecycle; enable `longhornWrites` separately for attach,
detach, backup, recurring-job, salvage, engine, and backup-target workflows.

Highland retains the legacy manager proxy, stream endpoints, metrics scraper, watches, actions, and
route URLs while registering Longhorn as a managed provider. `managerUrl`,
`HIGHLAND_MANAGER_URL`, and `HIGHLAND_LONGHORN_NAMESPACE` remain valid; when no provider-native
Longhorn block is configured, Highland synthesizes it from those values.

To preview the common inventory without changing Longhorn data, enable `storage.enabled` and keep
the legacy pages available. Common claim/PV links use authoritative Kubernetes CSI identities; they
never derive a Longhorn volume from a PVC display name. Unsupported/untested manager versions lower
version-sensitive capabilities and produce a warning rather than enabling actions optimistically.

Rollback is configuration-only:

1. stop new common writes and let durable operations become terminal;
2. set `storage.enabled=false` to return navigation to the legacy Longhorn experience;
3. keep the same manager URL and namespace;
4. roll Highland back without changing Longhorn CRDs, volumes, replicas, backups, or settings.

Bolt-on and embedded installs remain separate. Disabling the adapter removes Longhorn-specific
handlers safely and Highland can run with the universal core alone. In legacy-only compatibility
mode a required manager may still control readiness; in provider mode an optional outage is reported
only on the Longhorn provider.

## Runtime write policy

When admin policy control is installed, **Longhorn native workflows** is independent from portable
PVC and snapshot workflows. Enabling it makes the supported Longhorn action registry eligible for
its existing operator/admin roles; it does not bypass manager availability, version checks,
resource preflight, signed confirmation, or durable-operation reconciliation. Disabling it blocks
new Longhorn plans and submissions while already-approved operations continue safely.

The UI can enable this gate only when
`adminPolicyControl.ceiling.longhornWrites=true` was installed through Helm/GitOps.
