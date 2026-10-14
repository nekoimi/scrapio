module.exports = (schemas, paths, {ref,obj,str,integer,arr,route}) => {
  schemas.Stage.properties.input_mode.enum = ['single','union','join','batch'];
  schemas.Stage.properties.input_mode.description = 'Single, streaming union, finite keyed join or finite batch barrier. Join/batch wait for every upstream success and drained outbox, within the immutable run; no time-window subscription.';
  schemas.GatherConfig = obj({key_fields:arr(str({pattern:'^[a-z][a-z0-9_]{0,63}$'}),5),missing:{enum:['reject','skip']},max_rows:integer(1,1000),max_bytes:integer(1024,4194304)},['missing','max_rows','max_bytes']);
  schemas.GatherConfig.properties.key_fields.uniqueItems = true;
  schemas.Stage.properties.gather = ref('GatherConfig');
  schemas.Stage.allOf = [...(schemas.Stage.allOf || []),{if:{properties:{input_mode:{enum:['join','batch']}},required:['input_mode']},then:{required:['gather']},else:{not:{required:['gather']}}}];
  schemas.Stage.allOf.push({if:{properties:{input_mode:{const:'join'}},required:['input_mode']},then:{properties:{gather:{required:['key_fields'],properties:{key_fields:{minItems:1}}}}}},{if:{properties:{input_mode:{const:'batch'}},required:['input_mode']},then:{properties:{gather:{properties:{key_fields:{maxItems:0},missing:{const:'reject'}}}}}});
  schemas.TaskMappingPreviewRequest.properties.input_mode = {enum:['single','union','join','batch']};
  paths['/tasks/mapping-preview'].post.description = 'For join/batch this previews a typed fragment only, not a complete prepared input. Batch maps scalar elements into downstream array fields; final complete input is validated at barrier closure.';
  route('get','/runs/{run_id}/gather-groups','listCoGatherGroups','O5');
  const op = paths['/runs/{run_id}/gather-groups'].get;
  op['x-implementation-status']='implemented';
  op.description='Owner-scoped, cursor-paginated persisted join/batch groups with frozen values, source receipt IDs, error, status and child item. Missing/duplicate keys and incomplete upstreams never silently execute.';
  op.parameters.push({name:'cursor',in:'query',schema:str()},{name:'limit',in:'query',schema:integer(1,100)});
  paths['/runs/{run_id}/replay'].post.description += ' Join/batch cannot replay a single upstream output; use complete historical inputs or a table snapshot. A pruned descendant barrier missing any required incoming branch is rejected.';
  paths['/runs/{run_id}'].get.description += ' Gather stages expose gather_sealed, buffered_rows and gather_counts. Sealed groups are durable before work is released; pending groups prevent stage completion.';
};
