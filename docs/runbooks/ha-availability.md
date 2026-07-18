# Runbook: HA availability (PDB, topology, leader)

## Symptoms

- Alert `HighlandAPIReadyReplicasLow` or `HighlandStorageOperationLeaderAbsent`
- Voluntary disruption (drain) stuck
- Pods pending due to topology constraints

## Immediate actions

1. `kubectl get pdb,deploy,pods -n highland-system`
2. Confirm PDB only exists when replicas ≥ 2; one-replica installs must not render PDB.
3. Soft topology uses `ScheduleAnyway` by default — strict production spreads may leave pods pending if zones lack capacity.
4. For operation leader: inspect Lease in the Highland namespace and controller logs.

## Recovery

1. Free node capacity or relax topology for small clusters.
2. Roll API deployment carefully (`maxUnavailable: 0` default keeps capacity during surge).
3. After leader recovery, confirm no duplicate external mutations for active operations.

## Related

- ADR-0004 DEC-6
- `chart/examples/values-production-ha.yaml`
