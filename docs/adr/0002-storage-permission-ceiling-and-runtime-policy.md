# ADR 0002: Separate storage permissions from runtime write policy

Status: Accepted

## Context

Highland storage workflows need Kubernetes mutation permissions, but operators also need to disable
or re-enable new workflows without editing a Helm release or restarting the API. A runtime switch
must not be able to grant its own ServiceAccount additional Kubernetes privileges.

## Decision

Highland uses three independent authorization layers:

1. Helm/GitOps installs a bounded Kubernetes RBAC permission ceiling.
2. The namespaced singleton `HighlandPolicy/highland` requests a runtime subset of that ceiling.
3. Every plan and submission enforces Highland's authenticated viewer/operator/admin action policy.

The effective policy is the intersection of the requested runtime policy and the installed ceiling.
The API may update only the named policy object and never creates or changes Kubernetes RBAC.
Broadening requires a fresh signed challenge, impact acknowledgement, exact cluster identity, and
the phrase `ENABLE STORAGE CHANGES`. Ceph pool deletion has a second independent phrase.

Disabling admission blocks new plans and submissions immediately. Durable operations that were
already approved continue to a terminal state because interrupting storage mutation midway can be
less safe than completing it.

## Consequences

- Default installs remain read-only and receive no new writer permissions.
- Enabling a runtime checkbox cannot exceed permissions reviewed at install time.
- Policy changes take effect through watches without an API restart.
- Helm upgrades preserve the existing runtime policy spec.
- Runtime policy availability is now operationally critical; Highland fails closed and exposes
  status, audit events, SSE invalidation, and Prometheus metrics when it is stale or unavailable.
