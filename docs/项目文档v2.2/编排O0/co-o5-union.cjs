// O5-03 first delivery: independent multi-source union, not keyed Join.
module.exports = (schemas, paths, { str }) => {
  schemas.Stage.properties.input_mode = {
    enum: ['single', 'union'],
    description: 'Absent means single. More than one incoming edge requires explicit union. Each edge independently maps a complete input; no field joining or batch aggregation. Closure waits for every parent and its outbox. Up to 5 incoming edges.',
  };
  schemas.TaskDefinition.properties.edges.maxItems = 60;
  schemas.ReplayStageRequest.properties.edge_id = str({pattern:'^[a-z][a-z0-9_]{0,63}$'});
  schemas.ReplayStageRequest.allOf = [{if:{required:['edge_id']},then:{required:['output_ids']}}];
  paths['/runs/{run_id}/replay'].post.description += ' For historical outputs with multiple incoming edges, edge_id is required; a single edge defaults automatically. Only that edge maps the selected outputs. Excluded incoming edges are recorded in retry_scope. Shared descendants run with the retained sources only, not a whole-graph regression.';
  paths['/runs/{run_id}'].get.description += ' Each stage includes upstreams with edge, port and parent status; union input closes only after ALL parents terminate and drain their dispatch plans. Empty/failed/limited parents remain visible.';
};
