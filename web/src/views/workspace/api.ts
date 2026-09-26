import request from '/@/utils/request';

export interface Project { id:number; code:string; name:string; goal:string; owner:string; status:string }
export interface Dataset { id:number; project_id:number; code:string; name:string; record_type:string; schema_version:number; unique_key_fields:string|string[]; empty_value_policy:string; status:string }
export interface DatasetField { field_key:string; label:string; field_type:string; required:boolean; multiple:boolean }
export interface SchemaField { key:string; label:string; type:string; required:boolean; multiple:boolean }
export interface DatasetSchema { unique_key_fields:string[]; empty_value_policy:string; fields:SchemaField[] }
export interface Workflow { id:number; project_id?:number; dataset_id?:number; source_id:number; code:string; name:string; resource_type:string; enabled:boolean; published_version_id?:number }
export interface Version { id:number; workflow_id:number; version:number; status:string; definition:string|Definition; created_at:string }
export interface Definition { persistence:string; trigger:{type:string;url:string;fetch:{mode:string}}; listing?:{detail_selector:string;next_selector?:string;max_pages:number;max_empty_pages:number}; budget?:{max_discovered_per_page:number;max_tasks:number;max_pages:number;max_depth:number;max_duration_seconds:number;max_domains:number;allowed_domains:string[]}; nodes:Array<{name:string;type:string;config:Record<string,any>}> }
export interface Template { code:string; name:string; description:string; record_type:string; definition:Definition }
export interface Sample { id:number; workflow_version_id:number; source:string; page_role:string; content_type:string; content_hash:string; note:string; page_url:string; created_at:string }
export interface Preview { dry_run:boolean; passed:boolean; fetched_live:boolean; sample_id?:number; version_id:number; page_role:string; discovered_urls?:string[]; next_url?:string; steps:Array<{candidate:number;node:string;type:string;values:Record<string,any>}>; decisions:Array<{index:number;decision:string;canonical_key?:string;values?:Record<string,any>;changed_fields?:string[];reason?:string}>; error?:string }
export interface RecordRow { id:number; dataset_id:number; canonical_key:string; normalized:string|Record<string,any>; last_seen_at:string; last_decision:string; last_changed_at?:string; source_count:number }
export interface RecordFilter { dataset_id:number; query?:string; source_id?:number; activity?:'created'|'updated'|''; since?:string }
export interface SourceCoverage { source_id:number; source_name:string; records:number; observations:number; last_observed_at:string }
export interface SavedView { id:number; dataset_id:number; name:string; filter:RecordFilter }
export interface ExportJob { id:number; dataset_id:number; status:string; row_count:number; error?:string }
export interface ProjectHealth { project_id:number; last_effective_at?:string; created_7d:number; updated_7d:number; issues:number; datasets:Array<{dataset_id:number;name:string;records:number;created_7d:number;updated_7d:number;last_effective_at?:string}>; collectors:Array<{workflow_id:number;name:string;dataset_id?:number;last_run_id?:number;last_run_status:string;last_run_at?:string;last_success_at?:string;issue?:string}> }
export interface Run { id:number; workflow_id:number; workflow_version_id:number; trigger_type:string; status:string; created_at:string; started_at?:string; finished_at?:string; summary:string }
export interface Schedule { workflow_id:number; cron:string; timezone:string; enabled:boolean; concurrency_policy:'skip'|'queue'; next_run_at?:string; last_run_at?:string }
export interface ScheduleEvent { id:number; scheduled_at:string; status:string; run_id?:number; reason:string }
export interface Task { id:number; run_id:number; parent_task_id?:number; step_name:string; status:string; output_document_id?:number; error_message?:string; input:string }

async function get<T>(url:string, params?:Record<string,any>):Promise<T> { const response:any=await request({url,method:'get',params}); return response.data as T; }
async function post<T>(url:string, data:any):Promise<T> { const response:any=await request({url,method:'post',data}); return response.data as T; }
export const api={
 projects:()=>get<Project[]>('/api/v2/projects/list'),
 projectHealth:(project_id:number)=>get<ProjectHealth>('/api/v2/projects/health',{project_id}),
 createProject:(data:any)=>post<Project>('/api/v2/projects/create',data),
 datasets:(project_id?:number)=>get<Dataset[]>('/api/v2/datasets/list',project_id?{project_id}:{}),
 dataset:(id:number,version?:number)=>get<{dataset:Dataset;schema_version:number;fields:DatasetField[]}>('/api/v2/datasets/detail',{id,version}),
 createDataset:(data:any)=>post<Dataset>('/api/v2/datasets/create',data),
 updateSchema:(id:number,schema:DatasetSchema)=>post<Dataset>('/api/v2/datasets/schema/update',{id,...schema}),
 records:(filter:RecordFilter,page=1)=>get<{list:RecordRow[];total:number}>('/api/v2/records/list',{...filter,page,size:20}),
 recordCoverage:(dataset_id:number)=>get<SourceCoverage[]>('/api/v2/records/coverage',{dataset_id}),
 recordViews:(dataset_id:number)=>get<SavedView[]>('/api/v2/records/views',{dataset_id}),
 saveRecordView:(dataset_id:number,name:string,filter:RecordFilter)=>post<SavedView>('/api/v2/records/views/save',{dataset_id,name,filter}),
 deleteRecordView:(dataset_id:number,id:number)=>post<void>('/api/v2/records/views/delete',{dataset_id,id}),
 exportDirect:(filter:RecordFilter)=>request({url:'/api/v2/records/export/direct',method:'post',data:filter,responseType:'blob'}) as Promise<Blob>,
 exportCreate:(filter:RecordFilter)=>post<ExportJob>('/api/v2/records/export/create',filter),
 exportStatus:(id:number)=>get<ExportJob>('/api/v2/records/export/status',{id}),
 exportDownload:(id:number)=>request({url:'/api/v2/records/export/download',method:'get',params:{id},responseType:'blob'}) as Promise<Blob>,
 record:(id:number)=>get<any>('/api/v2/records/detail',{id}),
 workflows:(project_id?:number)=>get<{list:Workflow[];total:number}>('/api/v2/workflows/list',{project_id,page:1,size:100}),
 workflow:(id:number)=>get<{workflow:Workflow;versions:Version[]}>('/api/v2/workflows/detail',{id}),
 templates:()=>get<{list:Template[]}>('/api/v2/workflows/templates'),
 createWorkflow:(data:any)=>post<{workflow:Workflow;version:Version}>('/api/v2/workflows/create',data),
 createVersion:(workflow_id:number,definition:string)=>post<Version>('/api/v2/workflows/versions/create',{workflow_id,definition}),
 validate:(id:number)=>post<void>('/api/v2/workflows/versions/validate',{id}),
 sampleList:(version_id:number)=>get<{list:Sample[]}>('/api/v2/workflows/samples/list',{version_id}),
 sampleCreate:(data:any)=>post<Sample>('/api/v2/workflows/samples/create',data),
 sampleDelete:(id:number)=>post<void>('/api/v2/workflows/samples/delete',{id}),
 samplePreview:(data:any)=>post<Preview>('/api/v2/workflows/samples/preview',data),
 sampleCheck:(id:number)=>post<{passed:boolean;checks:Preview[];error?:string}>('/api/v2/workflows/versions/check-samples',{id}),
 publish:(id:number)=>post<void>('/api/v2/workflows/versions/publish',{id}),
 run:(workflow_id:number)=>post<{run_id:number;task_id:number}>('/api/v2/workflows/run',{workflow_id}),
 schedule:(workflow_id:number)=>get<{schedule:Schedule|null;events:ScheduleEvent[]}>('/api/v2/workflows/schedule',{workflow_id}),
 saveSchedule:(data:Pick<Schedule,'workflow_id'|'cron'|'timezone'|'enabled'|'concurrency_policy'>)=>post<Schedule>('/api/v2/workflows/schedule/save',data),
 runs:(page=1,project_id?:number)=>get<{list:Run[];total:number}>('/api/v2/runs/list',{page,size:20,project_id}),
 workflowRuns:(workflow_id:number)=>get<{list:Run[];total:number}>('/api/v2/runs/list',{workflow_id,page:1,size:20}),
	runDetail:(id:number)=>get<{run:Run;tasks:Task[];coverage?:Record<string,any>;limit_events?:Array<{task_id:number;page_role:string;url:string;reason:string}>}>('/api/v2/runs/detail',{id}),
 document:(id:number)=>get<any>('/api/v2/documents/detail',{id}),
};

export function jsonValue(value:any):Record<string,any> { if (value && typeof value==='object') return value; try { return JSON.parse(value||'{}'); } catch { return {}; } }
export function errorText(error:any):string { return error?.reason || error?.msg || error?.message || '请求失败'; }
export async function workflowPages(project_id:number):Promise<Workflow[]> { const rows:Workflow[]=[];for(let page=1;page<=10;page++){const result=await get<{list:Workflow[];total:number}>('/api/v2/workflows/list',{project_id,page,size:100});rows.push(...(result.list||[]));if(rows.length>=result.total||(result.list||[]).length<100)break;}return rows; }
