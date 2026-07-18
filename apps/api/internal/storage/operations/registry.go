package operations

import "context"

// ActionPlanner is the per-action planning surface (ENG-E2.2).
// Planner.Plan currently dispatches via switch; extraction migrates cases into
// implementations registered in actionPlanners without changing Plan's contract.
type ActionPlanner interface {
	// Plan mutates plan with action-specific checks, resources, and dependencies.
	Plan(ctx context.Context, p *Planner, plan *Plan, request Request) error
}

// actionPlanners is the extraction target for per-action planners.
// Until ENG-E2.3–E2.6 complete, Plan() uses the switch in planner.go and this
// map stays empty. Tests assert coverage via plannerDispatchIDs instead.
var actionPlanners = map[string]ActionPlanner{}

// plannerDispatchIDs is the authoritative checklist of action IDs that
// Planner.Plan must handle (switch cases and/or actionPlanners entries).
// When registering a new action in actionRegistry, add the ID here and a
// matching case (or ActionPlanner) so TestActionRegistrationExhaustive passes.
var plannerDispatchIDs = map[string]struct{}{
	"create-pvc":                       {},
	"expand-pvc":                       {},
	"create-snapshot":                  {},
	"restore-snapshot":                 {},
	"clone-pvc":                        {},
	"delete-snapshot":                  {},
	"delete-pvc":                       {},
	"create-ceph-rbd-storageclass":     {},
	"create-cephfs-storageclass":       {},
	"delete-ceph-storageclass":         {},
	"create-ceph-blockpool":            {},
	"delete-ceph-blockpool":            {},
	"longhorn-volume-attach":           {},
	"longhorn-volume-detach":           {},
	"longhorn-volume-replica-count":    {},
	"longhorn-volume-backup":           {},
	"longhorn-recurring-job-add":       {},
	"longhorn-recurring-job-remove":    {},
	"longhorn-volume-salvage":          {},
	"longhorn-engine-upgrade":          {},
	"longhorn-backup-target-configure": {},
	"longhorn-backup-delete":           {},
	"longhorn-backup-restore":          {},
}

// knownFeatureFlags are the only FeatureFlag values actionEnabledByPolicy accepts.
var knownFeatureFlags = map[string]struct{}{
	"storage.writes.enabled":                            {},
	"providers.rookCeph.writes.enabled":                 {},
	"providers.rookCeph.writes.allowStorageClassDelete": {},
	"providers.rookCeph.writes.allowPoolDelete":         {},
}

// RegisterActionPlanner wires an extracted planner. Overwrites on re-register
// so tests can inject stubs. Production registration is additive only.
func RegisterActionPlanner(actionID string, planner ActionPlanner) {
	if actionID == "" || planner == nil {
		return
	}
	actionPlanners[actionID] = planner
}

// PlannerFor returns a registered ActionPlanner, if any.
func PlannerFor(actionID string) (ActionPlanner, bool) {
	p, ok := actionPlanners[actionID]
	return p, ok
}

// HasPlannerSupport reports whether the action is covered by the dispatch
// checklist and/or a registered ActionPlanner.
func HasPlannerSupport(actionID string) bool {
	if _, ok := actionPlanners[actionID]; ok {
		return true
	}
	_, ok := plannerDispatchIDs[actionID]
	return ok
}
