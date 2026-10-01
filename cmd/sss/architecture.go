package main

// serverAdapter is the domain boundary. Discovery is read-only; BuildPlan is
// pure; Execute revalidates every volatile process identity before signalling.
type serverAdapter interface {
	Discover(elevated bool) ([]Server, error)
	BuildPlan(items []Server, ids []string, includeSystem bool) plan
	Execute(plan plan) []result
}

type linuxServerAdapter struct{}

func (linuxServerAdapter) Discover(elevated bool) ([]Server, error) {
	return discover(elevated)
}

func (linuxServerAdapter) BuildPlan(items []Server, ids []string, includeSystem bool) plan {
	return buildPlan(items, ids, includeSystem)
}

func (linuxServerAdapter) Execute(plan plan) []result {
	return executePlan(plan)
}

type serverOrchestrator struct {
	adapter serverAdapter
}

func newServerOrchestrator(adapter serverAdapter) serverOrchestrator {
	return serverOrchestrator{adapter: adapter}
}

func (o serverOrchestrator) Discover(elevated bool) ([]Server, error) {
	return o.adapter.Discover(elevated)
}

func (o serverOrchestrator) Stop(items []Server, ids []string, includeSystem, yes bool, token string) outcome {
	return makeStopOutcome(items, ids, includeSystem, yes, token, o.adapter.Execute)
}

func (o serverOrchestrator) StopConfirmed(item Server) result {
	plan := o.adapter.BuildPlan([]Server{item}, []string{item.ID}, false)
	if len(plan.Actions) != 1 {
		reason := "cible non arrêtable"
		if len(plan.Conflicts) > 0 {
			reason = plan.Conflicts[0].Reason
		}
		return result{ID: item.ID, Status: "conflict", Message: reason}
	}
	results := o.adapter.Execute(plan)
	if len(results) == 0 {
		return result{ID: item.ID, Status: "failed", Message: "aucun résultat d’arrêt"}
	}
	return results[0]
}

func canStop(item Server) bool {
	return item.PID > 0 && !item.System && item.StartTime != ""
}
