import request from '/@/utils/request';
import { Session } from '/@/utils/storage';

export interface Identity { id: string; username: string }
export interface Capabilities {
 supports_publication?:boolean;
 supports_version_runs?:boolean;
 supports_output_checks?:boolean;
 supports_logical_tables?:boolean;
	 supports_samples?: boolean;
	 supports_sample_checks?: boolean;
	 supports_snapshot_capture?: boolean;
	 supports_extraction_preview?: boolean;
	 supports_http_capture?: boolean;
	 supports_offline_capture?: boolean;
	 supports_json_capture_check?: boolean;
  editor_protocol: string;
  browser_ready: boolean;
  editor_service_connected?: boolean;
  supported_actions: string[];
  supports_live_inspection: boolean;
	 supports_record_preview?: boolean;
  definition_versions: number[];
  reason?: string;
}
export interface HomeState { status: 'pending' | 'ready'; reason?: string }
export interface Collector { published_version_id?:string; id: string; name: string; entry_url: string; entry_type: 'web' | 'json'; status: string; definition: Record<string, any>; revision: number; validation_status?: 'not_validated' | 'valid' | 'invalid' | 'stale'; validation_errors?: string[]; save_summary?: { revision: number; changed_fields: string[]; saved_at: string }; updated_at: string }
export interface BrowserSession { session_id: string; collector_id: string; draft_revision: number; status: string; expires_at: string; page_state_id: string; current_url: string; viewport: { width: number; height: number }; frame_url: string; heartbeat_interval_seconds: number; available: boolean; needs_reopen: boolean }

export interface EditorAction {
  type: string; value?: string; locator?: { strategy: 'css' | 'xpath'; expression: string };
  position?: { x: number; y: number }; timeout_ms: number; confirmed: boolean;
  page_state_id: string; expected_revision: number; record: boolean; step_id?: string;
}
export interface EditorCommand {
  command_id: string; session_id: string; status: 'queued' | 'running' | 'succeeded' | 'failed' | 'uncertain';
  type: string; request: EditorAction;
  result: { status: string; before_page_state_id: string; after_page_state_id: string; final_url: string;
    duration_ms: number; error?: string; error_code?: string; locator?: { strategy: 'css' | 'xpath'; expression: string };
    recording_status?: string; recording_error?: string; recorded_revision?: number };
}
export interface EditorCheckpoint {
  checkpoint_id: string; name: string; draft_revision: number; through_step_id: string;
  snapshot: { definition_version: number; entry_url: string; steps: Record<string, any>[] }; created_at: string;
}

export interface EditorLocator { strategy: 'css' | 'xpath'; expression: string }
export interface InspectedElement {
  element_id: string; tag: string; text: string; attributes: Record<string, string>;
  bounds: { x: number; y: number; width: number; height: number };
  parent_id: string; child_count: number; boundary: string;
}
export interface InspectionInput {
  page_state_id: string; position?: { x: number; y: number }; element_id?: string;
  mode?: 'record' | 'field'; relation?: 'self' | 'parent' | 'child'; child_index?: number;
  expand_similar?: boolean; scope?: EditorLocator; locator?: EditorLocator; previous_fingerprint?: string;
}
export interface LocatorCheck {
  page_state_id: string; locator: EditorLocator; match_count: number; truncated: boolean;
  elements: InspectedElement[]; sample_values: string[]; fingerprint: string;
  comparison: 'first-check' | 'unchanged' | 'changed'; warnings: string[]; scope_match_count: number;
}
export interface ElementInspection { page_state_id: string; element: InspectedElement; locators: (LocatorCheck & { kind: string; warning: string })[]; warnings: string[] }
export interface DOMChildren { page_state_id: string; root: InspectedElement; items: InspectedElement[]; next_offset: number | null; total: number; warnings: string[] }
export interface PageHighlight { page_state_id: string; elements: InspectedElement[]; kind: 'hover' | 'selected' | 'similar' }
export interface FieldOptions {field_key?:string;type?:'string'|'integer'|'number'|'boolean'|'date'|'datetime'|'json';required?:boolean;multiple?:boolean;clean?:'trim'|'whitespace';regex?:string;date_format?:string}
export interface FieldRule extends FieldOptions { name:string; locator?:EditorLocator; extract:'text'|'link'|'image'|'attribute'; attribute?:string }
export interface RecordPlan { mode:'single'|'repeated'; locator?:EditorLocator; max_records:number; fields:FieldRule[]; detail?:{locator:EditorLocator;return_strategy:'back'|'navigate-list';max_details:number}; detail_fields?:FieldRule[]; next_page?:{locator:EditorLocator;kind:'link'|'click';max_pages:number} }
export interface SelectionRule { target:'record'|'field'|'detail'|'next'; locator:EditorLocator; scope?:EditorLocator; extract?:FieldRule['extract'] }
export interface FieldPreview { name:string;value:string;raw_values:string[];match_count:number;truncated:boolean;errors:string[];source?:{element_id:string;bounds:InspectedElement['bounds']} }
export interface RecordPreview {page_state_id:string;stage:'list'|'detail';current_url:string;records:{index:number;fields:FieldPreview[];values:Record<string,string>}[];detail_urls:{record_index:number;url?:string;error?:string;raw_url:string}[];next_pages:{kind:'link'|'click';url?:string;error?:string;disabled:boolean}[];record_match_count:number;truncated:boolean;warnings:string[];network_accessed:boolean;dry_run:boolean}
export interface HTTPEntryRequest {method:'GET'|'POST';query:Record<string,string>;headers:Record<string,string>;body?:unknown;credential_ref?:string;timeout_ms:number}
export interface JSONRecordPlan {array_pointer:string;max_records:number;fields:(FieldOptions&{name:string;pointer:string})[]}
export interface InputCapture {capture_id:string;collector_id:string;draft_revision:number;source:'http'|'offline'|'browser';format:'json'|'html';status:'running'|'succeeded'|'failed'|'uncertain';final_url:string;base_url:string;session_id:string;page_state_id:string;status_code:number;content_type:string;content:string;content_hash:string;byte_count:number;error_code:string;error_stage:string;network_accessed:boolean|null;dry_run:boolean;sanitized:boolean;created_at:string;deadline_at:string}
export interface ExtractedField {name:string;field_key:string;match_count:number;raw_values:unknown[];raw_json:string;value_json:string;value:unknown;errors:string[];valid:boolean;truncated:boolean;locator?:EditorLocator;pointer?:string}
export interface ExtractionPreview {collector_id:string;revision:number;capture_id:string;capture_revision:number;content_hash:string;definition_hash:string;source_url:string;page_state_id:string;result:{interpreter_version:string;stage:'list'|'detail';step_id:string;match_count:number;match_count_lower_bound:boolean;truncated:boolean;valid_count:number;invalid_count:number;records:{index:number;fields:ExtractedField[];values:Record<string,unknown>;valid:boolean}[];warnings:string[];network_accessed:boolean;dry_run:boolean}}
export interface JSONCaptureCheck {capture_id:string;content_hash:string;revision:number;records:{index:number;fields:{name:string;pointer:string;found:boolean;raw_value:string}[]}[];match_count:number;truncated:boolean;network_accessed:boolean;dry_run:boolean}
export interface SampleMask {x:number;y:number;width:number;height:number}
export interface SavedSample {sample_id:string;collector_id:string;capture_id:string;name:string;stage:'list'|'detail';step_id:string;kind:'normal'|'missing_field';revision:number;saved_revision:number;definition_hash:string;expected:unknown;expected_json:string;expected_hash:string;actions:{command_id:string;type:string;status:string;before_page_state_id:string;after_page_state_id:string;error_code:string;created_at:string}[];actions_truncated:boolean;protected:boolean;input_retained:boolean;screenshot_url:string;screenshot_hash:string;screenshot_policy:string;masks:SampleMask[];created_at:string;updated_at:string;capture?:InputCapture}
export interface SampleDifference {code:string;record_index?:number;field_key?:string;expected_json?:string;actual_json?:string}
export interface SampleCheck {check_id:string;sample_id:string;sample_revision:number;collector_revision:number;definition_hash:string;content_hash:string;expected_hash:string;interpreter_version:string;status:'passed'|'failed'|'unconfigured';error_code:string;error_message?:string;definition_json?:string;created_at:string;expected_json?:string;result?:ExtractionPreview['result'];comparison?:{status:string;assertion_count:number;differences:SampleDifference[]}}
export interface CreateSampleInput {name:string;capture_id:string;expected_revision:number;step_id:string;stage:'list'|'detail';kind:'normal'|'missing_field';expected_json:string;protected:boolean;include_screenshot:boolean;screenshot_reviewed:boolean;masks:SampleMask[]}
export interface TableField {field_key:string;name:string;type:NonNullable<FieldOptions['type']>;nullable:boolean;multiple:boolean}
export interface TableSchema {fields:TableField[];unique_key:string[]}
export interface LogicalTable {table_id:string;name:string;schema_version:number;schema:TableSchema;schema_hash:string;created_at:string}
export interface OutputMapping {source_field_key:string;target_field_key:string}
export interface OutputCheckInput {expected_revision:number;capture_id?:string;sample_id?:string;sample_revision?:number;step_id:string;stage:'list'|'detail';table_id?:string;schema_version?:number;proposed_table?:{name:string;schema:TableSchema};mapping:OutputMapping[];update_policy:'update'|'keep_existing';empty_policy:'preserve_existing'|'overwrite'}
export interface OutputIssue {code:string;source_field_key?:string;target_field_key?:string}
export interface OutputCheck {check_id:string;collector_id:string;collector_revision:number;definition_hash:string;capture_id:string;content_hash:string;sample_id:string;sample_revision:number;table_id:string;schema_version:number;schema_hash:string;config:OutputCheckInput;ready:boolean;expires_at:string;created_at:string;result:{ready:boolean;compatibility:{compatible:boolean;issues:OutputIssue[];warnings:OutputIssue[]};counts:Record<string,number>;rows:{index:number;decision:string;canonical_key:string;key_json:string;values_json:string;values_hash:string;record_id?:string;record_revision?:number;changed_fields:string[];issues:OutputIssue[];reason?:string}[];warnings:string[];network_accessed:boolean;dry_run:boolean}}

async function get<T>(url: string): Promise<T> {
  const response: any = await request({ url, method: 'get' });
  return response.data as T;
}

export interface PublishIssue {code:string;message:string;step_id?:string;field_key?:string;sample_id?:string;stage?:string}
export interface PublishCheck {check_id:string;collector_id:string;collector_revision:number;trial_id:string;definition_hash:string;manifest_hash:string;capability_hash:string;accept_limited:boolean;ready:boolean;result:{ready:boolean;issues:PublishIssue[];warnings:PublishIssue[]};manifest:{revision:number;schema_hash:string;schema_version:number;output_check_id:string;samples:{sample_id:string;revision:number;step_id:string;stage:string;kind:string;content_hash:string;expected_hash:string}[]};capabilities:Record<string,unknown>;created_at:string;expires_at:string}
export interface PublishedVersion {version_id:string;collector_id:string;number:number;collector_revision:number;name:string;entry_type:'web'|'json';definition_hash:string;check_id:string;trial_id:string;contract_version:string;interpreter_version:string;capability_hash:string;manifest_hash:string;note:string;published_by:string;created_at:string;run_available:boolean;run_unavailable_reason:string;definition?:Record<string,any>;output_schema?:TableSchema;runtime_config?:TrialInput}

export interface TrialInput {expected_revision:number;mode:'live'|'offline';confirmed:boolean;allowed_origins:string[];budget:{seconds:number;pages:number;records:number;details:number};inputs:{step_id:string;stage:'list'|'detail';capture_id:string}[]}
export interface Trial {trial_id:string;collector_id:string;collector_revision:number;definition_hash:string;status:string;cancel_requested:boolean;current_step_id:string;current_stage:string;event_seq:number;input:TrialInput;summary:{status?:string;stop_reason?:string;failed_step_id?:string;failed_stage?:string;pages?:number;candidates?:number;network_accessed?:boolean;dry_run?:boolean;warnings?:string[];output?:OutputCheck['result']};created_at:string;started_at?:string;finished_at?:string;dry_run:boolean;formal_records_written:boolean}
export interface TrialDocument {document_id:string;step_id:string;stage:'list'|'detail';parent_record_index:number;list_page:number;capture_id?:string;source_url:string;content_hash:string;extraction:ExtractionPreview['result']}
export interface TrialEvent {sequence:number;payload:{step_id?:string;stage?:string;status:string;code?:string}}
async function streamTrial(id:string,after:number,signal:AbortSignal,onEvent:(event:TrialEvent)=>void):Promise<void>{
 const base=(import.meta.env.VITE_API_URL||'').replace(/\/+$/,'');
 const response=await fetch(`${base}/api/v3/trials/${id}/events?after=${after}`,{headers:{Authorization:Session.get('token')||'',Accept:'text/event-stream'},credentials:'same-origin',signal});
 if(!response.ok||!response.body||!response.headers.get('Content-Type')?.includes('text/event-stream'))throw Object.assign(new Error('试采事件流不可用'),{terminal:[401,403,404].includes(response.status)});
 const reader=response.body.getReader(),decoder=new TextDecoder();let buffer='';
 try{while(!signal.aborted){const {value,done}=await reader.read();if(done)return;buffer+=decoder.decode(value,{stream:true}).replace(/\r\n/g,'\n');let boundary=buffer.indexOf('\n\n');while(boundary>=0){const lines=buffer.slice(0,boundary).split('\n');buffer=buffer.slice(boundary+2);const idLine=lines.find(line=>line.startsWith('id:'));const data=lines.filter(line=>line.startsWith('data:')).map(line=>line.slice(5).trim()).join('\n');if(idLine&&data)onEvent({sequence:Number(idLine.slice(3)),payload:JSON.parse(data)});boundary=buffer.indexOf('\n\n')}}}finally{await reader.cancel().catch(()=>undefined);reader.releaseLock()}
}

async function streamBrowserSession(id: string, signal: AbortSignal, onSession: (session: BrowserSession) => void): Promise<void> {
  const baseURL = (import.meta.env.VITE_API_URL || '').replace(/\/+$/, '');
  const response = await fetch(`${baseURL}/api/v3/browser-sessions/${id}/events`, {
    headers: { Authorization: Session.get('token') || '', Accept: 'text/event-stream' },
    credentials: 'same-origin',
    signal,
  });
  if (!response.ok || !response.body) {
    const body = await response.json().catch(() => undefined);
    throw Object.assign(new Error(body?.error?.message || `会话事件流连接失败（${response.status}）`), { terminal: [401, 403, 404, 410].includes(response.status) });
  }
  if (!response.headers.get('Content-Type')?.includes('text/event-stream')) {
    await response.body.cancel();
    throw Object.assign(new Error('会话事件流响应格式错误'), { terminal: true });
  }
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  try {
    while (!signal.aborted) {
      const { value, done } = await reader.read();
      if (done) return;
      buffer += decoder.decode(value, { stream: true }).replace(/\r\n/g, '\n');
      let boundary = buffer.indexOf('\n\n');
      while (boundary >= 0) {
        const message = buffer.slice(0, boundary);
        buffer = buffer.slice(boundary + 2);
        const data = message.split('\n').filter(line => line.startsWith('data:')).map(line => line.slice(5).trim()).join('\n');
        if (data) onSession(JSON.parse(data) as BrowserSession);
        boundary = buffer.indexOf('\n\n');
      }
    }
  } finally {
    await reader.cancel().catch(() => undefined);
    reader.releaseLock();
  }
}

export const appApi = {
  createPublishCheck:(id:string,input:{expected_revision:number;trial_id:string;accept_limited:boolean},key:string)=>request({url:`/api/v3/collectors/${id}/publish-checks`,method:'post',data:input,headers:{'Idempotency-Key':key}}).then((response:any)=>response.data as PublishCheck),
  publishCheck:(id:string,checkID:string)=>get<PublishCheck>(`/api/v3/collectors/${id}/publish-checks/${checkID}`),
  publishCheckByKey:(id:string,key:string)=>request({url:`/api/v3/collectors/${id}/publish-checks/by-key`,method:'get',headers:{'Idempotency-Key':key}}).then((response:any)=>response.data as PublishCheck),
  publishVersion:(id:string,input:{expected_revision:number;check_id:string;note:string},key:string)=>request({url:`/api/v3/collectors/${id}/versions`,method:'post',data:input,headers:{'Idempotency-Key':key}}).then((response:any)=>response.data as PublishedVersion),
  versions:(id:string,cursor='')=>get<{items:PublishedVersion[];has_more:boolean;next_cursor:string}>(`/api/v3/collectors/${id}/versions?limit=25${cursor?`&cursor=${encodeURIComponent(cursor)}`:''}`),
  version:(id:string)=>get<PublishedVersion>(`/api/v3/versions/${id}`),
  versionByKey:(key:string)=>request({url:'/api/v3/versions/by-key',method:'get',headers:{'Idempotency-Key':key}}).then((response:any)=>response.data as PublishedVersion),
  versionEvidence:(id:string)=>get<{version_id:string;check:PublishCheck;samples:{sample_id:string;name:string;revision:number;step_id:string;stage:string;kind:string;comparison:{status:string;assertion_count:number;differences:SampleDifference[]}}[]}>(`/api/v3/versions/${id}/evidence`),
  streamTrial,
  trials:(id:string)=>get<{items:Trial[];limit:number}>(`/api/v3/collectors/${id}/trials`),
  trial:(id:string)=>get<Trial>(`/api/v3/trials/${id}`),
  trialResults:(id:string,cursor='')=>get<{items:TrialDocument[];next_cursor:string;has_more:boolean}>(`/api/v3/trials/${id}/results?limit=5${cursor?`&cursor=${encodeURIComponent(cursor)}`:''}`),
  trialByKey:(key:string)=>request({url:'/api/v3/trials/by-key',method:'get',headers:{'Idempotency-Key':key}}).then((response:any)=>response.data as Trial),
  createTrial:(id:string,input:TrialInput,key:string)=>request({url:`/api/v3/collectors/${id}/trials`,method:'post',data:input,headers:{'Idempotency-Key':key}}).then((response:any)=>response.data as Trial),
  cancelTrial:(id:string)=>request({url:`/api/v3/trials/${id}/cancel`,method:'post'}).then((response:any)=>response.data as Trial),
  deleteTrial:(id:string)=>request({url:`/api/v3/trials/${id}`,method:'delete'}),
  tables:(cursor='')=>get<{items:LogicalTable[];next_cursor:string;has_more:boolean}>(`/api/v3/tables?limit=25${cursor?`&cursor=${encodeURIComponent(cursor)}`:''}`),
  table:(id:string)=>get<LogicalTable>(`/api/v3/tables/${id}`),
  outputCheck:(id:string,checkID:string)=>get<OutputCheck>(`/api/v3/collectors/${id}/output-checks/${checkID}`),
  outputCheckByKey:(id:string,key:string)=>request({url:`/api/v3/collectors/${id}/output-checks/by-key`,method:'get',headers:{'Idempotency-Key':key}}).then((response:any)=>response.data as OutputCheck),
  createOutputCheck:(id:string,input:OutputCheckInput,key:string)=>request({url:`/api/v3/collectors/${id}/output-checks`,method:'post',data:input,headers:{'Idempotency-Key':key}}).then((response:any)=>response.data as OutputCheck),
  confirmOutput:(id:string,revision:number,checkID:string)=>request({url:`/api/v3/collectors/${id}/output`,method:'put',data:{expected_revision:revision,check_id:checkID}}).then((response:any)=>response.data as Collector),
  samples:(id:string,cursor='')=>get<{items:SavedSample[];has_more:boolean;next_cursor:string}>(`/api/v3/collectors/${id}/samples?limit=25${cursor?`&cursor=${encodeURIComponent(cursor)}`:''}`),
  sample:(id:string)=>get<SavedSample>(`/api/v3/samples/${id}`),
  createSample:(id:string,data:CreateSampleInput,key:string)=>request({url:`/api/v3/collectors/${id}/samples`,method:'post',data,headers:{'Idempotency-Key':key}}).then((response:any)=>response.data as SavedSample),
  updateSample:(id:string,data:{expected_revision:number;name?:string;expected_json?:string;protected?:boolean})=>request({url:`/api/v3/samples/${id}`,method:'patch',data}).then((response:any)=>response.data as SavedSample),
  sampleByKey:(key:string)=>request({url:'/api/v3/samples/by-key',method:'get',headers:{'Idempotency-Key':key}}).then((response:any)=>response.data as SavedSample),
  deleteSample:(id:string,revision:number)=>request({url:`/api/v3/samples/${id}?expected_revision=${revision}`,method:'delete'}),
  sampleChecks:(id:string)=>get<{items:SampleCheck[];retention_limit:number}>(`/api/v3/samples/${id}/checks`),
  sampleCheck:(id:string,checkID:string)=>get<SampleCheck>(`/api/v3/samples/${id}/checks/${checkID}`),
  checkSample:(id:string,revision:number,sampleRevision:number,key:string)=>request({url:`/api/v3/samples/${id}/checks`,method:'post',data:{expected_revision:revision,sample_revision:sampleRevision},headers:{'Idempotency-Key':key}}).then((response:any)=>response.data as SampleCheck),
  sampleScreenshot:(url:string)=>request({url,method:'get',responseType:'blob'}).then((response:any)=>response as Blob),
  previewSample:(id:string,sampleID:string,revision:number)=>request({url:`/api/v3/collectors/${id}/previews`,method:'post',data:{sample_id:sampleID,expected_revision:revision}}).then((response:any)=>response.data as ExtractionPreview),
  me: () => get<Identity>('/api/v3/me'),
  capabilities: () => get<Capabilities>('/api/v3/capabilities'),
  home: () => get<HomeState>('/api/v3/home'),
  collectors: (cursor = '') => get<{ items: Collector[]; has_more: boolean; next_cursor?: string }>(`/api/v3/collectors?limit=25${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`),
  collector: (id: string) => get<Collector>(`/api/v3/collectors/${id}/draft`),
  copyCollector: (id: string) => request({ url: `/api/v3/collectors/${id}/copies`, method: 'post', headers: { 'Idempotency-Key': crypto.randomUUID() } }).then((response: any) => response.data as Collector),
  createCollector: (data: { name?: string; entry_url: string; entry_type: 'web' | 'json' }) => request({ url: '/api/v3/collectors', method: 'post', data, headers: { 'Idempotency-Key': crypto.randomUUID() } }).then((response: any) => response.data as Collector),
  updateCollector: (id: string, data: { name: string; expected_revision: number; definition: Record<string, any> }) => request({ url: `/api/v3/collectors/${id}/draft`, method: 'put', data }).then((response: any) => response.data as Collector),
  validateCollector: (id: string) => request({ url: `/api/v3/collectors/${id}/validate`, method: 'post' }).then((response: any) => response.data as { collector: Collector; validation: { valid: boolean; errors: string[] } }),
  createBrowserSession: (collectorId: string, draftRevision: number, key: string) => request({ url: '/api/v3/browser-sessions', method: 'post', data: { collector_id: collectorId, draft_revision: draftRevision, viewport: { width: 1280, height: 800 } }, headers: { 'Idempotency-Key': key } }).then((response: any) => response.data as BrowserSession),
  browserSession: (id: string) => get<BrowserSession>(`/api/v3/browser-sessions/${id}`),
  heartbeatBrowserSession: (id: string) => request({ url: `/api/v3/browser-sessions/${id}/heartbeat`, method: 'post' }).then((response: any) => response.data as BrowserSession),
  streamBrowserSession,
  resumeBrowserSession: (id: string) => request({ url: `/api/v3/browser-sessions/${id}/resume`, method: 'post' }).then((response: any) => response.data as BrowserSession),
  closeBrowserSession: (id: string) => request({ url: `/api/v3/browser-sessions/${id}`, method: 'delete' }),
  createBrowserCommand: (id: string, data: EditorAction, key: string) => request({ url: `/api/v3/browser-sessions/${id}/commands`, method: 'post', data, headers: { 'Idempotency-Key': key } }).then((response: any) => response.data as EditorCommand),
  browserCommand: (id: string, commandId: string) => get<EditorCommand>(`/api/v3/browser-sessions/${id}/commands/${commandId}`),
  browserCommands: (id: string) => get<{ items: EditorCommand[]; limit: number }>(`/api/v3/browser-sessions/${id}/commands`),
  checkpoints: (id: string) => get<{ items: EditorCheckpoint[] }>(`/api/v3/collectors/${id}/checkpoints`),
  createCheckpoint: (id: string, data: { name: string; expected_revision: number; through_step_id: string }) => request({ url: `/api/v3/collectors/${id}/checkpoints`, method: 'post', data }).then((response: any) => response.data as EditorCheckpoint),
  deleteCheckpoint: (id: string, key: string) => request({ url: `/api/v3/collectors/${id}/checkpoints/${key}`, method: 'delete' }),
  browserFrame: (url: string, pageStateId: string) => request({ url: `${url}?page_state_id=${encodeURIComponent(pageStateId)}`, method: 'get', responseType: 'blob' }).then((response: any) => response as Blob),
  inspectElement: (id: string, input: InspectionInput) => request({ url: `/api/v3/browser-sessions/${id}/inspect`, method: 'post', data: input }).then((response: any) => response.data as ElementInspection),
  checkLocator: (id: string, input: InspectionInput) => request({ url: `/api/v3/browser-sessions/${id}/locator-checks`, method: 'post', data: input }).then((response: any) => response.data as LocatorCheck),
  previewRecords: (id:string, pageState:string, revision:number, stepID:string, stage:'list'|'detail') => request({url:`/api/v3/browser-sessions/${id}/record-previews`,method:'post',data:{page_state_id:pageState,expected_revision:revision,step_id:stepID,stage}}).then((response:any)=>response.data as RecordPreview),
  createCapture:(input:{collector_id:string;expected_revision:number;source:'http'|'offline';format:'json'|'html';content?:string;confirmed:boolean},key:string)=>request({url:'/api/v3/captures',method:'post',data:input,headers:{'Idempotency-Key':key}}).then((response:any)=>response.data as InputCapture),
  capture:(id:string)=>get<InputCapture>(`/api/v3/captures/${id}`),
  captureByKey:(key:string)=>request({url:'/api/v3/captures/by-key',method:'get',headers:{'Idempotency-Key':key}}).then((response:any)=>response.data as InputCapture),
	previewCollector:(id:string,captureID:string,revision:number,stepID:string,stage:'list'|'detail')=>request({url:`/api/v3/collectors/${id}/previews`,method:'post',data:{capture_id:captureID,expected_revision:revision,step_id:stepID,stage}}).then((response:any)=>response.data as ExtractionPreview),
	captureBrowserPage:(id:string,revision:number,pageState:string,key:string)=>request({url:`/api/v3/browser-sessions/${id}/captures`,method:'post',data:{expected_revision:revision,page_state_id:pageState},headers:{'Idempotency-Key':key}}).then((response:any)=>response.data as InputCapture),
  checkCaptureJSON:(id:string,revision:number,stepID:string)=>request({url:`/api/v3/captures/${id}/json-checks`,method:'post',data:{expected_revision:revision,step_id:stepID}}).then((response:any)=>response.data as JSONCaptureCheck),
  domChildren: (id: string, pageState: string, elementId = '', offset = 0) => get<DOMChildren>(`/api/v3/browser-sessions/${id}/dom?page_state_id=${encodeURIComponent(pageState)}&element_id=${encodeURIComponent(elementId)}&offset=${offset}&limit=50`),
};
