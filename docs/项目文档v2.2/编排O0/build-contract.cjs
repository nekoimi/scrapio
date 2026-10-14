// O0 canonical contract with O1..O4 implemented extensions.
const fs = require('node:fs');
const path = require('node:path');
const ref = name => ({ $ref: '#/components/schemas/' + name });
const str = (extra = {}) => ({ type: 'string', ...extra });
const integer = (min, max) => ({ type: 'integer', minimum: min, maximum: max });
const arr = (items, max, min = 0) => ({ type: 'array', items, minItems: min, maxItems: max });
const obj = (properties, required = Object.keys(properties), extra = {}) => ({ type: 'object', additionalProperties: false, properties, required, ...extra });
const key = str({ pattern: '^[a-z][a-z0-9_]{0,63}$' });
const schemas = {
  BoundedObject: { type: 'object', description: 'O0 placeholder for projected response or bounded values. Byte limits are enforced outside JSON Schema.', maxProperties: 100 },
  Binding: { oneOf: [obj({ kind: { const: 'field' }, path: str({ pattern: '^(input|output|item)(\\.[a-z][a-z0-9_]*)?$' }) }), obj({ kind: { const: 'constant' }, value: {} }), obj({ kind: { const: 'template' }, value: str({ maxLength: 2048 }) })] },
  Field: obj({ key, label: str({ minLength: 1, maxLength: 64 }), type: { enum: ['string', 'url', 'integer', 'number', 'boolean', 'date', 'datetime', 'json'] }, required: { type: 'boolean' }, nullable: { type: 'boolean' }, multiple: { type: 'boolean' }, empty_allowed: { type: 'boolean' }, default: {} }, ['key', 'type', 'required', 'nullable', 'multiple']),
  Locator: obj({ strategy: { enum: ['css', 'xpath', 'jsonpath'] }, expression: str({ minLength: 1, maxLength: 2048 }) }),
  OutputField: obj({ key, source: { enum: ['input', 'constant', 'element'] }, input_key: key, value: {}, locator: ref('Locator'), extract: { enum: ['text', 'link', 'image', 'attribute'] }, attribute: str({ minLength: 1, maxLength: 128 }), clean: { enum: ['none', 'trim', 'whitespace'] } }, ['key', 'source']),
  Action: obj({ id: key, kind: { enum: ['navigate', 'click', 'input', 'wait', 'scroll'] }, locator: ref('Locator'), value: ref('Binding'), timeout_ms: integer(1, 30000), replay_class: { enum: ['read_only', 'receipt_required'] } }, ['id', 'kind', 'timeout_ms', 'replay_class']),
  Fetch: obj({ kind: { enum: ['http_html', 'http_json', 'browser'] }, url: ref('Binding'), method: { enum: ['GET', 'POST'] }, query: { type: 'object', additionalProperties: ref('Binding'), maxProperties: 30 }, body: ref('Binding'), credential_ref: str({ maxLength: 128 }), allowed_origins: arr(str({ pattern: '^https?://[^/]+$' }), 10, 1) }, ['kind', 'url', 'allowed_origins']),
  Pagination: obj({ kind: { enum: ['none', 'url', 'cursor', 'click', 'load_more'] }, next: ref('Locator'), end_condition: { enum: ['single_page', 'next_missing', 'empty_verified'] }, resume: { enum: ['url_or_cursor', 'restart_item'] }, max_pages: integer(1, 100), max_records: integer(1, 10000), max_duration_ms: integer(1, 600000) }, ['kind', 'end_condition', 'resume', 'max_pages', 'max_records', 'max_duration_ms']),
  CollectorDefinition: obj({ definition_version: { const: 'collector.v1' }, interpreter_version: { const: 'co.interpreter.v1' }, input_fields: arr(ref('Field'), 30, 1), fetch: ref('Fetch'), actions: arr(ref('Action'), 50), records: ref('Locator'), output: obj({ port: { const: 'records' }, fields: arr(ref('Field'), 30, 1), identity_fields: arr(key, 5, 1), mapping: arr(ref('OutputField'), 30, 1) }), pagination: ref('Pagination') }),
  SaveBinding: obj({ table_id: str({ minLength: 1 }), schema_version: integer(1, 100000), schema_hash: str({ minLength: 1 }), mapping: { type: 'object', minProperties: 1, maxProperties: 30, additionalProperties: ref('Binding') }, unique_key: arr(key, 5, 1), update_policy: { enum: ['merge_present', 'replace_present'] }, null_policy: { enum: ['keep', 'clear'] } }),
  Stage: obj({ id: key, name: str({ minLength: 1, maxLength: 64 }), collector_version_id: str({ minLength: 1 }), save: ref('SaveBinding'), on_error: { enum: ['continue', 'pause_stage'] } }),
  Filter: obj({ mode: { enum: ['all', 'any'] }, rules: arr(obj({ path: str({ pattern: '^(output|item)(\\.[a-z][a-z0-9_]*)?$' }), op: { enum: ['nonempty', 'eq', 'neq', 'contains'] }, value: {} }, ['path', 'op']), 10, 1) }),
  Edge: obj({ id: key, from: key, to: key, port: { const: 'records' }, mapping: { type: 'object', minProperties: 1, maxProperties: 30, additionalProperties: ref('Binding') }, filter: ref('Filter'), expand: obj({ path: str({ pattern: '^output\\.[a-z][a-z0-9_]*$' }) }), emit: { enum: ['valid', 'created', 'changed'] } }, ['id', 'from', 'to', 'port', 'mapping', 'emit']),
  TaskDefinition: obj({ definition_version: { const: 'task.v1' }, entry_stage: key, source: obj({ kind: { enum: ['manual', 'table', 'file'] }, inputs: arr(ref('BoundedObject'), 100, 1), source_check_id: str({ minLength: 1 }) }, ['kind']), stages: arr(ref('Stage'), 12, 1), edges: arr(ref('Edge'), 24), budget: obj({ max_work_items: integer(1, 10000), max_outputs: integer(1, 100000), max_pending: integer(1, 10000), browser_slots: integer(1, 8), max_duration_ms: integer(1, 86400000) }), overlap_policy: { const: 'skip' } }),
  CollectorDraftWrite: obj({ expected_revision: integer(0, 2147483647), definition: ref('CollectorDefinition') }),
  TaskDraftWrite: obj({ expected_revision: integer(0, 2147483647), definition: ref('TaskDefinition') }),
  CreateResource: obj({ name: str({ minLength: 1, maxLength: 64 }) }),
  TestRequest: obj({ expected_revision: integer(0, 2147483647), input: ref('BoundedObject'), mode: { enum: ['live', 'offline'] }, sample_id: str() }, ['expected_revision', 'input', 'mode']),
  CheckRequest: obj({ expected_revision: integer(0, 2147483647), evidence_id: str({ minLength: 1 }) }),
  PublishRequest: obj({ expected_revision: integer(0, 2147483647), check_id: str({ minLength: 1 }) }),
  TriggerRequest: obj({ task_version_id: str({ minLength: 1 }), source_check_id: str(), inputs: arr(ref('BoundedObject'), 100, 1) }, ['task_version_id']),
  TaskTrialRequest: obj({ expected_revision: integer(0, 2147483647), inputs: arr(ref('BoundedObject'), 100, 1), mode: { enum: ['live', 'offline'] }, sample_ids: arr(str(), 100) }, ['expected_revision', 'inputs', 'mode']),
  SampleRequest: obj({ expected_revision: integer(0, 2147483647), input: ref('BoundedObject'), asset_ids: arr(str(), 20), expected_outputs: arr(ref('BoundedObject'), 100) }),
  EditorSessionRequest: obj({ expected_revision: integer(0, 2147483647), input: ref('BoundedObject'), ttl_seconds: integer(30, 600), viewport: obj({ width: integer(320, 2560), height: integer(240, 1600) }) }),
  InspectionRequest: obj({ operation: { enum: ['inspect', 'dom', 'locator-check', 'locator-recheck', 'snapshot'] }, page_state_id: str({ minLength: 1 }), input: ref('BoundedObject') }),
  CommandRequest: obj({ command_id: str({ minLength: 1 }), page_state_id: str({ minLength: 1 }), action: { enum: ['navigate', 'click', 'input', 'wait', 'scroll'] }, confirmed: { type: 'boolean' }, locator: ref('Locator'), value: str({ maxLength: 4096 }), timeout_ms: integer(1, 30000) }, ['command_id', 'page_state_id', 'action', 'confirmed', 'timeout_ms']),
  InputCheckRequest: obj({ expected_revision: integer(0, 2147483647), inputs: arr(ref('BoundedObject'), 100, 1) }),
  OutputCheckRequest: obj({ expected_revision: integer(0, 2147483647), stage_id: key, binding: ref('SaveBinding'), evidence_id: str({ minLength: 1 }) }),
  TableCreateRequest: obj({ name: str({ minLength: 1, maxLength: 64 }), fields: arr(ref('Field'), 30, 1), unique_key: arr(key, 5, 1) }),
  SourceCheckRequest: obj({ task_id: str(), expected_revision: integer(0, 2147483647), table_id: str(), schema_version: integer(1, 100000), record_ids: arr(str(), 10000), filters: ref('Filter'), mapping: { type: 'object', additionalProperties: ref('Binding'), minProperties: 1, maxProperties: 30 }, limit: integer(1, 10000) }, ['task_id', 'expected_revision', 'table_id', 'schema_version', 'mapping', 'limit']),
  ImportRequest: obj({ task_id: str(), expected_revision: integer(0, 2147483647), asset_id: str(), content_hash: str(), mapping: { type: 'object', additionalProperties: ref('Binding'), minProperties: 1, maxProperties: 30 }, max_rows: integer(1, 10000) }),
  ScheduleRequest: obj({ expected_revision: integer(0, 2147483647), task_version_id: str(), enabled: { type: 'boolean' }, cron: str({ maxLength: 128 }), timezone: str({ maxLength: 64 }), overlap_policy: { const: 'skip' } }),
  ControlRequest: obj({ action: { enum: ['pause', 'resume', 'cancel'] }, expected_control_epoch: integer(0, 2147483647) }),
  ReplayRequest: obj({ scope: { enum: ['failed_items', 'stage'] }, work_item_ids: arr(str(), 10000, 1), stage_id: key, version_policy: { enum: ['original', 'explicit'] }, task_version_id: str(), check_id: str() }, ['scope', 'version_policy']),
  Error: obj({ error: obj({ code: str(), message: str(), retryable: { type: 'boolean' }, location: obj({ task_id: str(), stage_id: str(), work_item_id: str(), step_id: str(), field: str() }, []), details: ref('BoundedObject') }, ['code', 'message', 'retryable']), request_id: str() }),
  Operation: obj({ id: str(), status: { enum: ['queued', 'running', 'succeeded', 'failed', 'cancelled'] }, resource_id: str(), error: ref('BoundedObject') }, ['id', 'status']),
  Envelope: obj({ data: ref('BoundedObject'), request_id: str() }),
  OperationEnvelope: obj({ data: ref('Operation'), request_id: str() }),
};
const paths = {};
function route(method, url, name, phase, request, response = 'Envelope', async = false) {
  const parameters = [...url.matchAll(/\{(\w+)\}/g)].map(m => ({ name: m[1], in: 'path', required: true, schema: str({ minLength: 1 }) }));
  if (method !== 'get') parameters.push({ name: 'Idempotency-Key', in: 'header', required: method !== 'put', schema: str({ minLength: 1, maxLength: 128 }) });
  if (method === 'get' && /\/(collectors|tasks|tables|records|stages|work-items|outputs|events|samples|versions)$/.test(url)) parameters.push({ name: 'cursor', in: 'query', schema: str() }, { name: 'limit', in: 'query', schema: integer(1, 100) });
  const op = { operationId: name, tags: [phase], 'x-implementation-status': 'planned', 'x-delivery-phase': phase, parameters, responses: { [async ? '202' : '200']: { description: async ? 'Accepted; query original operation' : 'Result', content: { 'application/json': { schema: ref(response) } } }, '400': { description: 'Contract or input invalid', content: { 'application/json': { schema: ref('Error') } } }, '401': { description: 'Authentication required' }, '403': { description: 'Owner/resource denied' }, '409': { description: 'Revision, idempotency, version or write conflict', content: { 'application/json': { schema: ref('Error') } } } } };
  if (request) op.requestBody = { required: true, content: { 'application/json': { schema: ref(request) } } };
  (paths[url] ||= {})[method] = op;
}
route('get', '/capabilities', 'getCoCapabilities', 'O1');
for (const type of ['collectors', 'tasks']) {
  const phase = type === 'collectors' ? 'O1' : 'O2', singular = type.slice(0, -1), cap = singular[0].toUpperCase() + singular.slice(1), root = '/' + type, item = root + '/{' + singular + '_id}';
  route('get', root, 'list' + cap, phase); route('post', root, 'create' + cap, phase, 'CreateResource');
  route('get', item, 'get' + cap, phase); route('get', item + '/draft', 'get' + cap + 'Draft', phase);
  route('put', item + '/draft', 'save' + cap + 'Draft', phase, cap + 'DraftWrite');
  route('post', item + '/publish-checks', 'check' + cap + 'Publication', phase, 'CheckRequest', 'OperationEnvelope', true);
  route('post', item + '/versions', 'publish' + cap, phase, 'PublishRequest'); route('get', item + '/versions', 'list' + cap + 'Versions', phase);
}
route('post', '/collectors/{collector_id}/tests', 'testCoCollector', 'O1', 'TestRequest', 'OperationEnvelope', true);
route('get', '/collectors/{collector_id}/samples', 'listCoSamples', 'O1');
route('post', '/collectors/{collector_id}/samples', 'saveCoSample', 'O1', 'SampleRequest');
route('post', '/collectors/{collector_id}/editor-sessions', 'createCoEditorSession', 'O1', 'EditorSessionRequest', 'OperationEnvelope', true);
route('get', '/sessions/{session_id}', 'getCoEditorSession', 'O1');
route('post', '/sessions/{session_id}/inspections', 'inspectCoEditor', 'O1', 'InspectionRequest');
route('post', '/sessions/{session_id}/commands', 'commandCoEditor', 'O1', 'CommandRequest', 'OperationEnvelope', true);
route('get', '/sessions/{session_id}/commands/{command_id}', 'getCoCommandReceipt', 'O1');
route('get', '/sessions/{session_id}/frame', 'getCoFrame', 'O1');
paths['/sessions/{session_id}/frame'].get.responses['200'] = { description: 'PNG bytes; metadata in bounded response headers', content: { 'image/png': { schema: { type: 'string', format: 'binary' } } } };
for (const check of ['input-checks', 'output-checks']) route('post', '/tasks/{task_id}/' + check, 'checkCoTask' + check.replace(/-./g, s => s[1].toUpperCase()), 'O2', check === 'input-checks' ? 'InputCheckRequest' : 'OutputCheckRequest', 'OperationEnvelope', true);
for (const action of ['trials', 'runs']) route('post', '/tasks/{task_id}/' + action, 'createCo' + action, 'O2', action === 'trials' ? 'TaskTrialRequest' : 'TriggerRequest', 'OperationEnvelope', true);
route('get', '/runs', 'listCoRuns', 'O2'); route('get', '/runs/{run_id}', 'getCoRun', 'O2');
for (const resource of ['stages', 'work-items', 'outputs', 'events']) route('get', '/runs/{run_id}/' + resource, 'listCoRun' + resource.replace(/-./g, s => s[1].toUpperCase()), 'O2');
paths['/runs/{run_id}/events'].get.parameters.push({ name: 'Last-Event-ID', in: 'header', schema: str() });
paths['/runs/{run_id}/events'].get.responses['200'].content['text/event-stream'] = { schema: str(), 'x-event-payload': 'Ordered metadata only; no frame bytes' };
route('post', '/runs/{run_id}/control', 'controlCoRun', 'O2', 'ControlRequest', 'OperationEnvelope', true);
route('post', '/runs/{run_id}/replay-checks', 'checkCoReplay', 'O2', 'ReplayRequest', 'OperationEnvelope', true);
route('post', '/runs/{run_id}/replays', 'replayCoRun', 'O2', 'ReplayRequest', 'OperationEnvelope', true);
route('get', '/work-items/{work_item_id}', 'getCoWorkItem', 'O2'); route('get', '/work-items/{work_item_id}/lineage', 'getCoLineage', 'O3');
for (const type of ['tables', 'tables/{table_id}/records']) route('get', '/' + type, 'listCo' + type.replace(/[^a-z]/g, ''), 'O2');
route('post', '/tables', 'createCoDataTable', 'O2', 'TableCreateRequest');
route('get', '/tables/{table_id}/schema', 'getCoTableSchema', 'O2'); route('get', '/records/{record_id}/observations', 'getCoObservations', 'O2');
route('post', '/source-checks', 'checkCoSource', 'O4', 'SourceCheckRequest', 'OperationEnvelope', true);
route('post', '/imports', 'createCoImport', 'O4', 'ImportRequest', 'OperationEnvelope', true);
route('get', '/tasks/{task_id}/schedule', 'getCoSchedule', 'O4'); route('put', '/tasks/{task_id}/schedule', 'saveCoSchedule', 'O4', 'ScheduleRequest');
route('get', '/operations/by-key', 'getCoOperationByKey', 'O1', undefined, 'OperationEnvelope');
paths['/operations/by-key'].get.parameters.push(...['operation', 'key'].map(name => ({ name, in: 'query', required: true, schema: str({ minLength: 1 }) })));
route('get', '/operations/{operation_id}', 'getCoOperation', 'O1', undefined, 'OperationEnvelope');
require('./co-o1.cjs')(schemas,paths,{ref,obj,str,integer,arr,route});
require('./co-o2.cjs')(schemas,paths,{ref,obj,str,integer,arr,route});
require('./co-o3.cjs')(schemas,paths,{ref,obj,str,integer,arr,route});
require('./co-o4.cjs')(schemas,paths,{ref,obj,str,integer,arr,route});
require('./co-o5.cjs')(schemas,paths,{ref,obj,str,integer,arr,route});
require('./co-o5-outputs.cjs')(schemas,paths,{ref,obj,str,integer,arr,route});
require('./co-o5-union.cjs')(schemas,paths,{ref,obj,str,integer,arr,route});
require('./co-o5-gather.cjs')(schemas,paths,{ref,obj,str,integer,arr,route});
const openapi = { openapi: '3.1.0', jsonSchemaDialect: 'http://json-schema.org/draft-07/schema#', info: { title: 'scrapio independent collectors and tasks', version: 'co.contract.v1', description: 'O1 collectors, O2 durable tasks/tables and O3 multi-stage graphs/dispatches are mounted under /api/v4. O4 finite sources, interval schedules, scoped replay and governance are mounted. Operations carry bounded polymorphic results; TestResult, SessionView and CollectorVersionView describe concrete projections. Real release verification pending.' }, servers: [{ url: '/api/v4' }], security: [{ SessionBearer: [] }], paths, components: { securitySchemes: { SessionBearer: { type: 'http', scheme: 'bearer' } }, schemas } };

// Artificial fixtures are deliberately hosted under the reserved example.test domain.
const field = (key, type = 'string', required = true) => ({ key, type, required, nullable: false, multiple: false });
const input = fields => [field('url', 'url'), ...fields.map(k => field(k))];
const domField = (key, expression, extract = 'text', attribute) => ({ key, source: 'element', locator: { strategy: 'css', expression }, extract, clean: 'trim', ...(attribute ? { attribute } : {}) });
const collector = (id, inputFields, fields, mapping, records, next) => ({ id, version_id: id + '_v1', definition: { definition_version: 'collector.v1', interpreter_version: 'co.interpreter.v1', input_fields: inputFields, fetch: { kind: 'browser', url: { kind: 'field', path: 'input.url' }, allowed_origins: ['https://fixture.example.test'] }, actions: [], records: { strategy: 'css', expression: records }, output: { port: 'records', fields, identity_fields: [id === 'ranking' ? 'movie_id' : id === 'movie' ? 'movie_id' : 'comment_id'], mapping }, pagination: { kind: next ? 'url' : 'none', ...(next ? { next: { strategy: 'css', expression: '.next' } } : {}), end_condition: next ? 'next_missing' : 'single_page', resume: 'url_or_cursor', max_pages: next ? 2 : 1, max_records: 10, max_duration_ms: 30000 } } });
const collectors = [
  collector('ranking', input([]), [field('movie_id'), field('title'), field('rank', 'integer'), field('detail_url', 'url')], [domField('movie_id', 'a.title', 'attribute', 'data-movie-id'), domField('title', 'a.title'), domField('rank', '.rank'), domField('detail_url', 'a.title', 'link')], '.movie', true),
  collector('movie', input(['movie_id']), [field('movie_id'), field('title'), field('rating', 'number'), field('comments_url', 'url')], [{ key: 'movie_id', source: 'input', input_key: 'movie_id' }, domField('title', 'h1'), domField('rating', '.rating'), domField('comments_url', '.comments', 'link')], 'main', false),
  collector('comment', input(['movie_id']), [field('movie_id'), field('comment_id'), field('author'), field('content')], [{ key: 'movie_id', source: 'input', input_key: 'movie_id' }, domField('comment_id', '.content', 'attribute', 'data-comment-id'), domField('author', '.author'), domField('content', '.content')], '.comment', true),
];
const save = c => ({ table_id: c.id + '_table', schema_version: 1, schema_hash: c.id + '_schema_v1_fixture', mapping: Object.fromEntries(c.definition.output.fields.map(f => [f.key, { kind: 'field', path: 'output.' + f.key }])), unique_key: c.id === 'comment' ? ['movie_id', 'comment_id'] : ['movie_id'], update_policy: 'merge_present', null_policy: 'keep' });
const task = { definition_version: 'task.v1', entry_stage: 'ranking', source: { kind: 'manual', inputs: [{ url: 'https://fixture.example.test/top250' }] }, stages: collectors.map(c => ({ id: c.id, name: { ranking: '榜单', movie: '电影详情', comment: '短评' }[c.id], collector_version_id: c.version_id, save: save(c), on_error: 'continue' })), edges: [
  { id: 'ranking_movie', from: 'ranking', to: 'movie', port: 'records', mapping: { url: { kind: 'field', path: 'output.detail_url' }, movie_id: { kind: 'field', path: 'output.movie_id' } }, filter: { path:'output.detail_url',op:'exists' }, emit: 'valid' },
  { id: 'movie_comment', from: 'movie', to: 'comment', port: 'records', mapping: { url: { kind: 'field', path: 'output.comments_url' }, movie_id: { kind: 'field', path: 'output.movie_id' } }, filter: { path:'output.comments_url',op:'exists' }, emit: 'valid' },
], budget: { max_work_items: 20, max_outputs: 100, max_pending: 10, browser_slots: 2, max_duration_ms: 600000 }, overlap_policy: 'skip' };
const baseline = { provenance: 'synthetic; not captured from Douban; no network calls', created_at: '2026-10-13', real_targets: { list: 'https://movie.douban.com/top250', detail: 'https://movie.douban.com/subject/1292052/', short_comments: 'https://movie.douban.com/subject/1292052/comments', verification: 'pending; login, selectors, comment IDs and pagination not confirmed' }, collectors, task,
  samples: [
    { collector_id: 'ranking', input: { url: 'https://fixture.example.test/top250' }, html: 'list.html', expected: [{ movie_id: 'm001', title: '示例电影一', rank: 1, detail_url: 'https://fixture.example.test/subject/m001' }, { movie_id: 'm002', title: '示例电影二', rank: 2, detail_url: 'https://fixture.example.test/subject/m002' }], coverage: 'page1 only; next page intentionally not supplied' },
    { collector_id: 'movie', input: { url: 'https://fixture.example.test/subject/m001', movie_id: 'm001' }, html: 'detail.html', expected: [{ movie_id: 'm001', title: '示例电影一', rating: 9.2, comments_url: 'https://fixture.example.test/subject/m001/comments' }], coverage: 'single input' },
    { collector_id: 'comment', input: { url: 'https://fixture.example.test/subject/m001/comments', movie_id: 'm001' }, html: 'comments.html', expected: [{ movie_id: 'm001', comment_id: 'c001', author: '用户甲', content: '人工样例评论一' }, { movie_id: 'm001', comment_id: 'c002', author: '用户乙', content: '人工样例评论二' }], coverage: 'page1 only; next page intentionally not supplied' },
  ],
  json_sample: { input: { movie_id: 'm001' }, response: [{ comment_id: 'c001', author: '用户甲', content: '人工样例评论一' }], expected: [{ movie_id: 'm001', comment_id: 'c001', author: '用户甲', content: '人工样例评论一' }] },
  event_cases: [
    { id: 'outbox_crash', sequence: ['output_commit', 'dispatcher_crash', 'queue_empty', 'dispatcher_resume', 'child_created'], expected: 'queue_empty does not finish target stage' },
    { id: 'batch_rollback', sequence: ['write_begin', 'sql_failure', 'rollback', 'worker_restart'], expected: 'no new output, checkpoint or dispatch' },
    { id: 'unknown_action', sequence: ['click_sent', 'receipt_connection_lost', 'query_original', 'no_receipt'], expected: 'awaiting_confirmation; no repeated click' },
    { id: 'cancel_dispatch', sequence: ['cancel_control_lock', 'dispatch_control_lock'], expected: 'dispatch_cancelled; no new child' },
    { id: 'unchanged', sequence: ['unchanged_output_commit', 'emit_valid'], expected: 'child_created' },
    { id: 'replay_changed', sequence: ['identity_commit', 'retry_same_identity_different_values'], expected: 'OUTPUT_CHANGED_ON_REPLAY; original output stays immutable' },
  ] };
function generate() {
  fs.writeFileSync(path.join(__dirname, 'openapi.json'), JSON.stringify(openapi, null, 2) + '\n');
  fs.mkdirSync(path.join(__dirname, 'fixtures'), { recursive: true });
  fs.writeFileSync(path.join(__dirname, 'fixtures/baseline.json'), JSON.stringify(baseline, null, 2) + '\n');
}
if (require.main === module) { generate(); process.stdout.write('Generated O1/O2/O3/O4 OpenAPI and synthetic graph baseline.\n'); }
module.exports = { openapi, baseline };
