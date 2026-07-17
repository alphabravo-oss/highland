# Piraeus / LINSTOR provider

Highland's `linstor` provider observes and explains an independently deployed Piraeus/LINSTOR CSI
data plane. It claims only `linstor.csi.linbit.com`. Highland does not install, upgrade, configure,
roll back, or uninstall Piraeus Operator, LINSTOR, DRBD, or LINSTOR CSI. Stopping or uninstalling
Highland leaves provisioning, attachment, replication, snapshots, existing volumes, and workloads
running.

## Enable the provider

The Kubernetes-only view needs no LINSTOR credentials:

```yaml
providers:
  linstor:
    enabled: true
    namespace: piraeus-datastore
```

Add a fixed controller URL for nodes, pools, resources, snapshots, remotes, schedules, and bounded
error reports:

```yaml
providers:
  linstor:
    enabled: true
    namespace: piraeus-datastore
    controller:
      url: https://linstor-controller.piraeus-datastore.svc:3371
      port: 3371
      existingSecret: linstor-highland-reader # optional key: token
      caSecret: linstor-controller-ca         # optional key: ca.crt
      timeout: 5s
```

Verified HTTPS is the default. `allowHttp: true` and port 3370 are for disposable in-cluster labs.
`insecureSkipVerify` is also lab-only. URLs cannot contain credentials, queries, or fragments. The
browser never receives the controller URL, bearer token, CA, or raw upstream access.

## Sources and behavior

Cluster-scoped Piraeus `piraeus.io/v1` CRDs provide lifecycle intent and convergence; namespaced workloads provide component readiness.
The fixed read-only LINSTOR REST client provides version, node, storage-pool, resource-group,
resource/replica, snapshot, remote, schedule, and error-report observations. Kubernetes remains
authoritative for StorageClasses, PVCs, PVs, attachments, snapshots, and workloads.

CSI handles are parsed as `<resource-name>[/<volume-number>]`, matching the upstream LINSTOR CSI
driver. Highland attaches backend detail only after an exact resource-definition match. Missing or
ambiguous matches become explicit conditions; names are never guessed.

Responses are capped, paginated, briefly cached, recursively scrubbed for secret-like fields, and
obtained only from fixed GET endpoints. Redirects, arbitrary paths, raw CSI sockets, CLI execution,
and user-selected Kubernetes resources are not supported.

## RBAC and failure isolation

The chart grants only `get`, `list`, and `watch` for the four allowlisted Piraeus CRDs and
Deployments, StatefulSets, and DaemonSets in the configured namespace. It grants no Piraeus,
LINSTOR, Secret, device, or storage mutation. NetworkPolicy egress is limited to DNS, Kubernetes,
and the configured LINSTOR namespace/port.

Without a controller URL, the provider remains useful and reports controller runtime detail as
unconfigured. An unavailable or unauthorized controller degrades only LINSTOR runtime pages.
Missing CRDs, denied RBAC, or failed components appear as provider conditions and do not make
unrelated providers unavailable. Disable the provider to remove Highland registration and its
read-only RBAC; no LINSTOR object or data is deleted.

## Change or remove the integration

- Rotate a bearer token by updating the named Secret's `token` key and restarting the Highland API
  deployment. The token is never stored in Highland's ConfigMap or returned to the browser.
- Rotate the controller CA by updating the named Secret's `ca.crt` key and restarting the API. Keep
  `insecureSkipVerify` disabled outside disposable labs.
- Change a controller URL, namespace, or Secret name with a Helm values update. The next Highland
  rollout rebinds only its observation client and RBAC; it does not modify LINSTOR.
- Disable the integration with `providers.linstor.enabled=false`. LINSTOR disappears from Highland
  after rollout while its Operator, CSI pods, volumes, and workloads continue independently.
- Uninstall Highland only after following Highland's own uninstall procedure. Because Piraeus and
  LINSTOR are not subcharts or owned resources, Highland's release removal does not uninstall them.

The upstream LINSTOR GUI remains the full native management console. Highland adds provider-neutral
Kubernetes ownership, workload impact, cross-provider inventory, bounded diagnostics, and unified
access—not a second LINSTOR control plane.

See [the implementation plan](../plans/linstor-provider.md) and
[validation record](../validation/linstor-provider.md) for the complete contract and evidence.
