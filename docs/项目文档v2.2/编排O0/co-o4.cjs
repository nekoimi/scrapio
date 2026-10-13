// Actual finite O4 source assets, interval schedules, explicit replay and governance.
module.exports=(schemas,paths,{ref,obj,str,integer,arr,route})=>{
 for(const [url,methods] of Object.entries(paths)){for(const [method,op] of Object.entries(methods))if(op['x-delivery-phase']==='O4')delete methods[method];if(!Object.keys(methods).length)delete paths[url]}
 schemas.TaskSource={oneOf:[obj({kind:{const:'manual'},inputs:arr(ref('BoundedObject'),100,1)}),obj({kind:{enum:['table','file']},inputs:arr(ref('BoundedObject'),0),snapshot_id:str({minLength:1}),snapshot_hash:str({minLength:1}),count:integer(1,1000),accept_partial:{type:'boolean'}},['kind','inputs','snapshot_id','snapshot_hash','count'])]};
 schemas.TaskDefinition.properties.source=ref('TaskSource');
 schemas.Stage.properties.refresh_seconds=integer(0,2592000);
 schemas.GraphTaskDefinition={allOf:[ref('TaskDefinition')]};
 schemas.SingleStageTaskDefinition={allOf:[ref('TaskDefinition'),{properties:{stages:{maxItems:1,minItems:1},edges:{maxItems:0}}}]};
 schemas.TaskView.properties.archived={type:'boolean'};
 schemas.SourcePreviewRequest=obj({kind:{enum:['table','file']},collector_version_id:str({minLength:1}),table_id:str(),schema_version:integer(1,100000),record_ids:arr(str(),1000),filter:ref('BoundedObject'),limit:integer(1,1000),format:{enum:['csv','json']},content:str({maxLength:786432}),mapping:{type:'object',minProperties:1,maxProperties:30,additionalProperties:str({minLength:1})}},['kind','collector_version_id','mapping']);
 schemas.SourcePreviewRequest.allOf=[{if:{properties:{kind:{const:'table'}}},then:{required:['table_id','schema_version','limit']}},{if:{properties:{kind:{const:'file'}}},then:{required:['format','content']}}];
 schemas.ScheduleRequest=obj({task_version_id:str({minLength:1}),interval_seconds:integer(60,2592000),enabled:{type:'boolean'},expected_revision:integer(0,2147483647)});
 schemas.UpgradePreviewRequest=obj({stage_id:str({minLength:1}),collector_version_id:str({minLength:1})});
 schemas.ArchiveTaskRequest=obj({expected_revision:integer(1,2147483647)});
 schemas.ReplayStageRequest=obj({stage_id:str({minLength:1}),task_version_id:str({minLength:1}),item_ids:arr(str({minLength:1}),100,1),output_ids:arr(str({minLength:1}),100,1),source:ref('TaskSource'),confirmed:{const:true}},['stage_id','confirmed']);
 schemas.ReplayStageRequest.oneOf=[{required:['item_ids'],not:{anyOf:[{required:['output_ids']},{required:['source']}]}},{required:['output_ids'],not:{anyOf:[{required:['item_ids']},{required:['source']}]}},{required:['source'],not:{anyOf:[{required:['item_ids']},{required:['output_ids']}]}}];
 schemas.ExportTableRequest=obj({record_ids:arr(str(),1000),limit:integer(1,1000)},['limit']);
 schemas.SchemaUpgradeRequest=obj({expected_version:integer(1,100000),schema:ref('TableSchema'),confirmed:{const:true}});
 const add=(method,url,name,request)=>{route(method,url,name,'O4',request);paths[url][method]['x-implementation-status']='implemented'};
 add('get','/governance','getCoGovernance');add('post','/sources/preview','previewCoSourceSnapshot','SourcePreviewRequest');add('get','/sources/{source_id}','getCoSourceSnapshot');
 add('get','/tasks/{task_id}/schedule','getCoIntervalSchedule');add('post','/tasks/{task_id}/schedule','saveCoIntervalSchedule','ScheduleRequest');add('post','/tasks/{task_id}/upgrade-preview','previewCoTaskUpgrade','UpgradePreviewRequest');add('post','/tasks/{task_id}/archive','archiveCoTask','ArchiveTaskRequest');add('post','/runs/{run_id}/replay','replayCoStage','ReplayStageRequest');add('post','/tables/{table_id}/export','exportCoTable','ExportTableRequest');add('post','/tables/{table_id}/schema','upgradeCoTableSchema','SchemaUpgradeRequest');
 paths['/sources/{source_id}'].get.parameters.push({name:'offset',in:'query',schema:integer(0,1000)},{name:'limit',in:'query',schema:integer(1,100)});
 paths['/sources/preview'].post.description='Finite REPEATABLE READ table snapshot or CSV/JSON file preview. At most1000 rows,4MiB frozen evidence,file768KiB,request1MiB. Confirm fingerprint/count and partial input in task source; retries query original key. No latest reads on resume.';
 paths['/tasks/{task_id}/schedule'].post.description='Fixed UTC interval60s..30days, explicit task version, revision CAS. Original due-time durable key, overlap skipped, downtime coalesced. Confirmed source snapshots reused; no implicit live table subscription.';
 paths['/runs/{run_id}/replay'].post.description='New finite run from explicit stage and descendants. Original item inputs, historical output+mapping or confirmed table snapshot. Task version same task; retry_of and unexecuted stage scope retained; missing evidence blocks. No full regression claim.';
 paths['/runs/{run_id}/retry-failed'].post.description='Single-stage original-version failed-only replay. Use /runs/{run_id}/replay for explicit multi-stage scope or repaired version.';
};
