package co

import "github.com/nekoimi/scrapio/internal/collector"

// Stable Kahn ordering visits a shared descendant once, after every parent.
// Acyclic disconnected roots are invalid too: one explicit root owns inputs.
func (d TaskDefinition) TopologicalStages() ([]string, error) {
	degree := map[string]int{}
	for _, st := range d.Stages {
		degree[st.ID] = 0
	}
	for _, edge := range d.Edges {
		degree[edge.To]++
	}
	for _, st := range d.Stages {
		if st.ID != d.EntryStage && degree[st.ID] == 0 {
			return nil, collector.Invalid("stages."+st.ID, "阶段无法从任务入口到达")
		}
	}
	queue := []string{d.EntryStage}
	order := []string{}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		order = append(order, id)
		for _, edge := range d.Edges {
			if edge.From == id {
				degree[edge.To]--
				if degree[edge.To] == 0 {
					queue = append(queue, edge.To)
				}
			}
		}
	}
	if len(order) != len(d.Stages) {
		return nil, collector.Invalid("edges", "存在循环或无法到达的阶段，请移除回环连接")
	}
	return order, nil
}

// Union streams each independently mapped input immediately. Closing waits
// for ALL parents, whose terminal states include draining their outboxes.
func upstreamClosure(d TaskDefinition, stage string, states map[string]string) (bool, string) {
	count := 0
	severity := "succeeded"
	for _, edge := range d.Edges {
		if edge.To != stage {
			continue
		}
		count++
		state := states[edge.From]
		if !terminal(state) {
			return false, ""
		}
		if state == "partial" || state == "failed" || state == "cancelled" {
			severity = "partial"
		}
		if state == "limited" && severity == "succeeded" {
			severity = "limited"
		}
	}
	if count == 0 {
		return false, ""
	}
	if count == 1 {
		for _, edge := range d.Edges {
			if edge.To == stage {
				return true, "upstream_" + states[edge.From]
			}
		}
	}
	return true, "upstream_" + severity
}

func replayEdge(d TaskDefinition, stage, edgeID string) (TaskEdge, error) {
	edges := []TaskEdge{}
	for _, edge := range d.Edges {
		if edge.To == stage {
			edges = append(edges, edge)
		}
	}
	if edgeID == "" && len(edges) == 1 {
		return edges[0], nil
	}
	for _, edge := range edges {
		if edge.ID == edgeID {
			return edge, nil
		}
	}
	return TaskEdge{}, collector.Invalid("edge_id", "历史输出补采需选择目标阶段的一条上游连接；多来源不能隐式选择")
}
