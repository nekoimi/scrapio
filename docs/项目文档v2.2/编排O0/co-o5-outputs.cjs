// Selected O5-02: one fetch path, independent durable named outputs.
module.exports = (schemas, paths, { obj, str, arr, ref }) => {
  const port = str({ pattern: '^[a-z][a-z0-9_]{0,63}$' });
  const output = structuredClone(schemas.CollectorDefinition.properties.output);
  output.properties.port = port;
  schemas.NamedOutput = obj({ records: ref('Locator'), output });
  schemas.CollectorDefinition.properties.additional_outputs = arr(ref('NamedOutput'), 4);
  schemas.Candidate.properties.port = port;
  const table = schemas.SaveBinding;
  table.properties.mode = { const: 'table' };
  schemas.SaveBinding = { oneOf: [table, obj({ mode: { const: 'pass_through' } })] };
  schemas.Stage.properties.additional_saves = arr(obj({ port, save: ref('SaveBinding') }), 4);
  schemas.Edge.properties.port = port;
  schemas.TaskSavePreviewRequest.properties.port = port;
  paths['/tasks/save-preview'].post.description += ' Optional port defaults to records. Candidates must belong to the selected port. Pass-through returns passed without business writes.';
  paths['/runs/{run_id}/outputs'].get.description += ' Includes port; output identity is scoped by work item and port. Each page commits all ports, observations, dispatch plans and checkpoint atomically.';
  paths['/runs/{run_id}/observations'].get.description += ' Includes port and passed decision for durable pass-through results.';
};
